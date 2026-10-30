package usecase

import (
	"context"
	"math"

	"github.com/google/uuid"

	"photo-annotated/internal/domain/survey"
)

// SurveyStatsFilter: rentang survey_date (a_date) inklusif "YYYY-MM-DD" dan RW (sama persis); kosong = semua.
type SurveyStatsFilter struct {
	From, To string
	RW       string
}

// SurveyCounts: jumlah pendataan, bersedia, dan menolak.
type SurveyCounts struct{ Households, Consented, Declined int }

// SurveyRWStat: angka satu RW.
type SurveyRWStat struct {
	RW string
	SurveyCounts
}

// SurveyStudentStat: angka satu mahasiswa. Group kosong bila tidak punya kelompok.
type SurveyStudentStat struct {
	UserID               uuid.UUID
	Name, NIM, Group     string
	Households, Declined int
}

// SurveyStatsData adalah hasil mentah repo. Answers[id][nilai] = jumlah pendataan bersedia yang menjawab
// nilai itu untuk pertanyaan id.
type SurveyStatsData struct {
	ByRW      []SurveyRWStat
	ByStudent []SurveyStudentStat
	Answers   map[string]map[string]int
}

// SurveyStatsRepo menghitung statistik pendataan (spec-pendataan §5.8) dari submission ber-answers yang
// belum dihapus.
type SurveyStatsRepo interface {
	// SurveyStats: Answers hanya untuk id di answerIDs dan hanya dari pendataan yang bersedia.
	SurveyStats(ctx context.Context, f SurveyStatsFilter, answerIDs []string) (*SurveyStatsData, error)
}

// SurveyItemStat: distribusi jawaban satu butir B atau C.
type SurveyItemStat struct {
	ID, Label string
	Counts    map[string]int // per id opsi (B: yes/no; C: yes/no/na)
	YesShare  float64        // yes / (jumlah jawaban), 3 desimal; 0 bila tidak ada jawaban
}

// SurveyStatsResult adalah isi GET /admin/stats/survey.
type SurveyStatsResult struct {
	Totals    SurveyCounts
	ByRW      []SurveyRWStat
	ByStudent []SurveyStudentStat
	B, C      []SurveyItemStat
	R3Sorting map[string]int // semua opsi r3_sorting ikut (0 bila tidak ada)
}

// SurveyStats menghitung statistik pendataan. Daftar butir B/C dan labelnya diambil dari definisi
// instrumen (survey.V1), bukan ditulis ulang.
type SurveyStats struct{ repo SurveyStatsRepo }

func NewSurveyStats(repo SurveyStatsRepo) *SurveyStats { return &SurveyStats{repo: repo} }

const r3SortingID = "r3_sorting"

func (u *SurveyStats) Stats(ctx context.Context, f SurveyStatsFilter) (*SurveyStatsResult, error) {
	form := survey.V1
	var bQs, cQs []survey.Question
	for _, s := range form.Sections {
		for _, q := range s.Questions {
			if q.Type != survey.TypeChoice || !q.Required {
				continue
			}
			switch {
			case len(s.ID) == 2 && s.ID[0] == 'B':
				bQs = append(bQs, q)
			case s.ID == "C":
				cQs = append(cQs, q)
			}
		}
	}
	ids := []string{r3SortingID}
	for _, q := range append(append([]survey.Question{}, bQs...), cQs...) {
		ids = append(ids, q.ID)
	}
	d, err := u.repo.SurveyStats(ctx, f, ids)
	if err != nil {
		return nil, err
	}
	out := &SurveyStatsResult{ByRW: d.ByRW, ByStudent: d.ByStudent, R3Sorting: map[string]int{}}
	for _, r := range d.ByRW {
		out.Totals.Households += r.Households
		out.Totals.Consented += r.Consented
		out.Totals.Declined += r.Declined
	}
	item := func(q survey.Question) SurveyItemStat {
		st := SurveyItemStat{ID: q.ID, Label: q.Label, Counts: map[string]int{}}
		total := 0
		for _, o := range q.Options {
			n := d.Answers[q.ID][o.ID]
			st.Counts[o.ID] = n
			total += n
		}
		if total > 0 {
			st.YesShare = math.Round(float64(st.Counts["yes"])/float64(total)*1000) / 1000
		}
		return st
	}
	for _, q := range bQs {
		out.B = append(out.B, item(q))
	}
	for _, q := range cQs {
		out.C = append(out.C, item(q))
	}
	if q, ok := form.Question(r3SortingID); ok {
		for _, o := range q.Options {
			out.R3Sorting[o.ID] = d.Answers[r3SortingID][o.ID]
		}
	}
	return out, nil
}
