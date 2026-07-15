package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/SiaFoundation/s3d/s3"
	"go.lumeweb.com/s3-server/internal/backend"
)

// AdminStatsFetcher fetches upload stats from the s3d admin handler and
// account info from the backend's AccountClient. It is safe for concurrent
// use: each call creates its own recorder.
type AdminStatsFetcher struct {
	AdminHandler  http.Handler
	AccountClient func() backend.AccountClient
}

// FetchStats calls the s3d admin /stats/uploads endpoint and the account
// client, returning the merged data as a StatsEvent for SSE publishing.
func (f *AdminStatsFetcher) FetchStats(ctx context.Context) (*StatsEvent, error) {
	if f.AdminHandler == nil {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/stats/uploads", nil)
	if err != nil {
		return nil, err
	}
	rec := &captureRecorder{header: make(http.Header)}
	f.AdminHandler.ServeHTTP(rec, req)
	if rec.code == 0 {
		rec.code = http.StatusOK
	}
	if rec.code >= 300 {
		return nil, nil
	}

	var s3stats s3.UploadStats
	if err := json.Unmarshal(rec.body.Bytes(), &s3stats); err != nil {
		return nil, err
	}

	evt := &StatsEvent{
		PendingObjects:   s3stats.PendingObjects,
		PendingSize:      s3stats.PendingSize,
		UploadedObjects:  s3stats.UploadedObjects,
		UploadedSize:     s3stats.UploadedSize,
		UnpinnedObjects:  s3stats.UnpinnedObjects,
		FailedUploads:    s3stats.FailedUploads,
		OrphanedObjects:  s3stats.OrphanedObjects,
		MultipartUploads: s3stats.MultipartUploads,
	}

	// Fetch account info: non-fatal. If the client is nil or the call
	// fails, the account fields stay zero-valued and the UI hides them.
	if f.AccountClient != nil {
		if ac := f.AccountClient(); ac != nil {
			if info, err := ac.Account(ctx); err == nil {
				evt.AccountMaxPinnedData = info.MaxPinnedData
				evt.AccountRemainingStorage = info.RemainingStorage
				evt.AccountPinnedData = info.PinnedData
				evt.AccountPinnedSize = info.PinnedSize
				evt.AccountReady = info.Ready
			}
		}
	}

	return evt, nil
}

// captureRecorder is a minimal http.ResponseWriter for capturing admin responses.
type captureRecorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (r *captureRecorder) Header() http.Header         { return r.header }
func (r *captureRecorder) WriteHeader(code int)        { r.code = code }
func (r *captureRecorder) Write(p []byte) (int, error) { return r.body.Write(p) }
