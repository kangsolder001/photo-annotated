package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/turnstile"
	"photo-annotated/internal/usecase"
)

// headerTransport menambahkan header ke setiap request (mis. Authorization milik satu pengguna).
type headerTransport struct {
	base http.RoundTripper
	h    map[string]string
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.h {
		if r.Header.Get(k) == "" {
			r.Header.Set(k, v)
		}
	}
	return t.base.RoundTrip(r)
}

// with mengembalikan salinan stack yang client-nya selalu mengirim header h.
func (s *stack) with(h map[string]string) *stack {
	c := *s
	c.client = &http.Client{Timeout: s.client.Timeout, Transport: headerTransport{base: http.DefaultTransport, h: h}}
	return &c
}

// login mendaftarkan nomor, mengirim kode lewat stub, memverifikasi, dan mengembalikan stack yang
// membawa token sesi pengguna itu.
func (s *authStack) login(t *testing.T, phone, canonical string) *stack {
	t.Helper()
	resp, reg := s.postJSON(t, "/api/v1/auth/register", `{"name":"U","email":"u@example.com","phone":"`+phone+`","password":"rahasia123","nim":"N`+canonical[3:]+`","group_id":1,"turnstile_token":"human"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("register %s: %d %v", phone, resp.StatusCode, reg)
	}
	if res, err := s.disp.DispatchOnce(context.Background()); err != nil || res.Outcome != usecase.DispatchSent {
		t.Fatalf("dispatch: %+v %v", res, err)
	}
	resp, m := s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+reg["challenge_id"].(string)+`","code":"`+s.wa.LastCodeFor(canonical)+`"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify: %d %v", resp.StatusCode, m)
	}
	return s.with(map[string]string{"Authorization": "Bearer " + m["access_token"].(string)})
}

func TestE2EOwnerIsolationInOTPMode(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	bob := s.login(t, "081200000002", "6281200000002")

	// tanpa sesi: rute pemilik → 401
	anon := s.stack
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/uploads/presign"}, {"POST", "/api/v1/submissions"},
		{"GET", "/api/v1/submissions/" + uuid.NewString()}, {"GET", "/api/v1/photos/" + uuid.NewString() + "/url"},
		{"PUT", "/api/v1/submissions/" + uuid.NewString()},
	} {
		if resp, m := anon.authed(t, tc.method, tc.path, ""); resp.StatusCode != 401 || errField(m) != "UNAUTHORIZED" {
			t.Fatalf("%s %s tanpa sesi: %d %v", tc.method, tc.path, resp.StatusCode, m)
		}
	}

	sub := alice.submitted(t, 2)
	if sub.Token != "" {
		t.Fatal("mode otp tidak boleh menerbitkan edit_token")
	}
	var owner uuid.UUID
	if err := s.pool.QueryRow(context.Background(), `SELECT owner_id FROM submissions WHERE id = $1`, sub.ID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var photoOwners int
	_ = s.pool.QueryRow(context.Background(), `SELECT count(*) FROM photos WHERE owner_id = $1`, owner).Scan(&photoOwners)
	if photoOwners != 2 {
		t.Fatalf("foto milik pemilik = %d, ingin 2", photoOwners)
	}

	// pemilik bisa membaca dan mengedit
	if code, _, body := alice.get(t, "/api/v1/submissions/"+sub.ID.String(), nil); code != 200 {
		t.Fatalf("pemilik baca: %d %s", code, body)
	}
	if code, _, _ := alice.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/url", nil); code != 200 {
		t.Fatalf("pemilik url foto: %d", code)
	}
	if r := alice.edit(t, sub.ID, "", editBody(1, sub.Photo...)); r.Status != 200 || r.Version != 2 {
		t.Fatalf("pemilik edit: %d %s", r.Status, r.Raw)
	}

	// pengguna lain: 404 untuk semuanya (tidak bisa mem-probe id)
	if code, _, _ := bob.get(t, "/api/v1/submissions/"+sub.ID.String(), nil); code != 404 {
		t.Fatalf("bob baca: %d", code)
	}
	if code, _, _ := bob.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/url", nil); code != 404 {
		t.Fatalf("bob url foto: %d", code)
	}
	if r := bob.edit(t, sub.ID, "", editBody(2, sub.Photo...)); r.Status != 404 {
		t.Fatalf("bob edit: %d %s", r.Status, r.Raw)
	}
	// bob tidak bisa memakai foto alice (baik yang sudah dipakai maupun yang belum)
	free := alice.uploadedPhoto(t, 0x55)
	// foto milik orang lain dijawab sama persis dengan foto yang tidak ada (tidak bisa di-probe)
	for _, id := range []uuid.UUID{free, sub.Photo[0]} {
		if r := bob.submit(t, submitBody(uuid.New(), id)); r.Status != 422 || r.Code != "PHOTO_NOT_FOUND" {
			t.Fatalf("bob memakai foto alice: %d %s", r.Status, r.Raw)
		}
	}
	if r := alice.submit(t, submitBody(uuid.New(), free)); r.Status != 201 {
		t.Fatalf("alice memakai fotonya sendiri: %d %s", r.Status, r.Raw)
	}
	// X-Edit-Token tidak berarti apa-apa pada mode otp
	if code, _, _ := bob.get(t, "/api/v1/submissions/"+sub.ID.String(), map[string]string{"X-Edit-Token": "apa-saja"}); code != 404 {
		t.Fatalf("bob dengan edit token: %d", code)
	}
}

