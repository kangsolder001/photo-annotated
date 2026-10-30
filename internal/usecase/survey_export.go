package usecase

import (
	"context"
	"time"

	"photo-annotated/internal/domain"
)

// SurveyExportFilter membatasi export CSV pendataan (spec-pendataan §5.7). From inklusif, To eksklusif,
// atas created_at submission (sama dengan export JSONL); nil = tanpa batas.
type SurveyExportFilter struct {
	From, To *time.Time
	Consent  *bool
	Review   *domain.ReviewStatus
}

// SurveyExportRow adalah satu pendataan (satu rumah tangga) untuk CSV. Tanpa nomor HP/email.
type SurveyExportRow struct {
	Submission                  domain.Submission // ID, CreatedAt, Review, FormVersion, Answers
	Student, NIM, Group         string            // kosong bila tidak ada
	PhotoCount, AnnotationCount int
}

// SurveyExportRepo membaca pendataan (submission ber-answers yang belum dihapus) per halaman, urut
// (created_at, id).
type SurveyExportRepo interface {
	SurveyExportPage(ctx context.Context, after *AdminCursor, f SurveyExportFilter, limit int) ([]SurveyExportRow, error)
}

// SurveyExportEncoder menulis satu baris export pendataan (mis. CSV).
type SurveyExportEncoder interface {
	Encode(row *SurveyExportRow) error
}

// SurveyExport mengalirkan pendataan per halaman dengan memori tetap.
type SurveyExport struct {
	repo     SurveyExportRepo
	pageSize int
}

func NewSurveyExport(repo SurveyExportRepo, pageSize int) *SurveyExport {
	if pageSize < 1 {
		panic("usecase: SurveyExport pageSize harus >= 1")
	}
	return &SurveyExport{repo: repo, pageSize: pageSize}
}

// Run menulis semua baris lewat enc dan memanggil afterPage setelah tiap halaman. Mengembalikan jumlah
// baris yang sudah ditulis.
func (e *SurveyExport) Run(ctx context.Context, f SurveyExportFilter, enc SurveyExportEncoder, afterPage func() error) (int, error) {
	var after *AdminCursor
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		page, err := e.repo.SurveyExportPage(ctx, after, f, e.pageSize)
		if err != nil {
			return n, err
		}
		for i := range page {
			if err := enc.Encode(&page[i]); err != nil {
				return n, err
			}
			n++
		}
		if afterPage != nil {
			if err := afterPage(); err != nil {
				return n, err
			}
		}
		if len(page) < e.pageSize {
			return n, nil
		}
		last := page[len(page)-1].Submission
		after = &AdminCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}
