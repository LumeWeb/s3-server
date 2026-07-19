package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/SiaFoundation/s3d/s3"
	s3dSia "github.com/SiaFoundation/s3d/sia"
	"github.com/SiaFoundation/s3d/sia/persist/sqlite"
	"github.com/samber/lo"
	"github.com/shirou/gopsutil/v4/disk"
	"go.lumeweb.com/s3-server/internal/config"
	"go.sia.tech/core/types"
	sdk "go.sia.tech/siastorage"
	"go.uber.org/zap"
)

// s3dFactory is the production Factory that initializes real s3d components.
type s3dFactory struct {
	log        *zap.Logger
	diskQuery  func(string) (*disk.UsageStat, error)
}

// NewS3DFactory creates a production Factory backed by real s3d packages.
func NewS3DFactory(log *zap.Logger) Factory {
	return &s3dFactory{log: log, diskQuery: disk.Usage}
}

func (f *s3dFactory) resolveDiskLimit(s3Cfg config.S3Config) (uint64, bool, error) {
	limitBytes, hasLimit, err := s3Cfg.DiskUsageLimit.BytesWith(s3Cfg.Directory, f.diskQuery)
	if err != nil {
		if s3Cfg.DiskUsageLimit.IsAuto() {
			f.log.Warn("failed to query disk usage for auto limit; defaulting to conservative cap", zap.Error(err))
			return config.AutoFallbackGB * 1024 * 1024 * 1024, true, nil
		}
		return 0, false, fmt.Errorf("failed to resolve disk usage limit: %w", err)
	}
	return limitBytes, hasLimit, nil
}

func (f *s3dFactory) Init(ctx context.Context, s3Cfg config.S3Config, sqliteStore S3DStore) (Backend, http.Handler, func(), error) {
	appKey, indexerURL, err := sqliteStore.AppKey()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get app key: %w", err)
	}
	if indexerURL == "" {
		indexerURL = s3Cfg.IndexerURL
	}
	if indexerURL == "" {
		return nil, nil, nil, fmt.Errorf("no indexer URL configured: set s3.indexer_url in panel.yml")
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
	limitBytes, hasLimit, err := f.resolveDiskLimit(s3Cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if hasLimit {
		siaOpts = append(siaOpts, s3dSia.WithDiskUsageLimit(limitBytes))
	}
	if s3Cfg.UploadWastePct > 0 {
		siaOpts = append(siaOpts, s3dSia.WithUploadWaste(s3Cfg.UploadWastePct))
	}

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

	// Open a second read-only handle for stats queries. SQLite WAL allows
	// concurrent readers, so this won't contend with s3d's primary handle.
	dbPath := filepath.Join(s3Cfg.Directory, "s3d.db")
	statsDB, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to open stats database: %w", err)
	}
	statsDB.SetMaxOpenConns(2)

	cleanup := func() {
		if err := statsDB.Close(); err != nil {
			f.log.Error("failed to close stats database during cleanup", zap.Error(err))
		}
		if err := siaBackend.Close(); err != nil {
			f.log.Error("failed to close sia backend during cleanup", zap.Error(err))
		}
		if err := sqliteStore.Close(); err != nil {
			f.log.Error("failed to close database during cleanup", zap.Error(err))
		}
	}

	return &backendAdapter{inner: siaBackend, statsDB: statsDB}, realS3Handler, cleanup, nil
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
	inner   *s3dSia.Sia
	statsDB *sql.DB // read-only handle for stats queries
}

func (a *backendAdapter) S3Backend() s3.Backend {
	return a.inner
}

func (a *backendAdapter) ListBuckets(ctx context.Context, accessKeyID string) ([]s3.BucketInfo, error) {
	return a.inner.ListBuckets(ctx, accessKeyID)
}

// ListAllBuckets returns all buckets across all users with owner names.
func (a *backendAdapter) ListAllBuckets(ctx context.Context) ([]BucketInfo, error) {
	rows, err := a.statsDB.QueryContext(ctx,
		`SELECT b.name, COALESCE(u.name, '') as owner, b.created_at
		 FROM buckets b
		 LEFT JOIN users u ON u.id = b.user_id
		 ORDER BY b.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var buckets []BucketInfo
	for rows.Next() {
		var bi BucketInfo
		var ts int64
		if err := rows.Scan(&bi.Name, &bi.Owner, &ts); err != nil {
			return nil, err
		}
		bi.CreatedAt = time.Unix(ts, 0)
		buckets = append(buckets, bi)
	}
	return buckets, rows.Err()
}

func (a *backendAdapter) BucketStats(ctx context.Context, bucketName string) (count int, size int64, err error) {
	const q = `SELECT COUNT(*), COALESCE(SUM(o.size), 0)
		FROM objects o
		JOIN buckets b ON b.id = o.bucket_id
		WHERE b.name = ? AND o.is_latest = 1 AND o.is_delete_marker = 0`
	row := a.statsDB.QueryRowContext(ctx, q, bucketName)
	if err = row.Scan(&count, &size); err != nil {
		return 0, 0, fmt.Errorf("failed to query bucket stats: %w", err)
	}
	return count, size, nil
}

func (a *backendAdapter) BucketOwner(ctx context.Context, bucketName string) (string, error) {
	var owner string
	err := a.statsDB.QueryRowContext(ctx,
		`SELECT u.name FROM buckets b JOIN users u ON u.id = b.user_id WHERE b.name = ?`,
		bucketName,
	).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return owner, err
}

func (a *backendAdapter) BucketVersioning(ctx context.Context, bucketName string) (string, error) {
	var status string
	err := a.statsDB.QueryRowContext(ctx,
		`SELECT versioning_status FROM buckets WHERE name = ?`,
		bucketName,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return status, err
}

func (a *backendAdapter) BucketCountForUser(ctx context.Context, userName string) (int, error) {
	var count int
	err := a.statsDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM buckets b JOIN users u ON u.id = b.user_id WHERE u.name = ?`,
		userName,
	).Scan(&count)
	return count, err
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
	return lo.Map(keys, func(k s3dSia.AccessKeyInfo, _ int) AccessKeyInfo {
		return AccessKeyInfo{
			AccessKeyID: k.AccessKeyID,
			SecretKey:   k.SecretKey,
			UserName:    k.UserName,
		}
	}), nil
}

func (a *sqliteStoreAdapter) Close() error {
	return a.inner.Close()
}
