package status

// Status represents the current state of the s3d backend.
type Status string

const (
	// Starting indicates the backend is currently initializing (async).
	// The HTTP server is up but S3 API calls will return 503 until ready.
	Starting Status = "starting"

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
