package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/property-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type propertyResponse struct {
	ID        string   `json:"id"`
	UserID    string   `json:"user_id"`
	Address   string   `json:"address"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	Kind      string   `json:"kind"`
	SourceURL *string  `json:"source_url,omitempty"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func toResponse(p *store.Property) propertyResponse {
	return propertyResponse{
		ID:        p.ID.String(),
		UserID:    p.UserID.String(),
		Address:   p.Address,
		Latitude:  p.Latitude,
		Longitude: p.Longitude,
		Kind:      p.Kind,
		SourceURL: p.SourceURL,
		Status:    p.Status,
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

type createRequest struct {
	Address   string   `json:"address"`
	Kind      string   `json:"kind"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	SourceURL string   `json:"source_url,omitempty"`
}

// CreateProperty handles POST /v1/properties.
func (h *Handlers) CreateProperty(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.Address == "" {
		writeError(w, http.StatusBadRequest, "missing_address", "address is required")
		return
	}
	if req.Kind != "rental" && req.Kind != "for_sale" {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be 'rental' or 'for_sale'")
		return
	}

	prop, err := h.properties.Create(r.Context(), store.CreateInput{
		UserID:    userID,
		Address:   req.Address,
		Kind:      req.Kind,
		SourceURL: req.SourceURL,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(prop))
}

// ListProperties handles GET /v1/properties.
//
// Query: ?status=&kind=&page_size=&page=
func (h *Handlers) ListProperties(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 25
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 0 {
		page = 0
	}

	props, err := h.properties.List(r.Context(), store.ListInput{
		UserID: userID,
		Status: q.Get("status"),
		Kind:   q.Get("kind"),
		Limit:  pageSize,
		Offset: page * pageSize,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	out := make([]propertyResponse, 0, len(props))
	for _, p := range props {
		out = append(out, toResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "page": page, "page_size": pageSize})
}

type propertyDetail struct {
	propertyResponse
	Units []unitResponse `json:"units"`
	Notes []noteResponse `json:"notes"`
}

// GetProperty handles GET /v1/properties/{id}.
func (h *Handlers) GetProperty(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	prop, err := h.properties.FindOwned(r.Context(), id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup_failed", err.Error())
		return
	}
	if prop == nil {
		writeError(w, http.StatusNotFound, "not_found", "property not found")
		return
	}
	units, err := h.units.ListByProperty(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "units_failed", err.Error())
		return
	}
	notes, err := h.notes.ListByProperty(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "notes_failed", err.Error())
		return
	}
	resp := propertyDetail{propertyResponse: toResponse(prop)}
	for _, u := range units {
		resp.Units = append(resp.Units, toUnitResponse(u))
	}
	for _, n := range notes {
		resp.Notes = append(resp.Notes, toNoteResponse(n))
	}
	if resp.Units == nil {
		resp.Units = []unitResponse{}
	}
	if resp.Notes == nil {
		resp.Notes = []noteResponse{}
	}
	writeJSON(w, http.StatusOK, resp)
}

type updateRequest struct {
	Status string `json:"status"`
}

// UpdateProperty handles PATCH /v1/properties/{id} — currently only status update.
func (h *Handlers) UpdateProperty(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	switch req.Status {
	case "toured", "shortlisted", "rejected", "archived":
	default:
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be toured/shortlisted/rejected/archived")
		return
	}

	prop, err := h.properties.UpdateStatus(r.Context(), id, userID, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	if prop == nil {
		writeError(w, http.StatusNotFound, "not_found", "property not found")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(prop))
}

// ArchiveProperty handles DELETE /v1/properties/{id} — soft-archive.
func (h *Handlers) ArchiveProperty(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	archived, err := h.properties.Archive(r.Context(), id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "archive_failed", err.Error())
		return
	}
	if !archived {
		writeError(w, http.StatusNotFound, "not_found", "property not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authedUser parses the user UUID from the JWT subject. Returns (uuid, true) on
// success, or writes 401/400 and returns (nil, false).
func authedUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	sub := authx.UserIDFrom(r.Context())
	if sub == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing user")
		return uuid.Nil, false
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_subject", err.Error())
		return uuid.Nil, false
	}
	return id, true
}

// ownedOrAbort returns the property if owned by user, or writes 404 / 500.
func (h *Handlers) ownedOrAbort(w http.ResponseWriter, r *http.Request, propertyID, userID uuid.UUID) (*store.Property, bool) {
	prop, err := h.properties.FindOwned(r.Context(), propertyID, userID)
	if err != nil {
		if errors.Is(err, errNotFound) { // unused for now
			writeError(w, http.StatusNotFound, "not_found", "property not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "lookup_failed", err.Error())
		return nil, false
	}
	if prop == nil {
		writeError(w, http.StatusNotFound, "not_found", "property not found")
		return nil, false
	}
	return prop, true
}

var errNotFound = errors.New("not found")
