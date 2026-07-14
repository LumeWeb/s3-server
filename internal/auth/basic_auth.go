package auth

import (
	"crypto/subtle"
	"net/http"

	"go.lumeweb.com/s3-server/internal/store"
)

// BasicAuth wraps an http.Handler with HTTP Basic authentication.
// The username must be "admin" and the password is validated against
// the store's admin password hash. This is intended for server-to-server
// access to the s3d admin API endpoints (prometheus, stats, system).
func BasicAuth(s store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="s3-server admin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(user), []byte("admin")) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="s3-server admin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !s.ValidateAdminPassword(pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="s3-server admin"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
