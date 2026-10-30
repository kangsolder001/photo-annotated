package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"photo-annotated/internal/adapter/password"
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
	group   int // kelompok aktif untuk registrasi
}

// seedGroup membuat kelompok aktif dan mengembalikan id-nya.
func seedGroup(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(bg, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// nimFor: NIM unik per nomor (dipakai test yang mendaftarkan banyak nomor).
func nimFor(phone string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	return "N" + d[len(d)-10:]
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
	r.group = seedGroup(t, pool, "Kelompok Uji")
	r.uc = usecase.NewOTPRequest(r.users, postgres.NewGroupRepo(pool), r.otps, codes, testHasher, r.channel, limiter, r.clock, usecase.OTPRequestConfig{
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
	return r.uc.Register(bg, usecase.RegisterInput{Name: " Budi Santoso ", Email: "Budi@Example.com", Phone: phone, Password: "rahasia123",
		NIM: nimFor(phone), GroupID: r.group, ClientIP: &ip})
}

// testHasher: bcrypt dengan cost minimum agar test cepat.
var testHasher = func() *password.Bcrypt {
	h, err := password.NewBcrypt(bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return h
}()

// activate menyelesaikan registrasi nomor itu seperti verifikasi kode yang berhasil.
func (r *otpReqRig) activate(t *testing.T, phone string) *domain.User {
	t.Helper()
	u, err := r.users.GetByPhone(bg, phone)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := testHasher.Hash("rahasia123")
	if err := r.users.CompleteRegistration(bg, u.ID, usecase.Registration{Name: u.Name, Email: u.Email, PasswordHash: h, NIM: nimFor(phone), GroupID: &r.group}, r.clock.t); err != nil {
		t.Fatal(err)
	}
	return u
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
	if c.Status != domain.OTPQueued || c.UserID != u.ID || c.Phone != u.Phone || c.ExpiresAt != nil || c.Purpose != domain.OTPPurposeRegister {
		t.Fatalf("challenge = %+v", c)
	}
	// data registrasi menunggu di challenge; pengguna belum punya password sampai kode diverifikasi
	if c.RegName != "Budi Santoso" || c.RegEmail != "budi@example.com" || !testHasher.Compare(c.RegPasswordHash, "rahasia123") || u.PasswordHash != "" {
		t.Fatalf("data registrasi: %+v / user hash %q", c, u.PasswordHash)
	}
	if !bytes.Equal(c.CodeHash, r.codes.Hash(c.ID, r.codes.Code(c.ID))) {
		t.Fatal("code_hash harus hash dari kode turunan challenge id")
	}
	if c.ClientIP == nil || c.ClientIP.String() != "203.0.113.7" {
		t.Fatalf("client ip = %v", c.ClientIP)
	}
}

// Nomor yang sudah aktif tidak bisa didaftarkan ulang: respons sama, tetapi tidak ada challenge,
// tidak ada pesan, dan profil/password pemilik tidak berubah.
func TestOTPRegisterActiveNumberIsDecoy(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{}) // cooldown 0
	if _, err := r.register("081234567890"); err != nil {
		t.Fatal(err)
	}
	owner := r.activate(t, "6281234567890")
	ip := netip.MustParseAddr("198.51.100.1")
	tk, err := r.uc.Register(bg, usecase.RegisterInput{Name: "Penyerang", Email: "evil@example.com", Phone: "+62 812 3456 7890", Password: "punyapenyerang", NIM: "EVIL00001", GroupID: r.group, ClientIP: &ip})
	if err != nil || tk.ResendAfter != time.Minute {
		t.Fatalf("respons harus sama dengan nomor baru: %+v %v", tk, err)
	}
	if _, err := r.otps.Get(bg, tk.ChallengeID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("nomor aktif tidak boleh mendapat challenge baru")
	}
	got, _ := r.users.Get(bg, owner.ID)
	if got.Name != "Budi Santoso" || got.Email != "budi@example.com" || testHasher.Compare(got.PasswordHash, "punyapenyerang") || !testHasher.Compare(got.PasswordHash, "rahasia123") {
		t.Fatalf("profil/password pemilik berubah: %+v", got)
	}
	if v, _ := r.uc.Status(bg, tk.ChallengeID); v.Status != domain.OTPQueued {
		t.Fatalf("status challenge palsu = %s", v.Status)
	}
}

// Pendaftaran ulang nomor yang belum diverifikasi hanya mengganti challenge: data penyerang tersimpan
// di challenge penyerang, tidak pernah di baris pengguna. Kode dikirim ke HP pemilik nomor, jadi
// penyerang tidak bisa menyelesaikannya.
func TestOTPRegisterPendingNumberReplacesChallengeOnly(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{})
	victim, err := r.register("081234567890")
	if err != nil {
		t.Fatal(err)
	}
	attacker, err := r.uc.Register(bg, usecase.RegisterInput{Name: "Penyerang", Email: "evil@example.com", Phone: "081234567890", Password: "punyapenyerang", NIM: "EVIL00001", GroupID: r.group})
	if err != nil {
		t.Fatal(err)
	}
	vc, _ := r.otps.Get(bg, victim.ChallengeID)
	ac, _ := r.otps.Get(bg, attacker.ChallengeID)
	if vc.Status != domain.OTPExpired || ac.Status != domain.OTPQueued || ac.RegName != "Penyerang" {
		t.Fatalf("korban=%s penyerang=%+v", vc.Status, ac)
	}
	u, _ := r.users.GetByPhone(bg, "6281234567890")
	if u.Name != "Budi Santoso" || u.PasswordHash != "" || u.Status != domain.UserPending {
		t.Fatalf("baris pengguna tidak boleh berisi data penyerang: %+v", u)
	}
}

func TestOTPForgotKnownAndUnknownLookTheSame(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute})
	if _, err := r.register("081234567890"); err != nil {
		t.Fatal(err)
	}
	r.activate(t, "6281234567890")
	if _, err := r.register("081277776666"); err != nil { // pending: diperlakukan seperti tak terdaftar
		t.Fatal(err)
	}
	r.clock.t = r.clock.t.Add(2 * time.Minute)
	known, errK := r.uc.Forgot(bg, "081234567890", nil)
	unknown, errU := r.uc.Forgot(bg, "081299998888", nil)
	pending, errP := r.uc.Forgot(bg, "081277776666", nil)
	if errK != nil || errU != nil || errP != nil {
		t.Fatal(errK, errU, errP)
	}
	if known.ResendAfter != unknown.ResendAfter || known.EstimatedWait == 0 || unknown.EstimatedWait == 0 {
		t.Fatalf("bentuk berbeda: %+v vs %+v", known, unknown)
	}
	kc, err := r.otps.Get(bg, known.ChallengeID)
	if err != nil || kc.Purpose != domain.OTPPurposeReset || kc.RegPasswordHash != "" {
		t.Fatalf("challenge reset: %+v %v", kc, err)
	}
	for _, id := range []uuid.UUID{unknown.ChallengeID, pending.ChallengeID} {
		if _, err := r.otps.Get(bg, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("nomor tak aktif tidak boleh mendapat kode reset: %v", err)
		}
	}
	if _, err := r.users.GetByPhone(bg, "6281299998888"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("lupa password tidak boleh membuat pengguna: %v", err)
	}
	// cooldown berlaku sama untuk keduanya
	_, errK = r.uc.Forgot(bg, "081234567890", nil)
	_, errU = r.uc.Forgot(bg, "081299998888", nil)
	dk, _ := domain.AsError(errK)
	du, _ := domain.AsError(errU)
	if dk == nil || du == nil || dk.Code != "RATE_LIMITED" || du.Code != "RATE_LIMITED" || dk.RetryAfter != du.RetryAfter {
		t.Fatalf("batas tidak seragam: %v / %v", errK, errU)
	}
}

func TestOTPResendCopiesRegistrationData(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute})
	first, err := r.register("081234567890")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.uc.Resend(bg, first.ChallengeID, nil); errCode(err) != "RATE_LIMITED" {
		t.Fatalf("kirim ulang tetap tunduk cooldown: %v", err)
	}
	r.clock.t = r.clock.t.Add(time.Minute)
	second, err := r.uc.Resend(bg, first.ChallengeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := r.otps.Get(bg, first.ChallengeID)
	b, _ := r.otps.Get(bg, second.ChallengeID)
	if a.Status != domain.OTPExpired || b.Purpose != domain.OTPPurposeRegister || b.RegName != a.RegName || b.RegPasswordHash != a.RegPasswordHash {
		t.Fatalf("lama=%+v baru=%+v", a, b)
	}
	if _, err := r.uc.Resend(bg, uuid.New(), nil); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("id tak dikenal: %v", err)
	}
	// kirim ulang untuk id palsu tetap palsu (dan tunduk batas yang sama)
	r.clock.t = r.clock.t.Add(time.Minute)
	d, _ := r.uc.Forgot(bg, "081299998888", nil)
	r.clock.t = r.clock.t.Add(time.Minute)
	d2, err := r.uc.Resend(bg, d.ChallengeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.otps.Get(bg, d2.ChallengeID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("kirim ulang challenge palsu tidak boleh membuat challenge asli")
	}
}

