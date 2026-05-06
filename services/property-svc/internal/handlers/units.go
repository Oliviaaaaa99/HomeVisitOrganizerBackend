package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/property-svc/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type unitRequest struct {
	UnitLabel     *string  `json:"unit_label,omitempty"`
	UnitType      string   `json:"unit_type"`
	PriceCents    *int64   `json:"price_cents,omitempty"`
	Sqft          *int     `json:"sqft,omitempty"`
	Beds          *int     `json:"beds,omitempty"`
	Baths         *float64 `json:"baths,omitempty"`
	AvailableFrom *string  `json:"available_from,omitempty"` // YYYY-MM-DD
}

type unitResponse struct {
	ID            string   `json:"id"`
	PropertyID    string   `json:"property_id"`
	UnitLabel     *string  `json:"unit_label,omitempty"`
	UnitType      string   `json:"unit_type"`
	PriceCents    *int64   `json:"price_cents,omitempty"`
	Sqft          *int     `json:"sqft,omitempty"`
	Beds          *int     `json:"beds,omitempty"`
	Baths         *float64 `json:"baths,omitempty"`
	AvailableFrom *string  `json:"available_from,omitempty"`
	CreatedAt     string   `json:"created_at"`
}

func toUnitResponse(u *store.Unit) unitResponse {
	var availStr *string
	if u.AvailableFrom != nil {
		s := u.AvailableFrom.UTC().Format("2006-01-02")
		availStr = &s
	}
	return unitResponse{
		ID:            u.ID.String(),
		PropertyID:    u.PropertyID.String(),
		UnitLabel:     u.UnitLabel,
		UnitType:      u.UnitType,
		PriceCents:    u.PriceCents,
		Sqft:          u.Sqft,
		Beds:          u.Beds,
		Baths:         u.Baths,
		AvailableFrom: availStr,
		CreatedAt:     u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// CreateUnit handles POST /v1/properties/{id}/units.
func (h *Handlers) CreateUnit(w http.ResponseWriter, r *http.Request) {
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
	var req unitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.UnitType == "" {
		writeError(w, http.StatusBadRequest, "missing_unit_type", "unit_type is required")
		return
	}

	in := store.UnitInput{
		PropertyID: propertyID,
		UnitLabel:  req.UnitLabel,
		UnitType:   req.UnitType,
		PriceCents: req.PriceCents,
		Sqft:       req.Sqft,
		Beds:       req.Beds,
		Baths:      req.Baths,
	}
	if req.AvailableFrom != nil {
		t, err := time.Parse("2006-01-02", *req.AvailableFrom)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_date", "available_from must be YYYY-MM-DD")
			return
		}
		in.AvailableFrom = &t
	}

	unit, err := h.units.Create(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toUnitResponse(unit))
}
