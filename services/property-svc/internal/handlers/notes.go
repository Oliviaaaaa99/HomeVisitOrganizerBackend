package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/property-svc/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type noteRequest struct {
	Body string `json:"body"`
}

type noteResponse struct {
	ID         string  `json:"id"`
	PropertyID string  `json:"property_id"`
	UnitID     *string `json:"unit_id,omitempty"`
	Body       string  `json:"body"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func toNoteResponse(n *store.Note) noteResponse {
	var unitID *string
	if n.UnitID != nil {
		s := n.UnitID.String()
		unitID = &s
	}
	return noteResponse{
		ID:         n.ID.String(),
		PropertyID: n.PropertyID.String(),
		UnitID:     unitID,
		Body:       n.Body,
		CreatedAt:  n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  n.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// UpdateNote handles PATCH /v1/notes/{id}.
func (h *Handlers) UpdateNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req noteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "missing_body", "body is required")
		return
	}
	note, err := h.notes.Update(r.Context(), id, userID, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	if note == nil {
		writeError(w, http.StatusNotFound, "not_found", "note not found")
		return
	}
	writeJSON(w, http.StatusOK, toNoteResponse(note))
}

// DeleteNote handles DELETE /v1/notes/{id}.
func (h *Handlers) DeleteNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	deleted, err := h.notes.Delete(r.Context(), id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not_found", "note not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateNote handles POST /v1/properties/{id}/notes — a note about the
// place as a whole (neighborhood, building, owner). unit_id is left NULL.
func (h *Handlers) CreateNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	propertyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if _, ok := h.ownedOrAbort(w, r, propertyID, userID); !ok {
		return
	}
	var req noteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "missing_body", "body is required")
		return
	}
	note, err := h.notes.CreatePropertyNote(r.Context(), propertyID, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toNoteResponse(note))
}

// CreateUnitNote handles POST /v1/units/{id}/notes — a note specific to a
// unit (layout, view, what's broken). property_id is derived from the unit
// in the SQL.
func (h *Handlers) CreateUnitNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	unitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	// Verify the user owns the parent property of this unit.
	const ownQ = `
		SELECT 1 FROM units u JOIN properties p ON p.id = u.property_id
		WHERE u.id = $1 AND p.user_id = $2`
	var dummy int
	if err := h.pg.QueryRow(r.Context(), ownQ, unitID, userID).Scan(&dummy); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "unit not found")
		return
	}
	var req noteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "missing_body", "body is required")
		return
	}
	note, err := h.notes.CreateUnitNote(r.Context(), unitID, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toNoteResponse(note))
}
