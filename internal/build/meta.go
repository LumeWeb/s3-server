package build

// meta.go holds build-time metadata variables set via -ldflags.

var (
	// Version is the binary version, set via -ldflags "-X go.lumeweb.com/s3-server/internal/build.Version=..."
	Version = "dev"

	// Commit is the git commit hash, set via -ldflags.
	Commit = "unknown"

	// BuildTime is the build timestamp, set via -ldflags.
	BuildTime = "unknown"
)
