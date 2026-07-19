package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	koanfyaml "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/shirou/gopsutil/v4/disk"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type SSLMode string

const (
	SSLModeNone     SSLMode = "none"
	SSLModePlatform SSLMode = "platform"
	SSLModeManaged  SSLMode = "managed"
)

const (
	EnvPrefix    = "S3_SERVER_"
	EnvDelimiter = "__"
	Delimiter    = "."
	StructTag    = "koanf"
	ConfigFile   = "panel.yml"

	DefaultOnboardingState = "pending"
)

type KeyPair struct {
	AccessKey string `koanf:"access_key" json:"access_key"`
	SecretKey string `koanf:"secret_key" json:"secret_key"`
}

type SSLConfig struct {
	Mode       SSLMode `koanf:"mode" json:"mode"`
	ACMEEmail  string  `koanf:"acme_email" json:"acme_email,omitempty"`
	ACMEDirURL string  `koanf:"acme_dir_url" json:"acme_dir_url,omitempty"`
}

type LogConfig struct {
	Level  string `koanf:"level" json:"level"`
	Format string `koanf:"format" json:"format"`
}

// DiskUsageLimit represents the S3 disk usage limit setting.
// Valid values: "auto" (default), "0" (unlimited), or a positive number in GB (e.g. "100").
type DiskUsageLimit string

const (
	DiskUsageLimitAuto      DiskUsageLimit = "auto"
	DiskUsageLimitUnlimited DiskUsageLimit = "0"
)

// DefaultDiskUsageLimit is the default disk usage limit mode.
const DefaultDiskUsageLimit = DiskUsageLimitAuto

// AutoPercent is the percentage of total disk capacity used when
// DiskUsageLimit is "auto".
const AutoPercent uint64 = 80

// AutoFallbackGB is the conservative cap applied when auto-mode disk query
// fails, preventing unbounded writes in restricted environments.
const AutoFallbackGB uint64 = 25

// String returns the raw value of the disk usage limit.
func (d DiskUsageLimit) String() string { return string(d) }

// IsLimited returns true if a limit should be enforced.
func (d DiskUsageLimit) IsLimited() bool {
	return d != "" && d != DiskUsageLimitUnlimited && d != DiskUsageLimitAuto
}

// IsAuto returns true when the limit mode is auto.
func (d DiskUsageLimit) IsAuto() bool {
	return d == "" || d == DiskUsageLimitAuto
}

// Bytes returns the limit in bytes. For "auto" it queries the filesystem;
// for "0" or empty it returns (0, false); for numeric values it converts GB to bytes.
func (d DiskUsageLimit) Bytes(directory string) (uint64, bool, error) {
	return d.BytesWith(directory, disk.Usage)
}

// BytesWith is the testable variant of Bytes that accepts a custom disk query
// function instead of calling disk.Usage directly.
func (d DiskUsageLimit) BytesWith(directory string, diskQuery func(string) (*disk.UsageStat, error)) (uint64, bool, error) {
	if d == "" || d == DiskUsageLimitUnlimited {
		return 0, false, nil
	}
	if d == DiskUsageLimitAuto {
		stat, err := diskQuery(directory)
		if err != nil {
			return 0, false, err
		}
		return stat.Total * AutoPercent / 100, true, nil
	}
	gb, err := strconv.ParseUint(string(d), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid disk_usage_limit %q: must be auto, 0, or a positive integer", d)
	}
	if gb == 0 {
		return 0, false, nil
	}
	return gb * 1024 * 1024 * 1024, true, nil
}

// MarshalJSON serializes the disk usage limit as a JSON string.
func (d DiskUsageLimit) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(string(d))), nil
}

// UnmarshalJSON parses the disk usage limit from a JSON string.
func (d *DiskUsageLimit) UnmarshalJSON(data []byte) error {
	return d.UnmarshalText(data)
}

