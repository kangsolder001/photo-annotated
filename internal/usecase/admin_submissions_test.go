package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type adminSubsRig struct {
	*subRig
	uc *usecase.AdminSubmissions
}

func newAdminSubsRig(t *testing.T) *adminSubsRig {
	t.Helper()
	base := newSubRig(t, rigOpts{noEdit: true})
	sg, err := signer.New(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return &adminSubsRig{subRig: base, uc: usecase.NewAdminSubmissions(postgres.NewAdminSubmissionRepo(base.pool), base.subs, sg, base.clock,
		usecase.AdminSubmissionsConfig{PublicBaseURL: "https://api.example.com", ViewURLTTL: 5 * time.Minute, EditWindow: 24 * time.Hour, RetentionDays: 90})}
}

// user membuat annotator aktif dengan nama dan nomor tertentu.
func (r *adminSubsRig) user(t *testing.T, name, phone string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := r.pool.Exec(bg, `INSERT INTO users (id, name, email, phone, status, phone_verified_at) VALUES ($1, $2, $3, $4, 'active', now())`,
		id, name, strings.ToLower(strings.Fields(name)[0])+"@example.com", phone); err != nil {
		t.Fatal(err)
	}
	return id
}

// submitAt membuat submission milik owner pada waktu at, dengan satu foto per daftar label.
func (r *adminSubsRig) submitAt(t *testing.T, owner *uuid.UUID, at time.Time, photos ...[]string) uuid.UUID {
	t.Helper()
	r.clock.t = at
	in := usecase.SubmitInput{ClientRequestID: uuid.New(), OwnerID: owner}
	for _, names := range photos {
		p := r.uploaded(t, owner)
		sp := usecase.SubmitPhoto{PhotoID: p.ID, Annotations: []domain.Annotation{}}
		for i, n := range names {
			a := ann(n)
			a.X = 0.01 * float64(i)
			sp.Annotations = append(sp.Annotations, a)
		}
		in.Photos = append(in.Photos, sp)
	}
	out, err := r.uc2().Submit(bg, in)
	if err != nil {
		t.Fatal(err)
	}
	return out.SubmissionID
}

func (r *adminSubsRig) uc2() *usecase.Submit { return r.subRig.uc }

func listIDs(t *testing.T, r *adminSubsRig, q usecase.AdminListQuery) ([]uuid.UUID, *usecase.AdminListResult) {
	t.Helper()
	res, err := r.uc.List(bg, q)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for _, it := range res.Items {
		ids = append(ids, it.Submission.ID)
	}
	return ids, res
}

func TestAdminSubmissionsListFiltersAndCounts(t *testing.T) {
	r := newAdminSubsRig(t)
	budi := r.user(t, "Budi Santoso", "6281234567890")
	siti := r.user(t, "Siti Aminah", "6285711112222")
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	s1 := r.submitAt(t, &budi, t0, []string{"Retak", "Retak", "Penyok"}, []string{"Penyok"})
	s2 := r.submitAt(t, &siti, t0.Add(time.Hour), []string{"Basah"})
	s3 := r.submitAt(t, nil, t0.Add(2*time.Hour), []string{}) // anonim, tanpa kotak
	// baris lama (teks bebas) di s2
	if _, err := r.pool.Exec(bg, `INSERT INTO annotations (photo_id, text, x, y, w, h)
		SELECT id, 'catatan lama', 0.5, 0.5, 0.1, 0.1 FROM photos WHERE submission_id = $1`, s2); err != nil {
		t.Fatal(err)
	}
	// submission yang semua fotonya sudah dihapus (menunggu cleanup) tidak tampil
	gone := r.submitAt(t, &budi, t0.Add(3*time.Hour), []string{"Retak"})
	if _, err := r.pool.Exec(bg, `UPDATE photos SET status = 'deleted' WHERE submission_id = $1`, gone); err != nil {
		t.Fatal(err)
	}

	ids, res := listIDs(t, r, usecase.AdminListQuery{})
	if len(ids) != 3 || ids[0] != s3 || ids[1] != s2 || ids[2] != s1 || res.Next != nil {
		t.Fatalf("urutan terbaru dulu = %v", ids)
	}
	first := res.Items[2]
	if first.PhotoCount != 2 || first.AnnotationCount != 4 || first.LegacyTextCount != 0 || first.Annotator == nil ||
		first.Annotator.Name != "Budi Santoso" || first.Annotator.Phone != "6281234567890" || first.Annotator.Email != "budi@example.com" {
		t.Fatalf("baris s1 = %+v", first)
	}
	if len(first.Labels) != 2 || first.Labels[0].Name != "Retak" || first.Labels[0].Count != 2 || first.Labels[1].Count != 2 ||
		first.Labels[0].Color != "#123456" {
		t.Fatalf("label s1 = %+v", first.Labels)
	}
	if first.Cover == nil || first.Cover.Photo.Position != 0 || len(first.Cover.Annotations) != 3 ||
		!strings.Contains(first.CoverURL, "/api/v1/photos/"+first.Cover.Photo.ID.String()+"/content?exp=") {
		t.Fatalf("sampul s1 = %+v %s", first.Cover, first.CoverURL)
	}
	if want := t0.Add(90 * 24 * time.Hour); first.RetentionDeletesAt == nil || !first.RetentionDeletesAt.Equal(want) {
		t.Fatalf("retention_deletes_at = %v", first.RetentionDeletesAt)
	}
	if mid := res.Items[1]; mid.LegacyTextCount != 1 || mid.AnnotationCount != 2 || len(mid.Labels) != 1 {
		t.Fatalf("baris s2 = %+v", mid)
	}
	if anon := res.Items[0]; anon.Annotator != nil || anon.AnnotationCount != 0 || len(anon.Labels) != 0 || anon.Cover == nil {
		t.Fatalf("baris anonim = %+v", anon)
	}

	from, to := t0.Add(30*time.Minute), t0.Add(2*time.Hour)
	retak, basah := lbl("Retak"), lbl("Basah")
	for name, tc := range map[string]struct {
		q    usecase.AdminListQuery
		want []uuid.UUID
	}{
		"rentang waktu":      {usecase.AdminListQuery{From: &from, To: &to}, []uuid.UUID{s2}},
		"label":              {usecase.AdminListQuery{LabelID: &retak}, []uuid.UUID{s1}},
		"label lain":         {usecase.AdminListQuery{LabelID: &basah}, []uuid.UUID{s2}},
		"annotator":          {usecase.AdminListQuery{UserID: &siti}, []uuid.UUID{s2}},
		"nama sebagian":      {usecase.AdminListQuery{NameQ: "santo"}, []uuid.UUID{s1}},
		"nama % literal":     {usecase.AdminListQuery{NameQ: "%"}, nil},
		"awalan nomor":       {usecase.AdminListQuery{NameQ: "0857", PhoneQ: "62857"}, []uuid.UUID{s2}},
		"masih teks lama":    {usecase.AdminListQuery{Unlabeled: true}, []uuid.UUID{s2}},
		"terlama dulu":       {usecase.AdminListQuery{Oldest: true}, []uuid.UUID{s1, s2, s3}},
		"gabungan tanpa isi": {usecase.AdminListQuery{LabelID: &retak, UserID: &siti}, nil},
	} {
		if got, _ := listIDs(t, r, tc.q); !sameIDList(got, tc.want) {
			t.Errorf("%s: %v, ingin %v", name, got, tc.want)
		}
	}
}

func sameIDList(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAdminSubmissionsCursorPagination(t *testing.T) {
	r := newAdminSubsRig(t)
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	var all []uuid.UUID // urutan pembuatan
	for i := 0; i < 5; i++ {
		// dua submission berbagi created_at: pemecah seri id
		all = append(all, r.submitAt(t, nil, t0.Add(time.Duration(i/2)*time.Minute), []string{"x"}))
	}
	for _, oldest := range []bool{false, true} {
		var got []uuid.UUID
		q := usecase.AdminListQuery{Limit: 2, Oldest: oldest}
		for pages := 0; ; pages++ {
			ids, res := listIDs(t, r, q)
			got = append(got, ids...)
			if res.Next == nil {
				if pages != 2 {
					t.Fatalf("oldest=%v: %d halaman", oldest, pages+1)
				}
				break
			}
			q.After = res.Next
		}
		seen := map[uuid.UUID]bool{}
		for i, id := range got {
			if seen[id] {
				t.Fatalf("oldest=%v: duplikat di %d", oldest, i)
			}
			seen[id] = true
		}
		if len(got) != 5 {
			t.Fatalf("oldest=%v: %d item", oldest, len(got))
		}
		if oldest && got[0] != all[0] && got[0] != all[1] {
			t.Fatalf("terlama dulu harus dimulai dari submission pertama: %v", got)
		}
	}
	for _, n := range []int{-1, 51} {
		if _, err := r.uc.List(bg, usecase.AdminListQuery{Limit: n}); errCode(err) != "INVALID_QUERY" {
			t.Fatalf("limit %d: %v", n, err)
		}
	}
}

func TestAdminSubmissionDetail(t *testing.T) {
	r := newAdminSubsRig(t)
	budi := r.user(t, "Budi Santoso", "6281234567890")
	id := r.submitAt(t, &budi, time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), []string{"Retak"}, []string{})
	d, err := r.uc.Detail(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Annotator == nil || d.Annotator.Phone != "6281234567890" || len(d.View.Detail.Photos) != 2 || len(d.PhotoURLs) != 2 ||
		d.RetentionDeletesAt == nil || !d.View.Editable {
		t.Fatalf("detail = %+v", d)
	}
	for _, p := range d.View.Detail.Photos {
		if !strings.Contains(d.PhotoURLs[p.Photo.ID], p.Photo.ID.String()) || len(p.Photo.SHA256) != 32 {
			t.Fatalf("foto %s: url %q", p.Photo.ID, d.PhotoURLs[p.Photo.ID])
		}
	}
	if _, err := r.uc.Detail(bg, uuid.New()); errCode(err) != "NOT_FOUND" {
		t.Fatalf("id tak ada: %v", err)
	}
	// annotator yang akunnya sudah dihapus → null, submission tetap terbaca
	if _, err := r.pool.Exec(bg, `DELETE FROM users WHERE id = $1`, budi); err != nil {
		t.Fatal(err)
	}
	if d, err := r.uc.Detail(bg, id); err != nil || d.Annotator != nil {
		t.Fatalf("annotator terhapus: %+v %v", d, err)
	}
}
