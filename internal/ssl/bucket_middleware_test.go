package ssl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.lumeweb.com/s3-server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingProvisioner tracks ProvisionCert calls via an atomic counter.
type countingProvisioner struct {
	calls  atomic.Int32
	mu     sync.Mutex
	domain string
	err    error
}

func (p *countingProvisioner) ProvisionCert(_ context.Context, domain string) error {
	p.mu.Lock()
	p.domain = domain
	p.mu.Unlock()
	p.calls.Add(1)
	return p.err
}

func (p *countingProvisioner) Domain() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.domain
}

func TestBucketCreateMiddleware_NotPut(t *testing.T) {
	prov := &countingProvisioner{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(0), prov.calls.Load())
}

func TestBucketCreateMiddleware_ObjectPut(t *testing.T) {
	prov := &countingProvisioner{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	// PUT with object path — not a bucket creation
	req := httptest.NewRequest(http.MethodPut, "/mybucket/myobject", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(0), prov.calls.Load())
}

func TestBucketCreateMiddleware_BucketCreated(t *testing.T) {
	prov := &countingProvisioner{}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool {
		return prov.calls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "mybucket.s3.example.com", prov.Domain())
}

func TestBucketCreateMiddleware_BucketCreated_MultipleHostBases(t *testing.T) {
	prov := &countingProvisioner{}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com", "storage.example.org"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool {
		return prov.calls.Load() == 2
	}, time.Second, 10*time.Millisecond)
}

func TestBucketCreateMiddleware_BucketCreationFailed(t *testing.T) {
	prov := &countingProvisioner{}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict) // BucketAlreadyExists
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, int32(0), prov.calls.Load())
}

func TestBucketCreateMiddleware_NoProvisioner(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// nil provisioner — middleware should pass through
	h, _ := BucketCreateMiddleware(inner, nil, []string{"s3.example.com"}, testutil.NewTestLogger())

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBucketCreateMiddleware_NoHostBases(t *testing.T) {
	prov := &countingProvisioner{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, _ := BucketCreateMiddleware(inner, prov, nil, testutil.NewTestLogger())

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(0), prov.calls.Load())
}

func TestBucketCreateMiddleware_EmptyBucketName(t *testing.T) {
	prov := &countingProvisioner{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	// Root path — not a bucket creation
	req := httptest.NewRequest(http.MethodPut, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(0), prov.calls.Load())
}

func TestBucketCreateMiddleware_ProvisionError(t *testing.T) {
	prov := &countingProvisioner{err: assert.AnError}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// ProvisionCert was called even though it returned an error
	require.Eventually(t, func() bool {
		return prov.calls.Load() == 1
	}, time.Second, 10*time.Millisecond)
}

func TestBucketCreateMiddleware_DefaultStatusOK(t *testing.T) {
	prov := &countingProvisioner{}

	// Handler doesn't call WriteHeader — Go defaults to 200
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) //nolint:errcheck
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Default status is 0 (not explicitly set) — treated as 200 OK
	require.Eventually(t, func() bool {
		return prov.calls.Load() == 1
	}, time.Second, 10*time.Millisecond)
}
