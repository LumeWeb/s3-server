package views

import (
	"github.com/samber/lo"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/views/components"
)

// UserInfo is the panel view model for a s3d user.
type UserInfo struct {
	Name        string
	KeyCount    int
	BucketCount int
	CreatedAt   string
}

// UserKeyGroup groups keys by their owning user for display.
type UserKeyGroup struct {
	UserName    string
	Keys        []config.KeyPair
	BucketCount int
}

// BucketInfo is the panel view model for an S3 bucket.
type BucketInfo struct {
	Name        string
	Owner       string
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

// UserSelectOptions converts a slice of user names to SelectOption values
// for use in dropdowns.
func UserSelectOptions(users []string) []components.SelectOption {
	return lo.Map(users, func(u string, _ int) components.SelectOption {
		return components.SelectOption{Value: u, Label: u}
	})
}

// UploadStats is the panel view model for background upload pipeline stats.
// It mirrors github.com/SiaFoundation/s3d/s3.UploadStats and adds account info.
type UploadStats struct {
	PendingObjects   int64
	PendingSize      int64
	UploadedObjects  int64
	UploadedSize     int64
	UnpinnedObjects  int64
	FailedUploads    int64
	OrphanedObjects  int64
	MultipartUploads int64

	// Account info: zero values when the account client is not available.
	AccountMaxPinnedData   uint64
	AccountRemainingStorage uint64
	AccountPinnedData       uint64
	AccountPinnedSize       uint64
	AccountReady            bool
}
