package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/usecase"
)

// Editor dipenuhi oleh *usecase.Edit.
type Editor interface {
	Edit(ctx context.Context, in usecase.EditInput) (*usecase.EditOutput, error)
}

type EditHandler struct{ editor Editor }

func NewEditHandler(e Editor) *EditHandler { return &EditHandler{editor: e} }

type editRequest struct {
	Version     int                        `json:"version"`
	FormVersion int                        `json:"form_version"`
	Answers     map[string]json.RawMessage `json:"answers"`
	Photos      []submitPhotoDTO           `json:"photos"`
}

type editResponse struct {
	SubmissionID uuid.UUID `json:"submission_id"`
	Version      int       `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Put menangani PUT /api/v1/submissions/{id} (spec §5.4). Isi request adalah keadaan akhir
// (bukan diff). Field tak dikenal, termasuk client_request_id, ditolak.
func (h *EditHandler) Put(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	var req editRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	in := usecase.EditInput{SubmissionID: id, Caller: callerFrom(c), Version: req.Version, Photos: toSubmitPhotos(req.Photos),
		FormVersion: req.FormVersion, Answers: req.Answers}
	out, err := h.editor.Edit(c.Request.Context(), in)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, editResponse{SubmissionID: out.SubmissionID, Version: out.Version, UpdatedAt: out.UpdatedAt.UTC()})
}
