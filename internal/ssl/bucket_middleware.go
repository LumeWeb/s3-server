package ssl

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// BucketCertProvisioner is called when a bucket is created to eagerly
// provision a TLS certificate for virtual-hosted-style access.
type BucketCertProvisioner interface {
	ProvisionCert(ctx context.Context, domain string) error
}

// maxProvisionWorkers limits concurrent ACME provisioning goroutines.
const maxProvisionWorkers = 4

// BucketCreateMiddleware wraps an http.Handler (the s3d S3 handler) and
// intercepts successful PUT bucket creation requests. When SSL is managed
// and host bases are configured, it provisions a certificate for
// {bucket}.{hostbase} via a bounded worker pool.
//
// The pool uses a context scoped to the provisioner's lifetime. Call
// the returned cancel func during shutdown to abort in-flight ACME I/O.
//
// Detection: PUT request where URL path is /{bucket} with no object key
// (single path segment after the leading slash). Response status 200 means
// the bucket was created successfully.
func BucketCreateMiddleware(next http.Handler, prov BucketCertProvisioner, hostBases []string, log *zap.Logger) (http.Handler, context.CancelFunc) {
	if prov == nil || len(hostBases) == 0 {
		return next, func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	provisionCh := make(chan string, len(hostBases)*4)

	var wg sync.WaitGroup
	for range maxProvisionWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case domain, ok := <-provisionCh:
					if !ok {
						return
					}
					if err := prov.ProvisionCert(ctx, domain); err != nil {
						if ctx.Err() != nil {
							return
						}
						log.Warn("failed to provision bucket cert", zap.String("domain", domain), zap.Error(err))
					}
				}
			}
		}()
	}

	shutdown := func() {
		cancel()
		wg.Wait()
		// close(provisionCh) omitted: workers exit via ctx cancellation;
		// closing would race with concurrent sends from in-flight HTTP handlers.
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// Bucket created successfully: enqueue cert provisioning
		// Go's default status code is 200 if WriteHeader was never called.
		if rw.status == http.StatusOK || rw.status == 0 {
			if ctx.Err() != nil {
				return
			}
			for _, base := range hostBases {
				domain := bucket + "." + base
				select {
				case provisionCh <- domain:
				default:
					log.Warn("provision queue full, skipping", zap.String("domain", domain))
				}
			}
		}
	})

	return handler, shutdown
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
