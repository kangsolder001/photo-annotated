package usecase

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// OTPDispatchConfig adalah parameter pengiriman OTP (spec §4.0 langkah 3, §9.5).
type OTPDispatchConfig struct {
	CodeTTL         time.Duration // expires_at = sent_at + CodeTTL
	MessageTemplate string        // memuat {code}; {ttl_minutes} opsional
	// AdminMessageTemplate: pesan untuk challenge login admin (memuat {code}); kosong = MessageTemplate.
	AdminMessageTemplate string
	RetryDelays          []time.Duration // jeda sebelum percobaan ke-2, ke-3, …; total percobaan = len+1
	// StuckAfter: challenge sending yang diklaim lebih lama dari ini dianggap ditinggal worker yang
	// mati, lalu diantrekan ulang. Harus melebihi timeout satu pengiriman.
	StuckAfter time.Duration
}

// DispatchOutcome adalah hasil satu putaran dispatcher.
type DispatchOutcome string

const (
	DispatchIdle       DispatchOutcome = "idle"       // antrean kosong
	DispatchSent       DispatchOutcome = "sent"       // terkirim
	DispatchRetry      DispatchOutcome = "retry"      // gagal sementara, dijadwalkan ulang
	DispatchFailed     DispatchOutcome = "failed"     // gagal permanen; user diberi tahu lewat status
	DispatchSuperseded DispatchOutcome = "superseded" // challenge diganti permintaan baru saat sedang dikirim
	DispatchAborted    DispatchOutcome = "aborted"    // shutdown di tengah kirim; dikembalikan ke antrean
	DispatchRequeued   DispatchOutcome = "requeued"   // challenge yang ditinggal worker mati diantrekan ulang
)

// DispatchResult menjelaskan satu putaran untuk log dan metrik. Phone sudah disamarkan.
type DispatchResult struct {
	Outcome     DispatchOutcome
	ChallengeID uuid.UUID
	Phone       string
	Attempt     int
	Reason      string // failure_reason bila gagal
	Requeued    int    // jumlah challenge yang diantrekan ulang (DispatchRequeued)
	Alert       bool   // perlu perhatian operator (salah konfigurasi)
	Cause       error  // penyebab kegagalan kirim (tanpa isi pesan/kode/token)
	// ChannelChanged true bila ketersediaan kanal berubah karena hasil ini; Available nilai barunya.
	ChannelChanged bool
	Available      bool
}

// OTPDispatcher mengambil challenge queued satu per satu dan mengirimkannya lewat OTPSender.
// Hanya satu goroutine yang menjalankannya per proses; klaim memakai FOR UPDATE SKIP LOCKED
// sehingga dua proses pun tidak mengirim challenge yang sama berbarengan.
type OTPDispatcher struct {
	otps    OTPRepo
	sender  OTPSender
	codes   OTPCodes
	channel *Channel
	phones  *PhoneLimiter
	clock   Clock
	cfg     OTPDispatchConfig
	wake    chan struct{}
}

func NewOTPDispatcher(otps OTPRepo, sender OTPSender, codes OTPCodes, channel *Channel, phones *PhoneLimiter, clock Clock, cfg OTPDispatchConfig) *OTPDispatcher {
	if cfg.CodeTTL <= 0 || cfg.StuckAfter <= 0 || !strings.Contains(cfg.MessageTemplate, "{code}") ||
		(cfg.AdminMessageTemplate != "" && !strings.Contains(cfg.AdminMessageTemplate, "{code}")) {
		panic("usecase: OTPDispatchConfig tidak valid")
	}
	return &OTPDispatcher{otps: otps, sender: sender, codes: codes, channel: channel, phones: phones, clock: clock, cfg: cfg,
		wake: make(chan struct{}, 1)}
}

// Wake membangunkan Run lebih awal (dipanggil setelah challenge baru diantrekan). Tidak memblokir.
func (d *OTPDispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *OTPDispatcher) maxAttempts() int { return len(d.cfg.RetryDelays) + 1 }

// message menyusun isi pesan; login admin memakai template sendiri agar superadmin tahu itu kode admin.
func (d *OTPDispatcher) message(code string, purpose domain.OTPPurpose) string {
	tmpl := d.cfg.MessageTemplate
	if purpose == domain.OTPPurposeAdminLogin && d.cfg.AdminMessageTemplate != "" {
		tmpl = d.cfg.AdminMessageTemplate
	}
	minutes := int(d.cfg.CodeTTL.Round(time.Minute) / time.Minute)
	return strings.NewReplacer("{code}", code, "{ttl_minutes}", strconv.Itoa(max(minutes, 1))).Replace(tmpl)
}

// RequeueStuck mengembalikan challenge yang ditinggal worker mati ke antrean. Pesannya mungkin sudah
// sempat terkirim; mengirim ulang aman karena kode diturunkan dari challenge ID (kodenya sama).
func (d *OTPDispatcher) RequeueStuck(ctx context.Context) (int, error) {
	now := d.clock.Now()
	return d.otps.RequeueStuck(ctx, now.Add(-d.cfg.StuckAfter), now)
}

