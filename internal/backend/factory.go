package backend

import (
	"context"
	"fmt"
	"net/http"

	"github.com/SiaFoundation/s3d/s3"
	s3dSia "github.com/SiaFoundation/s3d/sia"
	"github.com/SiaFoundation/s3d/sia/persist/sqlite"
	"go.lumeweb.com/s3-server/internal/config"
	sdk "go.sia.tech/siastorage"
	"go.sia.tech/core/types"
	"go.uber.org/zap"
)

// DefaultUserName is the single s3d user under which panel-managed access keys live.
const DefaultUserName = "s3d"

// s3dFactory is the production Factory that initializes real s3d components.
type s3dFactory struct {
	log *zap.Logger
}

// NewS3DFactory creates a production Factory backed by real s3d packages.
func NewS3DFactory(log *zap.Logger) Factory {
	return &s3dFactory{log: log}
}

func (f *s3dFactory) Init(ctx context.Context, s3Cfg config.S3Config, sqliteStore S3DStore) (Backend, http.Handler, func(), error) {
	appKey, indexerURL, err := sqliteStore.AppKey()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get app key: %w", err)
	}
	if indexerURL == "" {
		indexerURL = s3Cfg.IndexerURL
	}

	builder := sdk.NewBuilder(indexerURL, sdk.AppMetadata{
		ID:          types.HashBytes([]byte("s3d")),
		Name:        "S3d",
		Description: "A S3-compatible storage service backed by Sia",
		LogoURL:     "https://example.com/logo.png",
		ServiceURL:  "https://github.com/Siafoundation/s3d",
	})
	sdkClient, err := builder.SDK(appKey, sdk.WithLogger(f.log.Named("sdk")))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create SDK client: %w", err)
	}

	var siaOpts []s3dSia.Option
	siaOpts = append(siaOpts, s3dSia.WithLogger(f.log.Named("backend")))

	// sqliteStore is our S3DStore interface, but s3dSia.New needs the concrete *sqlite.Store.
	concreteSqlite, ok := sqliteStore.(*sqliteStoreAdapter)
	if !ok {
		return nil, nil, nil, fmt.Errorf("unexpected sqlite store type")
	}

	siaBackend, err := s3dSia.New(ctx, s3dSia.NewSDK(sdkClient), concreteSqlite.inner, s3Cfg.Directory, siaOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create sia backend: %w", err)
	}

	realS3Handler := s3.New(siaBackend, s3.WithHostBucketBases(s3Cfg.HostBases), s3.WithLogger(f.log))
	cleanup := func() {
		if err := siaBackend.Close(); err != nil {
			f.log.Error("failed to close sia backend during cleanup", zap.Error(err))
		}
		if err := sqliteStore.Close(); err != nil {
			f.log.Error("failed to close database during cleanup", zap.Error(err))
		}
	}

	return &backendAdapter{inner: siaBackend}, realS3Handler, cleanup, nil
}

func (f *s3dFactory) OpenDatabase(dbPath string) (S3DStore, error) {
	s, err := sqlite.OpenDatabase(dbPath, f.log)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	return &sqliteStoreAdapter{inner: s}, nil
}

// backendAdapter wraps *s3dSia.Sia to implement the Backend interface.
type backendAdapter struct {
	inner *s3dSia.Sia
}

func (a *backendAdapter) S3Backend() s3.Backend {
	return a.inner
}

func (a *backendAdapter) ListBuckets(ctx context.Context, accessKeyID string) ([]s3.BucketInfo, error) {
	return a.inner.ListBuckets(ctx, accessKeyID)
}

func (a *backendAdapter) CreateBucket(ctx context.Context, accessKeyID, name string) error {
	return a.inner.CreateBucket(ctx, accessKeyID, name)
}

func (a *backendAdapter) DeleteBucket(ctx context.Context, accessKeyID, name string) error {
	return a.inner.DeleteBucket(ctx, accessKeyID, name)
}

func (a *backendAdapter) FlushObjects(ctx context.Context) error {
	return a.inner.FlushObjects(ctx)
}

func (a *backendAdapter) PutBucketVersioning(ctx context.Context, accessKeyID, bucket, status string) error {
	return a.inner.PutBucketVersioning(ctx, accessKeyID, bucket, status)
}

func (a *backendAdapter) GetBucketVersioning(ctx context.Context, accessKeyID, bucket string) (string, error) {
	return a.inner.GetBucketVersioning(ctx, accessKeyID, bucket)
}

func (a *backendAdapter) PutBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string, config s3.LifecycleConfiguration) error {
	return a.inner.PutBucketLifecycleConfiguration(ctx, accessKeyID, bucket, config)
}

func (a *backendAdapter) GetBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) (s3.LifecycleConfiguration, error) {
	return a.inner.GetBucketLifecycleConfiguration(ctx, accessKeyID, bucket)
}

func (a *backendAdapter) DeleteBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) error {
	return a.inner.DeleteBucketLifecycleConfiguration(ctx, accessKeyID, bucket)
}

func (a *backendAdapter) BackupSQLite3(ctx context.Context, destPath string) error {
	return a.inner.BackupSQLite3(ctx, destPath)
}

func (a *backendAdapter) Close() error {
	return a.inner.Close()
}

// sqliteStoreAdapter wraps *sqlite.Store to implement the S3DStore interface.
type sqliteStoreAdapter struct {
	inner *sqlite.Store
}

func (a *sqliteStoreAdapter) AppKey() (types.PrivateKey, string, error) {
	return a.inner.AppKey()
}

func (a *sqliteStoreAdapter) SetAppKey(key types.PrivateKey, indexerURL string) error {
	return a.inner.SetAppKey(key, indexerURL)
}

func (a *sqliteStoreAdapter) CreateUser(name string) error {
	return a.inner.CreateUser(name)
}

func (a *sqliteStoreAdapter) DeleteUser(name string) error {
	return a.inner.DeleteUser(name)
}

func (a *sqliteStoreAdapter) ListUsers() ([]string, error) {
	return a.inner.ListUsers()
}

func (a *sqliteStoreAdapter) CreateAccessKey(userName, accessKeyID, secretKey string) error {
	return a.inner.CreateAccessKey(userName, accessKeyID, secretKey)
}

func (a *sqliteStoreAdapter) DeleteAccessKey(accessKeyID string) error {
	return a.inner.DeleteAccessKey(accessKeyID)
}

func (a *sqliteStoreAdapter) ListAccessKeys(userName *string) ([]AccessKeyInfo, error) {
	keys, err := a.inner.ListAccessKeys(userName)
	if err != nil {
		return nil, err
	}
	out := make([]AccessKeyInfo, len(keys))
	for i, k := range keys {
		out[i] = AccessKeyInfo{
			AccessKeyID: k.AccessKeyID,
			SecretKey:   k.SecretKey,
			UserName:    k.UserName,
		}
	}
	return out, nil
}

func (a *sqliteStoreAdapter) Close() error {
	return a.inner.Close()
}
