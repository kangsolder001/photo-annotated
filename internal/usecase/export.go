package usecase

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
)

// ExportConfig adalah parameter export dataset (spec §5.7).
type ExportConfig struct {
	PublicBaseURL string
	URLTTL        time.Duration // umur URL foto bila include_urls (1 jam)
	PageSize      int
}

// ExportQuery adalah filter export. From inklusif, To eksklusif (atas created_at submission).
type ExportQuery struct {
	From, To    *time.Time
	IncludeURLs bool
}

// ExportItem adalah satu baris export: satu foto beserta anotasinya.
type ExportItem struct {
	SubmissionID uuid.UUID
	PhotoID      uuid.UUID
	StorageKey   string
	SHA256       []byte
	URL          string // kosong bila tidak diminta
	Width        int
	Height       int
	Annotations  []ExportAnnotation
	CreatedAt    time.Time
}

// ExportAnnotation memuat kotak dalam dua bentuk agar konsumen tidak perlu tahu aturan normalisasi.
type ExportAnnotation struct {
	Text     string
	BBoxNorm [4]float64 // [x, y, w, h] 0..1 (nilai asli di DB)
	BBoxPx   [4]int     // [x, y, w, h] dalam piksel foto hasil proses
}

// Export mengalirkan dataset per halaman dengan memori tetap.
type Export struct {
	repo   ExportRepo
	signer URLSigner
	clock  Clock
	cfg    ExportConfig
}

func NewExport(repo ExportRepo, signer URLSigner, clock Clock, cfg ExportConfig) *Export {
	if cfg.PageSize < 1 || cfg.URLTTL <= 0 {
		panic("usecase: ExportConfig tidak valid")
	}
	return &Export{repo: repo, signer: signer, clock: clock, cfg: cfg}
}

// Run menulis semua item lewat enc, memanggil afterPage setelah tiap halaman (untuk flush dan
// memperpanjang deadline tulis). Mengembalikan jumlah item yang sudah ditulis.
func (e *Export) Run(ctx context.Context, q ExportQuery, enc ExportEncoder, afterPage func() error) (int, error) {
	var after *ExportCursor
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		page, err := e.repo.ExportPage(ctx, after, q.From, q.To, e.cfg.PageSize)
		if err != nil {
			return n, err
		}
		var exp time.Time
		if q.IncludeURLs {
			exp = e.clock.Now().Add(e.cfg.URLTTL)
		}
		for i := range page {
			item := toExportItem(&page[i])
			if q.IncludeURLs {
				item.URL = viewURL(e.cfg.PublicBaseURL, e.signer, item.PhotoID, exp)
			}
			if err := enc.Encode(item); err != nil {
				return n, err
			}
			n++
		}
		if afterPage != nil {
			if err := afterPage(); err != nil {
				return n, err
			}
		}
		if len(page) < e.cfg.PageSize {
			return n, nil
		}
		last := page[len(page)-1]
		after = &ExportCursor{CreatedAt: last.CreatedAt, SubmissionID: last.SubmissionID, Position: last.Photo.Position, PhotoID: last.Photo.ID}
	}
}

func toExportItem(p *ExportPhoto) *ExportItem {
	item := &ExportItem{
		SubmissionID: p.SubmissionID, PhotoID: p.Photo.ID, StorageKey: p.Photo.StorageKey, SHA256: p.Photo.SHA256,
		Width: p.Photo.Width, Height: p.Photo.Height, CreatedAt: p.CreatedAt,
		Annotations: make([]ExportAnnotation, len(p.Annotations)),
	}
	for i, a := range p.Annotations {
		item.Annotations[i] = ExportAnnotation{
			Text:     a.Text,
			BBoxNorm: [4]float64{a.X, a.Y, a.W, a.H},
			BBoxPx:   bboxPx(a.X, a.Y, a.W, a.H, p.Photo.Width, p.Photo.Height),
		}
	}
	return item
}

// bboxPx membulatkan kotak ternormalisasi ke piksel dan menjaganya tetap di dalam foto (toleransi
// 1e-6 pada validasi bisa membuat x+w sedikit di atas 1).
func bboxPx(x, y, w, h float64, width, height int) [4]int {
	px := int(math.Round(x * float64(width)))
	py := int(math.Round(y * float64(height)))
	pw := min(int(math.Round(w*float64(width))), width-px)
	ph := min(int(math.Round(h*float64(height))), height-py)
	return [4]int{px, py, max(pw, 1), max(ph, 1)}
}
