package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

type otpReqRig struct {
	pool    *pgxpool.Pool
	users   *postgres.UserRepo
	otps    *postgres.OTPRepo
	codes   *signer.OTPCodes
	channel *usecase.Channel
	clock   *fakeClock
	uc      *usecase.OTPRequest
}

type otpReqOpts struct {
	cooldown time.Duration
	perHour  int
	maxQueue int
	daily    int
}

func newOTPReqRig(t *testing.T, o otpReqOpts) *otpReqRig {
	t.Helper()
	if o.perHour == 0 {
		o.perHour = 5
	}
	if o.maxQueue == 0 {
		o.maxQueue = 200
	}
	if o.daily == 0 {
		o.daily = 500
	}
	pool := pgtest.Pool(t)
	codes, err := signer.NewOTPCodes("pepper-0123456789abcdef0123456789abcdef", 6)
	if err != nil {
		t.Fatal(err)
	}
	r := &otpReqRig{
		pool: pool, users: postgres.NewUserRepo(pool), otps: postgres.NewOTPRepo(pool), codes: codes,
		channel: usecase.NewChannel(3),
		clock:   &fakeClock{t: time.Now().UTC().Truncate(time.Microsecond)},
	}
	limiter := usecase.NewPhoneLimiterWithClock(o.cooldown, o.perHour, func() time.Time { return r.clock.t })
	r.uc = usecase.NewOTPRequest(r.users, r.otps, codes, r.channel, limiter, r.clock, usecase.OTPRequestConfig{
		ResendCooldown: 60 * time.Second, MaxQueue: o.maxQueue, DailyGlobalCap: o.daily, SendInterval: 1500 * time.Millisecond, CodeTTL: 5 * time.Minute,
	})
	return r
}

func errCode(err error) string {
	if de, ok := domain.AsError(err); ok {
		return de.Code
	}
	return ""
}

func (r *otpReqRig) register(phone string) (*usecase.OTPTicket, error) {
	ip := netip.MustParseAddr("203.0.113.7")
	return r.uc.Register(bg, usecase.RegisterInput{Name: " Budi Santoso ", Email: "Budi@Example.com", Phone: phone, ClientIP: &ip})
}

func TestOTPRegisterCreatesPendingUserAndQueuedChallenge(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute})
	tk, err := r.register("0812-3456-7890")
	if err != nil {
		t.Fatal(err)
	}
	if tk.ResendAfter != time.Minute || tk.EstimatedWait != 1500*time.Millisecond {
		t.Fatalf("ticket = %+v", tk)
	}
	u, err := r.users.GetByPhone(bg, "6281234567890")
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Budi Santoso" || u.Email != "budi@example.com" || u.Status != domain.UserPending {
		t.Fatalf("user = %+v", u)
	}
	c, err := r.otps.Get(bg, tk.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != domain.OTPQueued || c.UserID != u.ID || c.Phone != u.Phone || c.ExpiresAt != nil {
		t.Fatalf("challenge = %+v", c)
	}
	if !bytes.Equal(c.CodeHash, r.codes.Hash(c.ID, r.codes.Code(c.ID))) {
		t.Fatal("code_hash harus hash dari kode turunan challenge id")
	}
	if c.ClientIP == nil || c.ClientIP.String() != "203.0.113.7" {
		t.Fatalf("client ip = %v", c.ClientIP)
	}
}

func TestOTPRegisterExistingNumberKeepsProfileAndReplacesChallenge(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{}) // cooldown 0
	first, err := r.register("081234567890")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := r.users.GetByPhone(bg, "6281234567890")
	if err := r.users.Activate(bg, u.ID, r.clock.t); err != nil {
		t.Fatal(err)
	}
	ip := netip.MustParseAddr("198.51.100.1")
	second, err := r.uc.Register(bg, usecase.RegisterInput{Name: "Penyerang", Email: "evil@example.com", Phone: "+62 812 3456 7890", ClientIP: &ip})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := r.users.Get(bg, u.ID)
	if got.Name != "Budi Santoso" || got.Email != "budi@example.com" || got.Status != domain.UserActive {
		t.Fatalf("profil tertimpa: %+v", got)
	}
	old, _ := r.otps.Get(bg, first.ChallengeID)
	cur, _ := r.otps.Get(bg, second.ChallengeID)
	if old.Status != domain.OTPExpired || cur.Status != domain.OTPQueued || cur.UserID != u.ID {
		t.Fatalf("lama=%s baru=%s", old.Status, cur.Status)
	}
}

func TestOTPRequestKnownAndUnknownLookTheSame(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute})
	if _, err := r.register("081234567890"); err != nil {
		t.Fatal(err)
	}
	r.clock.t = r.clock.t.Add(2 * time.Minute)
	known, errK := r.uc.Request(bg, "081234567890", nil)
	unknown, errU := r.uc.Request(bg, "081299998888", nil)
	if errK != nil || errU != nil {
		t.Fatal(errK, errU)
	}
	if known.ResendAfter != unknown.ResendAfter || known.EstimatedWait == 0 || unknown.EstimatedWait == 0 {
		t.Fatalf("bentuk berbeda: %+v vs %+v", known, unknown)
	}
	if _, err := r.otps.Get(bg, unknown.ChallengeID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("nomor tak terdaftar tidak boleh membuat challenge: %v", err)
	}
	if _, err := r.users.GetByPhone(bg, "6281299998888"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("otp/request tidak boleh membuat pengguna: %v", err)
	}
	// cooldown berlaku sama untuk keduanya
	_, errK = r.uc.Request(bg, "081234567890", nil)
	_, errU = r.uc.Request(bg, "081299998888", nil)
	dk, _ := domain.AsError(errK)
	du, _ := domain.AsError(errU)
	if dk == nil || du == nil || dk.Code != "RATE_LIMITED" || du.Code != "RATE_LIMITED" || dk.RetryAfter != du.RetryAfter {
		t.Fatalf("batas tidak seragam: %v / %v", errK, errU)
	}
}

