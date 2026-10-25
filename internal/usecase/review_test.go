package usecase_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// AC 16: review dengan version lama → 409; tolak tanpa alasan → 422; review massal per item; edit
// pengguna mengembalikan ke pending; hapus foto oleh admin tidak.
func TestAdminReviewFlow(t *testing.T) {
	r := newAdminSubsRig(t)
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	id := r.submitAt(t, nil, t0, []string{"Retak"}, []string{"Penyok"})
	r.clock.t = t0.Add(time.Hour)

	res, err := r.uc.Review(bg, id, 1, "accepted", "")
	if err != nil || res.Review.Status != domain.ReviewAccepted || res.Review.ReviewedAt == nil || res.RetentionDeletesAt != nil {
		t.Fatalf("terima: %+v %v", res, err)
	}
	if _, err := r.uc.Review(bg, id, 2, "accepted", ""); errCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("versi yang belum ada: %v", err)
	}
	for name, tc := range map[string]struct {
		version        int
		status, reason string
		code           string
	}{
		"tolak tanpa alasan":    {1, "rejected", "", "INVALID_REVIEW_REASON"},
		"pending dengan alasan": {1, "pending", "x", "INVALID_REVIEW_REASON"},
		"status aneh":           {1, "approved", "", "INVALID_REVIEW_STATUS"},
		"versi nol":             {0, "accepted", "", "INVALID_VERSION"},
	} {
		if _, err := r.uc.Review(bg, id, tc.version, tc.status, tc.reason); errCode(err) != tc.code {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := r.uc.Review(bg, uuid.New(), 1, "accepted", ""); errCode(err) != "NOT_FOUND" {
		t.Fatalf("id tak ada: %v", err)
	}
	res, err = r.uc.Review(bg, id, 1, "rejected", " Kotak tidak menutupi kerusakan ")
	if err != nil || res.Review.Reason != "Kotak tidak menutupi kerusakan" || res.RetentionDeletesAt == nil ||
		!res.RetentionDeletesAt.Equal(t0.AddDate(0, 0, 90)) {
		t.Fatalf("tolak: %+v %v", res, err)
	}

	// hapus satu foto oleh admin: versi naik, review tetap
	del := newDeleteRigFrom(t, r)
	d, _ := r.subs.GetDetail(bg, id)
	if _, err := del.DeletePhoto(bg, d.Photos[1].Photo.ID); err != nil {
		t.Fatal(err)
	}
	s, _ := r.subs.Get(bg, id)
	if s.Version != 2 || s.Review.Status != domain.ReviewRejected {
		t.Fatalf("setelah hapus foto: v%d %s", s.Version, s.Review.Status)
	}
}

// Edit pengguna yang berhasil mengembalikan review ke pending (juga untuk accepted).
func TestUserEditResetsReview(t *testing.T) {
	er := newEditRig(t, false)
	out, p := er.submitted(t, nil, 1)
	sg, _ := signer.New(testSecret)
	adm := usecase.NewAdminSubmissions(postgres.NewAdminSubmissionRepo(er.pool), er.subs, sg, er.clock,
		usecase.AdminSubmissionsConfig{PublicBaseURL: "https://x", ViewURLTTL: time.Minute, EditWindow: 24 * time.Hour, RetentionDays: 90})
	if _, err := adm.Review(bg, out.SubmissionID, 1, "accepted", "bagus"); err != nil {
		t.Fatal(err)
	}
	if v, err := er.rd.Submission(bg, usecase.Caller{EditToken: out.EditToken}, out.SubmissionID); err != nil ||
		v.Detail.Submission.Review.Status != domain.ReviewAccepted || v.Detail.Submission.Review.Reason != "bagus" {
		t.Fatalf("pemilik melihat review: %+v %v", v, err)
	}
	if _, err := er.edit(out, 1, ep(p[0], "diperbaiki")); err != nil {
		t.Fatal(err)
	}
	s, _ := er.subs.Get(bg, out.SubmissionID)
	if s.Review.Status != domain.ReviewPending || s.Review.Reason != "" || s.Review.ReviewedAt != nil {
		t.Fatalf("edit harus mereset review: %+v", s.Review)
	}
	// review dengan versi yang dilihat sebelum edit → konflik
	if _, err := adm.Review(bg, out.SubmissionID, 1, "accepted", ""); errCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("review versi lama: %v", err)
	}
}

func TestAdminReviewMany(t *testing.T) {
	r := newAdminSubsRig(t)
	a := r.submitAt(t, nil, time.Now(), []string{"x"})
	b := r.submitAt(t, nil, time.Now(), []string{"x"})
	if _, err := r.subs.BumpVersion(bg, b, 1, time.Now()); err != nil { // b diedit: versi 2
		t.Fatal(err)
	}
	missing := uuid.New()
	res, err := r.uc.ReviewMany(bg, []usecase.ReviewItem{{a, 1}, {b, 1}, {missing, 1}}, "rejected", "buram")
	if err != nil || len(res) != 3 || res[0].Result != "ok" || res[1].Result != "version_conflict" || res[2].Result != "not_found" {
		t.Fatalf("review massal: %+v %v", res, err)
	}
	if s, _ := r.subs.Get(bg, a); s.Review.Status != domain.ReviewRejected || s.Review.Reason != "buram" {
		t.Fatalf("a: %+v", s.Review)
	}
	if _, err := r.uc.ReviewMany(bg, []usecase.ReviewItem{{a, 1}}, "rejected", ""); errCode(err) != "INVALID_REVIEW_REASON" {
		t.Fatalf("tolak massal tanpa alasan: %v", err)
	}
	for name, items := range map[string][]usecase.ReviewItem{
		"kosong": nil, "duplikat": {{a, 1}, {a, 1}}, "versi nol": {{a, 0}},
	} {
		if _, err := r.uc.ReviewMany(bg, items, "accepted", ""); errCode(err) != "INVALID_ITEMS" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// filter daftar admin menurut status review + antrean (terlama dulu)
	pending := domain.ReviewPending
	ids, _ := listIDs(t, r, usecase.AdminListQuery{Review: &pending, Oldest: true})
	if len(ids) != 1 || ids[0] != b {
		t.Fatalf("antrean review = %v", ids)
	}
}
