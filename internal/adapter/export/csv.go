package export

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"photo-annotated/internal/domain/survey"
	"photo-annotated/internal/usecase"
)

// SurveyCSV menulis export pendataan: satu baris per rumah tangga, UTF-8 dengan BOM agar rapi di Excel
// (spec-pendataan §5.7). Kolom pertanyaan mengikuti urutan definisi instrumen; teks opsi "Lainnya" pada
// multi mendapat kolom sendiri (<id>_other) tepat setelah pertanyaannya.
type SurveyCSV struct {
	w    *bufio.Writer
	csv  *csv.Writer
	form *survey.Form
	cols []string // id kolom jawaban (pertanyaan + <id>_other)
}

var _ usecase.SurveyExportEncoder = (*SurveyCSV)(nil)

var fixedColumns = []string{"submission_id", "created_at", "review_status", "mahasiswa", "nim", "kelompok", "photo_count", "annotation_count"}

// NewSurveyCSV menulis BOM + baris header ke buffer (terkirim pada Flush pertama).
func NewSurveyCSV(w io.Writer) *SurveyCSV {
	bw := bufio.NewWriterSize(w, 64<<10)
	e := &SurveyCSV{w: bw, csv: csv.NewWriter(bw), form: survey.V1}
	for _, q := range e.form.Questions() {
		e.cols = append(e.cols, q.ID)
		if _, ok := q.OtherOption(); ok && q.Type == survey.TypeMulti {
			e.cols = append(e.cols, survey.OtherKey(q.ID))
		}
	}
	_, _ = bw.WriteString("\xEF\xBB\xBF")
	_ = e.csv.Write(append(append([]string{}, fixedColumns...), e.cols...))
	return e
}

// safeCell mencegah formula injection di spreadsheet: sel yang diawali = + - @ (atau tab/CR) diberi
// awalan '.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// cell mengubah satu jawaban menjadi teks: choice → teks opsi, multi → teks opsi digabung "; ".
func (e *SurveyCSV) cell(id string, v any) string {
	if v == nil {
		return ""
	}
	q, ok := e.form.Question(id)
	if !ok { // kolom <id>_other
		s, _ := v.(string)
		return s
	}
	switch q.Type {
	case survey.TypeChoice:
		s, _ := v.(string)
		return q.OptionLabel(s)
	case survey.TypeMulti:
		list, _ := v.([]any)
		parts := make([]string, 0, len(list))
		for _, x := range list {
			s, _ := x.(string)
			parts = append(parts, q.OptionLabel(s))
		}
		return strings.Join(parts, "; ")
	case survey.TypeInt:
		switch n := v.(type) {
		case float64:
			return strconv.FormatFloat(n, 'f', -1, 64)
		case int64:
			return strconv.FormatInt(n, 10)
		}
		return fmt.Sprint(v)
	}
	s, _ := v.(string)
	return s
}

func (e *SurveyCSV) Encode(r *usecase.SurveyExportRow) error {
	s := r.Submission
	review := string(s.Review.Status)
	if review == "" {
		review = "pending"
	}
	rec := []string{s.ID.String(), s.CreatedAt.UTC().Format(time.RFC3339), review, r.Student, r.NIM, r.Group,
		strconv.Itoa(r.PhotoCount), strconv.Itoa(r.AnnotationCount)}
	for _, id := range e.cols {
		rec = append(rec, e.cell(id, s.Answers[id]))
	}
	for i := range rec {
		rec[i] = safeCell(rec[i])
	}
	return e.csv.Write(rec)
}

// Flush mengirim isi buffer ke writer di bawahnya.
func (e *SurveyCSV) Flush() error {
	e.csv.Flush()
	if err := e.csv.Error(); err != nil {
		return err
	}
	return e.w.Flush()
}
