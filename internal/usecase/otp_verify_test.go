package usecase_test

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// sentChallenge mendaftarkan nomor dan mengirim kodenya lewat stub; mengembalikan id dan kode.
func (r *dispatchRig) sentChallenge(t *testing.T, phone string) (uuid.UUID, string) {
	t.Helper()
	id := r.register(t, phone)
	r.once(t, usecase.DispatchSent)
	canon, _ := domain.NormalizePhone(phone)
	code := r.stub.LastCodeFor(canon)
	if code == "" {
		t.Fatal("kode tidak terkirim")
	}
	return id, code
}

func wrongCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func attemptsLeft(err error) any {
	if de, ok := domain.AsError(err); ok && de.Details != nil {
		return de.Details["attempts_left"]
	}
	return nil
}

func TestVerifyCorrectCodeLogsIn(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id, code := r.sentChallenge(t, "081234567890")
	ip := netipMust("203.0.113.5")
	res, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: code, ClientIP: &ip})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Status != domain.UserActive || res.User.PhoneVerifiedAt == nil || res.User.LastLoginAt == nil {
		t.Fatalf("user = %+v", res.User)
	}
	if !res.ExpiresAt.Equal(r.clock.t.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expires = %v", res.ExpiresAt)
	}
	// token tidak pernah disimpan polos: hanya SHA-256-nya
	var stored []byte
	if err := r.pool.QueryRow(bg, `SELECT token_hash FROM sessions WHERE user_id = $1`, res.User.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(res.Token))
	if !bytes.Equal(stored, h[:]) || bytes.Contains(stored, []byte(res.Token)) {
		t.Fatal("token_hash harus SHA-256 dari token")
	}
	var plain int
	_ = r.pool.QueryRow(bg, `SELECT count(*) FROM sessions WHERE encode(token_hash, 'escape') LIKE '%' || $1 || '%'`, res.Token).Scan(&plain)
	if plain != 0 {
		t.Fatal("token polos tersimpan")
	}
	if v := r.status(t, id); v.Status != domain.OTPConsumed {
		t.Fatalf("challenge = %s", v.Status)
	}
	// sekali pakai
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: code}); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("pakai ulang: %v", err)
	}
	// sesi berlaku
	s, err := r.sess.Authenticate(bg, res.Token)
	if err != nil || s.UserID != res.User.ID {
		t.Fatalf("authenticate: %v", err)
	}
	if u, err := r.sess.Me(bg, s.UserID); err != nil || u.Name != "Budi" {
		t.Fatalf("me: %+v %v", u, err)
	}
}

func TestVerifyWrongCodesExhaustChallenge(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id, code := r.sentChallenge(t, "081234567890")
	for i := 1; i <= 4; i++ {
		_, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: wrongCode(code)})
		if errCode(err) != "OTP_INVALID" || attemptsLeft(err) != 5-i {
			t.Fatalf("percobaan %d: %v left=%v", i, err, attemptsLeft(err))
		}
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: wrongCode(code)}); errCode(err) != "OTP_TOO_MANY_ATTEMPTS" {
		t.Fatalf("percobaan 5: %v", err)
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: code}); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("kode benar setelah dibatalkan harus ditolak: %v", err)
	}
	if v := r.status(t, id); v.Status != domain.OTPExpired {
		t.Fatalf("status = %s", v.Status)
	}
}

func TestVerifyStates(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	queued := r.register(t, "081200000001")
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: queued, Code: "123456"}); errCode(err) != "OTP_NOT_SENT" {
		t.Fatalf("queued: %v", err)
	}
	for _, bad := range []string{"", "12a456", " 123456", "1234567890123456789"} {
		if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: queued, Code: bad}); errCode(err) != "INVALID_CODE" {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	c, _ := r.otps.Get(bg, queued)
	if c.VerifyAttempts != 0 {
		t.Fatalf("input salah format tidak boleh dihitung: %d", c.VerifyAttempts)
	}

	r.once(t, usecase.DispatchSent)
	code := r.stub.LastCodeFor("6281200000001")
	r.clock.t = r.clock.t.Add(5 * time.Minute)
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: queued, Code: code}); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("kedaluwarsa: %v", err)
	}

	r.stub.FailNext(1)
	failed := r.register(t, "081200000002")
	r.disp = usecase.NewOTPDispatcher(r.otps, &hookSender{inner: failingSender{}}, r.codes, r.channel, usecase.NewPhoneLimiter(0, 5), r.clock,
		usecase.OTPDispatchConfig{CodeTTL: 5 * time.Minute, MessageTemplate: "{code}", StuckAfter: time.Minute})
	r.once(t, usecase.DispatchFailed)
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: failed, Code: "123456"}); errCode(err) != "OTP_NOT_SENT" {
		t.Fatalf("failed: %v", err)
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: uuid.New(), Code: "123456"}); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("tak dikenal: %v", err)
	}
}

