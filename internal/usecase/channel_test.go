package usecase_test

import (
	"errors"
	"sync"
	"testing"

	"photo-annotated/internal/usecase"
)

func TestChannelStartsAvailable(t *testing.T) {
	c := usecase.NewChannel(3)
	if !c.Available() {
		t.Fatal("kanal harus dianggap tersedia sampai ada bukti sebaliknya")
	}
	if s := c.State(); !s.Available || !s.ProbeOK || s.FailStreak != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestChannelBecomesUnavailableAfterThresholdConsecutiveSendFailures(t *testing.T) {
	c := usecase.NewChannel(3)
	for i := 1; i <= 2; i++ {
		if avail, changed := c.RecordSend(false); !avail || changed {
			t.Fatalf("kegagalan ke-%d belum boleh mengubah status: avail=%v changed=%v", i, avail, changed)
		}
	}
	avail, changed := c.RecordSend(false)
	if avail || !changed || c.Available() {
		t.Fatalf("kegagalan ke-3 harus menonaktifkan: avail=%v changed=%v", avail, changed)
	}
	// kegagalan lebih lanjut tidak mengubah status lagi
	if _, changed := c.RecordSend(false); changed {
		t.Fatal("tidak ada perubahan lagi")
	}
}

func TestChannelSuccessfulSendResetsStreak(t *testing.T) {
	c := usecase.NewChannel(3)
	c.RecordSend(false)
	c.RecordSend(false)
	c.RecordSend(true) // berturut-turut diputus
	c.RecordSend(false)
	c.RecordSend(false)
	if !c.Available() {
		t.Fatal("2 gagal setelah 1 sukses belum mencapai ambang")
	}
	avail, changed := c.RecordSend(true)
	if !avail || changed || c.State().FailStreak != 0 {
		t.Fatalf("%v %v %+v", avail, changed, c.State())
	}
}

func TestChannelProbeFailureMakesItUnavailableAndSuccessRecoversEvenAfterSendFailures(t *testing.T) {
	c := usecase.NewChannel(3)
	if avail, changed := c.RecordProbe(errors.New("terputus")); avail || !changed {
		t.Fatalf("probe gagal: avail=%v changed=%v", avail, changed)
	}
	if avail, changed := c.RecordProbe(errors.New("masih terputus")); avail || changed {
		t.Fatalf("probe gagal berulang tidak mengubah status: avail=%v changed=%v", avail, changed)
	}
	if avail, changed := c.RecordProbe(nil); !avail || !changed {
		t.Fatalf("probe sukses harus memulihkan: avail=%v changed=%v", avail, changed)
	}
	// dinonaktifkan oleh pengiriman → hanya probe sukses yang bisa memulihkan (tak ada pengiriman baru
	// saat kanal dinyatakan tidak tersedia, jadi tanpa ini kanal terkunci selamanya)
	for i := 0; i < 3; i++ {
		c.RecordSend(false)
	}
	if c.Available() {
		t.Fatal("harus tidak tersedia")
	}
	avail, changed := c.RecordProbe(nil)
	if !avail || !changed || c.State().FailStreak != 0 {
		t.Fatalf("probe sukses harus mereset hitungan gagal: %v %v %+v", avail, changed, c.State())
	}
}

func TestChannelProbeDownOverridesSendSuccess(t *testing.T) {
	c := usecase.NewChannel(3)
	c.RecordProbe(errors.New("terputus"))
	if avail, _ := c.RecordSend(true); avail {
		t.Fatal("pengiriman sukses tidak boleh menutupi probe yang menunjukkan terputus")
	}
}

func TestChannelConcurrentUse(t *testing.T) {
	c := usecase.NewChannel(5)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.RecordSend(i%7 != 0)
			c.RecordProbe(nil)
			_ = c.Available()
			_ = c.State()
		}()
	}
	wg.Wait()
}

func TestNewChannelPanicsOnInvalidThreshold(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ambang 0 harus panic")
		}
	}()
	usecase.NewChannel(0)
}
