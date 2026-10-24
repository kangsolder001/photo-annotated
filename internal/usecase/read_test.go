package usecase_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

type readRig struct {
	*subRig
	rd *usecase.Reader
	sg *signer.Signer
}

func newReadRig(t *testing.T, noEdit bool) *readRig {
	t.Helper()
	base := newSubRig(t, rigOpts{noEdit: noEdit})
	sg, err := signer.New(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	var tok usecase.EditTokenSigner = base.edit
	if noEdit {
		tok = nil
	}
	rd := usecase.NewReader(base.subs, base.photos, base.store, sg, tok, base.clock, usecase.ReadConfig{
		PublicBaseURL: "https://api.example.com", EditWindow: 24 * time.Hour, ViewURLTTL: 5 * time.Minute,
	})
	return &readRig{subRig: base, rd: rd, sg: sg}
}

// submitted membuat submission nyata (n foto berisi 2 anotasi di foto pertama).
func (r *readRig) submitted(t *testing.T, owner *uuid.UUID, n int) (*usecase.SubmitOutput, []*domain.Photo) {
	t.Helper()
	var photos []*domain.Photo
	for i := 0; i < n; i++ {
		photos = append(photos, r.uploaded(t, owner))
	}
	req := in(photos...)
	req.OwnerID = owner
	req.Photos[0].Annotations = []domain.Annotation{
		{Text: "pertama", X: 0.12, Y: 0.30, W: 0.25, H: 0.18},
		{Text: "kedua", X: 0.55, Y: 0.10, W: 0.30, H: 0.12},
	}
	out, err := r.uc.Submit(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	return out, photos
}

func isNotFound(t *testing.T, err error) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Kind != domain.KindNotFound || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	if de.Message != "resource not found" {
		t.Fatalf("pesan not-found harus seragam agar tidak jadi oracle: %q", de.Message)
	}
}

// ---------------------------------------------------------------- Submission

func TestReaderSubmissionWithEditToken(t *testing.T) {
	r := newReadRig(t, false)
	out, photos := r.submitted(t, nil, 3)

	v, err := r.rd.Submission(bg, usecase.Caller{EditToken: out.EditToken}, out.SubmissionID)
	if err != nil {
		t.Fatal(err)
	}
	d := v.Detail
	if d.Submission.ID != out.SubmissionID || d.Submission.Version != 1 || len(d.Photos) != 3 {
		t.Fatalf("detail: %+v", d)
	}
	for i, p := range photos {
		if d.Photos[i].Photo.ID != p.ID || d.Photos[i].Photo.Position != i {
			t.Fatalf("urutan foto salah di %d", i)
		}
	}
	if len(d.Photos[0].Annotations) != 2 || d.Photos[0].Annotations[1].Text != "kedua" {
		t.Fatalf("anotasi: %+v", d.Photos[0].Annotations)
	}
	wantUntil := d.Submission.CreatedAt.Add(24 * time.Hour)
	if !v.EditableUntil.Equal(wantUntil) || !v.Editable {
		t.Fatalf("editable_until=%v editable=%v", v.EditableUntil, v.Editable)
	}
}

func TestReaderEditableFlagFollowsServerClock(t *testing.T) {
	r := newReadRig(t, false)
	out, _ := r.submitted(t, nil, 1)
	c := usecase.Caller{EditToken: out.EditToken}
	created := out.CreatedAt

	for name, c2 := range map[string]struct {
		at   time.Time
		want bool
	}{
		"tepat setelah dibuat": {created, true},
		"sebelum batas":        {created.Add(24*time.Hour - time.Nanosecond), true},
		"tepat pada batas":     {created.Add(24 * time.Hour), false},
		"setelah batas":        {created.Add(30 * time.Hour), false},
	} {
		r.clock.t = c2.at
		v, err := r.rd.Submission(bg, c, out.SubmissionID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Editable != c2.want {
			t.Errorf("%s: editable = %v, want %v", name, v.Editable, c2.want)
		}
		if !v.EditableUntil.Equal(created.Add(24 * time.Hour)) {
			t.Errorf("%s: editable_until bergeser: %v", name, v.EditableUntil)
		}
	}
}

func TestReaderSubmissionExcludesDeletedPhotos(t *testing.T) {
	r := newReadRig(t, false)
	out, photos := r.submitted(t, nil, 3)
	if _, err := r.pool.Exec(bg, `UPDATE photos SET status='deleted' WHERE id=$1`, photos[1].ID); err != nil {
		t.Fatal(err)
	}
	v, err := r.rd.Submission(bg, usecase.Caller{EditToken: out.EditToken}, out.SubmissionID)
	if err != nil || len(v.Detail.Photos) != 2 || v.Detail.Photos[1].Photo.ID != photos[2].ID {
		t.Fatalf("detail: %+v %v", v, err)
	}
}

func TestReaderSubmissionAccessIsUniformNotFound(t *testing.T) {
	r := newReadRig(t, false)
	mine, _ := r.submitted(t, nil, 1)
	other, _ := r.submitted(t, nil, 1)
	alice := uuid.New()

	cases := map[string]struct {
		c  usecase.Caller
		id uuid.UUID
	}{
		"tanpa token":                   {usecase.Caller{}, mine.SubmissionID},
		"token salah":                   {usecase.Caller{EditToken: "salah"}, mine.SubmissionID},
		"token submission lain":         {usecase.Caller{EditToken: other.EditToken}, mine.SubmissionID},
		"id tidak ada (token sah lain)": {usecase.Caller{EditToken: mine.EditToken}, uuid.New()},
		"user tanpa kepemilikan":        {usecase.Caller{UserID: &alice}, mine.SubmissionID},
	}
	for name, c := range cases {
		_, err := r.rd.Submission(bg, c.c, c.id)
		if err == nil {
			t.Errorf("%s: seharusnya ditolak", name)
			continue
		}
		isNotFound(t, err)
	}
}

func TestReaderAdminAndOwnerAccess(t *testing.T) {
	r := newReadRig(t, false)
	alice, bob := pgtest.CreateUser(t, r.pool), pgtest.CreateUser(t, r.pool)
	out, _ := r.submitted(t, &alice, 1)

	if _, err := r.rd.Submission(bg, usecase.Caller{Admin: true}, out.SubmissionID); err != nil {
		t.Fatalf("admin harus boleh membaca: %v", err)
	}
	if _, err := r.rd.Submission(bg, usecase.Caller{UserID: &alice}, out.SubmissionID); err != nil {
		t.Fatalf("pemilik harus boleh membaca: %v", err)
	}
	_, err := r.rd.Submission(bg, usecase.Caller{UserID: &bob}, out.SubmissionID)
	isNotFound(t, err)
}

func TestReaderIgnoresEditTokenWhenNotConfigured(t *testing.T) {
	r := newReadRig(t, true) // mode otp: tidak ada token edit
	alice := pgtest.CreateUser(t, r.pool)
	out, _ := r.submitted(t, &alice, 1)
	// token (dari signer lain) tidak berarti apa pun
	_, err := r.rd.Submission(bg, usecase.Caller{EditToken: r.edit.Token(out.SubmissionID)}, out.SubmissionID)
	isNotFound(t, err)
	if _, err := r.rd.Submission(bg, usecase.Caller{UserID: &alice}, out.SubmissionID); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- PhotoURL

func TestReaderPhotoURL(t *testing.T) {
	r := newReadRig(t, false)
	out, photos := r.submitted(t, nil, 2)
	c := usecase.Caller{EditToken: out.EditToken}

	pu, err := r.rd.PhotoURL(bg, c, photos[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := r.clock.t.Add(5 * time.Minute); !pu.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", pu.ExpiresAt, want)
	}
	u, err := url.Parse(pu.URL)
	if err != nil || u.Scheme+"://"+u.Host != "https://api.example.com" || u.Path != "/api/v1/photos/"+photos[1].ID.String()+"/content" {
		t.Fatalf("URL salah: %s (%v)", pu.URL, err)
	}
	exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if exp != pu.ExpiresAt.Unix() || !r.sg.VerifyView(photos[1].ID, exp, u.Query().Get("sig")) {
		t.Fatal("tanda tangan di URL harus sah untuk foto ini")
	}
	if r.sg.VerifyView(photos[0].ID, exp, u.Query().Get("sig")) {
		t.Fatal("tanda tangan harus terikat pada foto")
	}
}

func TestReaderPhotoURLAccessAndStatus(t *testing.T) {
	r := newReadRig(t, false)
	mine, mp := r.submitted(t, nil, 1)
	other, op := r.submitted(t, nil, 1)
	pending := r.uploaded(t, nil) // belum dipakai submission mana pun
	cMine := usecase.Caller{EditToken: mine.EditToken}

	deleted := r.uploaded(t, nil)
	rej := r.uploaded(t, nil)
	_, _ = r.pool.Exec(bg, `UPDATE photos SET status='deleted' WHERE id=$1`, deleted.ID)
	_, _ = r.pool.Exec(bg, `UPDATE photos SET status='rejected' WHERE id=$1`, rej.ID)
	_ = other

	for name, c := range map[string]struct {
		c   usecase.Caller
		pid uuid.UUID
	}{
		"foto pending (belum punya submission)": {cMine, pending.ID},
		"foto deleted":                          {cMine, deleted.ID},
		"foto rejected":                         {cMine, rej.ID},
		"foto milik submission lain":            {cMine, op[0].ID},
		"tanpa token":                           {usecase.Caller{}, mp[0].ID},
		"token salah":                           {usecase.Caller{EditToken: "x"}, mp[0].ID},
		"foto tidak ada":                        {cMine, uuid.New()},
	} {
		_, err := r.rd.PhotoURL(bg, c.c, c.pid)
		if err == nil {
			t.Errorf("%s: seharusnya ditolak", name)
			continue
		}
		isNotFound(t, err)
	}
	if _, err := r.rd.PhotoURL(bg, usecase.Caller{Admin: true}, op[0].ID); err != nil {
		t.Fatalf("admin boleh: %v", err)
	}
}

// ---------------------------------------------------------------- PhotoContent

func (r *readRig) viewSig(photoID uuid.UUID, ttl time.Duration) (exp int64, sig string) {
	e := r.clock.t.Add(ttl)
	return e.Unix(), r.sg.SignView(photoID, e)
}

func TestReaderPhotoContentServesFile(t *testing.T) {
	r := newReadRig(t, false)
	_, photos := r.submitted(t, nil, 1)
	exp, sig := r.viewSig(photos[0].ID, 5*time.Minute)

	res, err := r.rd.PhotoContent(bg, photos[0].ID, exp, sig)
	if err != nil {
		t.Fatal(err)
	}
	defer res.File.Close()
	got, _ := io.ReadAll(res.File)
	want, _ := os.ReadFile(filepath.Join(r.dir, photos[0].StorageKey))
	if !bytes.Equal(got, want) || res.ContentType != "image/webp" {
		t.Fatalf("isi/tipe salah: %d byte, %q", len(got), res.ContentType)
	}
	p, _ := r.photos.Get(bg, photos[0].ID)
	if res.ETag != `"`+hex.EncodeToString(p.SHA256)+`"` {
		t.Fatalf("etag = %q", res.ETag)
	}
	if res.File.Info().Size != int64(len(want)) || res.File.Info().ModTime.IsZero() {
		t.Fatalf("info: %+v", res.File.Info())
	}
}

func TestReaderPhotoContentRejections(t *testing.T) {
	r := newReadRig(t, false)
	_, photos := r.submitted(t, nil, 2)
	exp, sig := r.viewSig(photos[0].ID, 5*time.Minute)

	cases := []struct {
		name string
		call func() error
		code string
		kind domain.Kind
	}{
		{"tanda tangan diubah", func() error {
			_, err := r.rd.PhotoContent(bg, photos[0].ID, exp, tamper(sig))
			return err
		}, "INVALID_SIGNATURE", domain.KindForbidden},
		{"tanda tangan kosong", func() error { _, err := r.rd.PhotoContent(bg, photos[0].ID, exp, ""); return err }, "INVALID_SIGNATURE", domain.KindForbidden},
		{"exp diubah", func() error { _, err := r.rd.PhotoContent(bg, photos[0].ID, exp+60, sig); return err }, "INVALID_SIGNATURE", domain.KindForbidden},
		{"foto lain", func() error { _, err := r.rd.PhotoContent(bg, photos[1].ID, exp, sig); return err }, "INVALID_SIGNATURE", domain.KindForbidden},
		{"id tidak dikenal dijawab sama", func() error { _, err := r.rd.PhotoContent(bg, uuid.New(), exp, sig); return err }, "INVALID_SIGNATURE", domain.KindForbidden},
		{"tanda tangan upload dipakai untuk lihat", func() error {
			up := r.sg.SignUpload(photos[0].ID, time.Unix(exp, 0), "image/webp", 1)
			_, err := r.rd.PhotoContent(bg, photos[0].ID, exp, up)
			return err
		}, "INVALID_SIGNATURE", domain.KindForbidden},
	}
	for _, tc := range cases {
		err := tc.call()
		de, ok := domain.AsError(err)
		if !ok || de.Code != tc.code || de.Kind != tc.kind {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}

	// kedaluwarsa (tanda tangan sah) → 410; tepat pada exp masih boleh
	r.clock.t = time.Unix(exp, 0)
	if res, err := r.rd.PhotoContent(bg, photos[0].ID, exp, sig); err != nil {
		t.Fatalf("tepat pada exp harus lolos: %v", err)
	} else {
		res.File.Close()
	}
	r.clock.t = time.Unix(exp+1, 0)
	_, err := r.rd.PhotoContent(bg, photos[0].ID, exp, sig)
	if de, ok := domain.AsError(err); !ok || de.Code != "URL_EXPIRED" || de.Kind != domain.KindGone {
		t.Fatalf("err = %v", err)
	}
}

func TestReaderPhotoContentWhenPhotoOrFileIsGone(t *testing.T) {
	r := newReadRig(t, false)
	_, photos := r.submitted(t, nil, 2)

	// foto dihapus setelah URL diterbitkan
	e1, s1 := r.viewSig(photos[0].ID, 5*time.Minute)
	_, _ = r.pool.Exec(bg, `UPDATE photos SET status='deleted' WHERE id=$1`, photos[0].ID)
	_, err := r.rd.PhotoContent(bg, photos[0].ID, e1, s1)
	isNotFound(t, err)

	// file hilang dari disk
	e2, s2 := r.viewSig(photos[1].ID, 5*time.Minute)
	if err := os.Remove(filepath.Join(r.dir, photos[1].StorageKey)); err != nil {
		t.Fatal(err)
	}
	_, err = r.rd.PhotoContent(bg, photos[1].ID, e2, s2)
	if de, ok := domain.AsError(err); !ok || de.Code != "PHOTO_FILE_MISSING" || de.Kind != domain.KindNotFound {
		t.Fatalf("err = %v", err)
	}
}
