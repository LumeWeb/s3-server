package sse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/backend"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/testutil"
	"go.sia.tech/core/types"
)

// safeRecorder wraps httptest.ResponseRecorder with a mutex so that
// concurrent SSE goroutines can write while the test reads the body.
type safeRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *safeRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(p)
}

func (r *safeRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.WriteHeader(code)
}

func (r *safeRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Body.String()
}

// stubS3DStore implements just enough of backend.S3DStore for broker tests.
type stubKeyStore struct {
	keys []backend.AccessKeyInfo
}

func (s *stubKeyStore) ListAccessKeys(userName *string) ([]backend.AccessKeyInfo, error) {
	return s.keys, nil
}

func (s *stubKeyStore) AppKey() (types.PrivateKey, string, error)    { return types.PrivateKey{}, "", nil }
func (s *stubKeyStore) SetAppKey(types.PrivateKey, string) error     { return nil }
func (s *stubKeyStore) CreateUser(string) error                      { return nil }
func (s *stubKeyStore) DeleteUser(string) error                      { return nil }
func (s *stubKeyStore) ListUsers() ([]string, error)                 { return nil, nil }
func (s *stubKeyStore) CreateAccessKey(string, string, string) error { return nil }
func (s *stubKeyStore) DeleteAccessKey(string) error                 { return nil }
func (s *stubKeyStore) Close() error                                 { return nil }

func newTestBroker(t *testing.T) (*Broker, *storeMocks.MockStore) {
	mockStore := storeMocks.NewMockStore(t)
	log := testutil.NewTestLogger()
	keyStore := func() backend.S3DStore { return &stubKeyStore{keys: []backend.AccessKeyInfo{{AccessKeyID: "k1"}}} }
	return NewBroker(mockStore, nil, keyStore, log), mockStore
}

func TestBroker_PublishDashboard(t *testing.T) {
	b, _ := newTestBroker(t)

	evt := DashboardEvent{
		S3Status: "running",
		KeyCount: 2,
		Version:  "0.2.0",
		Uptime:   "10s",
	}
	err := b.PublishDashboard(evt)
	require.NoError(t, err)
}

func TestBroker_ServeHTTP(t *testing.T) {
	b, _ := newTestBroker(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		b.ServeHTTP(rec, req)
	}()

	// give the SSE connection time to establish
	time.Sleep(100 * time.Millisecond)

	// publish an event
	err := b.PublishDashboard(DashboardEvent{
		S3Status: "running",
		KeyCount: 1,
		Version:  "0.2.0",
	})
	require.NoError(t, err)

	// give time for the event to be written
	time.Sleep(100 * time.Millisecond)

	// close the connection
	cancel()
	<-done
}

func TestBroker_StatusLoop(t *testing.T) {
	b, _ := newTestBroker(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.StartStatusLoop(ctx, "0.2.0")

	// let it publish at least once
	time.Sleep(100 * time.Millisecond)
	cancel()
}

func TestBroker_Shutdown(t *testing.T) {
	b, _ := newTestBroker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := b.Shutdown(ctx)
	require.NoError(t, err)
}

func TestBroker_SSEWireFormat(t *testing.T) {
	b, _ := newTestBroker(t)

	ctx, cancel := context.WithCancel(context.Background())

	rec := &safeRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.ServeHTTP(rec, req)
	}()

	time.Sleep(100 * time.Millisecond)

	b.PublishDashboard(DashboardEvent{ //nolint:errcheck
		S3Status: "stopped",
		KeyCount: 0,
		Version:  "0.2.0",
	})

	time.Sleep(100 * time.Millisecond)

	// Shutdown the broker to ensure all subscriber goroutines finish
	// before we read the body: avoids race on httptest.ResponseRecorder.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	require.NoError(t, b.Shutdown(shutdownCtx))

	cancel()
	<-done

	body := rec.BodyString()
	assert.Contains(t, body, ": connected")
	assert.Contains(t, body, "event: dashboard")
	assert.Contains(t, body, `"s3_status":"stopped"`)
}

func TestBroker_MultipleClients(t *testing.T) {
	b, _ := newTestBroker(t)

	ctx, cancel := context.WithCancel(context.Background())

	rec1 := &safeRecorder{ResponseRecorder: httptest.NewRecorder()}
	req1 := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec2 := &safeRecorder{ResponseRecorder: httptest.NewRecorder()}
	req2 := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)

	done1 := make(chan struct{})
	done2 := make(chan struct{})

	go func() {
		defer close(done1)
		b.ServeHTTP(rec1, req1)
	}()
	go func() {
		defer close(done2)
		b.ServeHTTP(rec2, req2)
	}()

	time.Sleep(100 * time.Millisecond)

	err := b.PublishDashboard(DashboardEvent{S3Status: "running"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Shutdown the broker to ensure all subscriber goroutines finish
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	require.NoError(t, b.Shutdown(shutdownCtx))

	cancel()
	<-done1
	<-done2

	assert.Contains(t, rec1.BodyString(), "event: dashboard")
	assert.Contains(t, rec2.BodyString(), "event: dashboard")
}

func TestBroker_DropOldestBuffer(t *testing.T) {
	b, _ := newTestBroker(t)

	// publish many events without any subscriber: should not block or panic
	for i := 0; i < 100; i++ {
		b.PublishDashboard(DashboardEvent{KeyCount: i}) //nolint:errcheck
	}
}

func TestDashboardEventJSON(t *testing.T) {
	evt := DashboardEvent{
		S3Status: "running",
		KeyCount: 2,
		Version:  "0.2.0",
		Uptime:   "1h30m0s",
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"s3_status":"running"`)
	assert.Contains(t, string(data), `"key_count":2`)
}

// Regression PR10: Concurrent SetStatsFetcher/SetInitError (writers) and
// publishStatus/publishStats (readers) must not race. The RWMutex on the
// Broker prevents a data race between setting and reading the fetcher/initError
// function pointers. Run with -race to detect.
func TestBroker_ConcurrentSetAndPublish_NoRace(t *testing.T) {
	b, _ := newTestBroker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	b.StartStatusLoop(ctx, "test-version")

	var wg sync.WaitGroup

	// Concurrently set and unset the stats fetcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			b.SetStatsFetcher(&stubStatsFetcher{})
		}
	}()

	// Concurrently set and unset the initError function
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			b.SetInitError(func() string { return "test error" })
		}
	}()

	wg.Wait()

	// Wait for the status loop to finish (ctx expires)
	cancel()

	// No panic / race = pass
}

// stubStatsFetcher implements StatsFetcher for broker tests.
type stubStatsFetcher struct{}

func (s *stubStatsFetcher) FetchStats(ctx context.Context) (*StatsEvent, error) {
	return &StatsEvent{PendingObjects: 1}, nil
}

// Regression PR10: FetchStats must respect context cancellation. The
// AdminStatsFetcher uses http.NewRequestWithContext, so a cancelled context
// must abort the request promptly.
func TestFetchStats_ContextCancellation(t *testing.T) {
	// handler that blocks until the context is cancelled, simulating slow admin API
	blockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	fetcher := &AdminStatsFetcher{AdminHandler: blockHandler}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = fetcher.FetchStats(ctx)
	elapsed := time.Since(start)

	// Should return promptly after context cancellation, not hang
	assert.Less(t, elapsed, 2*time.Second, "FetchStats must not hang after context cancellation")
}
