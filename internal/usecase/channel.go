package usecase

import "sync"

// Channel melacak apakah kanal OTP (WhatsApp) layak dipakai, agar FE bisa memberi tahu pengguna
// sebelum mereka menunggu kode yang tidak akan datang (spec §9.5):
//
//	tersedia = probe terakhir menunjukkan tersambung  DAN  kegagalan kirim berturut-turut < ambang
//
// Probe yang berhasil juga mereset hitungan kegagalan. Itu disengaja: saat tidak tersedia, tidak ada
// request baru yang diantrekan, sehingga tanpa probe tidak akan pernah ada pengiriman yang bisa
// memulihkan status. Satu probe sukses mengizinkan beberapa pengiriman percobaan.
type Channel struct {
	mu         sync.Mutex
	threshold  int
	probeOK    bool
	failStreak int
}

// NewChannel membuat pelacak; kanal dianggap tersedia sampai ada bukti sebaliknya.
func NewChannel(failureThreshold int) *Channel {
	if failureThreshold < 1 {
		panic("usecase: ambang kegagalan kanal harus >= 1")
	}
	return &Channel{threshold: failureThreshold, probeOK: true}
}

func (c *Channel) availableLocked() bool { return c.probeOK && c.failStreak < c.threshold }

func (c *Channel) Available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.availableLocked()
}

// ChannelState adalah potret status kanal (untuk log dan metrik).
type ChannelState struct {
	Available  bool
	ProbeOK    bool
	FailStreak int
}

func (c *Channel) State() ChannelState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ChannelState{Available: c.availableLocked(), ProbeOK: c.probeOK, FailStreak: c.failStreak}
}

// RecordProbe mencatat hasil probe. changed true bila ketersediaan berubah (untuk mencatat transisi).
func (c *Channel) RecordProbe(err error) (available, changed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before := c.availableLocked()
	c.probeOK = err == nil
	if err == nil {
		c.failStreak = 0
	}
	after := c.availableLocked()
	return after, before != after
}

// RecordSend mencatat hasil satu pengiriman.
func (c *Channel) RecordSend(ok bool) (available, changed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before := c.availableLocked()
	if ok {
		c.failStreak = 0
	} else {
		c.failStreak++
	}
	after := c.availableLocked()
	return after, before != after
}