func TestVerifyConcurrentCorrectCodeSucceedsOnce(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id, code := r.sentChallenge(t, "081234567890")
	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: code})
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errCode(err) == "OTP_EXPIRED", errCode(err) == "OTP_TOO_MANY_ATTEMPTS":
		default:
			t.Fatalf("error tak terduga: %v", err)
		}
	}
	var sessions int
	_ = r.pool.QueryRow(bg, `SELECT count(*) FROM sessions`).Scan(&sessions)
	if ok != 1 || sessions != 1 {
		t.Fatalf("berhasil = %d, sesi = %d; ingin tepat 1", ok, sessions)
	}
}

func TestVerifyConcurrentGuessesCannotExceedLimit(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id, code := r.sentChallenge(t, "081234567890")
	const n = 30
	var wg sync.WaitGroup
	invalid := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: wrongCode(code)}); errCode(err) == "OTP_INVALID" {
				invalid <- struct{}{}
			}
		}()
	}
	wg.Wait()
	if len(invalid) > 4 {
		t.Fatalf("%d tebakan dinilai (OTP_INVALID); maksimal 4 sebelum batal", len(invalid))
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: code}); err == nil {
		t.Fatal("setelah batas tercapai kode benar tidak boleh diterima")
	}
}

func TestVerifyDecoyMimicsRealChallenge(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	tk, err := r.req.Request(bg, "081299998888", nil) // tidak terdaftar
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: tk.ChallengeID, Code: "123456"}); errCode(err) != "OTP_NOT_SENT" {
		t.Fatalf("sebelum terkirim: %v", err)
	}
	r.clock.t = r.clock.t.Add(tk.EstimatedWait)
	for i := 1; i <= 4; i++ {
		_, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: tk.ChallengeID, Code: "123456"})
		if errCode(err) != "OTP_INVALID" || attemptsLeft(err) != 5-i {
			t.Fatalf("tebakan %d: %v", i, err)
		}
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: tk.ChallengeID, Code: "123456"}); errCode(err) != "OTP_TOO_MANY_ATTEMPTS" {
		t.Fatalf("tebakan 5: %v", err)
	}
	if v := r.status(t, tk.ChallengeID); v.Status != domain.OTPExpired {
		t.Fatalf("status palsu setelah batas = %s", v.Status)
	}
	if _, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: tk.ChallengeID, Code: "123456"}); errCode(err) != "OTP_EXPIRED" {
		t.Fatalf("setelah batas: %v", err)
	}
}

func TestSessionsRejectBadExpiredAndRevoked(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id, code := r.sentChallenge(t, "081234567890")
	res, err := r.verify.Verify(bg, usecase.VerifyInput{ChallengeID: id, Code: code})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"", "abc", res.Token + "x", res.Token[:42] + "A", "bukan token sama sekali!!"} {
		if _, err := r.sess.Authenticate(bg, tok); errCode(err) != "UNAUTHORIZED" {
			t.Fatalf("%q: %v", tok, err)
		}
	}
	s, err := r.sess.Authenticate(bg, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	saved := r.clock.t
	r.clock.t = res.ExpiresAt
	if _, err := r.sess.Authenticate(bg, res.Token); errCode(err) != "UNAUTHORIZED" {
		t.Fatalf("kedaluwarsa: %v", err)
	}
	r.clock.t = saved
	if err := r.sess.Logout(bg, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.sess.Logout(bg, s.ID); err != nil {
		t.Fatalf("logout idempoten: %v", err)
	}
	if _, err := r.sess.Authenticate(bg, res.Token); errCode(err) != "UNAUTHORIZED" {
		t.Fatalf("setelah logout: %v", err)
	}
}
