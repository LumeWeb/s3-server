package sse

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	sseserver "github.com/apt304/sse-go/server"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/store"
	"go.uber.org/zap"
)

const (
	// Topics for different event channels. All are delivered on the same
	// /_panel/api/events SSE connection.
	TopicDashboard = "dashboard"
	TopicKeys      = "keys"
	TopicBuckets   = "buckets"
	TopicStats     = "stats"

	bufferCapacity    = 16
	heartbeatInterval = 15 * time.Second
	statusInterval    = 5 * time.Second
)

// allTopics is the list every SSE client is subscribed to.
var allTopics = []string{TopicDashboard, TopicKeys, TopicBuckets, TopicStats}

// DashboardEvent is the JSON payload pushed to the dashboard SSE topic.
type DashboardEvent struct {
	S3Status  string `json:"s3_status"`
	KeyCount  int    `json:"key_count"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	InitError string `json:"init_error,omitempty"`
}

// KeyChangeEvent is pushed when access keys are added or deleted.
type KeyChangeEvent struct {
	Action string `json:"action"` // "created" or "deleted"
	Count  int    `json:"count"`
}

// BucketChangeEvent is pushed when buckets are created or deleted.
type BucketChangeEvent struct {
	Action string `json:"action"` // "created" or "deleted"
	Name   string `json:"name"`
}

// StatsEvent carries upload pipeline stats for the monitoring page.
type StatsEvent struct {
	PendingObjects   int64 `json:"pending_objects"`
	PendingSize      int64 `json:"pending_size"`
	UploadedObjects  int64 `json:"uploaded_objects"`
	UploadedSize     int64 `json:"uploaded_size"`
	UnpinnedObjects  int64 `json:"unpinned_objects"`
	FailedUploads   int64 `json:"failed_uploads"`
	OrphanedObjects  int64 `json:"orphaned_objects"`
	MultipartUploads int64 `json:"multipart_uploads"`
}

// StatsFetcher retrieves upload stats from the s3d admin API.
type StatsFetcher interface {
	FetchStats(ctx context.Context) (*StatsEvent, error)
}

// Broker owns the SSE server and provides helpers for publishing dashboard
// events and serving SSE connections.
type Broker struct {
	server        *sseserver.Server
	store         store.Store
	backendStatus func() status.Status
	keyStore      func() backend.S3DStore
	log           *zap.Logger

	// startedAt tracks server uptime for dashboard events.
	startedAt time.Time

	// mu guards statsFetcher and initError, which are set after construction
	// but read concurrently by StartStatusLoop's goroutine.
	mu           sync.RWMutex
	statsFetcher StatsFetcher
	initError    func() string
}

// NewBroker creates a Broker backed by a drop-oldest subscriber.
// backendStatus is called on each status tick; nil defaults to Stopped.
// statsFetcher may be nil — in that case stats events are not published.
func NewBroker(s store.Store, backendStatus func() status.Status, keyStore func() backend.S3DStore, log *zap.Logger) *Broker {
	sub := sseserver.NewDropOldestSubscriber(sseserver.Options{
		Buffer:            bufferCapacity,
		HeartbeatInterval: heartbeatInterval,
	})
	return &Broker{
		server:        sseserver.NewServer(sseserver.Config{}, sub),
		store:         s,
		backendStatus: backendStatus,
		keyStore:      keyStore,
		log:           log,
		startedAt:     time.Now(),
	}
}

// SetStatsFetcher sets the StatsFetcher used to publish periodic upload stats.
// Called after admin handler is available.
func (b *Broker) SetStatsFetcher(f StatsFetcher) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.statsFetcher = f
}

// SetInitError sets the function used to retrieve backend init error messages.
// Called after the backend manager is wired up.
func (b *Broker) SetInitError(f func() string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.initError = f
}

// ServeHTTP handles an SSE connection on all dashboard-related topics.
// Blocks until the client disconnects or the broker shuts down.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hooks := sseserver.LifecycleHooks{
		OnConnect: func(sub sseserver.Subscription) {
			b.log.Debug("sse client connected", zap.Strings("topics", sub.Topics))
		},
		OnDisconnect: func(sub sseserver.Subscription) {
			b.log.Debug("sse client disconnected", zap.Strings("topics", sub.Topics))
		},
	}
	b.server.ServeHTTP(w, r, allTopics, hooks)
}

// PublishDashboard pushes a DashboardEvent to all dashboard subscribers.
func (b *Broker) PublishDashboard(evt DashboardEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return b.server.Publish(sseserver.Event{
		Type: TopicDashboard,
		Data: data,
	}, TopicDashboard)
}

// PublishStats pushes upload stats to all stats subscribers.
func (b *Broker) PublishStats(evt StatsEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return b.server.Publish(sseserver.Event{
		Type: TopicStats,
		Data: data,
	}, TopicStats)
}

// NotifyKeyChange pushes a key change event.
func (b *Broker) NotifyKeyChange(action string, count int) {
	data, err := json.Marshal(KeyChangeEvent{Action: action, Count: count})
	if err != nil {
		b.log.Debug("failed to marshal key change event", zap.Error(err))
		return
	}
	if err := b.server.Publish(sseserver.Event{
		Type: TopicKeys,
		Data: data,
	}, TopicKeys); err != nil {
		b.log.Debug("failed to publish key change event", zap.Error(err))
	}
}

// NotifyBucketChange pushes a bucket change event.
func (b *Broker) NotifyBucketChange(action, name string) {
	data, err := json.Marshal(BucketChangeEvent{Action: action, Name: name})
	if err != nil {
		b.log.Debug("failed to marshal bucket change event", zap.Error(err))
		return
	}
	if err := b.server.Publish(sseserver.Event{
		Type: TopicBuckets,
		Data: data,
	}, TopicBuckets); err != nil {
		b.log.Debug("failed to publish bucket change event", zap.Error(err))
	}
}

// StartStatusLoop begins a background goroutine that periodically publishes
// dashboard status events and upload stats. Stops when ctx is cancelled.
func (b *Broker) StartStatusLoop(ctx context.Context, version string) {
	go func() {
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()

		// publish immediately on start
		b.publishStatus(version)
		b.publishStats(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.publishStatus(version)
				b.publishStats(ctx)
			}
		}
	}()
}

// Shutdown gracefully shuts down the SSE server, waiting for subscriber
// goroutines to finish.
func (b *Broker) Shutdown(ctx context.Context) error {
	return b.server.Shutdown(ctx)
}

func (b *Broker) publishStatus(version string) {
	st := status.Stopped
	if b.backendStatus != nil {
		st = b.backendStatus()
	}

	keyCount := 0
	if b.keyStore != nil {
		if ks := b.keyStore(); ks != nil {
			keys, err := ks.ListAccessKeys(nil)
			if err != nil {
				b.log.Debug("failed to list access keys for sse status", zap.Error(err))
			} else {
				keyCount = len(keys)
			}
		}
	}

	evt := DashboardEvent{
		S3Status:  st.String(),
		KeyCount:   keyCount,
		Version:    version,
		Uptime:     time.Since(b.startedAt).Truncate(time.Second).String(),
	}
	if b.initError != nil {
		b.mu.RLock()
		initErrFn := b.initError
		b.mu.RUnlock()
		evt.InitError = initErrFn()
	}
	if err := b.PublishDashboard(evt); err != nil {
		b.log.Debug("failed to publish dashboard status", zap.Error(err))
	}
}

func (b *Broker) publishStats(ctx context.Context) {
	b.mu.RLock()
	fetcher := b.statsFetcher
	b.mu.RUnlock()
	if fetcher == nil {
		return
	}
	evt, err := fetcher.FetchStats(ctx)
	if err != nil {
		b.log.Debug("failed to fetch stats for sse", zap.Error(err))
		return
	}
	if evt == nil {
		return
	}
	if err := b.PublishStats(*evt); err != nil {
		b.log.Debug("failed to publish stats event", zap.Error(err))
	}
}
