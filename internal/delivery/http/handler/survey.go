package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/domain/survey"
)

type surveyOptionDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Other bool   `json:"other,omitempty"`
}

type surveyQuestionDTO struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Help     string            `json:"help,omitempty"`
	Type     survey.Type       `json:"type"`
	Required bool              `json:"required"`
	Min      *int              `json:"min,omitempty"`
	Max      *int              `json:"max,omitempty"`
	Options  []surveyOptionDTO `json:"options,omitempty"`
}

type surveySectionDTO struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	Help      string              `json:"help,omitempty"`
	Questions []surveyQuestionDTO `json:"questions"`
}

type surveyFormDTO struct {
	Version     int                `json:"version"`
	ConsentText string             `json:"consent_text"`
	Sections    []surveySectionDTO `json:"sections"`
}

func toSurveyForm(f *survey.Form) surveyFormDTO {
	out := surveyFormDTO{Version: f.Version, ConsentText: f.ConsentText, Sections: []surveySectionDTO{}}
	for _, s := range f.Sections {
		sd := surveySectionDTO{ID: s.ID, Title: s.Title, Help: s.Help, Questions: []surveyQuestionDTO{}}
		for _, q := range s.Questions {
			qd := surveyQuestionDTO{ID: q.ID, Label: q.Label, Help: q.Help, Type: q.Type, Required: q.Required, Min: q.Min, Max: q.Max}
			for _, o := range q.Options {
				qd.Options = append(qd.Options, surveyOptionDTO{ID: o.ID, Label: o.Label, Other: o.Other})
			}
			sd.Questions = append(sd.Questions, qd)
		}
		out.Sections = append(out.Sections, sd)
	}
	return out
}

// SurveyForm menangani GET /api/v1/survey/form (publik, spec-pendataan §5.1): definisi instrumen
// terbaru. FE meng-cache per version.
func SurveyForm(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, toSurveyForm(survey.V1))
}