func TestOTPRequestValidationDoesNotConsumeQuota(t *testing.T) {
	r := newOTPReqRig(t, otpReqOpts{cooldown: time.Minute})
	cases := []struct {
		in   usecase.RegisterInput
		want string
	}{
		{usecase.RegisterInput{Name: "", Email: "a@b.co", Phone: "081234567890", Password: "rahasia123"}, "INVALID_NAME"},
		{usecase.RegisterInput{Name: "A", Email: "bukan-email", Phone: "081234567890", Password: "rahasia123"}, "INVALID_EMAIL"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "120363@g.us", Password: "rahasia123"}, "INVALID_PHONE"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "0212345678", Password: "rahasia123"}, "INVALID_PHONE"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890", Password: "pendek"}, "INVALID_PASSWORD"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890"}, "INVALID_PASSWORD"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890", Password: "rahasia123", GroupID: r.group}, "INVALID_NIM"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890", Password: "rahasia123", NIM: "251FS01017"}, "INVALID_GROUP"},
		{usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890", Password: "rahasia123", NIM: "251FS01017", GroupID: 999999}, "INVALID_GROUP"},
	}
	for _, tc := range cases {
		if _, err := r.uc.Register(bg, tc.in); errCode(err) != tc.want {
			t.Fatalf("%+v: err = %v, ingin %s", tc.in, err, tc.want)
		}
	}
	if _, err := r.uc.Forgot(bg, "+1 555 0100", nil); errCode(err) != "INVALID_PHONE" {
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
	if _, err := r.uc.Forgot(bg, "081234567890", nil); errCode(err) != "OTP_UNAVAILABLE" {
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
	uc := usecase.NewOTPRequest(r.users, postgres.NewGroupRepo(r.pool), repo, r.codes, testHasher, r.channel, limiter, r.clock, usecase.OTPRequestConfig{
		ResendCooldown: time.Minute, MaxQueue: 10, DailyGlobalCap: 10, SendInterval: time.Second, CodeTTL: 5 * time.Minute,
	})
	in := usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081234567890", Password: "rahasia123", NIM: "251FS01017", GroupID: r.group}
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
