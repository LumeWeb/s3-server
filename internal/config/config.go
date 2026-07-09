package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	koanfyaml "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
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

type S3Config struct {
	Directory         string   `koanf:"directory" json:"directory"`
	IndexerURL        string   `koanf:"indexer_url" json:"indexer_url"`
	AvailableIndexers []string `koanf:"available_indexers" json:"available_indexers"`
	HostBases         []string `koanf:"host_bases" json:"host_bases"`
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
	return PanelConfig{
		OnboardingState: DefaultOnboardingState,
		SSL: SSLConfig{
			Mode: SSLModeNone,
		},
		S3: S3Config{
			Directory:         "/var/lib/s3-server",
			IndexerURL:       "https://sia.pinner.xyz",
			AvailableIndexers: []string{"https://sia.pinner.xyz", "https://sia.storage"},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
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

	// load env overrides — S3_SERVER_ prefix maps to config keys
	if err := k.Load(env.Provider(EnvPrefix, Delimiter, func(key string) string {
		return strings.ReplaceAll(
			strings.ToLower(strings.TrimPrefix(key, EnvPrefix)),
			EnvDelimiter, Delimiter,
		)
	}), nil); err != nil {
		return defaults, fmt.Errorf("failed to load env vars: %w", err)
	}

	var cfg PanelConfig
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
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

	return cfg, nil
}

// stringToSliceHookFunc splits comma-separated strings into []string
// when the target type is []string. This allows env vars like
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

func BuildLogger(cfg LogConfig) (*zap.Logger, error) {
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

	return zap.New(zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), level)), nil
}