// UnmarshalText implements encoding.TextUnmarshaler for koanf/mapstructure compatibility.
func (d *DiskUsageLimit) UnmarshalText(data []byte) error {
	s := strings.TrimSpace(string(data))
	if unq, err := strconv.Unquote(s); err == nil {
		s = strings.TrimSpace(unq)
	}
	return d.Set(s)
}

// Set validates and assigns the disk usage limit from a raw string value.
func (d *DiskUsageLimit) Set(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		*d = DefaultDiskUsageLimit
		return nil
	}
	lower := strings.ToLower(s)
	if lower == "auto" {
		*d = DiskUsageLimitAuto
		return nil
	}
	if lower == "0" || lower == "unlimited" {
		*d = DiskUsageLimitUnlimited
		return nil
	}
	if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return fmt.Errorf("invalid disk_usage_limit %q: must be auto, 0, or a positive integer", s)
	}
	*d = DiskUsageLimit(s)
	return nil
}

type IndexerOption struct {
	URL         string `koanf:"url" json:"url"`
	Name        string `koanf:"name" json:"name"`
	Description string `koanf:"description" json:"description"`
	Logo        string `koanf:"logo" json:"logo,omitempty"` // identifier: "pinner", "sia-storage", or empty for star
	BrandColor  string `koanf:"brand_color" json:"brand_color,omitempty"`
	// ContrastColor is computed from BrandColor using WCAG relative luminance.
	// Returns "#000000" or "#FFFFFF": whichever has higher contrast against BrandColor.
	// Omitted from config files (koanf:"-") and computed at load time.
	ContrastColor string `koanf:"-" json:"contrast_color,omitempty"`
}

type S3Config struct {
	Directory         string          `koanf:"directory" json:"directory"`
	IndexerURL        string          `koanf:"indexer_url" json:"indexer_url"`
	AvailableIndexers []IndexerOption `koanf:"available_indexers" json:"available_indexers"`
	HostBases         []string        `koanf:"host_bases" json:"host_bases"`
	DiskUsageLimit    DiskUsageLimit  `koanf:"disk_usage_limit" json:"disk_usage_limit"`
	UploadWastePct    float64         `koanf:"upload_waste_pct" json:"upload_waste_pct"`
}

type PanelConfig struct {
	AdminPasswordHash string    `koanf:"admin_password_hash" json:"-"`
	OnboardingState   string    `koanf:"onboarding_state" json:"onboarding_state"`
	PlatformID        string    `koanf:"platform_id" json:"platform_id,omitempty"`
	AccessKeys        []KeyPair `koanf:"access_keys" json:"access_keys"`
	SSL               SSLConfig `koanf:"ssl" json:"ssl"`
	S3                S3Config  `koanf:"s3" json:"s3"`
	Log               LogConfig `koanf:"log" json:"log"`
}

// DefaultPlatformName is the fallback when PlatformID is empty or unknown.
const DefaultPlatformName = "deployment platform"

// platformNames maps deployment platform IDs to human-readable names.
// The deployment packaging sets S3_SERVER_PLATFORM_ID to identify itself.
var platformNames = map[string]string{
	"coolify": "Coolify",
	"k8s":     "Kubernetes",
	"docker":  "Docker",
	"compose": "Docker Compose",
	"systemd": "systemd",
}

// ResolvePlatformName returns the human-readable name for the configured
// PlatformID by looking it up in the platformNames registry.
// If the ID is empty or not found, DefaultPlatformName is returned.
func ResolvePlatformName(platformID string) string {
	if name, ok := platformNames[platformID]; ok {
		return name
	}
	return DefaultPlatformName
}

// DefaultS3DirectoryVal is the default directory for S3 data storage.
// Exported as a constant so other packages can compare without hardcoding.
const DefaultS3DirectoryVal = "/var/lib/s3-server"

// DefaultS3Directory returns the default S3 data directory.
func DefaultS3Directory() string {
	return DefaultS3DirectoryVal
}

