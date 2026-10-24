package usecase

import (
	"time"

	"photo-annotated/internal/ratelimit"
)

// PhoneLimiter membatasi permintaan OTP per nomor: jeda minimum antar permintaan (cooldown) dan
// batas per jam. Sengaja dipegang di memori dan berlaku SAMA untuk nomor terdaftar maupun tidak:
// batas yang dihitung dari tabel OTP hanya ada untuk nomor terdaftar, sehingga respons 429 akan
// membocorkan nomor mana yang terdaftar. State hilang saat restart (satu instance API).
type PhoneLimiter struct {
	cooldown *ratelimit.Limiter
	hourly   *ratelimit.Limiter
}

// NewPhoneLimiter membuat limiter dengan satu permintaan per cooldown dan maksimal perHour per jam.
func NewPhoneLimiter(cooldown time.Duration, perHour int) *PhoneLimiter {
	return NewPhoneLimiterWithClock(cooldown, perHour, time.Now)
}

// NewPhoneLimiterWithClock seperti NewPhoneLimiter dengan jam yang dapat diatur (untuk test).
func NewPhoneLimiterWithClock(cooldown time.Duration, perHour int, now func() time.Time) *PhoneLimiter {
	const maxPhones = 100_000
	return &PhoneLimiter{
		cooldown: ratelimit.NewWithOptions(1, cooldown, maxPhones, now),
		hourly:   ratelimit.NewWithOptions(perHour, time.Hour, maxPhones, now),
	}
}

// Allow mencatat satu permintaan untuk nomor kanonik phone. Bila ditolak, mengembalikan berapa lama
// harus menunggu. Penolakan tidak menghabiskan jatah apa pun.
func (p *PhoneLimiter) Allow(phone string) (ok bool, retryAfter time.Duration) {
	if ok, wait := p.cooldown.Allow(phone); !ok {
		return false, wait
	}
	if ok, wait := p.hourly.Allow(phone); !ok {
		p.cooldown.Undo(phone) // ditolak karena batas per jam: jangan sekaligus menahan cooldown
		return false, wait
	}
	return true, 0
}

// Refund mengembalikan jatah terbaru nomor itu, dipanggil saat pengiriman gagal permanen sehingga
// pengguna boleh mencoba lagi segera dan kegagalan kita tidak menghabiskan batas per jam miliknya.
func (p *PhoneLimiter) Refund(phone string) {
	p.cooldown.Undo(phone)
	p.hourly.Undo(phone)
}
