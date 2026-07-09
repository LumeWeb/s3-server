package views

import (
	"fmt"
	"strings"

	"go.lumeweb.com/s3-server/internal/config"

	p "github.com/rickb777/plural"
)

// UserInfo is the panel view model for a s3d user.
type UserInfo struct {
	Name      string
	KeyCount  int
	CreatedAt string
}

// UserKeyGroup groups keys by their owning user for display.
type UserKeyGroup struct {
	UserName string
	IsDefault bool
	Keys     []config.KeyPair
}

// BucketInfo is the panel view model for an S3 bucket.
type BucketInfo struct {
	Name        string
	CreatedAt   string
	ObjectCount int
	TotalSize   int64
	Versioning  string
}

// BackupInfo is the panel view model for an on-disk SQLite backup.
type BackupInfo struct {
	Filename  string
	Size      int64
	CreatedAt string
}

// UploadStats is the panel view model for background upload pipeline stats.
// It mirrors github.com/SiaFoundation/s3d/s3.UploadStats.
type UploadStats struct {
	PendingObjects   int64
	PendingSize      int64
	UploadedObjects  int64
	UploadedSize     int64
	UnpinnedObjects  int64
	FailedUploads    int64
	OrphanedObjects  int64
	MultipartUploads int64
}

// humanBytes returns a human-readable byte string.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// versioningButtonLabel returns the label text for the versioning toggle button.
func versioningButtonLabel(status string) string {
	if status == "Enabled" {
		return "Suspend Versioning"
	}
	return "Enable Versioning"
}

// countWord formats an integer count with a noun, choosing singular or plural
// using the rickb777/plural package. For zero, the plural form is used.
//
//	{{ countWord(1, "key") }}    → "1 key"
//	{{ countWord(2, "key") }}    → "2 keys"
func countWord(n int, noun string) string {
	cases := p.FromOne("%v "+noun, "%v "+noun+"s")
	// FormatInt(0) falls through to the last case (plural), which is correct.
	return cases.FormatInt(n)
}

// jsEscape escapes a string for safe embedding inside a JavaScript
// single-quoted string literal. Prevents XSS when user-controlled values
// are interpolated into Alpine.js @click expressions via fmt.Sprintf.
func jsEscape(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			b = append(b, '\\', '\'')
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '	':
			b = append(b, '\\', 't')
		case '<':
			b = append(b, '\\', 'x', '3', 'c')
		case '>':
			b = append(b, '\\', 'x', '3', 'e')
		default:
			b = append(b, s[i])
		}
	}
	return string(b)
}

// maskKey masks a secret key, showing only the last 4 characters.
// Prevents credential exposure via browser cache, proxy cache, or logs.
func maskKey(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}
