package backend

import (
	"context"
	"fmt"

	sdk "go.sia.tech/siastorage"
	"go.uber.org/zap"
)

// AccountInfo is a snapshot of the Sia account state relevant to the panel.
// It mirrors the fields from app.AccountResponse that the UI needs.
type AccountInfo struct {
	MaxPinnedData    uint64 `json:"max_pinned_data"`
	RemainingStorage uint64 `json:"remaining_storage"`
	PinnedData       uint64 `json:"pinned_data"`
	PinnedSize       uint64 `json:"pinned_size"`
	Ready            bool   `json:"ready"`
}

// AccountClient queries the Sia account info via a dedicated SDK instance.
// It is separate from s3d's internal SDK so the panel can query account
// state without going through s3d's admin API (which does not expose it).
type AccountClient interface {
	Account(ctx context.Context) (AccountInfo, error)
	Close() error
}

// sdkAccountClient wraps a *sdk.SDK to implement AccountClient.
type sdkAccountClient struct {
	sdk *sdk.SDK
}

// NewAccountClient creates an AccountClient using the provided SDK.
// The SDK must already be configured with the app key and indexer URL.
func NewAccountClient(sdkClient *sdk.SDK) AccountClient {
	return &sdkAccountClient{sdk: sdkClient}
}

func (c *sdkAccountClient) Account(ctx context.Context) (AccountInfo, error) {
	resp, err := c.sdk.Account(ctx)
	if err != nil {
		return AccountInfo{}, fmt.Errorf("failed to fetch account info: %w", err)
	}
	return AccountInfo{
		MaxPinnedData:    resp.MaxPinnedData,
		RemainingStorage: resp.RemainingStorage,
		PinnedData:       resp.PinnedData,
		PinnedSize:       resp.PinnedSize,
		Ready:            resp.Ready,
	}, nil
}

func (c *sdkAccountClient) Close() error {
	return c.sdk.Close()
}

// noopAccountClient returns zero values and no error. Used when onboarding
// is not complete or the SDK client is not yet available.
type noopAccountClient struct {
	log *zap.Logger
}

// NewNoopAccountClient returns an AccountClient that always returns zero
// values. Useful as a placeholder before onboarding completes.
func NewNoopAccountClient(log *zap.Logger) AccountClient {
	return &noopAccountClient{log: log}
}

func (c *noopAccountClient) Account(ctx context.Context) (AccountInfo, error) {
	return AccountInfo{}, nil
}

func (c *noopAccountClient) Close() error { return nil }
