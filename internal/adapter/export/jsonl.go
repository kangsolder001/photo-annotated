// Package export berisi encoder format export dataset (spec §5.7).
package export

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/usecase"
)

// JSONL menulis satu objek JSON per baris (application/x-ndjson). Bentuknya kontrak dengan
// konsumen dataset: ubah hanya dengan menambah field.
type JSONL struct {
	w   *bufio.Writer
	enc *json.Encoder
}

var _ usecase.ExportEncoder = (*JSONL)(nil)

func NewJSONL(w io.Writer) *JSONL {
	bw := bufio.NewWriterSize(w, 64<<10)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false) // teks anotasi apa adanya ("<", "&" tidak di-escape)
	return &JSONL{w: bw, enc: enc}
}

type jsonlImage struct {
	StorageKey string  `json:"storage_key"`
	SHA256     string  `json:"sha256"`
	URL        *string `json:"url"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
}

// jsonlAnnotation: label_id adalah kelas stabil; label = nama saat export. Baris lama (teks bebas):
// label_id dan label null, text berisi catatannya.
type jsonlAnnotation struct {
	LabelID  *int       `json:"label_id"`
	Label    *string    `json:"label"`
	Text     *string    `json:"text"`
	BBoxNorm [4]float64 `json:"bbox_norm"`
	BBoxPx   [4]int     `json:"bbox_px"`
}

type jsonlRow struct {
	SubmissionID uuid.UUID         `json:"submission_id"`
	PhotoID      uuid.UUID         `json:"photo_id"`
	Image        jsonlImage        `json:"image"`
	Annotations  []jsonlAnnotation `json:"annotations"`
	CreatedAt    time.Time         `json:"created_at"`
	ReviewStatus string            `json:"review_status"`
}

func (j *JSONL) Encode(it *usecase.ExportItem) error {
	row := jsonlRow{
		SubmissionID: it.SubmissionID, PhotoID: it.PhotoID, CreatedAt: it.CreatedAt.UTC(), ReviewStatus: it.ReviewStatus,
		Image:       jsonlImage{StorageKey: it.StorageKey, SHA256: hex.EncodeToString(it.SHA256), Width: it.Width, Height: it.Height},
		Annotations: make([]jsonlAnnotation, len(it.Annotations)),
	}
	if it.URL != "" {
		row.Image.URL = &it.URL
	}
	for i, a := range it.Annotations {
		ja := jsonlAnnotation{BBoxNorm: a.BBoxNorm, BBoxPx: a.BBoxPx}
		if a.LabelID != 0 {
			id, name := a.LabelID, a.Label
			ja.LabelID, ja.Label = &id, &name
		}
		if a.Text != "" {
			text := a.Text
			ja.Text = &text
		}
		row.Annotations[i] = ja
	}
	return j.enc.Encode(row) // Encode menambahkan newline
}

// Flush mengirim isi buffer ke writer di bawahnya.
func (j *JSONL) Flush() error { return j.w.Flush() }
