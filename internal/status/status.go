package status

// Status represents the current state of the s3d backend.
type Status string

const (
	// Running indicates the backend is initialized and serving requests.
	Running Status = "running"

	// Stopped indicates the backend is not initialized (startup state).
	Stopped Status = "stopped"

	// Error indicates the backend failed to initialize. The InitError
	// field on Manager carries the reason.
	Error Status = "error"
)

// String implements fmt.Stringer.
func (s Status) String() string {
	return string(s)
}
