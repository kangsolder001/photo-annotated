package handler

import (
	"context"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// LabelStatsService dipenuhi oleh *usecase.LabelStats.
type LabelStatsService interface {
	Labels(ctx context.Context, f usecase.LabelStatsFilter) (*usecase.LabelStatsResult, error)
}

// SystemReporter dipenuhi oleh *usecase.SystemStatus.
type SystemReporter interface {
	Report(ctx context.Context) *usecase.SystemReport
}

// AdminStatsHandler menangani statistik label dan status sistem (spec-labels-admin §5.9, §5.10).
type AdminStatsHandler struct {
	labels LabelStatsService
	system SystemReporter
}

func NewAdminStatsHandler(l LabelStatsService, s SystemReporter) *AdminStatsHandler {
	return &AdminStatsHandler{labels: l, system: s}
}

type labelStatDTO struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Archived    bool    `json:"archived"`
	Annotations int     `json:"annotations"`
	Photos      int     `json:"photos"`
	Submissions int     `json:"submissions"`
	Share       float64 `json:"share"`
}

// Labels menangani GET /api/v1/admin/stats/labels.
func (h *AdminStatsHandler) Labels(c *gin.Context) {
	var f usecase.LabelStatsFilter
	var err error
	if f.From, f.To, err = parseTimeRange(c); err != nil {
		httpx.WriteError(c, err)
		return
	}
	if v := c.Query("review_status"); v != "" {
		st, perr := domain.ParseReviewStatus(v)
		if perr != nil {
			httpx.WriteError(c, invalidQuery("review_status must be pending, accepted, or rejected"))
			return
		}
		f.Review = &st
	}
	res, err := h.labels.Labels(c.Request.Context(), f)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	labels := make([]labelStatDTO, len(res.Labels))
	for i, l := range res.Labels {
		labels[i] = labelStatDTO{ID: l.Label.ID, Name: l.Label.Name, Color: l.Label.Color, Archived: l.Label.Archived(),
			Annotations: l.Annotations, Photos: l.Photos, Submissions: l.Submissions, Share: l.Share}
	}
	t := res.Totals
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"totals": gin.H{"submissions": t.Submissions, "photos": t.Photos, "annotations": t.Annotations,
			"legacy_text_annotations": t.LegacyTextAnnotations},
		"labels": labels,
	})
}

func section[T any](s usecase.Section[T], ok func(T) gin.H) gin.H {
	if s.Err != "" {
		return gin.H{"error": s.Err}
	}
	return ok(s.Value)
}

// System menangani GET /api/v1/admin/system: keadaan saat ini, tanpa data pribadi atau secret.
func (h *AdminStatsHandler) System(c *gin.Context) {
	r := h.system.Report(c.Request.Context())
	var temp *float64
	if r.CPUTempC != nil {
		v := math.Round(*r.CPUTempC*10) / 10
		temp = &v
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"checked_at": r.CheckedAt.UTC(),
		"api": gin.H{"started_at": r.Config.StartedAt.UTC(), "version": r.Config.Version, "auth_mode": r.Config.AuthMode,
			"retention_enabled": r.Config.RetentionDays > 0},
		"database": func() gin.H {
			h := gin.H{"ok": r.Database.Value.OK, "latency_ms": r.Database.Value.LatencyMS}
			if r.Database.Err != "" {
				h["error"] = r.Database.Err
			}
			return h
		}(),
		"storage": section(r.Storage, func(s usecase.StorageSection) gin.H {
			return gin.H{"available": s.Available, "total_bytes": s.Total, "free_bytes": s.Free,
				"used_pct": math.Round(s.UsedPct*10) / 10, "min_free_bytes": s.MinFree, "photos_pending": s.PhotosPending}
		}),
		"uploads": section(r.Uploads, func(u usecase.UploadSection) gin.H { return gin.H{"active": u.Active, "max": u.Max} }),
		"whatsapp": section(r.WhatsApp, func(w usecase.WhatsAppSection) gin.H {
			return gin.H{"otp_available": w.Available, "last_probe_at": utcPtr(w.LastProbeAt), "queued": w.Queued,
				"max_queue": w.MaxQueue, "sent_24h": w.Sent24h, "failed_24h": w.Failed24h, "daily_cap": w.Cap}
		}),
		"cleanup": section(r.Cleanup, func(cl usecase.CleanupSection) gin.H {
			return gin.H{"last_run_at": utcPtr(cl.LastRunAt), "last_ok": cl.LastOK, "files_pending_delete": cl.FilesPendingDelete}
		}),
		"data": section(r.Data, func(d usecase.DataCounts) gin.H {
			return gin.H{"submissions": d.Submissions, "review_pending": d.ReviewPending, "accepted": d.Accepted,
				"rejected": d.Rejected, "retention_due_7d": d.RetentionDue7d, "users_active": d.UsersActive, "users_blocked": d.UsersBlocked}
		}),
		"host": gin.H{"cpu_temp_c": temp},
	})
}
