package postgres_test

import (
	"bytes"
	"errors"
	"net/netip"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
)

type authRig struct {
	pool     *pgxpool.Pool
	users    *postgres.UserRepo
	otps     *postgres.OTPRepo
	sessions *postgres.SessionRepo
}

func newAuthRig(t *testing.T) *authRig {
	t.Helper()
	pool := pgtest.Pool(t)
	return &authRig{pool: pool, users: postgres.NewUserRepo(pool), otps: postgres.NewOTPRepo(pool), sessions: postgres.NewSessionRepo(pool)}
}

var phoneSeq = 0

func nextPhone() string {
	phoneSeq++
	return "62812" + pad8(phoneSeq)
}

func pad8(n int) string {
	s := "00000000" + itoa(n)
	return s[len(s)-8:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func newUser(phone string) *domain.User {
	return &domain.User{ID: uuid.New(), Name: "Budi Santoso", Email: "budi@example.com", Phone: phone, CreatedAt: now()}
}

func (r *authRig) user(t *testing.T) *domain.User {
	t.Helper()
	u, created, err := r.users.FindOrCreatePending(ctx, newUser(nextPhone()))
	if err != nil || !created {
		t.Fatalf("user: %v created=%v", err, created)
	}
	return u
}

func hash32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func newOTP(u *domain.User) *domain.OTPChallenge {
	ip := netip.MustParseAddr("203.0.113.9")
	return &domain.OTPChallenge{
		ID: uuid.New(), UserID: u.ID, Phone: u.Phone, CodeHash: hash32(1), Status: domain.OTPQueued, Purpose: domain.OTPPurposeReset,
		NextAttemptAt: now(), ClientIP: &ip, CreatedAt: now(),
	}
}

func (r *authRig) setOTPCreated(t *testing.T, id uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := r.pool.Exec(ctx, `UPDATE otp_challenges SET created_at=$2 WHERE id=$1`, id, at); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- users

func TestUserFindOrCreatePending(t *testing.T) {
	r := newAuthRig(t)
	phone := nextPhone()
	first := newUser(phone)
	got, created, err := r.users.FindOrCreatePending(ctx, first)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got.ID != first.ID || got.Status != domain.UserPending || got.PhoneVerifiedAt != nil || got.LastLoginAt != nil {
		t.Fatalf("user baru: %+v", got)
	}
	// nomor yang sama dengan nama/email lain: dikembalikan yang lama, TANPA menimpa
	again := newUser(phone)
	again.Name, again.Email = "Orang Lain", "lain@example.com"
	got2, created2, err := r.users.FindOrCreatePending(ctx, again)
	if err != nil || created2 {
		t.Fatalf("created=%v err=%v", created2, err)
	}
	if got2.ID != first.ID || got2.Name != "Budi Santoso" || got2.Email != "budi@example.com" {
		t.Fatalf("data pengguna yang sudah ada tidak boleh ditimpa: %+v", got2)
	}
	// round trip lewat Get / GetByPhone
	for name, f := range map[string]func() (*domain.User, error){
		"Get":        func() (*domain.User, error) { return r.users.Get(ctx, first.ID) },
		"GetByPhone": func() (*domain.User, error) { return r.users.GetByPhone(ctx, phone) },
	} {
		u, err := f()
		if err != nil || u.ID != first.ID || u.Phone != phone || !u.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("%s: %+v %v", name, u, err)
		}
	}
	if _, err := r.users.Get(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get tak ada: %v", err)
	}
	if _, err := r.users.GetByPhone(ctx, "628100000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByPhone tak ada: %v", err)
	}
}

func TestUserFindOrCreateConcurrentSamePhone(t *testing.T) {
	r := newAuthRig(t)
	phone := nextPhone()
	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	ids := map[uuid.UUID]bool{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, c, err := r.users.FindOrCreatePending(ctx, newUser(phone))
			if err != nil {
				t.Errorf("err = %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids[u.ID] = true
			if c {
				created++
			}
		}()
	}
	wg.Wait()
	if created != 1 || len(ids) != 1 {
		t.Fatalf("dibuat=%d id berbeda=%d, want 1 dan 1", created, len(ids))
	}
}

func TestUserActivate(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	t1 := now().Add(time.Minute)
	if err := r.users.Activate(ctx, u.ID, t1); err != nil {
		t.Fatal(err)
	}
	got, _ := r.users.Get(ctx, u.ID)
	if got.Status != domain.UserActive || got.PhoneVerifiedAt == nil || !got.PhoneVerifiedAt.Equal(t1) || got.LastLoginAt == nil || !got.LastLoginAt.Equal(t1) {
		t.Fatalf("%+v", got)
	}
	// login berikutnya: waktu verifikasi pertama dipertahankan, last_login diperbarui
	t2 := t1.Add(time.Hour)
	if err := r.users.Activate(ctx, u.ID, t2); err != nil {
		t.Fatal(err)
	}
	got, _ = r.users.Get(ctx, u.ID)
	if !got.PhoneVerifiedAt.Equal(t1) || !got.LastLoginAt.Equal(t2) {
		t.Fatalf("verifikasi pertama harus dipertahankan: %+v", got)
	}
	if err := r.users.Activate(ctx, uuid.New(), t1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Activate tak ada: %v", err)
	}
}

func TestUserConstraints(t *testing.T) {
	r := newAuthRig(t)
	insert := func(name, email, phone, status string, verified *time.Time) error {
		_, err := r.pool.Exec(ctx, `INSERT INTO users (id, name, email, phone, status, phone_verified_at) VALUES ($1,$2,$3,$4,$5,$6)`,
			uuid.New(), name, email, phone, status, verified)
		return err
	}
	v := now()
	cases := map[string]struct {
		err  error
		code string
	}{
		"valid":                         {insert("A", "a@b.co", nextPhone(), "pending", nil), ""},
		"nama kosong":                   {insert("", "a@b.co", nextPhone(), "pending", nil), "23514"},
		"nama 101 karakter":             {insert("x"+repeat("y", 100), "a@b.co", nextPhone(), "pending", nil), "23514"},
		"email huruf besar":             {insert("A", "A@B.CO", nextPhone(), "pending", nil), "23514"},
		"email terlalu pendek":          {insert("A", "ab", nextPhone(), "pending", nil), "23514"},
		"nomor bukan kanonik (08…)":     {insert("A", "a@b.co", "08123456789", "pending", nil), "23514"},
		"nomor dengan +":                {insert("A", "a@b.co", "+6281234567890", "pending", nil), "23514"},
		"nomor JID":                     {insert("A", "a@b.co", "6281234567890@s.whatsapp.net", "pending", nil), "23514"},
		"status tak dikenal":            {insert("A", "a@b.co", nextPhone(), "banned", nil), "23514"},
		"aktif tanpa verifikasi":        {insert("A", "a@b.co", nextPhone(), "active", nil), "23514"},
		"pending tapi terverifikasi":    {insert("A", "a@b.co", nextPhone(), "pending", &v), "23514"},
		"aktif dan terverifikasi (sah)": {insert("A", "a@b.co", nextPhone(), "active", &v), ""},
	}
	for name, c := range cases {
		if pgCode(c.err) != c.code {
			t.Errorf("%s: err = %v, want kode %q", name, c.err, c.code)
		}
	}
	// email tidak unik, nomor unik
	phone := nextPhone()
	if err := insert("A", "sama@b.co", phone, "pending", nil); err != nil {
		t.Fatal(err)
	}
	if err := insert("B", "sama@b.co", nextPhone(), "pending", nil); err != nil {
		t.Fatalf("email yang sama harus boleh dipakai beberapa pengguna: %v", err)
	}
	if err := insert("C", "c@b.co", phone, "pending", nil); pgCode(err) != "23505" {
		t.Fatalf("nomor kembar harus ditolak UNIQUE: %v", err)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestDeletePendingUsersBeforeCascades(t *testing.T) {
	r := newAuthRig(t)
	cutoff := now()
	oldPending, youngPending, oldActive, oldButRecentlyActive := r.user(t), r.user(t), r.user(t), r.user(t)
	for id, at := range map[uuid.UUID]time.Time{
		oldPending.ID: cutoff.Add(-time.Hour), youngPending.ID: cutoff.Add(time.Hour), oldActive.ID: cutoff.Add(-time.Hour),
		oldButRecentlyActive.ID: cutoff.Add(-48 * time.Hour),
	} {
		if _, err := r.pool.Exec(ctx, `UPDATE users SET created_at=$2 WHERE id=$1`, id, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.users.Activate(ctx, oldActive.ID, cutoff); err != nil {
		t.Fatal(err)
	}
	ch := newOTP(oldPending)
	if err := r.otps.Replace(ctx, ch); err != nil {
		t.Fatal(err)
	}
	r.setOTPCreated(t, ch.ID, cutoff.Add(-2*time.Hour))
	// regresi review: pengguna pending lama yang baru saja meminta kode tidak boleh dihapus (CASCADE
	// akan menghapus challenge yang sedang dipakai, sehingga kode yang sudah terkirim tidak bisa dipakai)
	live := newOTP(oldButRecentlyActive)
	if err := r.otps.Replace(ctx, live); err != nil {
		t.Fatal(err)
	}
	n, err := r.users.DeletePendingBefore(ctx, cutoff, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := r.users.Get(ctx, oldPending.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("pengguna pending lama harus terhapus")
	}
	for _, u := range []*domain.User{youngPending, oldActive, oldButRecentlyActive} {
		if _, err := r.users.Get(ctx, u.ID); err != nil {
			t.Fatalf("pengguna %s tidak boleh terhapus: %v", u.ID, err)
		}
	}
	if _, err := r.otps.Get(ctx, ch.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("challenge pengguna yang dihapus harus ikut terhapus (CASCADE)")
	}
	if _, err := r.otps.Get(ctx, live.ID); err != nil {
		t.Fatalf("challenge aktif harus tetap ada: %v", err)
	}
}

func TestUserDeletionKeepsSubmissionsAndPhotos(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	subs, photos := postgres.NewSubmissionRepo(r.pool), postgres.NewPhotoRepo(r.pool)
	s := newSub(&u.ID)
	if err := subs.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	p := validPhoto()
	p.OwnerID = &u.ID
	if err := photos.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	gotS, err := subs.Get(ctx, s.ID)
	if err != nil || gotS.OwnerID != nil {
		t.Fatalf("submission harus bertahan dengan owner NULL: %+v %v", gotS, err)
	}
	gotP, err := photos.Get(ctx, p.ID)
	if err != nil || gotP.OwnerID != nil {
		t.Fatalf("foto harus bertahan dengan owner NULL: %+v %v", gotP, err)
	}
	// FK owner: id yang tak ada ditolak
	bogus := uuid.New()
	bad := newSub(&bogus)
	if err := subs.Create(ctx, bad); pgCode(err) != "23503" {
		t.Fatalf("owner_id tak dikenal harus ditolak FK: %v", err)
	}
}

// ---------------------------------------------------------------- OTP

func TestOTPReplaceKeepsOneActivePerPhone(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	a := newOTP(u)
	if err := r.otps.Replace(ctx, a); err != nil {
		t.Fatal(err)
	}
	b := newOTP(u)
	if err := r.otps.Replace(ctx, b); err != nil {
		t.Fatal(err)
	}
	ga, _ := r.otps.Get(ctx, a.ID)
	gb, _ := r.otps.Get(ctx, b.ID)
	if ga.Status != domain.OTPExpired || gb.Status != domain.OTPQueued {
		t.Fatalf("lama=%s baru=%s, want expired/queued", ga.Status, gb.Status)
	}
	// challenge lain yang sudah final tidak disentuh
	if err := r.otps.Expire(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	c := newOTP(u)
	if err := r.otps.Replace(ctx, c); err != nil {
		t.Fatal(err)
	}
	// indeks parsial: dua challenge aktif untuk nomor yang sama mustahil
	d := newOTP(u)
	if _, err := r.pool.Exec(ctx, `INSERT INTO otp_challenges (id,user_id,phone,code_hash,status,next_attempt_at,created_at,purpose) VALUES ($1,$2,$3,$4,'queued',now(),now(),'reset')`,
		d.ID, u.ID, u.Phone, hash32(2)); pgCode(err) != "23505" {
		t.Fatalf("dua challenge aktif harus ditolak indeks unik parsial: %v", err)
	}
	// nomor lain tidak terpengaruh
	u2 := r.user(t)
	if err := r.otps.Replace(ctx, newOTP(u2)); err != nil {
		t.Fatal(err)
	}
}

func TestOTPReplaceConcurrentNeverLeavesTwoActive(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.otps.Replace(ctx, newOTP(u))
			if err != nil && codeOf(err) != "OTP_REQUEST_IN_PROGRESS" {
				t.Errorf("error tak terduga: %v", err)
			}
		}()
	}
	wg.Wait()
	var active int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM otp_challenges WHERE phone=$1 AND status IN ('queued','sending','sent')`, u.Phone).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("challenge aktif = %d, want tepat 1", active)
	}
}

func TestOTPRoundTripAndLatest(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	c := newOTP(u)
	if err := r.otps.Replace(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := r.otps.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != u.ID || got.Phone != u.Phone || !bytes.Equal(got.CodeHash, hash32(1)) || got.Status != domain.OTPQueued ||
		got.ClientIP == nil || got.ClientIP.String() != "203.0.113.9" || got.SentAt != nil || got.ExpiresAt != nil ||
		got.FailureReason != "" || got.WAMessageID != "" || got.VerifyAttempts != 0 || got.SendAttempts != 0 {
		t.Fatalf("%+v", got)
	}
	// Latest mengembalikan yang terbaru
	c2 := newOTP(u)
	c2.CreatedAt = c.CreatedAt.Add(time.Second)
	if err := r.otps.Replace(ctx, c2); err != nil {
		t.Fatal(err)
	}
	latest, err := r.otps.LatestForPhone(ctx, u.Phone)
	if err != nil || latest.ID != c2.ID {
		t.Fatalf("latest = %v err=%v", latest, err)
	}
	if _, err := r.otps.LatestForPhone(ctx, "628100000001"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("nomor tanpa challenge: %v", err)
	}
	if _, err := r.otps.Get(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get tak ada: %v", err)
	}
}

func TestOTPCounts(t *testing.T) {
	r := newAuthRig(t)
	u1, u2 := r.user(t), r.user(t)
	base := now()
	mk := func(u *domain.User, at time.Time, status domain.OTPStatus) {
		c := newOTP(u)
		c.CreatedAt = at
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
		r.setOTPCreated(t, c.ID, at)
		if status != domain.OTPQueued {
			if _, err := r.pool.Exec(ctx, `UPDATE otp_challenges SET status=$2 WHERE id=$1`, c.ID, string(status)); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk(u1, base.Add(-3*time.Hour), domain.OTPExpired)
	mk(u1, base.Add(-30*time.Minute), domain.OTPExpired)
	mk(u1, base.Add(-5*time.Minute), domain.OTPQueued)
	mk(u2, base.Add(-10*time.Minute), domain.OTPQueued)

	if n, _ := r.otps.CountForPhoneSince(ctx, u1.Phone, base.Add(-time.Hour)); n != 2 {
		t.Fatalf("u1 sejam terakhir = %d, want 2", n)
	}
	if n, _ := r.otps.CountForPhoneSince(ctx, u2.Phone, base.Add(-time.Hour)); n != 1 {
		t.Fatalf("u2 = %d, want 1", n)
	}
	if n, _ := r.otps.CountCreatedSince(ctx, base.Add(-4*time.Hour)); n != 4 {
		t.Fatalf("global = %d, want 4", n)
	}
	if n, _ := r.otps.CountCreatedSince(ctx, base.Add(-20*time.Minute)); n != 2 {
		t.Fatalf("global 20 menit = %d, want 2", n)
	}
	if n, _ := r.otps.CountQueued(ctx); n != 2 {
		t.Fatalf("queued = %d, want 2", n)
	}
}

func TestOTPClaimNextOrderAndReadiness(t *testing.T) {
	r := newAuthRig(t)
	base := now()
	var us []*domain.User
	var cs []*domain.OTPChallenge
	for i := 0; i < 3; i++ {
		u := r.user(t)
		c := newOTP(u)
		c.CreatedAt = base.Add(time.Duration(i) * time.Second)
		c.NextAttemptAt = base // siap tepat pada base
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
		us, cs = append(us, u), append(cs, c)
	}
	// challenge ke-1 baru siap 1 jam lagi (backoff)
	if err := r.otps.Requeue(ctx, cs[0].ID, base); err == nil {
		t.Fatal("Requeue pada challenge yang bukan sending harus konflik")
	}
	if _, err := r.pool.Exec(ctx, `UPDATE otp_challenges SET next_attempt_at=$2 WHERE id=$1`, cs[0].ID, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	first, err := r.otps.ClaimNext(ctx, base)
	if err != nil || first.ID != cs[1].ID {
		t.Fatalf("yang diklaim pertama harus yang tertua yang SIAP (cs[1]): %v %v", first, err)
	}
	if first.Status != domain.OTPSending || first.SendAttempts != 1 || !first.NextAttemptAt.Equal(base) {
		t.Fatalf("hasil klaim: %+v", first)
	}
	second, err := r.otps.ClaimNext(ctx, base)
	if err != nil || second.ID != cs[2].ID {
		t.Fatalf("kedua: %v %v", second, err)
	}
	if _, err := r.otps.ClaimNext(ctx, base); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("antrean kosong (cs[0] belum siap): %v", err)
	}
	third, err := r.otps.ClaimNext(ctx, base.Add(2*time.Hour))
	if err != nil || third.ID != cs[0].ID {
		t.Fatalf("setelah backoff lewat: %v %v", third, err)
	}
	_ = us
}

func TestOTPClaimNextEachItemExactlyOnceUnderConcurrency(t *testing.T) {
	r := newAuthRig(t)
	const n = 12
	want := map[uuid.UUID]bool{}
	for i := 0; i < n; i++ {
		c := newOTP(r.user(t))
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
		want[c.ID] = true
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[uuid.UUID]int{}
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				c, err := r.otps.ClaimNext(ctx, now().Add(time.Minute))
				if errors.Is(err, domain.ErrNotFound) {
					return
				}
				if err != nil {
					t.Errorf("err = %v", err)
					return
				}
				mu.Lock()
				got[c.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(got) != n {
		t.Fatalf("diklaim %d dari %d", len(got), n)
	}
	for id, k := range got {
		if k != 1 || !want[id] {
			t.Fatalf("challenge %s diklaim %d kali (harus tepat sekali)", id, k)
		}
	}
}

func TestOTPSendTransitions(t *testing.T) {
	r := newAuthRig(t)
	mkSending := func() *domain.OTPChallenge {
		c := newOTP(r.user(t))
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, err := r.otps.ClaimNext(ctx, now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Run("MarkSent", func(t *testing.T) {
		c := mkSending()
		sent, exp := now(), now().Add(5*time.Minute)
		if err := r.otps.MarkSent(ctx, c.ID, "3EB04F18B37A", sent, exp); err != nil {
			t.Fatal(err)
		}
		got, _ := r.otps.Get(ctx, c.ID)
		if got.Status != domain.OTPSent || got.WAMessageID != "3EB04F18B37A" || got.SentAt == nil || !got.SentAt.Equal(sent) ||
			got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) || got.FailureReason != "" {
			t.Fatalf("%+v", got)
		}
		// sudah sent: transisi lain ditolak
		if err := r.otps.MarkSent(ctx, c.ID, "x", sent, exp); codeOf(err) != "OTP_STATE_CHANGED" {
			t.Fatalf("MarkSent dua kali: %v", err)
		}
		if err := r.otps.MarkFailed(ctx, c.ID, domain.OTPFailSendFailed); codeOf(err) != "OTP_STATE_CHANGED" {
			t.Fatalf("MarkFailed setelah sent: %v", err)
		}
	})
	t.Run("Requeue", func(t *testing.T) {
		c := mkSending()
		next := now().Add(3 * time.Second)
		if err := r.otps.Requeue(ctx, c.ID, next); err != nil {
			t.Fatal(err)
		}
		got, _ := r.otps.Get(ctx, c.ID)
		if got.Status != domain.OTPQueued || !got.NextAttemptAt.Equal(next) || got.SendAttempts != 1 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("MarkFailed", func(t *testing.T) {
		c := mkSending()
		if err := r.otps.MarkFailed(ctx, c.ID, domain.OTPFailWAUnavailable); err != nil {
			t.Fatal(err)
		}
		got, _ := r.otps.Get(ctx, c.ID)
		if got.Status != domain.OTPFailed || got.FailureReason != domain.OTPFailWAUnavailable {
			t.Fatalf("%+v", got)
		}
		// setelah gagal, nomor boleh meminta kode baru
		if err := r.otps.Replace(ctx, newOTPFor(got)); err != nil {
			t.Fatalf("setelah failed harus bisa membuat challenge baru: %v", err)
		}
	})
	t.Run("diganti saat sedang dikirim", func(t *testing.T) {
		c := mkSending()
		if err := r.otps.Replace(ctx, newOTPFor(c)); err != nil { // permintaan baru → yang lama expired
			t.Fatal(err)
		}
		if err := r.otps.MarkSent(ctx, c.ID, "x", now(), now().Add(time.Minute)); codeOf(err) != "OTP_STATE_CHANGED" {
			t.Fatalf("MarkSent pada challenge yang sudah diganti harus konflik: %v", err)
		}
		if got, _ := r.otps.Get(ctx, c.ID); got.Status != domain.OTPExpired {
			t.Fatalf("status = %s", got.Status)
		}
	})
	t.Run("MarkFailed dengan alasan tak dikenal ditolak CHECK", func(t *testing.T) {
		c := mkSending()
		if err := r.otps.MarkFailed(ctx, c.ID, "ALASAN_LAIN"); pgCode(err) != "23514" {
			t.Fatalf("err = %v", err)
		}
	})
}

func newOTPFor(c *domain.OTPChallenge) *domain.OTPChallenge {
	n := *c
	n.ID = uuid.New()
	n.Status = domain.OTPQueued
	n.FailureReason, n.WAMessageID = "", ""
	n.SentAt, n.ExpiresAt = nil, nil
	n.VerifyAttempts, n.SendAttempts = 0, 0
	n.NextAttemptAt, n.CreatedAt = now(), now()
	return &n
}

func TestOTPRequeueStuck(t *testing.T) {
	r := newAuthRig(t)
	base := now()
	stuck, fresh := newOTP(r.user(t)), newOTP(r.user(t))
	for _, c := range []*domain.OTPChallenge{stuck, fresh} {
		c.NextAttemptAt = base.Add(-time.Hour)
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.otps.ClaimNext(ctx, base.Add(-10*time.Minute)); err != nil { // diklaim 10 menit lalu
		t.Fatal(err)
	}
	if _, err := r.otps.ClaimNext(ctx, base.Add(-time.Second)); err != nil { // diklaim baru saja
		t.Fatal(err)
	}
	n, err := r.otps.RequeueStuck(ctx, base.Add(-2*time.Minute), base)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1", n, err)
	}
	st, _ := r.otps.Get(ctx, stuck.ID)
	fr, _ := r.otps.Get(ctx, fresh.ID)
	if st.Status != domain.OTPQueued || !st.NextAttemptAt.Equal(base) || fr.Status != domain.OTPSending {
		t.Fatalf("stuck=%s fresh=%s", st.Status, fr.Status)
	}
}

func TestOTPVerifyAttemptsAreAtomic(t *testing.T) {
	r := newAuthRig(t)
	c := newOTP(r.user(t))
	if err := r.otps.Replace(ctx, c); err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	var seen []int
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := r.otps.IncrementVerifyAttempts(ctx, c.ID)
			if err != nil {
				t.Errorf("err = %v", err)
				return
			}
			mu.Lock()
			seen = append(seen, v)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Ints(seen)
	for i, v := range seen {
		if v != i+1 {
			t.Fatalf("setiap kenaikan harus mengembalikan nilai unik 1..%d (percobaan tebakan tidak boleh hilang): %v", n, seen)
		}
	}
	if _, err := r.otps.IncrementVerifyAttempts(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("id tak ada: %v", err)
	}
}

func TestOTPConsumeIsSingleUse(t *testing.T) {
	r := newAuthRig(t)
	c := newOTP(r.user(t))
	if err := r.otps.Replace(ctx, c); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.otps.Consume(ctx, c.ID); err != nil || ok {
		t.Fatalf("challenge queued tidak boleh dipakai: ok=%v err=%v", ok, err)
	}
	got, _ := r.otps.ClaimNext(ctx, now().Add(time.Minute))
	if err := r.otps.MarkSent(ctx, got.ID, "id", now(), now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := r.otps.Consume(ctx, c.ID)
			if err != nil {
				t.Errorf("err = %v", err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("dari %d verifikasi serentak dengan kode benar hanya boleh 1 yang menang, dapat %d", n, wins)
	}
	if g, _ := r.otps.Get(ctx, c.ID); g.Status != domain.OTPConsumed {
		t.Fatalf("status = %s", g.Status)
	}
}

func TestOTPExpireAndDeleteBefore(t *testing.T) {
	r := newAuthRig(t)
	cutoff := now()
	mk := func(status string, age time.Duration) uuid.UUID {
		c := newOTP(r.user(t))
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE otp_challenges SET status=$2, created_at=$3 WHERE id=$1`, c.ID, status, cutoff.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	oldConsumed, oldFailed, oldExpired := mk("consumed", time.Hour), mk("failed", time.Hour), mk("expired", time.Hour)
	oldQueued, oldSending := mk("queued", time.Hour), mk("sending", time.Hour) // masih berjalan: jangan disentuh
	youngConsumed := mk("consumed", -time.Hour)

	n, err := r.otps.DeleteBefore(ctx, cutoff, 100)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v, want 3", n, err)
	}
	for _, id := range []uuid.UUID{oldConsumed, oldFailed, oldExpired} {
		if _, err := r.otps.Get(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s harus terhapus", id)
		}
	}
	for _, id := range []uuid.UUID{oldQueued, oldSending, youngConsumed} {
		if _, err := r.otps.Get(ctx, id); err != nil {
			t.Fatalf("%s tidak boleh terhapus: %v", id, err)
		}
	}
	// Expire: hanya challenge aktif
	if err := r.otps.Expire(ctx, oldQueued); err != nil {
		t.Fatal(err)
	}
	if g, _ := r.otps.Get(ctx, oldQueued); g.Status != domain.OTPExpired {
		t.Fatal("Expire harus menandai expired")
	}
	if err := r.otps.Expire(ctx, youngConsumed); err != nil {
		t.Fatal(err)
	}
	if g, _ := r.otps.Get(ctx, youngConsumed); g.Status != domain.OTPConsumed {
		t.Fatal("Expire tidak boleh mengubah challenge yang sudah final")
	}
}

func TestOTPConstraints(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	exec := func(status string, code []byte, phone string, failure *string, sent, expires *time.Time) error {
		_, err := r.pool.Exec(ctx, `INSERT INTO otp_challenges (id,user_id,phone,code_hash,status,failure_reason,next_attempt_at,created_at,sent_at,expires_at,purpose)
			VALUES ($1,$2,$3,$4,$5,$6,now(),now(),$7,$8,'reset')`, uuid.New(), u.ID, phone, code, status, failure, sent, expires)
		return err
	}
	f := "SEND_FAILED"
	t0 := now()
	cases := map[string]struct {
		err  error
		code string
	}{
		"code_hash 31 byte":                        {exec("expired", hash32(1)[:31], u.Phone, nil, nil, nil), "23514"},
		"nomor bukan kanonik":                      {exec("expired", hash32(1), "08123456789", nil, nil, nil), "23514"},
		"status tak dikenal":                       {exec("hilang", hash32(1), u.Phone, nil, nil, nil), "23514"},
		"alasan gagal pada status bukan failed":    {exec("expired", hash32(1), u.Phone, &f, nil, nil), "23514"},
		"expires_at tanpa sent_at":                 {exec("expired", hash32(1), u.Phone, nil, nil, &t0), "23514"},
		"failed dengan alasan (sah)":               {exec("failed", hash32(1), u.Phone, &f, nil, nil), ""},
		"sent dengan sent_at dan expires_at (sah)": {exec("expired", hash32(1), u.Phone, nil, &t0, &t0), ""},
	}
	for name, c := range cases {
		if pgCode(c.err) != c.code {
			t.Errorf("%s: err = %v, want kode %q", name, c.err, c.code)
		}
	}
}

// ---------------------------------------------------------------- sessions

func newSession(u *domain.User, hashByte byte, expiresIn time.Duration) *domain.Session {
	ip := netip.MustParseAddr("203.0.113.9")
	n := now()
	return &domain.Session{ID: uuid.New(), UserID: u.ID, TokenHash: hash32(hashByte), CreatedAt: n, ExpiresAt: n.Add(expiresIn), ClientIP: &ip}
}

func TestSessionCreateGetRevoke(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	s := newSession(u, 7, 30*24*time.Hour)
	if err := r.sessions.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := r.sessions.GetByTokenHash(ctx, hash32(7))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s.ID || got.UserID != u.ID || !got.ExpiresAt.Equal(s.ExpiresAt) || got.RevokedAt != nil ||
		got.ClientIP == nil || got.ClientIP.String() != "203.0.113.9" || !got.Active(now()) {
		t.Fatalf("%+v", got)
	}
	if _, err := r.sessions.GetByTokenHash(ctx, hash32(8)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("hash tak dikenal: %v", err)
	}
	// Revoke idempoten: waktu pencabutan pertama dipertahankan
	t1 := now().Add(time.Minute)
	if err := r.sessions.Revoke(ctx, s.ID, t1); err != nil {
		t.Fatal(err)
	}
	if err := r.sessions.Revoke(ctx, s.ID, t1.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = r.sessions.GetByTokenHash(ctx, hash32(7))
	if got.RevokedAt == nil || !got.RevokedAt.Equal(t1) || got.Active(now()) {
		t.Fatalf("revoked: %+v", got)
	}
	if err := r.sessions.Revoke(ctx, uuid.New(), t1); err != nil {
		t.Fatalf("mencabut sesi yang tak ada harus diam-diam sukses: %v", err)
	}
}

func TestSessionConstraints(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	if err := r.sessions.Create(ctx, newSession(u, 1, time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.sessions.Create(ctx, newSession(u, 1, time.Hour)); pgCode(err) != "23505" {
		t.Fatalf("token_hash kembar harus ditolak: %v", err)
	}
	bad := newSession(u, 2, time.Hour)
	bad.TokenHash = bad.TokenHash[:16]
	if err := r.sessions.Create(ctx, bad); pgCode(err) != "23514" {
		t.Fatalf("token_hash bukan 32 byte harus ditolak: %v", err)
	}
	past := newSession(u, 3, -time.Hour)
	if err := r.sessions.Create(ctx, past); pgCode(err) != "23514" {
		t.Fatalf("expires_at sebelum created_at harus ditolak: %v", err)
	}
	ghost := newSession(&domain.User{ID: uuid.New()}, 4, time.Hour)
	if err := r.sessions.Create(ctx, ghost); pgCode(err) != "23503" {
		t.Fatalf("user tak dikenal harus ditolak FK: %v", err)
	}
}

func TestSessionDeleteInactiveBeforeAndCascade(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	cutoff := now()
	mk := func(b byte, expiresAt time.Time, revokedAt *time.Time) *domain.Session {
		s := newSession(u, b, time.Hour)
		s.CreatedAt = expiresAt.Add(-48 * time.Hour)
		s.ExpiresAt = expiresAt
		s.RevokedAt = revokedAt
		if err := r.sessions.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	longAgo, recentRev := cutoff.Add(-10*24*time.Hour), cutoff.Add(time.Hour)
	oldExpired := mk(10, cutoff.Add(-time.Hour), nil)
	oldRevoked := mk(11, cutoff.Add(24*time.Hour), &longAgo)
	activeSess := mk(12, cutoff.Add(24*time.Hour), nil)
	recentlyRevoked := mk(13, cutoff.Add(24*time.Hour), &recentRev)
	justExpiredAfterCutoff := mk(14, cutoff.Add(time.Minute), nil)

	n, err := r.sessions.DeleteInactiveBefore(ctx, cutoff, 100)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v, want 2", n, err)
	}
	for _, s := range []*domain.Session{oldExpired, oldRevoked} {
		if _, err := r.sessions.GetByTokenHash(ctx, s.TokenHash); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("sesi %s harus terhapus", s.ID)
		}
	}
	for _, s := range []*domain.Session{activeSess, recentlyRevoked, justExpiredAfterCutoff} {
		if _, err := r.sessions.GetByTokenHash(ctx, s.TokenHash); err != nil {
			t.Fatalf("sesi %s tidak boleh terhapus: %v", s.ID, err)
		}
	}
	// hapus pengguna → sesinya ikut terhapus
	if _, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sessions.GetByTokenHash(ctx, activeSess.TokenHash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("sesi harus ikut terhapus bersama pengguna (CASCADE)")
	}
}

const bcryptLike = "$2a$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234"

func TestOTPRegistrationDataRoundTripAndConstraint(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	c := newOTP(u)
	c.Purpose, c.RegName, c.RegEmail, c.RegPasswordHash = domain.OTPPurposeRegister, "Budi", "budi@example.com", bcryptLike
	if err := r.otps.Replace(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := r.otps.Get(ctx, c.ID)
	if err != nil || got.Purpose != domain.OTPPurposeRegister || got.RegName != "Budi" || got.RegEmail != "budi@example.com" || got.RegPasswordHash != bcryptLike {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	claimed, err := r.otps.ClaimNext(ctx, now().Add(time.Second))
	if err != nil || claimed.RegPasswordHash != bcryptLike || claimed.Purpose != domain.OTPPurposeRegister {
		t.Fatalf("ClaimNext harus membawa kolom baru: %+v %v", claimed, err)
	}
	// challenge registrasi tanpa data, atau reset dengan data, ditolak database
	bad := newOTP(r.user(t))
	bad.Purpose = domain.OTPPurposeRegister
	if err := r.otps.Replace(ctx, bad); err == nil {
		t.Fatal("registrasi tanpa data harus ditolak")
	}
	bad2 := newOTP(r.user(t))
	bad2.RegPasswordHash = bcryptLike
	if err := r.otps.Replace(ctx, bad2); err == nil {
		t.Fatal("reset dengan data registrasi harus ditolak")
	}
}

func TestUserPasswordLifecycle(t *testing.T) {
	r := newAuthRig(t)
	u := r.user(t)
	if err := r.users.SetPassword(ctx, u.ID, bcryptLike); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("pengguna pending tidak boleh diberi password lewat reset: %v", err)
	}
	if err := r.users.CompleteRegistration(ctx, u.ID, "Nama Baru", "baru@example.com", bcryptLike, now()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.users.Get(ctx, u.ID)
	if got.Status != domain.UserActive || got.PhoneVerifiedAt == nil || got.Name != "Nama Baru" || got.PasswordHash != bcryptLike {
		t.Fatalf("registrasi: %+v", got)
	}
	if err := r.users.CompleteRegistration(ctx, u.ID, "X", "x@example.com", bcryptLike, now()); err == nil {
		t.Fatal("registrasi kedua untuk pengguna aktif harus ditolak")
	}
	other := bcryptLike[:59] + "9" // hash lain, tetap 60 karakter
	if err := r.users.SetPassword(ctx, u.ID, other); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.users.Get(ctx, u.ID); got.PasswordHash != other {
		t.Fatal("password tidak berganti")
	}
	// hash yang bukan bcrypt ditolak database
	if err := r.users.SetPassword(ctx, u.ID, "plaintext"); err == nil {
		t.Fatal("password polos harus ditolak")
	}
	// cabut semua sesi pengguna
	for i := byte(0); i < 3; i++ {
		if err := r.sessions.Create(ctx, &domain.Session{ID: uuid.New(), UserID: u.ID, TokenHash: hash32(10 + i), CreatedAt: now(), ExpiresAt: now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.sessions.RevokeAllForUser(ctx, u.ID, now()); err != nil {
		t.Fatal(err)
	}
	for i := byte(0); i < 3; i++ {
		s, _ := r.sessions.GetByTokenHash(ctx, hash32(10+i))
		if s.Active(now()) {
			t.Fatal("sesi harus dicabut")
		}
	}
}
