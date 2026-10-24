package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

const (
	superPhone  = "6281311112222"
	adminSecret = "admin-secret-0123456789abcdef0123456789"
)

type adminRig struct {
	otps    *postgres.OTPRepo
	codes   *signer.OTPCodes
	channel *usecase.Channel
	phones  *usecase.PhoneLimiter
	clock   *fakeClock
	uc      *usecase.AdminLogin
}

func newAdminRig(t *testing.T) *adminRig {
	t.Helper()
	pool := pgtest.Pool(t)
	codes, err := signer.NewOTPCodes("pepper-0123456789abcdef0123456789abcdef", 6)
	if err != nil {
		t.Fatal(err)
	}
	r := &adminRig{otps: postgres.NewOTPRepo(pool), codes: codes, channel: usecase.NewChannel(3),
		clock: &fakeClock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	r.phones = usecase.NewPhoneLimiterWithClock(time.Nanosecond, 100, func() time.Time { return r.clock.t })
	r.uc = r.login(t, superPhone)
	return r
}

// login merakit AdminLogin untuk nomor superadmin tertentu (seperti restart dengan config baru).
func (r *adminRig) login(t *testing.T, phone string) *usecase.AdminLogin {
	t.Helper()
	tokens, err := signer.NewAdminTokens(adminSecret, phone)
	if err != nil {
		t.Fatal(err)
	}
	return usecase.NewAdminLogin(r.otps, r.codes, r.channel, r.phones, tokens, r.clock, usecase.AdminLoginConfig{
		OTP:         usecase.OTPRequestConfig{ResendCooldown: time.Minute, MaxQueue: 200, DailyGlobalCap: 500, SendInterval: 1500 * time.Millisecond, CodeTTL: 5 * time.Minute},
		MaxAttempts: 5, TokenTTL: 8 * time.Hour, Name: "Super Admin", Phone: phone,
	})
}

// send meniru dispatcher: klaim challenge berikutnya dan tandai terkirim.
func (r *adminRig) send(t *testing.T) *domain.OTPChallenge {
	t.Helper()
	c, err := r.otps.ClaimNext(bg, r.clock.t)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := r.otps.MarkSent(bg, c.ID, "msg", r.clock.t, r.clock.t.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAdminLoginFlow(t *testing.T) {
	r := newAdminRig(t)
	res, err := r.uc.Request(bg, "0813-1111-2222", nil)
	if err != nil || !res.Superadmin || res.MaskedPhone != "62813****2222" {
		t.Fatalf("request: %+v %v", res, err)
	}
	c, err := r.otps.Get(bg, res.Ticket.ChallengeID)
	if err != nil || c.Purpose != domain.OTPPurposeAdminLogin || c.UserID != uuid.Nil || c.Phone != superPhone {
		t.Fatalf("challenge: %+v %v", c, err)
	}
	if v, _ := r.uc.Status(bg, c.ID); v.Status != domain.OTPQueued {
		t.Fatalf("status = %v", v.Status)
	}
	if _, err := r.uc.Verify(bg, c.ID, "123456"); errCode(err) != "OTP_NOT_SENT" {
		t.Fatalf("verify sebelum terkirim: %v", err)
	}
	r.send(t)
	if v, _ := r.uc.Status(bg, c.ID); v.Status != domain.OTPSent {
		t.Fatalf("status = %v", v.Status)
	}
	code := r.codes.Code(c.ID)
	if _, err := r.uc.Verify(bg, c.ID, "12a"); errCode(err) != "INVALID_CODE" {
		t.Fatalf("kode bukan angka: %v", err)
	}
	if _, err := r.uc.Verify(bg, c.ID, wrongCode(code)); errCode(err) != "OTP_INVALID" || attemptsLeft(err) != 4 {
		t.Fatalf("kode salah: %v", err)
	}
	s, err := r.uc.Verify(bg, c.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if s.Admin.Name != "Super Admin" || s.Admin.MaskedPhone != "62813****2222" || !s.ExpiresAt.Equal(r.clock.t.Add(8*time.Hour).Truncate(time.Second)) {
		t.Fatalf("sesi admin: %+v", s)
	}
	if exp, ok := r.uc.VerifyToken(s.Token); !ok || !exp.Equal(s.ExpiresAt) {
		t.Fatalf("token: %v %v", exp, ok)
	}
	if _, err := r.uc.Verify(bg, c.ID, code); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("kode dipakai ulang: %v", err)
	}
	// mengganti nomor superadmin (restart dengan config baru) membatalkan token lama
	if _, ok := r.login(t, "6281399990000").VerifyToken(s.Token); ok {
		t.Fatal("token untuk nomor lama diterima")
	}
	r.clock.t = s.ExpiresAt
	if _, ok := r.uc.VerifyToken(s.Token); ok {
		t.Fatal("token kedaluwarsa diterima")
	}
}

func TestAdminLoginOtherPhoneIsDecoy(t *testing.T) {
	r := newAdminRig(t)
	res, err := r.uc.Request(bg, "081399998888", nil)
	if err != nil || res.Superadmin || res.Ticket == nil {
		t.Fatalf("request: %+v %v", res, err)
	}
	if _, err := r.otps.Get(bg, res.Ticket.ChallengeID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tidak boleh ada challenge di DB: %v", err)
	}
	if _, err := r.otps.ClaimNext(bg, r.clock.t); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("antrean harus kosong: %v", err)
	}
	id := res.Ticket.ChallengeID
	if v, _ := r.uc.Status(bg, id); v.Status != domain.OTPQueued {
		t.Fatalf("status palsu awal = %v", v.Status)
	}
	r.clock.t = r.clock.t.Add(2 * time.Second)
	if v, _ := r.uc.Status(bg, id); v.Status != domain.OTPSent || v.ExpiresAt == nil {
		t.Fatalf("status palsu setelah tunggu = %+v", v)
	}
	for i := 4; i >= 1; i-- {
		if _, err := r.uc.Verify(bg, id, "123456"); errCode(err) != "OTP_INVALID" || attemptsLeft(err) != i {
			t.Fatalf("tebakan palsu: %v", err)
		}
	}
	if _, err := r.uc.Verify(bg, id, "123456"); errCode(err) != "OTP_TOO_MANY_ATTEMPTS" {
		t.Fatalf("batas palsu: %v", err)
	}
	if _, err := r.uc.Request(bg, "nomor", nil); errCode(err) != "INVALID_PHONE" {
		t.Fatalf("nomor tidak valid: %v", err)
	}
}

func TestAdminLoginSuperadminChangedMidFlow(t *testing.T) {
	r := newAdminRig(t)
	res, err := r.uc.Request(bg, superPhone, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.send(t)
	code := r.codes.Code(res.Ticket.ChallengeID)
	other := r.login(t, "6281399990000") // config diganti lalu restart di tengah alur
	if _, err := other.Verify(bg, res.Ticket.ChallengeID, code); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("verify setelah nomor diganti: %v", err)
	}
	if _, err := other.Resend(bg, res.Ticket.ChallengeID, nil); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("resend setelah nomor diganti: %v", err)
	}
}

func TestAdminLoginResendAndChannel(t *testing.T) {
	r := newAdminRig(t)
	res, err := r.uc.Request(bg, superPhone, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.clock.t = r.clock.t.Add(time.Second)
	t2, err := r.uc.Resend(bg, res.Ticket.ChallengeID, nil)
	if err != nil || t2.ChallengeID == res.Ticket.ChallengeID {
		t.Fatalf("resend: %+v %v", t2, err)
	}
	if old, _ := r.otps.Get(bg, res.Ticket.ChallengeID); old.Status != domain.OTPExpired {
		t.Fatalf("challenge lama = %s", old.Status)
	}
	if _, err := r.uc.Resend(bg, uuid.New(), nil); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("resend id tak dikenal: %v", err)
	}
	// challenge pengguna tidak dikenali endpoint admin
	if v, _ := r.uc.Status(bg, uuid.New()); v.Status != domain.OTPQueued {
		t.Fatalf("status id tak dikenal = %v", v.Status)
	}
	for i := 0; i < 3; i++ {
		r.channel.RecordSend(false)
	}
	for _, p := range []string{superPhone, "081399998888"} {
		if _, err := r.uc.Request(bg, p, nil); errCode(err) != "OTP_UNAVAILABLE" {
			t.Fatalf("%s saat kanal mati: %v", p, err)
		}
	}
}
