package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type presignRequest struct {
	Items []service.PresignItem `json:"items"`
}

type presignResponse struct {
	Uploads []service.PresignedUpload `json:"uploads"`
}

// Presign handles POST /v1/units/{id}/media:presign.
func (h *Handlers) Presign(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	unitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req presignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	uploads, err := h.media.Presign(r.Context(), unitID, userID, req.Items)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnitNotOwned):
			writeError(w, http.StatusNotFound, "unit_not_found", "")
		case errors.Is(err, service.ErrInvalidType):
			writeError(w, http.StatusBadRequest, "invalid_type", err.Error())
		case errors.Is(err, service.ErrQuotaExceeded):
			writeError(w, http.StatusUnprocessableEntity, "quota_exceeded", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "presign_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, presignResponse{Uploads: uploads})
}

type commitRequest struct {
	Items []service.CommitItem `json:"items"`
}

type commitResponse struct {
	Committed []service.Committed `json:"committed"`
}

// Commit handles POST /v1/units/{id}/media:commit.
func (h *Handlers) Commit(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	unitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req commitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	committed, err := h.media.Commit(r.Context(), unitID, userID, req.Items)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnitNotOwned):
			writeError(w, http.StatusNotFound, "unit_not_found", "")
		case errors.Is(err, service.ErrInvalidType):
			writeError(w, http.StatusBadRequest, "invalid_type", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "commit_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, commitResponse{Committed: committed})
}

// Delete handles DELETE /v1/media/{id} — soft-delete only.
func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	mediaID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	deleted, err := h.media.SoftDelete(r.Context(), mediaID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not_found", "media not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
