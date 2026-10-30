package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/usecase"
)

// SurveyStatsRepo menghitung statistik pendataan langsung dari answers (jsonb) tanpa tabel ringkasan.
type SurveyStatsRepo struct{ base }

var _ usecase.SurveyStatsRepo = (*SurveyStatsRepo)(nil)

func NewSurveyStatsRepo(pool *pgxpool.Pool) *SurveyStatsRepo { return &SurveyStatsRepo{base{pool}} }

// surveyWhere: pendataan yang belum dihapus dan lolos filter ($1 from, $2 to, $3 rw; NULL = semua).
const surveyWhere = `s.deleted_at IS NULL AND s.answers IS NOT NULL
	AND ($1::date IS NULL OR s.survey_date >= $1::date)
	AND ($2::date IS NULL OR s.survey_date <= $2::date)
	AND ($3::text IS NULL OR s.rw = $3)`

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *SurveyStatsRepo) SurveyStats(ctx context.Context, f usecase.SurveyStatsFilter, answerIDs []string) (*usecase.SurveyStatsData, error) {
	args := []any{nullStr(f.From), nullStr(f.To), nullStr(f.RW)}
	out := &usecase.SurveyStatsData{ByRW: []usecase.SurveyRWStat{}, ByStudent: []usecase.SurveyStudentStat{}, Answers: map[string]map[string]int{}}

	rows, err := r.db(ctx).Query(ctx, `
		SELECT COALESCE(s.rw, ''), count(*), count(*) FILTER (WHERE s.consent), count(*) FILTER (WHERE NOT s.consent)
		  FROM submissions s WHERE `+surveyWhere+`
		 GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: survey stats by rw: %w", err)
	}
	for rows.Next() {
		var st usecase.SurveyRWStat
		if err := rows.Scan(&st.RW, &st.Households, &st.Consented, &st.Declined); err != nil {
			rows.Close()
			return nil, fmt.Errorf("postgres: scan survey rw: %w", err)
		}
		out.ByRW = append(out.ByRW, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db(ctx).Query(ctx, `
		SELECT u.id, u.name, COALESCE(u.nim, ''), COALESCE(g.name, ''), count(*), count(*) FILTER (WHERE NOT s.consent)
		  FROM submissions s JOIN users u ON u.id = s.owner_id LEFT JOIN groups g ON g.id = u.group_id
		 WHERE `+surveyWhere+`
		 GROUP BY u.id, u.name, u.nim, g.name
		 ORDER BY count(*) DESC, u.name, u.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: survey stats by student: %w", err)
	}
	for rows.Next() {
		var st usecase.SurveyStudentStat
		if err := rows.Scan(&st.UserID, &st.Name, &st.NIM, &st.Group, &st.Households, &st.Declined); err != nil {
			rows.Close()
			return nil, fmt.Errorf("postgres: scan survey student: %w", err)
		}
		out.ByStudent = append(out.ByStudent, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db(ctx).Query(ctx, `
		SELECT a.key, a.value, count(*)
		  FROM submissions s CROSS JOIN LATERAL jsonb_each_text(s.answers) a
		 WHERE `+surveyWhere+` AND s.consent AND a.key = ANY($4)
		 GROUP BY a.key, a.value`, append(args, answerIDs)...)
	if err != nil {
		return nil, fmt.Errorf("postgres: survey stats answers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			key, value string
			n          int
		)
		if err := rows.Scan(&key, &value, &n); err != nil {
			return nil, fmt.Errorf("postgres: scan survey answer: %w", err)
		}
		if out.Answers[key] == nil {
			out.Answers[key] = map[string]int{}
		}
		out.Answers[key][value] = n
	}
	return out, rows.Err()
}
