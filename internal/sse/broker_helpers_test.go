package sse

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/testutil"
)

// --- PublishStats ---

func TestBroker_PublishStats(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	b := NewBroker(mockStore, nil, nil, testutil.NewTestLogger())

	evt := StatsEvent{
		PendingObjects:  5,
		PendingSize:     1024,
		UploadedObjects: 10,
		AccountReady:    true,
	}

	err := b.PublishStats(evt)
	require.NoError(t, err)
}

// --- NotifyKeyChange ---

func TestBroker_NotifyKeyChange(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	b := NewBroker(mockStore, nil, nil, testutil.NewTestLogger())

	// Should not panic and should not return an error
	b.NotifyKeyChange("created", 1)
	b.NotifyKeyChange("deleted", 3)
}

// --- NotifyBucketChange ---

func TestBroker_NotifyBucketChange(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	b := NewBroker(mockStore, nil, nil, testutil.NewTestLogger())

	b.NotifyBucketChange("created", "my-bucket")
	b.NotifyBucketChange("deleted", "old-bucket")
}

// --- SetStatsFetcher + publishStats integration ---

func TestBroker_PublishStats_WithFetcher(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	b := NewBroker(mockStore, nil, nil, testutil.NewTestLogger())

	// Set a stats fetcher that returns known data
	b.SetStatsFetcher(&stubStatsFetcher{})

	// publishStats is called by StartStatusLoop
	ctx, cancel := context.WithCancel(context.Background())
	b.StartStatusLoop(ctx, "v1.0.0")

	// Give it a moment to publish
	time.Sleep(100 * time.Millisecond)
	cancel()
	// Verify no panic and clean shutdown
}

// --- PublishStats with nil fetcher (no-op) ---

func TestBroker_PublishStats_NilFetcher(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	b := NewBroker(mockStore, nil, nil, testutil.NewTestLogger())

	// publishStats via StartStatusLoop: nil fetcher means it should be a no-op
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	b.StartStatusLoop(ctx, "v1.0.0")
	// Wait for context to expire
	<-ctx.Done()
}

// --- SetInitError ---

func TestBroker_SetInitError(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	b := NewBroker(mockStore, nil, nil, testutil.NewTestLogger())

	b.SetInitError(func() string { return "backend failed to start" })

	// publishStatus via StartStatusLoop should include the init error
	ctx, cancel := context.WithCancel(context.Background())
	b.StartStatusLoop(ctx, "v1.0.0")
	time.Sleep(100 * time.Millisecond)
	cancel()
}

// --- KeyChangeEvent JSON ---

func TestKeyChangeEventJSON(t *testing.T) {
	evt := KeyChangeEvent{Action: "created", Count: 2}
	data, err := json.Marshal(evt)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"action":"created"`)
	assert.Contains(t, string(data), `"count":2`)
}

func TestBucketChangeEventJSON(t *testing.T) {
	evt := BucketChangeEvent{Action: "deleted", Name: "test-bucket"}
	data, err := json.Marshal(evt)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"action":"deleted"`)
	assert.Contains(t, string(data), `"name":"test-bucket"`)
}

func TestStatsEventJSON(t *testing.T) {
	evt := StatsEvent{
		PendingObjects: 5,
		PendingSize:    1024,
		AccountReady:   true,
	}
	data, err := json.Marshal(evt)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"pending_objects":5`)
	assert.Contains(t, string(data), `"account_ready":true`)
}