func TestE2ETurnstileInModeNone(t *testing.T) {
	cf := fakeSiteverify(t)
	calls := 0
	ts, err := turnstile.New("secret", cf.URL, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	counting := verifierFunc(func(ctx context.Context, token, ip string) error {
		calls++
		return ts.Verify(ctx, token, ip)
	})
	st := newStack(t, stackOpts{uploadTurnstile: turnstile.NewReusable(counting, 4, 5*time.Minute)})

	if resp, m := st.postJSON(t, "/api/v1/uploads/presign", `{"content_type":"image/webp","size_bytes":1000,"width":10,"height":10}`); resp.StatusCode != 403 || errField(m) != "TURNSTILE_FAILED" {
		t.Fatalf("presign tanpa token: %d %v", resp.StatusCode, m)
	}
	if r := st.submit(t, submitBody(uuid.New(), uuid.New())); r.Status != 403 || r.Code != "TURNSTILE_FAILED" {
		t.Fatalf("submit tanpa token: %d", r.Status)
	}
	bot := st.with(map[string]string{"X-Turnstile-Token": "bot"})
	if resp, _ := bot.postJSON(t, "/api/v1/uploads/presign", `{"content_type":"image/webp","size_bytes":1000,"width":10,"height":10}`); resp.StatusCode != 403 {
		t.Fatalf("token salah: %d", resp.StatusCode)
	}

	// satu token: 3 presign (maks. foto) + 1 submit, siteverify hanya dipanggil sekali
	calls = 0
	human := st.with(map[string]string{"X-Turnstile-Token": "human"})
	var photos []uuid.UUID
	for i := 0; i < 3; i++ {
		photos = append(photos, human.uploadedPhoto(t, byte(0x20+i)))
	}
	r := human.submit(t, submitBody(uuid.New(), photos...))
	if r.Status != 201 || r.EditToken == "" {
		t.Fatalf("submit: %d %s", r.Status, r.Raw)
	}
	if calls != 1 {
		t.Fatalf("siteverify dipanggil %d kali, ingin 1", calls)
	}
	// pemakaian ke-5 ditolak
	if resp, _ := human.postJSON(t, "/api/v1/uploads/presign", `{"content_type":"image/webp","size_bytes":1000,"width":10,"height":10}`); resp.StatusCode != 403 {
		t.Fatalf("pemakaian ke-5: %d", resp.StatusCode)
	}
	// edit dan baca memakai edit_token, tanpa Turnstile
	if code, _, _ := st.get(t, "/api/v1/submissions/"+r.SubmissionID.String(), map[string]string{"X-Edit-Token": r.EditToken}); code != 200 {
		t.Fatalf("baca dengan edit token: %d", code)
	}
	if e := st.edit(t, r.SubmissionID, r.EditToken, editBody(1, photos...)); e.Status != 200 {
		t.Fatalf("edit: %d %s", e.Status, e.Raw)
	}
}

type verifierFunc func(ctx context.Context, token, ip string) error

func (f verifierFunc) Verify(ctx context.Context, token, ip string) error { return f(ctx, token, ip) }
