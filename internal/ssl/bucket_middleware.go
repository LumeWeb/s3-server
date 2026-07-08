package ssl

import (
	"context"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// BucketCertProvisioner is called when a bucket is created to eagerly
// provision a TLS certificate for virtual-hosted-style access.
type BucketCertProvisioner interface {
	ProvisionCert(ctx context.Context, domain string) error
}

// BucketCreateMiddleware wraps an http.Handler (the s3d S3 handler) and
// intercepts successful PUT bucket creation requests. When SSL is managed
// and host bases are configured, it provisions a certificate for
// {bucket}.{hostbase} in a background goroutine.
//
// Detection: PUT request where URL path is /{bucket} with no object key
// (single path segment after the leading slash). Response status 200 means
// the bucket was created successfully.
func BucketCreateMiddleware(next http.Handler, prov BucketCertProvisioner, hostBases []string, log *zap.Logger) http.Handler {
	if prov == nil || len(hostBases) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			next.ServeHTTP(w, r)
			return
		}

		// Check if this is a bucket creation: path is /{bucket} (single segment)
		path := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(path, "/", 2)
		bucket := parts[0]

		// Not a bucket creation if there's an object key or no bucket name
		if bucket == "" || len(parts) == 2 && parts[1] != "" {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap ResponseWriter to capture the status code
		rw := &statusCaptureWriter{ResponseWriter: w}
		next.ServeHTTP(rw, r)

		// Bucket created successfully — provision certs in background
		// Go's default status code is 200 if WriteHeader was never called.
		if rw.status == http.StatusOK || rw.status == 0 {
			for _, base := range hostBases {
				domain := bucket + "." + base
				go func(d string) {
					if err := prov.ProvisionCert(context.Background(), d); err != nil {
						log.Warn("failed to provision bucket cert", zap.String("domain", d), zap.Error(err))
					}
				}(domain)
			}
		}
	})
}

// statusCaptureWriter wraps http.ResponseWriter to capture the status code.
type statusCaptureWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCaptureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
