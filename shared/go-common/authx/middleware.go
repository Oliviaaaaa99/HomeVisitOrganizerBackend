package authx

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const ctxKeyUserID ctxKey = iota

// Middleware verifies the Authorization header and injects the user ID into
// the request context. Requests without a valid bearer token receive 401.
func Middleware(v *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				writeUnauthorized(w, "missing bearer token")
				return
			}
			tok := strings.TrimPrefix(authz, "Bearer ")
			claims, err := v.Verify(tok)
			if err != nil {
				writeUnauthorized(w, "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUserID, claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFrom returns the authenticated user ID stored in context. Empty
// string means the request was not authenticated.
func UserIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyUserID).(string); ok {
		return v
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","detail":"` + msg + `"}`))
}
