package handler

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/usecase"
)

// SurveyStatsService dipenuhi oleh *usecase.SurveyStats.
type SurveyStatsService interface {
	Stats(ctx context.Context, f usecase.SurveyStatsFilter) (*usecase.SurveyStatsResult, error)
}

type SurveyStatsHandler struct{ stats SurveyStatsService }

func NewSurveyStatsHandler(s SurveyStatsService) *SurveyStatsHandler {
	return &SurveyStatsHandler{stats: s}
}

type surveyCountsDTO struct {
	Households int `json:"households"`
	Consented  int `json:"consented"`
	Declined   int `json:"declined"`
}

type surveyRWDTO struct {
	RW string `json:"rw"`
	surveyCountsDTO
}

type surveyStudentDTO struct {
	UserID     uuid.UUID `json:"user_id"`
	Name       string    `json:"name"`
	NIM        *string   `json:"nim"`
	Group      *string   `json:"group"`
	Households int       `json:"households"`
	Declined   int       `json:"declined"`
}

type surveyBDTO struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Yes      int     `json:"yes"`
	No       int     `json:"no"`
	YesShare float64 `json:"yes_share"`
}

type surveyCDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Yes   int    `json:"yes"`
	No    int    `json:"no"`
	NA    int    `json:"na"`
}

func parseSurveyStatsFilter(c *gin.Context) (usecase.SurveyStatsFilter, error) {
	var f usecase.SurveyStatsFilter
	for _, p := range []struct {
		name string
		dst  *string
	}{{"from", &f.From}, {"to", &f.To}} {
		if v := c.Query(p.name); v != "" {
			if d, err := time.Parse(time.DateOnly, v); err != nil || d.Format(time.DateOnly) != v {
				return f, invalidQuery("%s must be a date YYYY-MM-DD", p.name)
			}
			*p.dst = v
		}
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return f, invalidQuery("from must not be after to")
	}
	if v := c.Query("rw"); v != "" {
		if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 10 {
			return f, invalidQuery("rw must be at most 10 characters")
		}
		f.RW = v
	}
	return f, nil
}

// Stats menangani GET /api/v1/admin/stats/survey (spec-pendataan §5.8).
func (h *SurveyStatsHandler) Stats(c *gin.Context) {
	f, err := parseSurveyStatsFilter(c)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	res, err := h.stats.Stats(c.Request.Context(), f)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	byRW := make([]surveyRWDTO, len(res.ByRW))
	for i, r := range res.ByRW {
		byRW[i] = surveyRWDTO{RW: r.RW, surveyCountsDTO: surveyCountsDTO(r.SurveyCounts)}
	}
	byStudent := make([]surveyStudentDTO, len(res.ByStudent))
	for i, s := range res.ByStudent {
		byStudent[i] = surveyStudentDTO{UserID: s.UserID, Name: s.Name, NIM: optString(s.NIM), Group: optString(s.Group),
			Households: s.Households, Declined: s.Declined}
	}
	b := make([]surveyBDTO, len(res.B))
	for i, s := range res.B {
		b[i] = surveyBDTO{ID: s.ID, Label: s.Label, Yes: s.Counts["yes"], No: s.Counts["no"], YesShare: s.YesShare}
	}
	cc := make([]surveyCDTO, len(res.C))
	for i, s := range res.C {
		cc[i] = surveyCDTO{ID: s.ID, Label: s.Label, Yes: s.Counts["yes"], No: s.Counts["no"], NA: s.Counts["na"]}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"totals": surveyCountsDTO(res.Totals), "by_rw": byRW, "by_student": byStudent, "b": b, "c": cc, "r3_sorting": res.R3Sorting,
	})
}
