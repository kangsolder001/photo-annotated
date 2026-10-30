package usecase

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CleanupStatus mencatat hasil putaran cleanup terakhir untuk status sistem (dipegang di memori).
type CleanupStatus struct {
	mu      sync.Mutex
	lastRun time.Time
	lastOK  bool
}

// Record dipanggil setelah setiap putaran cleanup.
func (s *CleanupStatus) Record(at time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRun, s.lastOK = at, err == nil
}

// Snapshot: lastRun nil bila belum pernah berjalan sejak proses start.
func (s *CleanupStatus) Snapshot() (lastRun *time.Time, lastOK bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastRun.IsZero() {
		return nil, false
	}
	t := s.lastRun
	return &t, s.lastOK
}

// Port kecil untuk status sistem; dipenuhi pool, localfs.Store, dan *Upload.
type (
	DBPinger interface {
		Ping(ctx context.Context) error
	}
	StorageProbe interface {
		Ready(ctx context.Context) error
		Usage() (total, free int64, err error)
	}
	UploadSlots interface{ Slots() (inUse, capacity int) }
)

// SystemConfig adalah nilai tetap yang ikut ditampilkan.
type SystemConfig struct {
	StartedAt     time.Time
	Version       string
	AuthMode      string
	MinFreeBytes  int64
	MaxQueue      int
	DailyCap      int
	RetentionDays int
}

// SystemDeps adalah sumber data status sistem. ReadCPUTemp boleh nil.
type SystemDeps struct {
	DB          DBPinger
	Stats       StatsRepo
	AdminStats  AdminStatsRepo
	Storage     StorageProbe
	Uploads     UploadSlots
	Channel     *Channel
	Cleanup     *CleanupStatus
	ReadCPUTemp func() (float64, error)
	Clock       Clock
}

// Bagian status sistem. Err terisi (pesan umum, tanpa detail internal) bila bagian itu gagal dihitung.
type (
	DBSection struct {
		OK        bool
		LatencyMS int64
	}
	StorageSection struct {
		Available            bool
		Total, Free, MinFree int64
		UsedPct              float64
		PhotosPending        *int // nil bila database gagal
	}
	UploadSection   struct{ Active, Max int }
	WhatsAppSection struct {
		Available               bool
		LastProbeAt             *time.Time
		Queued, MaxQueue        int
		Sent24h, Failed24h, Cap int
	}
	CleanupSection struct {
		LastRunAt          *time.Time
		LastOK             bool
		FilesPendingDelete int
	}
)

// Section membungkus satu bagian beserta error-nya.
type Section[T any] struct {
	Value T
	Err   string
}

// SystemReport adalah isi GET /admin/system. Tidak memuat data pribadi maupun secret.
type SystemReport struct {
	CheckedAt time.Time
	Config    SystemConfig
	Database  Section[DBSection]
	Storage   Section[StorageSection]
	Uploads   Section[UploadSection]
	WhatsApp  Section[WhatsAppSection]
	Cleanup   Section[CleanupSection]
	Data      Section[DataCounts]
	CPUTempC  *float64
}

// SystemStatus menghitung status sistem saat diminta (spec-labels-admin §5.10). Kegagalan satu bagian
// tidak menggagalkan bagian lain.
type SystemStatus struct {
	d   SystemDeps
	cfg SystemConfig
}

func NewSystemStatus(d SystemDeps, cfg SystemConfig) *SystemStatus {
	return &SystemStatus{d: d, cfg: cfg}
}

const systemPartTimeout = 2 * time.Second

func failMsg(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "unavailable"
}

func (s *SystemStatus) Report(ctx context.Context) *SystemReport {
	now := s.d.Clock.Now()
	rep := &SystemReport{CheckedAt: now, Config: s.cfg}

	// database: bagian lain yang membaca DB dilewati bila ping gagal (tidak menunggu timeout berkali-kali)
	pctx, cancel := context.WithTimeout(ctx, systemPartTimeout)
	start := time.Now()
	err := s.d.DB.Ping(pctx)
	cancel()
	rep.Database.Value = DBSection{OK: err == nil, LatencyMS: time.Since(start).Milliseconds()}
	dbOK := err == nil
	if err != nil {
		rep.Database.Err = failMsg(err)
	}

	var counts *StatsCounts
	var data *DataCounts
	dbErr := "database unavailable"
	if dbOK {
		qctx, cancel := context.WithTimeout(ctx, systemPartTimeout)
		if c, err := s.d.Stats.Counts(qctx, now.Add(-24*time.Hour)); err == nil {
			counts = &c
		} else {
			dbErr = failMsg(err)
		}
		cancel()
		qctx, cancel = context.WithTimeout(ctx, systemPartTimeout)
		days := s.cfg.RetentionDays
		if d, err := s.d.AdminStats.DataCounts(qctx, now.AddDate(0, 0, -days+7)); err == nil {
			if days == 0 {
				d.RetentionDue7d = 0 // retensi mati: tidak ada yang akan terhapus karena umur
			}
			data = &d
		} else {
			dbErr = failMsg(err)
		}
		cancel()
	}

	// storage: tersedia = penanda ada dan bisa ditulis (STORAGE_FULL tetap terhitung tersedia)
	sctx, cancel := context.WithTimeout(ctx, systemPartTimeout)
	readyErr := s.d.Storage.Ready(sctx)
	cancel()
	st := StorageSection{Available: readyErr == nil || isCode(readyErr, "STORAGE_FULL"), MinFree: s.cfg.MinFreeBytes}
	if total, free, err := s.d.Storage.Usage(); err == nil && total > 0 {
		st.Total, st.Free, st.UsedPct = total, free, float64(total-free)*100/float64(total)
	} else if st.Available {
		rep.Storage.Err = "unavailable"
	}
	if counts != nil {
		n := counts.PhotosPending
		st.PhotosPending = &n
	}
	rep.Storage.Value = st

	inUse, capacity := s.d.Uploads.Slots()
	rep.Uploads.Value = UploadSection{Active: inUse, Max: capacity}

	if counts != nil {
		ch := s.d.Channel.State()
		rep.WhatsApp.Value = WhatsAppSection{Available: ch.Available, LastProbeAt: ch.LastProbeAt, Queued: counts.OTPQueued,
			MaxQueue: s.cfg.MaxQueue, Sent24h: counts.OTPSent24h, Failed24h: counts.OTPFailed24h, Cap: s.cfg.DailyCap}
	} else {
		rep.WhatsApp.Err = dbErr
	}

	if data != nil {
		last, ok := s.d.Cleanup.Snapshot()
		rep.Cleanup.Value = CleanupSection{LastRunAt: last, LastOK: ok, FilesPendingDelete: data.FilesPendingDelete}
		rep.Data.Value = *data
	} else {
		rep.Cleanup.Err, rep.Data.Err = dbErr, dbErr
	}

	if s.d.ReadCPUTemp != nil {
		if t, err := s.d.ReadCPUTemp(); err == nil {
			rep.CPUTempC = &t
		}
	}
	return rep
}
