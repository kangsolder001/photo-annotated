package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type fakePing struct{ err error }

func (f fakePing) Ping(context.Context) error { return f.err }

type fakeStatsRepo struct {
	err    error
	counts usecase.StatsCounts
	data   usecase.DataCounts
}

func (f fakeStatsRepo) Counts(context.Context, time.Time) (usecase.StatsCounts, error) {
	return f.counts, f.err
}
func (f fakeStatsRepo) DataCounts(context.Context, time.Time) (usecase.DataCounts, error) {
	return f.data, f.err
}
func (f fakeStatsRepo) LabelStats(context.Context, usecase.LabelStatsFilter) (usecase.LabelStatsTotals, []usecase.LabelStat, error) {
	return usecase.LabelStatsTotals{}, nil, f.err
}

type fakeStorage struct {
	ready       error
	total, free int64
	usageErr    error
}

func (f fakeStorage) Ready(context.Context) error  { return f.ready }
func (f fakeStorage) Usage() (int64, int64, error) { return f.total, f.free, f.usageErr }
func (fakeSlots) Slots() (int, int)                { return 2, 200 }

type fakeSlots struct{}

// AC 22: SSD tidak tersedia, kanal WhatsApp mati, dan DB gagal tampil benar tanpa menggagalkan seluruh respons.
func TestSystemStatusSections(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	ch := usecase.NewChannel(3)
	cs := &usecase.CleanupStatus{}
	cfg := usecase.SystemConfig{Version: "abc123", AuthMode: "otp", MinFreeBytes: 5, MaxQueue: 200, DailyCap: 500, RetentionDays: 90}
	stats := fakeStatsRepo{counts: usecase.StatsCounts{PhotosPending: 4, OTPQueued: 1, OTPSent24h: 31, OTPFailed24h: 1},
		data: usecase.DataCounts{Submissions: 420, ReviewPending: 37, FilesPendingDelete: 2}}
	mk := func(db error, st fakeStorage, s fakeStatsRepo) *usecase.SystemReport {
		return usecase.NewSystemStatus(usecase.SystemDeps{DB: fakePing{db}, Stats: s, AdminStats: s, Storage: st, Uploads: fakeSlots{},
			Channel: ch, Cleanup: cs, ReadCPUTemp: func() (float64, error) { return 52.1, nil }, Clock: clock}, cfg).Report(bg)
	}

	// semua sehat
	cs.Record(clock.t, nil)
	r := mk(nil, fakeStorage{total: 100, free: 25}, stats)
	if !r.Database.Value.OK || r.Database.Err != "" || !r.Storage.Value.Available || r.Storage.Value.UsedPct != 75 ||
		*r.Storage.Value.PhotosPending != 4 || r.WhatsApp.Err != "" || !r.WhatsApp.Value.Available || r.WhatsApp.Value.Sent24h != 31 ||
		r.Data.Value.ReviewPending != 37 || r.Cleanup.Value.FilesPendingDelete != 2 || !r.Cleanup.Value.LastOK ||
		r.Uploads.Value.Active != 2 || *r.CPUTempC != 52.1 || r.Config.Version != "abc123" {
		t.Fatalf("sehat: %+v", r)
	}

	// SSD tidak ter-mount + kanal WhatsApp mati
	for i := 0; i < 3; i++ {
		ch.RecordSend(false)
	}
	r = mk(nil, fakeStorage{ready: domain.Unavailable("STORAGE_UNAVAILABLE", "x"), usageErr: errors.New("statfs")}, stats)
	if r.Storage.Value.Available || r.Storage.Err != "" || r.WhatsApp.Value.Available || r.WhatsApp.Err != "" {
		t.Fatalf("SSD/WA mati: %+v %+v", r.Storage, r.WhatsApp)
	}
	// SSD penuh tetap tersedia
	if r = mk(nil, fakeStorage{ready: domain.Unavailable("STORAGE_FULL", "x"), total: 100, free: 1}, stats); !r.Storage.Value.Available {
		t.Fatal("SSD penuh tetap terhitung tersedia")
	}

	// DB gagal: bagian yang membaca DB berisi error, bagian lain tetap terisi
	r = mk(errors.New("conn refused host=db user=x"), fakeStorage{total: 100, free: 50}, stats)
	if r.Database.Value.OK || r.Database.Err != "unavailable" || r.Data.Err == "" || r.WhatsApp.Err == "" || r.Cleanup.Err == "" ||
		r.Storage.Err != "" || r.Storage.Value.PhotosPending != nil || r.Uploads.Value.Max != 200 {
		t.Fatalf("DB gagal: %+v", r)
	}
	// retensi mati: angka "akan terhapus retensi" disembunyikan (0)
	cfgOff := cfg
	cfgOff.RetentionDays = 0
	due := fakeStatsRepo{counts: stats.counts, data: usecase.DataCounts{Submissions: 5, RetentionDue7d: 5}}
	r = usecase.NewSystemStatus(usecase.SystemDeps{DB: fakePing{nil}, Stats: due, AdminStats: due, Storage: fakeStorage{total: 100, free: 50},
		Uploads: fakeSlots{}, Channel: ch, Cleanup: cs, Clock: clock}, cfgOff).Report(bg)
	if r.Data.Err != "" || r.Data.Value.RetentionDue7d != 0 || r.Data.Value.Submissions != 5 {
		t.Fatalf("retensi mati: %+v", r.Data)
	}
	// query lambat → timeout
	r = mk(nil, fakeStorage{total: 100, free: 50}, fakeStatsRepo{err: context.DeadlineExceeded})
	if r.Data.Err != "timeout" || !r.Database.Value.OK {
		t.Fatalf("query lambat: %+v", r.Data)
	}
}
