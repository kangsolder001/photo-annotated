package usecase_test

import (
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

type labelRig struct {
	pool  *pgxpool.Pool
	clock *fakeClock
	uc    *usecase.Labels
}

func newLabelRig(t *testing.T, maxActive int) *labelRig {
	t.Helper()
	pool := pgtest.Pool(t)
	r := &labelRig{pool: pool, clock: &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}}
	r.uc = usecase.NewLabels(postgres.NewLabelRepo(pool), postgres.NewTxManager(pool), r.clock, usecase.LabelsConfig{MaxActive: maxActive})
	return r
}

func (r *labelRig) create(t *testing.T, name string) *usecase.LabelUsage {
	t.Helper()
	l, err := r.uc.Create(bg, usecase.LabelInput{Name: name})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return l
}

func activeNames(t *testing.T, r *labelRig) []string {
	t.Helper()
	ls, err := r.uc.Active(bg)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}

func TestLabelsCreateValidationAndPalette(t *testing.T) {
	r := newLabelRig(t, 200)
	a := r.create(t, " Retak ")
	if a.ID < 1 || a.Name != "Retak" || a.Color != domain.LabelPalette[0] || a.SortOrder != 0 || a.Archived() || a.UsageCount != 0 {
		t.Fatalf("label = %+v", a)
	}
	b, err := r.uc.Create(bg, usecase.LabelInput{Name: "Penyok", Description: "  Penyok di sisi  ", Color: "#AABBCC"})
	if err != nil || b.Color != "#aabbcc" || b.Description != "Penyok di sisi" || b.SortOrder != 1 {
		t.Fatalf("label kedua = %+v %v", b, err)
	}
	c := r.create(t, "Basah")
	if c.Color != domain.LabelPalette[2] {
		t.Fatalf("warna palet bergiliran: %s", c.Color)
	}
	for _, tc := range []struct {
		in   usecase.LabelInput
		code string
	}{
		{usecase.LabelInput{Name: "retak"}, "LABEL_NAME_TAKEN"},
		{usecase.LabelInput{Name: "RETAK "}, "LABEL_NAME_TAKEN"},
		{usecase.LabelInput{Name: ""}, "INVALID_LABEL_NAME"},
		{usecase.LabelInput{Name: "x", Color: "merah"}, "INVALID_LABEL_COLOR"},
		{usecase.LabelInput{Name: "x", Description: string(make([]byte, 201))}, "INVALID_LABEL_DESCRIPTION"},
	} {
		if _, err := r.uc.Create(bg, tc.in); errCode(err) != tc.code {
			t.Errorf("%+v: %v, ingin %s", tc.in, err, tc.code)
		}
	}
}

func TestLabelsLimitArchiveAndUnarchive(t *testing.T) {
	r := newLabelRig(t, 2)
	a := r.create(t, "A")
	r.create(t, "B")
	if _, err := r.uc.Create(bg, usecase.LabelInput{Name: "C"}); errCode(err) != "LABEL_LIMIT" {
		t.Fatalf("batas: %v", err)
	}
	ch, err := r.uc.Update(bg, a.ID, usecase.LabelInput{Name: "A", Color: a.Color, Archived: true})
	if err != nil || !ch.Archived || !ch.Label.Archived() {
		t.Fatalf("arsip: %+v %v", ch, err)
	}
	// nama label arsip tetap terpakai
	if _, err := r.uc.Create(bg, usecase.LabelInput{Name: "a"}); errCode(err) != "LABEL_NAME_TAKEN" {
		t.Fatalf("nama label arsip: %v", err)
	}
	r.create(t, "C")
	if got := activeNames(t, r); len(got) != 2 || got[0] != "B" || got[1] != "C" {
		t.Fatalf("aktif = %v", got)
	}
	if _, err := r.uc.Update(bg, a.ID, usecase.LabelInput{Name: "A", Color: a.Color}); errCode(err) != "LABEL_LIMIT" {
		t.Fatalf("buka arsip melewati batas: %v", err)
	}
	all, _ := r.uc.All(bg)
	if len(all) != 3 || all[2].Name != "A" || !all[2].Archived() {
		t.Fatalf("label arsip harus di akhir daftar admin: %+v", all)
	}
	// setelah ruang ada, buka arsip menaruh label di urutan terakhir dengan nama baru
	cl, _ := r.uc.Active(bg)
	if _, err := r.uc.Update(bg, cl[0].ID, usecase.LabelInput{Name: "B", Color: cl[0].Color, Archived: true}); err != nil {
		t.Fatal(err)
	}
	r.clock.t = r.clock.t.Add(time.Minute)
	ch, err = r.uc.Update(bg, a.ID, usecase.LabelInput{Name: "A2", Description: "baru", Color: "#000000"})
	if err != nil || !ch.Unarchived || ch.Label.Archived() || ch.Label.Name != "A2" || !ch.Label.UpdatedAt.Equal(r.clock.t) {
		t.Fatalf("buka arsip: %+v %v", ch, err)
	}
	if got := activeNames(t, r); len(got) != 2 || got[0] != "C" || got[1] != "A2" {
		t.Fatalf("aktif setelah buka arsip = %v", got)
	}
	if _, err := r.uc.Update(bg, 99999, usecase.LabelInput{Name: "x", Color: "#000000"}); errCode(err) != "NOT_FOUND" {
		t.Fatalf("id tak ada: %v", err)
	}
	if _, err := r.uc.Update(bg, a.ID, usecase.LabelInput{Name: "A2"}); errCode(err) != "INVALID_LABEL_COLOR" {
		t.Fatalf("PUT tanpa warna: %v", err)
	}
	if _, err := r.uc.Update(bg, a.ID, usecase.LabelInput{Name: "c", Color: "#000000"}); errCode(err) != "LABEL_NAME_TAKEN" {
		t.Fatalf("ganti nama ke nama lain yang ada: %v", err)
	}
}

