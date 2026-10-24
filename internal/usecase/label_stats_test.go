package usecase_test

import (
	"testing"
	"time"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// AC 21: statistik label cocok dengan hitungan manual, termasuk label 0 contoh, filter tanggal dan status review.
func TestLabelStatsMatchManualCounts(t *testing.T) {
	r := newAdminSubsRig(t)
	uc := usecase.NewLabelStats(postgres.NewStatsRepo(r.pool))
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	a := r.submitAt(t, nil, t0, []string{"Retak", "Retak", "Penyok"}, []string{"Retak"})
	b := r.submitAt(t, nil, t0.Add(48*time.Hour), []string{"Penyok"})
	r.submitAt(t, nil, t0.Add(72*time.Hour), []string{"Retak"}) // dihapus: tidak dihitung
	gone := r.submitAt(t, nil, t0.Add(96*time.Hour), []string{"Retak"})
	if _, err := r.pool.Exec(bg, `UPDATE submissions SET deleted_at = now() WHERE id = $1`, gone); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(bg, `INSERT INTO annotations (photo_id, text, x, y, w, h)
		SELECT id, 'lama', 0.5, 0.5, 0.1, 0.1 FROM photos WHERE submission_id = $1`, b); err != nil {
		t.Fatal(err)
	}
	// label tanpa contoh (diarsipkan)
	if _, err := r.pool.Exec(bg, `INSERT INTO labels (name, color, archived_at) VALUES ('Kosong', '#000000', now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(bg, `UPDATE submissions SET review_status = 'accepted', reviewed_at = now() WHERE id = $1`, a); err != nil {
		t.Fatal(err)
	}

	res, err := uc.Labels(bg, usecase.LabelStatsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// manual: 3 submission terlihat (a, b, +1), foto 4, kotak 7 (6 berlabel + 1 lama)
	if res.Totals != (usecase.LabelStatsTotals{Submissions: 3, Photos: 4, Annotations: 7, LegacyTextAnnotations: 1}) {
		t.Fatalf("totals = %+v", res.Totals)
	}
	by := map[string]usecase.LabelStatShare{}
	for _, l := range res.Labels {
		by[l.Label.Name] = l
	}
	if res.Labels[0].Label.Name != "Retak" || by["Retak"].Annotations != 4 || by["Retak"].Photos != 3 || by["Retak"].Submissions != 2 ||
		by["Retak"].Share != 0.667 || by["Penyok"].Annotations != 2 || by["Penyok"].Submissions != 2 || by["Penyok"].Share != 0.333 {
		t.Fatalf("label = %+v", res.Labels)
	}
	if k, ok := by["Kosong"]; !ok || k.Annotations != 0 || k.Share != 0 || !k.Label.Archived() || res.Labels[len(res.Labels)-1].Label.Name != "Kosong" {
		t.Fatalf("label 0 contoh harus ikut di akhir: %+v", res.Labels)
	}

	from, to := t0.Add(24*time.Hour), t0.Add(60*time.Hour)
	res, _ = uc.Labels(bg, usecase.LabelStatsFilter{From: &from, To: &to})
	if res.Totals.Submissions != 1 || res.Totals.Annotations != 2 {
		t.Fatalf("filter tanggal: %+v", res.Totals)
	}
	acc := domain.ReviewAccepted
	res, _ = uc.Labels(bg, usecase.LabelStatsFilter{Review: &acc})
	for _, l := range res.Labels {
		if l.Label.Name == "Retak" && (l.Annotations != 3 || l.Submissions != 1) {
			t.Fatalf("filter accepted: %+v", l)
		}
	}
}
