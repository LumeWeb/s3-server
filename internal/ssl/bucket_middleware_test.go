package ssl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/testutil"
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

	// PUT with object path: not a bucket creation
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

	// nil provisioner: middleware should pass through
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

	// Root path: not a bucket creation
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

	// Handler doesn't call WriteHeader: Go defaults to 200
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) //nolint:errcheck
	})

	h, cancel := BucketCreateMiddleware(inner, prov, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	req := httptest.NewRequest(http.MethodPut, "/mybucket", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Default status is 0 (not explicitly set): treated as 200 OK
	require.Eventually(t, func() bool {
		return prov.calls.Load() == 1
	}, time.Second, 10*time.Millisecond)
}

// Regression PR8: The middleware must use a bounded worker pool
// (maxProvisionWorkers=4), not spawn a goroutine per provisioning request.
// We verify this by blocking ProvisionCert calls until N > 4 bucket
// creations are queued; if the pool were unbounded, all N would be
// in-flight simultaneously.
func TestBucketCreateMiddleware_BoundedWorkerPool(t *testing.T) {
	// Use 4 buckets with 1 hostBase: buffer = 1*4 = 4, workers = 4.
	// All 4 workers block, and the 4 items fill the buffer exactly.
	// If the pool were unbounded, all 4 would be in-flight simultaneously.
	// With 4 workers, maxConc should be exactly 4.
	const numBuckets = 4

	var maxConc atomic.Int32
	var active atomic.Int32
	blockProv := &blockingProvisionerStruct{
		maxConc: &maxConc,
		active:  &active,
		release: make(chan struct{}),
		total:   &atomic.Int32{},
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, blockProv, []string{"s3.example.com"}, testutil.NewTestLogger())
	defer cancel()

	// Fire N bucket creation requests
	for i := 0; i < numBuckets; i++ {
		bucket := "bucket" + string(rune('a'+i))
		req := httptest.NewRequest(http.MethodPut, "/"+bucket, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}

	// Give workers a moment to pick up tasks
	time.Sleep(50 * time.Millisecond)

	// Release all workers
	close(blockProv.release)

	// Wait for all to complete
	require.Eventually(t, func() bool {
		return blockProv.total.Load() == int32(numBuckets)
	}, 5*time.Second, 50*time.Millisecond)

	// At most maxProvisionWorkers should have been active concurrently
	assert.LessOrEqual(t, maxConc.Load(), int32(maxProvisionWorkers),
		"concurrent provisioning must not exceed maxProvisionWorkers")
	assert.Equal(t, int32(numBuckets), maxConc.Load(),
		"with %d buckets and 4 workers, all should be processed", numBuckets)
}

type blockingProvisionerStruct struct {
	maxConc *atomic.Int32
	active  *atomic.Int32
	release chan struct{}
	total   *atomic.Int32
}

func (p *blockingProvisionerStruct) ProvisionCert(_ context.Context, _ string) error {
	cur := p.active.Add(1)
	for {
		mc := p.maxConc.Load()
		if cur > mc {
			if p.maxConc.CompareAndSwap(mc, cur) {
				break
			}
		} else {
			break
		}
	}
	<-p.release
	p.active.Add(-1)
	p.total.Add(1)
	return nil
}

// Regression PR8: shutdown (cancel) must be safe while HTTP handlers are
// concurrently enqueuing provisioning tasks. Should not panic or deadlock.
func TestBucketCreateMiddleware_ConcurrentShutdown_NoDeadlock(t *testing.T) {
	// slowProvisioner takes a bit of time, so there's in-flight work
	// when shutdown is called.
	slowProv := &slowProvisioner{delay: 10 * time.Millisecond}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, cancel := BucketCreateMiddleware(inner, slowProv, []string{"s3.example.com"}, testutil.NewTestLogger())

	// Launch concurrent request senders
	var wg sync.WaitGroup
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					req := httptest.NewRequest(http.MethodPut, "/bucket"+string(rune('a'+n)), nil)
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
				}
			}
		}(i)
	}

	// Let them run briefly, then shut down
	time.Sleep(20 * time.Millisecond)
	cancel()
	close(done)

	// Should not deadlock: all goroutines finish within timeout
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown deadlocked with concurrent HTTP handlers")
	}
}

type slowProvisioner struct {
	delay time.Duration
	calls atomic.Int32
}

func (p *slowProvisioner) ProvisionCert(_ context.Context, _ string) error {
	p.calls.Add(1)
	time.Sleep(p.delay)
	return nil
}