// DispatchOnce memproses paling banyak satu challenge.
func (d *OTPDispatcher) DispatchOnce(ctx context.Context) (DispatchResult, error) {
	c, err := d.otps.ClaimNext(ctx, d.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return DispatchResult{Outcome: DispatchIdle}, nil
	}
	if err != nil {
		return DispatchResult{}, err
	}
	res := DispatchResult{ChallengeID: c.ID, Phone: domain.MaskPhone(c.Phone), Attempt: c.SendAttempts}
	if c.SendAttempts > d.maxAttempts() {
		// diantrekan ulang setelah worker mati pada percobaan terakhir: jangan kirim lagi
		return d.fail(ctx, c, res, domain.OTPFailWAUnavailable, nil)
	}

	msgID, sendErr := d.sender.Send(ctx, c.Phone, d.message(d.codes.Code(c.ID), c.Purpose))
	if sendErr != nil && ctx.Err() != nil {
		// shutdown: kembalikan ke antrean dengan konteks baru agar proses berikutnya melanjutkan
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		res.Outcome = DispatchAborted
		return res, d.otps.Requeue(rctx, c.ID, d.clock.Now())
	}
	if sendErr == nil {
		res.Available, res.ChannelChanged = d.channel.RecordSend(true)
		now := d.clock.Now()
		res.Outcome = DispatchSent
		if err := d.otps.MarkSent(ctx, c.ID, msgID, now, now.Add(d.cfg.CodeTTL)); err != nil {
			return d.superseded(res, err)
		}
		return res, nil
	}

	var se *SendError
	if !errors.As(sendErr, &se) {
		se = &SendError{Reason: domain.OTPFailWAUnavailable, Retryable: true, Err: sendErr}
	}
	// Hanya kegagalan gateway yang menandakan kanal bermasalah. SEND_FAILED bisa khusus satu nomor
	// (mis. tanpa WhatsApp); bila ikut dihitung, satu nomor buruk yang dicoba 3× menutup kanal untuk
	// semua orang.
	if se.Reason == domain.OTPFailWAUnavailable {
		res.Available, res.ChannelChanged = d.channel.RecordSend(false)
	}
	res.Cause, res.Alert = se, se.Alert
	if se.Retryable && c.SendAttempts < d.maxAttempts() {
		res.Outcome, res.Reason = DispatchRetry, se.Reason
		if err := d.otps.Requeue(ctx, c.ID, d.clock.Now().Add(d.cfg.RetryDelays[c.SendAttempts-1])); err != nil {
			return d.superseded(res, err)
		}
		return res, nil
	}
	return d.fail(ctx, c, res, se.Reason, se)
}

// fail menandai challenge gagal dan mengembalikan jatah nomor itu, sehingga user boleh langsung
// meminta kode lagi (cooldown tidak berlaku setelah failed).
func (d *OTPDispatcher) fail(ctx context.Context, c *domain.OTPChallenge, res DispatchResult, reason string, cause *SendError) (DispatchResult, error) {
	res.Outcome, res.Reason = DispatchFailed, reason
	if cause != nil {
		res.Cause = cause
	}
	if err := d.otps.MarkFailed(ctx, c.ID, reason); err != nil {
		return d.superseded(res, err)
	}
	d.phones.Refund(c.Phone)
	return res, nil
}

// superseded: transisi status ditolak karena challenge sudah diganti permintaan baru (expired).
func (d *OTPDispatcher) superseded(res DispatchResult, err error) (DispatchResult, error) {
	var de *domain.Error
	if errors.As(err, &de) && de.Code == "OTP_STATE_CHANGED" {
		res.Outcome = DispatchSuperseded
		return res, nil
	}
	return res, err
}

// Run menjalankan dispatcher sampai ctx dibatalkan: memproses antrean selama ada isinya, lalu tidur
// sampai Wake atau idle berlalu (idle juga menentukan seberapa tepat jeda retry 3 s/10 s ditepati). Challenge yang tertinggal diantrekan ulang setiap StuckAfter.
// report dipanggil untuk setiap putaran yang memproses challenge atau gagal.
func (d *OTPDispatcher) Run(ctx context.Context, idle time.Duration, report func(DispatchResult, error)) {
	lastRequeue := time.Time{}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-d.wake:
		}
		if now := d.clock.Now(); now.Sub(lastRequeue) >= d.cfg.StuckAfter {
			if n, err := d.RequeueStuck(ctx); err != nil || n > 0 {
				report(DispatchResult{Outcome: DispatchRequeued, Requeued: n}, err)
			}
			lastRequeue = now
		}
		for ctx.Err() == nil {
			res, err := d.DispatchOnce(ctx)
			if err != nil || res.Outcome != DispatchIdle {
				report(res, err)
			}
			if err != nil {
				break // mis. DB putus: coba lagi setelah idle
			}
			if res.Outcome == DispatchIdle {
				break
			}
		}
		timer.Reset(idle) // sejak Go 1.23 Reset aman tanpa mengosongkan kanal timer
	}
}