// ResolveS3Directory returns the S3 data directory to use, given the configured
// directory and the data-dir flag value. If the configured directory is the
// default and doesn't exist on disk, it falls back to dataDir so local/dev
// launches with --data-dir work without manually creating /var/lib/s3-server.
func ResolveS3Directory(configuredDir, dataDir string) string {
	if configuredDir == DefaultS3DirectoryVal {
		if _, err := os.Stat(configuredDir); os.IsNotExist(err) {
			return dataDir
		}
	}
	return configuredDir
}

func DefaultConfig() PanelConfig {
	cfg := PanelConfig{
		OnboardingState: DefaultOnboardingState,
		SSL: SSLConfig{
			Mode: SSLModeNone,
		},
		S3: S3Config{
			Directory:      "/var/lib/s3-server",
			IndexerURL:     "https://sia.pinner.xyz",
			DiskUsageLimit: DefaultDiskUsageLimit,
			AvailableIndexers: []IndexerOption{
				{URL: "https://sia.pinner.xyz", Name: "Pinner", Description: "Our indexer, our support", Logo: "pinner", BrandColor: "#12A596"},
				{URL: "https://sia.storage", Name: "Sia Storage", Description: "Are you already using Sia Storage? Connect here.", Logo: "sia-storage", BrandColor: "#EFF2ED"},
			},
			// DefaultUploadWastePct from s3d (sia.DefaultUploadWastePct = 0.1):
			// maximum percentage of wasted space tolerated per slab before
			// objects are uploaded. Lower = faster uploads, more waste.
			// Higher = slower uploads, less waste.
			UploadWastePct: 0.1,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
	computeIndexerContrastColors(cfg.S3.AvailableIndexers)
	return cfg
}

// computeIndexerContrastColors computes ContrastColor for each indexer option
// based on WCAG 2.1 relative luminance. The color with the higher contrast
// ratio against BrandColor is chosen (black or white).
func computeIndexerContrastColors(indexers []IndexerOption) {
	for i := range indexers {
		indexers[i].ContrastColor = contrastColor(indexers[i].BrandColor)
	}
}

// contrastColor returns "#000000" or "#FFFFFF": whichever has higher WCAG 2.1
// contrast ratio against the given hex color string (e.g. "#12A596").
func contrastColor(hexStr string) string {
	r, g, b, ok := parseHexColor(hexStr)
	if !ok {
		return "#000000"
	}
	l := relativeLuminance(r, g, b)
	// Contrast ratio of white against bg = (1.0 + 0.05) / (l + 0.05)
	// Contrast ratio of black against bg = (l + 0.05) / (0.0 + 0.05)
	whiteRatio := 1.05 / (l + 0.05)
	blackRatio := (l + 0.05) / 0.05
	if blackRatio >= whiteRatio {
		return "#000000"
	}
	return "#FFFFFF"
}

// relativeLuminance computes the WCAG 2.1 relative luminance of an sRGB color.
func relativeLuminance(r, g, b uint8) float64 {
	ri := linearizeSRGB(float64(r) / 255.0)
	gi := linearizeSRGB(float64(g) / 255.0)
	bi := linearizeSRGB(float64(b) / 255.0)
	return 0.2126*ri + 0.7152*gi + 0.0722*bi
}

// linearizeSRGB converts an sRGB channel value [0,1] to linear RGB.
func linearizeSRGB(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// parseHexColor parses "#RRGGBB" into r, g, b. Returns ok=false on failure.
func parseHexColor(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	ri, err := strconv.ParseUint(s[0:2], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	gi, err := strconv.ParseUint(s[2:4], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	bi, err := strconv.ParseUint(s[4:6], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(ri), uint8(gi), uint8(bi), true
}

func ConfigPath(dataDir string) string {
	return filepath.Join(dataDir, ConfigFile)
}

func Load(path string) (PanelConfig, error) {
	k := koanf.New(Delimiter)

	// load defaults from struct
	defaults := DefaultConfig()
	if err := k.Load(structs.Provider(defaults, StructTag), nil); err != nil {
		return defaults, fmt.Errorf("failed to load defaults: %w", err)
	}

	// load yaml file (if exists)
	if _, err := os.Stat(path); err == nil {
		if err := k.Load(file.Provider(path), koanfyaml.Parser()); err != nil {
			return defaults, fmt.Errorf("failed to load config file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return defaults, fmt.Errorf("failed to stat config file: %w", err)
	}

	// load env overrides: S3_SERVER_ prefix maps to config keys
	hostBasesEnvKey := EnvPrefix + "S3" + EnvDelimiter + "HOST_BASES"
	_, hostBasesEnvSet := os.LookupEnv(hostBasesEnvKey)

	if err := k.Load(env.Provider(EnvPrefix, Delimiter, func(key string) string {
		mapped := strings.ReplaceAll(
			strings.ToLower(strings.TrimPrefix(key, EnvPrefix)),
			EnvDelimiter, Delimiter,
		)
		// Alias: S3_SERVER_S3__DOMAINS maps to s3.host_bases.
		// Skip when HOST_BASES is also set so it wins deterministically.
		if mapped == "s3.domains" {
			if hostBasesEnvSet {
				return ""
			}
			return "s3.host_bases"
		}
		return mapped
	}), nil); err != nil {
		return defaults, fmt.Errorf("failed to load env vars: %w", err)
	}

	var cfg PanelConfig
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				diskUsageLimitHookFunc(),
				stringToSliceHookFunc(),
				mapstructure.StringToTimeDurationHookFunc(),
			),
			Result:           &cfg,
			WeaklyTypedInput: true,
			TagName:          "koanf",
		},
	}); err != nil {
		return defaults, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Compute derived fields that aren't in config files.
	computeIndexerContrastColors(cfg.S3.AvailableIndexers)

	return cfg, nil
}

// diskUsageLimitHookFunc converts int/float64/string to DiskUsageLimit.
// This handles existing configs where disk_usage_limit was stored as a YAML number.
func diskUsageLimitHookFunc() mapstructure.DecodeHookFunc {
	return func(from, to reflect.Type, data interface{}) (interface{}, error) {
		if to != reflect.TypeOf(DiskUsageLimit("")) {
			return data, nil
		}
		switch v := data.(type) {
		case string:
			var d DiskUsageLimit
			if err := d.Set(v); err != nil {
				return nil, err
			}
			return d, nil
		case int:
			return DiskUsageLimit(strconv.Itoa(v)), nil
		case int64:
			return DiskUsageLimit(strconv.FormatInt(v, 10)), nil
		case float64:
			return DiskUsageLimit(strconv.FormatInt(int64(v), 10)), nil
		case uint64:
			return DiskUsageLimit(strconv.FormatUint(v, 10)), nil
		default:
			return data, nil
		}
	}
}

// stringToSliceHookFunc splits comma-separated strings into []string
// S3_SERVER_S3__HOST_BASES=s3.example.com,backup.example.com
// to populate a []string field.
func stringToSliceHookFunc() mapstructure.DecodeHookFunc {
	return func(from, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != reflect.TypeOf([]string{}) {
			return data, nil
		}
		s := data.(string)
		if s == "" {
			return []string{}, nil
		}
		parts := strings.Split(s, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		return parts, nil
	}
}

func Save(path string, cfg PanelConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	k := koanf.New(Delimiter)
	if err := k.Load(structs.Provider(cfg, StructTag), nil); err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	data, err := k.Marshal(koanfyaml.Parser())
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// BuildLogger creates a zap.Logger from LogConfig. It returns the
// AtomicLevel so callers can hot-reload the log level at runtime without
// restarting the process (e.g. via the settings UI).
func BuildLogger(cfg LogConfig) (*zap.Logger, zap.AtomicLevel, error) {
	level := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	if parsed, err := zapcore.ParseLevel(cfg.Level); err == nil {
		level = zap.NewAtomicLevelAt(parsed)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.RFC3339TimeEncoder

	var encoder zapcore.Encoder
	if cfg.Format == "human" {
		encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderCfg.EncodeDuration = zapcore.StringDurationEncoder
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	}

	return zap.New(zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), level)), level, nil
}
