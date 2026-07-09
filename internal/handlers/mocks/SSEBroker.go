package handlersmocks

import (
	"net/http"
	"sync"

	"github.com/stretchr/testify/mock"
)

// MockSSEBroker is a mock for the handlers.SSEBroker interface.
type MockSSEBroker struct {
	mu     sync.Mutex
	calls  []SSEBrokerCall
	mock.Mock
}

// SSEBrokerCall records a single broker call for inspection.
type SSEBrokerCall struct {
	Method string
	Args   []any
}

// NotifyKeyChange provides a mock function with given fields: action, count
func (m *MockSSEBroker) NotifyKeyChange(action string, count int) {
	m.mu.Lock()
	m.calls = append(m.calls, SSEBrokerCall{Method: "NotifyKeyChange", Args: []any{action, count}})
	m.mu.Unlock()
	m.Called(action, count)
}

// NotifyBucketChange provides a mock function with given fields: action, name
func (m *MockSSEBroker) NotifyBucketChange(action, name string) {
	m.mu.Lock()
	m.calls = append(m.calls, SSEBrokerCall{Method: "NotifyBucketChange", Args: []any{action, name}})
	m.mu.Unlock()
	m.Called(action, name)
}

// ServeHTTP provides a mock function with given fields: w, r
func (m *MockSSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.Called(w, r)
}

// Calls returns a snapshot of all calls made to the broker.
func (m *MockSSEBroker) Calls() []SSEBrokerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SSEBrokerCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// KeyChangeCalls returns only NotifyKeyChange calls.
func (m *MockSSEBroker) KeyChangeCalls() []SSEBrokerCall {
	all := m.Calls()
	var out []SSEBrokerCall
	for _, c := range all {
		if c.Method == "NotifyKeyChange" {
			out = append(out, c)
		}
	}
	return out
}
