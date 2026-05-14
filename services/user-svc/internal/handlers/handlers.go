// Package handlers wires HTTP routes for user-svc.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/clients"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/service"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	pg    *pgxpool.Pool
	rdb   *redis.Client
	auth  *service.Auth
	users *store.Users
	s3    *clients.S3 // optional — nil disables avatar endpoints
}

// New constructs a Handlers from its dependencies. Pass s3=nil to disable
// avatar endpoints (e.g. local runs without LocalStack).
func New(pg *pgxpool.Pool, rdb *redis.Client, auth *service.Auth, users *store.Users, s3 *clients.S3) *Handlers {
	return &Handlers{pg: pg, rdb: rdb, auth: auth, users: users, s3: s3}
}

// Health is the liveness probe — returns 200 unconditionally.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready is the readiness probe — verifies Postgres and Redis connectivity.
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"postgres": "ok"}
	status := http.StatusOK

	if err := h.pg.Ping(r.Context()); err != nil {
		resp["postgres"] = err.Error()
		status = http.StatusServiceUnavailable
	}
	// Redis is optional. Only report on it when a client was wired up.
	if h.rdb != nil {
		if err := h.rdb.Ping(r.Context()).Err(); err != nil {
			resp["redis"] = err.Error()
			status = http.StatusServiceUnavailable
		} else {
			resp["redis"] = "ok"
		}
	} else {
		resp["redis"] = "disabled"
	}
	writeJSON(w, status, resp)
}

type meResponse struct {
	ID          string  `json:"id"`
	Provider    string  `json:"provider"`
	EmailHash   *string `json:"email_hash,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// avatarURL returns a short-lived presigned GET URL for the user's avatar, or
// nil if they have no avatar, S3 isn't configured, or presigning fails. We
// treat presign failure as "no avatar" rather than failing the whole request
// — it lets /me keep working when the bucket is briefly unreachable.
func (h *Handlers) avatarURL(ctx context.Context, user *store.User) *string {
	if user.AvatarS3Key == nil || *user.AvatarS3Key == "" || h.s3 == nil {
		return nil
	}
	u, err := h.s3.PresignGet(ctx, *user.AvatarS3Key)
	if err != nil {
		slog.WarnContext(ctx, "presign avatar get failed", "err", err, "key", *user.AvatarS3Key)
		return nil
	}
	return &u
}

func (h *Handlers) toMeResponse(ctx context.Context, user *store.User) meResponse {
	return meResponse{
		ID:          user.ID.String(),
		Provider:    user.Provider,
		EmailHash:   user.EmailHash,
		AvatarURL:   h.avatarURL(ctx, user),
		DisplayName: user.DisplayName,
		CreatedAt:   user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// Me returns the authenticated user. Requires auth middleware in front.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	user, err := h.users.FindByID(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "lookup_failed"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user_gone"})
		return
	}
	writeJSON(w, http.StatusOK, h.toMeResponse(r.Context(), user))
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
}

// UpdateMe handles PATCH /v1/users/me — currently only display_name is
// editable. Empty string clears the name (column → NULL).
func (h *Handlers) UpdateMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if req.DisplayName == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no_fields"})
		return
	}
	user, err := h.users.SetDisplayName(r.Context(), uid, *req.DisplayName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save_failed"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user_gone"})
		return
	}
	writeJSON(w, http.StatusOK, h.toMeResponse(r.Context(), user))
}

type avatarPresignResponse struct {
	S3Key     string    `json:"s3_key"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PresignAvatar issues a one-shot signed PUT URL for the avatar object.
// The client uploads bytes directly to S3, then calls CommitAvatar.
func (h *Handlers) PresignAvatar(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	if h.s3 == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "avatars_disabled"})
		return
	}
	// One key per upload attempt — old keys aren't reused, so a partially-uploaded
	// S3 object never overwrites the active avatar before commit.
	key := path.Join("avatars", uid.String(), uuid.New().String()+".jpg")
	url, err := h.s3.PresignPut(r.Context(), key)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "presign_failed"})
		return
	}
	writeJSON(w, http.StatusOK, avatarPresignResponse{
		S3Key:     key,
		URL:       url,
		ExpiresAt: time.Now().UTC().Add(h.s3.PresignTTL()),
	})
}

type avatarCommitRequest struct {
	S3Key string `json:"s3_key"`
}

// CommitAvatar verifies the uploaded object exists, swaps the user's avatar
// pointer, and best-effort deletes the old object so the bucket doesn't grow
// unbounded as users replace their avatar.
func (h *Handlers) CommitAvatar(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	if h.s3 == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "avatars_disabled"})
		return
	}
	var req avatarCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	expectedPrefix := "avatars/" + uid.String() + "/"
	if req.S3Key == "" || len(req.S3Key) < len(expectedPrefix) || req.S3Key[:len(expectedPrefix)] != expectedPrefix {
		// Reject anything that doesn't look like a key we just issued for this
		// caller — don't let a user point their avatar at someone else's object.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_key"})
		return
	}
	if err := h.s3.HeadObject(r.Context(), req.S3Key); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "object_missing"})
		return
	}
	oldKey, err := h.users.SetAvatarKey(r.Context(), uid, req.S3Key)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save_failed"})
		return
	}
	if oldKey != nil && *oldKey != "" && *oldKey != req.S3Key {
		// Best-effort orphan cleanup. Failures here just leak a small object
		// to be cleaned up by the M4 retention sweeper later.
		_ = deleteBackground(r.Context(), h.s3, *oldKey)
	}
	avatarURL, err := h.s3.PresignGet(r.Context(), req.S3Key)
	if err != nil {
		// The object exists (we HeadObject'd above) and the DB write succeeded,
		// so the avatar is committed — we just can't sign a URL right now. Tell
		// the client the key; the next /me call will retry the presign.
		slog.WarnContext(r.Context(), "presign avatar get failed after commit", "err", err)
		writeJSON(w, http.StatusOK, map[string]string{"s3_key": req.S3Key})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"s3_key":     req.S3Key,
		"avatar_url": avatarURL,
	})
}

// DeleteAvatar clears the avatar pointer and removes the S3 object.
func (h *Handlers) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	if h.s3 == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "avatars_disabled"})
		return
	}
	oldKey, err := h.users.SetAvatarKey(r.Context(), uid, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save_failed"})
		return
	}
	if oldKey != nil && *oldKey != "" {
		_ = deleteBackground(r.Context(), h.s3, *oldKey)
	}
	w.WriteHeader(http.StatusNoContent)
}

func authedUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	uidStr := authx.UserIDFrom(r.Context())
	if uidStr == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return uuid.Nil, false
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_subject"})
		return uuid.Nil, false
	}
	return uid, true
}

func deleteBackground(parent context.Context, s3 *clients.S3, key string) error {
	// Detach from request lifecycle so a slow S3 round trip doesn't block the
	// HTTP response. 5s is enough; any longer and the orphan sweep can pick it up.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	if err := s3.DeleteObject(ctx, key); err != nil {
		return fmt.Errorf("orphan delete %q: %w", key, err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