func TestOTPRequestValidationDoesNotConsumeQuota(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute})
	cases := []struct {
		in   usecase.RegisterInput
		want string
	}{
		{usecase.RegisterInput{Name: "", Email: "a@b.co", Phone: "081234567890"}, "INVALID_NAME"},
		{usecase.RegisterInput{Name: "A", Email: "bukan-email", Phone: "081234567890"}, "INVALID_EMAIL"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "120363@g.us"}, "INVALID_PHONE"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "0212345678"}, "INVALID_PHONE"},
	}
	for _, tc := range cases {
		if _, err := r.uc.Register(bg, tc.in); errCode(err) != tc.want {
			t.Fatalf("%+v: err = %v, ingin %s", tc.in, err, tc.want)
		}
	}
	if _, err := r.uc.Request(bg, "+1 555 0100", nil); errCode(err) != "INVALID_PHONE" {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.register("081234567890"); err != nil {
		t.Fatalf("validasi gagal tidak boleh memakai jatah cooldown: %v", err)
	}
}

func TestOTPRequestChannelUnavailable(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{})
	for i := 0; i < 3; i++ {
		r.channel.RecordSend(false)
	}
	if _, err := r.register("081234567890"); errCode(err) != "OTP_UNAVAILABLE" {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.uc.Request(bg, "081234567890", nil); errCode(err) != "OTP_UNAVAILABLE" {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.users.GetByPhone(bg, "6281234567890"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("tidak boleh ada pengguna/challenge saat kanal tidak tersedia")
	}
	r.channel.RecordProbe(nil)
	if _, err := r.register("081234567890"); err != nil {
		t.Fatalf("setelah probe sukses harus pulih: %v", err)
	}
}

func TestOTPRequestQueueFullAndDailyCap(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute, maxQueue: 2, daily: 3})
	for _, p := range []string{"081200000001", "081200000002"} {
		if _, err := r.register(p); err != nil {
			t.Fatal(err)
		}
	}
	_, err := r.register("081200000003")
	de, _ := domain.AsError(err)
	if de == nil || de.Code != "OTP_BUSY" || de.RetryAfter != 3*time.Second {
		t.Fatalf("antrean penuh: %v", err)
	}
	// penolakan karena kapasitas tidak memakai jatah nomor itu
	if _, err := r.otps.ClaimNext(bg, r.clock.t); err != nil { // satu keluar dari antrean
		t.Fatal(err)
	}
	tk, err := r.register("081200000003")
	if err != nil {
		t.Fatalf("setelah antrean berkurang harus lolos (tanpa cooldown): %v", err)
	}
	if tk.EstimatedWait != 2*1500*time.Millisecond {
		t.Fatalf("estimated wait = %v (1 di depan + giliran sendiri)", tk.EstimatedWait)
	}
	// 3 challenge dibuat dalam 24 jam → batas harian tercapai walau antrean masih muat
	if _, err := r.otps.ClaimNext(bg, r.clock.t); err != nil {
		t.Fatal(err)
	}
	if _, err := r.register("081200000004"); errCode(err) != "OTP_BUSY" {
		t.Fatalf("batas harian: %v", err)
	}
	r.clock.t = r.clock.t.Add(24*time.Hour + time.Second)
	if _, err := r.register("081200000004"); err != nil {
		t.Fatalf("hari berikutnya harus lolos: %v", err)
	}
}

func TestOTPRegisterConcurrentSameNumber(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{perHour: 1000}) // tanpa cooldown: balapan sampai ke DB
	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.register("081234567890")
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errCode(err) == "OTP_REQUEST_IN_PROGRESS":
		default:
			t.Fatalf("error tak terduga: %v", err)
		}
	}
	if ok == 0 {
		t.Fatal("minimal satu permintaan harus berhasil")
	}
	var active, users int
	if err := r.pool.QueryRow(bg, `SELECT count(*) FROM otp_challenges WHERE status IN ('queued','sending','sent')`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(bg, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if active != 1 || users != 1 {
		t.Fatalf("active=%d users=%d, ingin 1 dan 1", active, users)
	}
}

// failingReplace menggagalkan Replace sekali, untuk memastikan jatah nomor dikembalikan.
type failingReplace struct {
	*postgres.OTPRepo
	fail bool
}

func (f *failingReplace) Replace(ctx context.Context, c *domain.OTPChallenge) error {
	if f.fail {
		f.fail = false
		return errors.New("db down")
	}
	return f.OTPRepo.Replace(ctx, c)
}

func TestOTPIssueFailureRefundsQuota(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{})
	repo := &failingReplace{OTPRepo: r.otps, fail: true}
	limiter := usecase.NewPhoneLimiterWithClock(time.Minute, 5, func() time.Time { return r.clock.t })
	uc := usecase.NewOTPRequest(r.users, repo, r.codes, r.channel, limiter, r.clock, usecase.OTPRequestConfig{
		ResendCooldown: time.Minute, MaxQueue: 10, DailyGlobalCap: 10, SendInterval: time.Second, CodeTTL: 5 * time.Minute,
	})
	in := usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890"}
	if _, err := uc.Register(bg, in); err == nil {
		t.Fatal("Replace gagal harus mengembalikan error")
	}
	if _, err := uc.Register(bg, in); err != nil {
		t.Fatalf("jatah harus dikembalikan setelah gagal menyimpan: %v", err)
	}
	if _, err := uc.Register(bg, in); errCode(err) != "RATE_LIMITED" {
		t.Fatalf("setelah berhasil, cooldown berlaku: %v", err)
	}
}
