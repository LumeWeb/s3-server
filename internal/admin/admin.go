package admin

import (
	"net/http"

	"github.com/SiaFoundation/s3d/s3"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.uber.org/zap"
)

// Handler returns an http.Handler that proxies admin/monitoring requests to the
// s3d admin API. It returns 503 when the s3d backend is not initialized.
func Handler(manager *backend.Manager, log *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := manager.Backend()
		if b == nil {
			http.Error(w, "s3d backend not initialized", http.StatusServiceUnavailable)
			return
		}

		admin := s3.NewAdmin(b.S3Backend(), s3.WithLogger(log.Named("admin")))
		admin.ServeHTTP(w, r)
	})
}
