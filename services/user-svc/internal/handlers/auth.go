package handlers

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/clients"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/service"
)

type exchangeRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"id_token"`
	Passcode string `json:"passcode,omitempty"`
}

// Exchange handles POST /v1/auth/exchange.
//
// Body: { "provider": "apple"|"google"|"dev", "id_token": "<provider id_token>" }
// Returns: { access_token, refresh_token, expires_at, refresh_expires_at, user_id }
func (h *Handlers) Exchange(w http.ResponseWriter, r *http.Request) {
	var req exchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if req.Provider == "" || req.IDToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_fields"})
		return
	}

	pair, err := h.auth.Exchange(r.Context(), req.Provider, req.IDToken, req.Passcode, r.UserAgent(), clientIP(r))
	if err != nil {
		if errors.Is(err, clients.ErrInvalidToken) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_id_token"})
			return
		}
		if errors.Is(err, service.ErrInvalidCredentials) {
			// Same error message whether the email is unknown or the passcode
			// is wrong — don't help attackers enumerate valid emails.
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":  "invalid_credentials",
				"detail": "Email or passcode is incorrect.",
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exchange_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh handles POST /v1/auth/refresh.
func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_refresh_token"})
		return
	}

	pair, err := h.auth.Refresh(r.Context(), req.RefreshToken, r.UserAgent(), clientIP(r))
	if err != nil {
		if errors.Is(err, service.ErrInvalidRefreshToken) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_refresh_token"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refresh_failed"})
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func clientIP(r *http.Request) string {
	// Best-effort: behind ALB X-Forwarded-For will be set; otherwise RemoteAddr.
	// We must return a value Postgres INET will accept (no brackets, no port).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF can be a comma-separated list — take the first.
		for i, c := range xff {
			if c == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