func TestLabelsReorder(t *testing.T) {
	r := newLabelRig(t, 200)
	a, b, c := r.create(t, "A"), r.create(t, "B"), r.create(t, "C")
	if _, err := r.uc.Reorder(bg, []int{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if got := activeNames(t, r); got[0] != "C" || got[1] != "A" || got[2] != "B" {
		t.Fatalf("urutan = %v", got)
	}
	for name, ids := range map[string][]int{
		"kurang":   {c.ID, a.ID},
		"duplikat": {c.ID, a.ID, a.ID},
		"asing":    {c.ID, a.ID, 99999},
		"kosong":   {},
	} {
		if _, err := r.uc.Reorder(bg, ids); errCode(err) != "LABEL_ORDER_STALE" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// label arsip tidak termasuk himpunan aktif
	if _, err := r.uc.Update(bg, b.ID, usecase.LabelInput{Name: "B", Color: b.Color, Archived: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.uc.Reorder(bg, []int{c.ID, a.ID, b.ID}); errCode(err) != "LABEL_ORDER_STALE" {
		t.Fatalf("label arsip ikut diurutkan: %v", err)
	}
	if _, err := r.uc.Reorder(bg, []int{a.ID, c.ID}); err != nil {
		t.Fatal(err)
	}
	// label baru diletakkan di urutan terakhir
	r.create(t, "D")
	if got := activeNames(t, r); len(got) != 3 || got[0] != "A" || got[2] != "D" {
		t.Fatalf("urutan = %v", got)
	}
}

func TestLabelsDeleteOnlyUnused(t *testing.T) {
	r := newLabelRig(t, 200)
	used, deletedOnly, free := r.create(t, "Dipakai"), r.create(t, "Foto dihapus"), r.create(t, "Bebas")
	pgtest.InsertLabeledAnnotation(t, r.pool, used.ID, "attached")
	pgtest.InsertLabeledAnnotation(t, r.pool, used.ID, "attached")
	pgtest.InsertLabeledAnnotation(t, r.pool, deletedOnly.ID, "deleted")

	all, _ := r.uc.All(bg)
	counts := map[string]int{}
	for _, l := range all {
		counts[l.Name] = l.UsageCount
	}
	if counts["Dipakai"] != 2 || counts["Foto dihapus"] != 0 || counts["Bebas"] != 0 {
		t.Fatalf("usage_count = %v", counts)
	}
	_, err := r.uc.Delete(bg, used.ID)
	if errCode(err) != "LABEL_IN_USE" {
		t.Fatalf("hapus label terpakai: %v", err)
	}
	if de, _ := domain.AsError(err); de.Details["usage_count"] != 2 {
		t.Fatalf("details = %v", de.Details)
	}
	// foto deleted yang belum dibersihkan tetap dihitung (FK RESTRICT)
	if _, err := r.uc.Delete(bg, deletedOnly.ID); errCode(err) != "LABEL_IN_USE" {
		t.Fatalf("hapus label pada foto deleted: %v", err)
	}
	if l, err := r.uc.Delete(bg, free.ID); err != nil || l.Name != "Bebas" {
		t.Fatalf("hapus label bebas: %v", err)
	}
	if _, err := r.uc.Delete(bg, free.ID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("hapus ulang: %v", err)
	}
	// id tidak dipakai ulang
	if n := r.create(t, "Baru"); n.ID <= free.ID {
		t.Fatalf("id dipakai ulang: %d <= %d", n.ID, free.ID)
	}
}

func TestLabelsConcurrentCreateRespectsLimit(t *testing.T) {
	r := newLabelRig(t, 3)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.uc.Create(bg, usecase.LabelInput{Name: "L" + string(rune('A'+i))})
			if err != nil && errCode(err) != "LABEL_LIMIT" {
				t.Errorf("error tak terduga: %v", err)
			}
		}()
	}
	wg.Wait()
	ls, _ := r.uc.Active(bg)
	if len(ls) != 3 {
		t.Fatalf("label aktif = %d, ingin tepat 3", len(ls))
	}
	seen := map[int]bool{}
	for _, l := range ls {
		if seen[l.SortOrder] {
			t.Fatalf("sort_order ganda: %+v", ls)
		}
		seen[l.SortOrder] = true
	}
}
