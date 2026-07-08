package testutil

import "go.uber.org/zap"

// NewTestLogger returns a discard logger suitable for tests.
func NewTestLogger() *zap.Logger {
	return zap.NewNop()
}
