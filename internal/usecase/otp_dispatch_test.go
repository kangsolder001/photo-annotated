package usecase_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/adapter/wanotif"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/testutil/wastub"
	"photo-annotated/internal/usecase"
)

const stubToken = "stub-token"

type dispatchRig struct {
	pool    *pgxpool.Pool
	otps    *postgres.OTPRepo
	verify  *usecase.OTPVerify
	sess    *usecase.Sessions
	users   *postgres.UserRepo
	stub    *wastub.Server
	codes   *signer.OTPCodes
	channel *usecase.Channel
	clock   *fakeClock
	req     *usecase.OTPRequest
	disp    *usecase.OTPDispatcher
}

// hookSender menjalankan before sesaat sebelum pengiriman sungguhan (untuk menyisipkan balapan).
type hookSender struct {
	inner  usecase.OTPSender
	before func()
}

func (h *hookSender) Send(ctx context.Context, phone, msg string) (string, error) {
	if h.before != nil {
		h.before()
	}
	return h.inner.Send(ctx, phone, msg)
}

func newDispatchRig(t *testing.T, token string) (*dispatchRig, *hookSender) {
	t.Helper()
	pool := pgtest.Pool(t)
	stub := wastub.NewServer(t, wastub.Options{Token: stubToken})
	wa, err := wanotif.New(stub.URL, token, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	codes, _ := signer.NewOTPCodes("pepper-0123456789abcdef0123456789abcdef", 6)
	r := &dispatchRig{
		pool: pool, otps: postgres.NewOTPRepo(pool), users: postgres.NewUserRepo(pool), stub: stub, codes: codes,
		channel: usecase.NewChannel(3), clock: &fakeClock{t: time.Now().UTC().Truncate(time.Microsecond)},
	}
	phones := usecase.NewPhoneLimiterWithClock(time.Minute, 5, func() time.Time { return r.clock.t })
	hs := &hookSender{inner: wa}
	r.disp = usecase.NewOTPDispatcher(r.otps, hs, codes, r.channel, phones, r.clock, usecase.OTPDispatchConfig{
		CodeTTL: 5 * time.Minute, MessageTemplate: "*{code}* kode Anda, berlaku {ttl_minutes} menit.",
		RetryDelays: []time.Duration{3 * time.Second, 10 * time.Second}, StuckAfter: 30 * time.Second,
	})
	r.req = usecase.NewOTPRequest(r.users, r.otps, codes, r.channel, phones, r.clock, usecase.OTPRequestConfig{
		ResendCooldown: time.Minute, MaxQueue: 200, DailyGlobalCap: 500, SendInterval: 1500 * time.Millisecond,
		CodeTTL: 5 * time.Minute, OnQueued: r.disp.Wake,
	})
	sessions := postgres.NewSessionRepo(pool)
	r.verify = usecase.NewOTPVerify(r.req, r.users, r.otps, sessions, postgres.NewTxManager(pool), codes, r.clock,
		usecase.OTPVerifyConfig{MaxAttempts: 5, SessionTTL: 30 * 24 * time.Hour})
	r.sess = usecase.NewSessions(sessions, r.users, r.clock)
	return r, hs
}

func (r *dispatchRig) register(t *testing.T, phone string) uuid.UUID {
	t.Helper()
	tk, err := r.req.Register(bg, usecase.RegisterInput{Name: "Budi", Email: "b@example.com", Phone: phone})
	if err != nil {
		t.Fatal(err)
	}
	return tk.ChallengeID
}

func (r *dispatchRig) once(t *testing.T, want usecase.DispatchOutcome) usecase.DispatchResult {
	t.Helper()
	res, err := r.disp.DispatchOnce(bg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != want {
		t.Fatalf("outcome = %s (reason %q, cause %v), ingin %s", res.Outcome, res.Reason, res.Cause, want)
	}
	return res
}

func (r *dispatchRig) status(t *testing.T, id uuid.UUID) *usecase.OTPStatusView {
	t.Helper()
	v, err := r.req.Status(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDispatchSendsDerivedCode(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	r.once(t, usecase.DispatchIdle)
	id := r.register(t, "081234567890")
	if v := r.status(t, id); v.Status != domain.OTPQueued || v.ResendAfter != time.Minute {
		t.Fatalf("status awal = %+v", v)
	}
	res := r.once(t, usecase.DispatchSent)
	if res.Phone != "62812****7890" || res.Attempt != 1 {
		t.Fatalf("hasil = %+v", res)
	}
	msgs := r.stub.Messages()
	if len(msgs) != 1 || msgs[0].To != "6281234567890" {
		t.Fatalf("pesan = %+v", msgs)
	}
	if got := wastub.CodeFrom(msgs[0].Text); got != r.codes.Code(id) || !strings.Contains(msgs[0].Text, "berlaku 5 menit") {
		t.Fatalf("isi pesan tidak sesuai: %q", msgs[0].Text)
	}
	c, _ := r.otps.Get(bg, id)
	if c.Status != domain.OTPSent || c.SentAt == nil || !c.ExpiresAt.Equal(c.SentAt.Add(5*time.Minute)) || c.WAMessageID == "" {
		t.Fatalf("challenge = %+v", c)
	}
	v := r.status(t, id)
	if v.Status != domain.OTPSent || v.ExpiresAt == nil {
		t.Fatalf("status = %+v", v)
	}
	r.clock.t = c.ExpiresAt.Add(time.Second)
	if v := r.status(t, id); v.Status != domain.OTPExpired || v.ExpiresAt != nil {
		t.Fatalf("setelah kedaluwarsa = %+v", v)
	}
}

func TestDispatchRetriesThenSends(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id := r.register(t, "081234567890")
	r.stub.FailNext(1)
	res := r.once(t, usecase.DispatchRetry)
	if res.Reason != domain.OTPFailSendFailed {
		t.Fatalf("reason = %q", res.Reason)
	}
	if v := r.status(t, id); v.Status != domain.OTPQueued {
		t.Fatalf("saat menunggu retry status = %s", v.Status)
	}
	r.once(t, usecase.DispatchIdle) // belum waktunya (3 detik)
	r.clock.t = r.clock.t.Add(3 * time.Second)
	if res := r.once(t, usecase.DispatchSent); res.Attempt != 2 {
		t.Fatalf("attempt = %d", res.Attempt)
	}
}

func TestDispatchPermanentFailureVisibleAndRefunds(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id := r.register(t, "081234567890")
	r.stub.FailNext(3)
	start := r.clock.t
	r.once(t, usecase.DispatchRetry)
	r.clock.t = r.clock.t.Add(3 * time.Second)
	r.once(t, usecase.DispatchRetry)
	r.clock.t = r.clock.t.Add(10 * time.Second)
	res := r.once(t, usecase.DispatchFailed)
	if res.Reason != domain.OTPFailSendFailed || res.Attempt != 3 {
		t.Fatalf("hasil = %+v", res)
	}
	if took := r.clock.t.Sub(start); took > 25*time.Second {
		t.Fatalf("kegagalan baru terlihat setelah %v", took)
	}
	v := r.status(t, id)
	if v.Status != domain.OTPFailed || v.FailureReason != domain.OTPFailSendFailed || v.ResendAfter != 0 {
		t.Fatalf("status = %+v", v)
	}
	if !r.channel.Available() {
		t.Fatal("SEND_FAILED berulang untuk satu nomor tidak boleh menutup kanal untuk semua orang")
	}
	// jatah dikembalikan: kirim ulang langsung boleh walau masih dalam cooldown 60 detik
	if _, err := r.req.Request(bg, "081234567890", nil); err != nil {
		t.Fatalf("kirim ulang setelah failed harus boleh: %v", err)
	}
}

func TestDispatchDisconnectedMarksChannelUnavailable(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	r.stub.SetDisconnected(true)
	for i, p := range []string{"081200000001", "081200000002", "081200000003"} {
		r.register(t, p)
		res := r.once(t, usecase.DispatchRetry)
		if res.Reason != domain.OTPFailWAUnavailable {
			t.Fatalf("reason = %q", res.Reason)
		}
		if i == 2 && (!res.ChannelChanged || res.Available) {
			t.Fatalf("kegagalan ke-3 harus menutup kanal: %+v", res)
		}
	}
	if _, err := r.req.Register(bg, usecase.RegisterInput{Name: "A", Email: "a@b.co", Phone: "081200000004"}); errCode(err) != "OTP_UNAVAILABLE" {
		t.Fatalf("err = %v", err)
	}
	r.stub.SetDisconnected(false)
	r.channel.RecordProbe(nil)
	r.clock.t = r.clock.t.Add(3 * time.Second)
	for i := 0; i < 3; i++ {
		r.once(t, usecase.DispatchSent)
	}
}

func TestDispatchRejectedTokenFailsWithoutRetryAndAlerts(t *testing.T) {
	r, _ := newDispatchRig(t, "salah")
	id := r.register(t, "081234567890")
	res := r.once(t, usecase.DispatchFailed)
	if !res.Alert || res.Reason != domain.OTPFailWAUnavailable {
		t.Fatalf("hasil = %+v", res)
	}
	if r.stub.AuthFailures() != 1 {
		t.Fatalf("auth failures = %d", r.stub.AuthFailures())
	}
	if v := r.status(t, id); v.Status != domain.OTPFailed {
		t.Fatalf("status = %s", v.Status)
	}
}

func TestDispatchSupersededWhileSending(t *testing.T) {
	r, hs := newDispatchRig(t, stubToken)
	first := r.register(t, "081234567890")
	var second uuid.UUID
	hs.before = func() {
		hs.before = nil
		r.clock.t = r.clock.t.Add(time.Minute) // lewat cooldown
		second = r.register(t, "081234567890")
	}
	r.once(t, usecase.DispatchSuperseded)
	if v := r.status(t, first); v.Status != domain.OTPExpired {
		t.Fatalf("challenge lama = %s", v.Status)
	}
	if v := r.status(t, second); v.Status != domain.OTPQueued {
		t.Fatalf("challenge baru = %s", v.Status)
	}
	r.once(t, usecase.DispatchSent)
	if got := r.stub.LastCodeFor("6281234567890"); got != r.codes.Code(second) {
		t.Fatal("pesan terakhir harus memuat kode challenge baru")
	}
}

func TestDispatchRequeuesStuckWithSameCode(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id := r.register(t, "081234567890")
	if _, err := r.otps.ClaimNext(bg, r.clock.t); err != nil { // worker mati setelah klaim
		t.Fatal(err)
	}
	if n, _ := r.disp.RequeueStuck(bg); n != 0 {
		t.Fatal("belum melewati StuckAfter")
	}
	r.clock.t = r.clock.t.Add(31 * time.Second)
	if n, err := r.disp.RequeueStuck(bg); err != nil || n != 1 {
		t.Fatalf("requeue = %d %v", n, err)
	}
	if res := r.once(t, usecase.DispatchSent); res.Attempt != 2 {
		t.Fatalf("attempt = %d", res.Attempt)
	}
	if wastub.CodeFrom(r.stub.Messages()[0].Text) != r.codes.Code(id) {
		t.Fatal("kode harus tetap sama setelah diantrekan ulang")
	}
}

func TestDispatchStuckOnLastAttemptFailsWithoutSending(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id := r.register(t, "081234567890")
	for i := 0; i < 3; i++ { // tiga klaim tanpa pernah selesai
		if _, err := r.otps.ClaimNext(bg, r.clock.t); err != nil {
			t.Fatal(err)
		}
		r.clock.t = r.clock.t.Add(31 * time.Second)
		if _, err := r.disp.RequeueStuck(bg); err != nil {
			t.Fatal(err)
		}
	}
	r.once(t, usecase.DispatchFailed)
	if r.stub.SendCalls() != 0 {
		t.Fatal("tidak boleh mengirim melebihi batas percobaan")
	}
	if v := r.status(t, id); v.Status != domain.OTPFailed || v.FailureReason != domain.OTPFailWAUnavailable {
		t.Fatalf("status = %+v", v)
	}
}

func TestDispatchShutdownRequeues(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	id := r.register(t, "081234567890")
	r.stub.SetHang(true)
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	res, err := r.disp.DispatchOnce(ctx)
	if err != nil || res.Outcome != usecase.DispatchAborted {
		t.Fatalf("hasil = %+v err %v", res, err)
	}
	c, _ := r.otps.Get(bg, id)
	if c.Status != domain.OTPQueued {
		t.Fatalf("status = %s", c.Status)
	}
	if r.channel.State().FailStreak != 0 {
		t.Fatal("shutdown bukan kegagalan kanal")
	}
}

func TestOTPStatusDecoyMimicsRealChallenge(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	tk, err := r.req.Request(bg, "081299998888", nil) // tidak terdaftar
	if err != nil {
		t.Fatal(err)
	}
	v := r.status(t, tk.ChallengeID)
	if v.Status != domain.OTPQueued || v.ResendAfter != time.Minute {
		t.Fatalf("awal = %+v", v)
	}
	r.clock.t = r.clock.t.Add(tk.EstimatedWait)
	v = r.status(t, tk.ChallengeID)
	if v.Status != domain.OTPSent || v.ExpiresAt == nil || !v.ExpiresAt.Equal(r.clock.t.Add(5*time.Minute)) {
		t.Fatalf("setelah perkiraan kirim = %+v", v)
	}
	r.clock.t = r.clock.t.Add(5 * time.Minute)
	if v := r.status(t, tk.ChallengeID); v.Status != domain.OTPExpired {
		t.Fatalf("setelah TTL = %+v", v)
	}
	if v := r.status(t, uuid.New()); v.Status != domain.OTPQueued || v.FailureReason != "" {
		t.Fatalf("id tak dikenal = %+v", v)
	}
	if r.stub.SendCalls() != 0 {
		t.Fatal("nomor tak terdaftar tidak boleh dikirimi pesan")
	}
}

func TestDispatcherRunWakesOnQueued(t *testing.T) {
	r, _ := newDispatchRig(t, stubToken)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.disp.Run(ctx, time.Hour, func(usecase.DispatchResult, error) {}) // idle sangat lama: hanya Wake yang membangunkan
	}()
	time.Sleep(50 * time.Millisecond) // putaran pertama (antrean kosong) selesai
	r.register(t, "081234567890")
	deadline := time.Now().Add(3 * time.Second)
	for len(r.stub.Messages()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(r.stub.Messages()) != 1 {
		t.Fatal("dispatcher tidak terbangun oleh challenge baru")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run tidak berhenti saat ctx dibatalkan")
	}
}

// failingSender selalu gagal permanen.
type failingSender struct{}

func (failingSender) Send(context.Context, string, string) (string, error) {
	return "", &usecase.SendError{Reason: domain.OTPFailSendFailed, Err: errors.New("rejected")}
}

func netipMust(s string) netip.Addr { return netip.MustParseAddr(s) }
