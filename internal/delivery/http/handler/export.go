package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// Exporter dipenuhi oleh *usecase.Export.
type Exporter interface {
	Run(ctx context.Context, q usecase.ExportQuery, enc usecase.ExportEncoder, afterPage func() error) (int, error)
}

// ExportStream adalah encoder yang menampung output dan bisa di-flush per halaman.
type ExportStream interface {
	usecase.ExportEncoder
	Flush() error
}

// ExportHandler menangani GET /api/v1/admin/export (spec §5.7).
type ExportHandler struct {
	exporter  Exporter
	newJSONL  func(io.Writer) ExportStream
	pageGrace time.Duration // deadline tulis diperpanjang sebesar ini setiap halaman
}

func NewExportHandler(e Exporter, newJSONL func(io.Writer) ExportStream, pageGrace time.Duration) *ExportHandler {
	return &ExportHandler{exporter: e, newJSONL: newJSONL, pageGrace: pageGrace}
}

func parseExportQuery(c *gin.Context) (usecase.ExportQuery, error) {
	var q usecase.ExportQuery
	if f := c.Query("format"); f != "" && f != "jsonl" {
		return q, domain.Invalid("UNSUPPORTED_FORMAT", "format must be jsonl")
	}
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &q.From}, {"to", &q.To}} {
		if v := c.Query(p.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return q, domain.Invalid("INVALID_QUERY", "%s must be an RFC 3339 timestamp", p.name)
			}
			*p.dst = &t
		}
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return q, domain.Invalid("INVALID_QUERY", "from must be before to")
	}
	if v := c.Query("label_id"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id < 1 {
			return q, domain.Invalid("INVALID_QUERY", "label_id must be a positive integer")
		}
		q.LabelID = &id
	}
	if v := c.Query("review_status"); v != "" {
		st, err := domain.ParseReviewStatus(v)
		if err != nil {
			return q, domain.Invalid("INVALID_QUERY", "review_status must be pending, accepted, or rejected")
		}
		q.Review = &st
	}
	if v := c.Query("include_urls"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return q, domain.Invalid("INVALID_QUERY", "include_urls must be true or false")
		}
		q.IncludeURLs = b
	}
	return q, nil
}

// Export mengalirkan satu baris JSON per foto. Status 200 dikirim sebelum data, jadi kegagalan di
// tengah jalan tidak bisa mengubah status: hasilnya dilaporkan lewat trailer X-Export-Complete
// (true/false) dan X-Export-Rows, dan dicatat di log.
func (h *ExportHandler) Export(c *gin.Context) {
	q, err := parseExportQuery(c)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "application/x-ndjson")
	hdr.Set("Content-Disposition", `attachment; filename="photo-annotated-`+time.Now().UTC().Format("20060102T150405Z")+`.jsonl"`)
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Trailer", "X-Export-Rows, X-Export-Complete")
	c.Status(http.StatusOK)

	rc := http.NewResponseController(c.Writer)
	stream := h.newJSONL(c.Writer)
	afterPage := func() error {
		if err := stream.Flush(); err != nil {
			return err
		}
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		// klien yang berhenti membaca diputus setelah pageGrace; export panjang tetap berjalan
		if err := rc.SetWriteDeadline(time.Now().Add(h.pageGrace)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}
	n, err := h.exporter.Run(c.Request.Context(), q, stream, afterPage)
	if err == nil {
		err = stream.Flush()
	}
	hdr.Set("X-Export-Rows", strconv.Itoa(n))
	hdr.Set("X-Export-Complete", strconv.FormatBool(err == nil))
	if err != nil {
		_ = c.Error(err)
	}
}

// SurveyExporter dipenuhi oleh *usecase.SurveyExport.
type SurveyExporter interface {
	Run(ctx context.Context, f usecase.SurveyExportFilter, enc usecase.SurveyExportEncoder, afterPage func() error) (int, error)
}

// SurveyExportStream adalah encoder CSV yang bisa di-flush per halaman.
type SurveyExportStream interface {
	usecase.SurveyExportEncoder
	Flush() error
}

// SurveyExportHandler menangani GET /api/v1/admin/export.csv (spec-pendataan §5.7).
type SurveyExportHandler struct {
	exporter  SurveyExporter
	newCSV    func(io.Writer) SurveyExportStream
	pageGrace time.Duration
}

func NewSurveyExportHandler(e SurveyExporter, newCSV func(io.Writer) SurveyExportStream, pageGrace time.Duration) *SurveyExportHandler {
	return &SurveyExportHandler{exporter: e, newCSV: newCSV, pageGrace: pageGrace}
}

func parseSurveyExportQuery(c *gin.Context) (usecase.SurveyExportFilter, error) {
	var f usecase.SurveyExportFilter
	var err error
	if f.From, f.To, err = parseTimeRange(c); err != nil {
		return f, err
	}
	switch v := c.Query("consent"); v {
	case "":
	case "yes", "no":
		b := v == "yes"
		f.Consent = &b
	default:
		return f, invalidQuery("consent must be yes or no")
	}
	if v := c.Query("review_status"); v != "" {
		st, perr := domain.ParseReviewStatus(v)
		if perr != nil {
			return f, invalidQuery("review_status must be pending, accepted, or rejected")
		}
		f.Review = &st
	}
	return f, nil
}

// Export mengalirkan CSV satu baris per pendataan. Seperti export JSONL, status 200 dikirim sebelum
// data; kegagalan di tengah dilaporkan lewat trailer X-Export-Complete dan X-Export-Rows.
func (h *SurveyExportHandler) Export(c *gin.Context) {
	f, err := parseSurveyExportQuery(c)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/csv; charset=utf-8")
	hdr.Set("Content-Disposition", `attachment; filename="pendataan-`+time.Now().UTC().Format("20060102T150405Z")+`.csv"`)
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Trailer", "X-Export-Rows, X-Export-Complete")
	c.Status(http.StatusOK)

	rc := http.NewResponseController(c.Writer)
	stream := h.newCSV(c.Writer)
	afterPage := func() error {
		if err := stream.Flush(); err != nil {
			return err
		}
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if err := rc.SetWriteDeadline(time.Now().Add(h.pageGrace)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}
	n, err := h.exporter.Run(c.Request.Context(), f, stream, afterPage)
	if err == nil {
		err = stream.Flush()
	}
	hdr.Set("X-Export-Rows", strconv.Itoa(n))
	hdr.Set("X-Export-Complete", strconv.FormatBool(err == nil))
	if err != nil {
		_ = c.Error(err)
	}
}
