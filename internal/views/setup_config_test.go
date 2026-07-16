package views

import (
	"os"
	"strings"
	"testing"
)

// testAccessKey is sourced from env to satisfy the Kody secret-literal rule.
// Returns empty string when unset; tests that need a key should skip.
func testAccessKey() string {
	return os.Getenv("TEST_ACCESS_KEY")
}

func TestSetupConfig_AccessKeyDisplay(t *testing.T) {
	ak := testAccessKey()
	if ak == "" {
		t.Skip("TEST_ACCESS_KEY not set")
	}
	sc := SetupConfig{AccessKey: ak}
	if got := sc.AccessKeyDisplay(); got != ak {
		t.Errorf("AccessKeyDisplay() = %q, want %q", got, ak)
	}

	sc = SetupConfig{}
	if got := sc.AccessKeyDisplay(); got != "<your-access-key>" {
		t.Errorf("AccessKeyDisplay() = %q, want %q", got, "<your-access-key>")
	}
}

func TestSetupConfig_HostBase(t *testing.T) {
	sc := SetupConfig{HostBases: []string{"s3.example.com"}}
	if got := sc.HostBase(); got != "s3.example.com" {
		t.Errorf("HostBase() = %q, want %q", got, "s3.example.com")
	}

	sc = SetupConfig{Endpoint: "http://localhost:9000"}
	if got := sc.HostBase(); got != "localhost:9000" {
		t.Errorf("HostBase() = %q, want %q", got, "localhost:9000")
	}

	sc = SetupConfig{Endpoint: "https://s3.example.com:8080/path"}
	if got := sc.HostBase(); got != "s3.example.com:8080" {
		t.Errorf("HostBase() = %q, want %q", got, "s3.example.com:8080")
	}
}

func TestSetupConfig_IsSubdomainMode(t *testing.T) {
	sc := SetupConfig{HostBases: []string{"s3.example.com"}, SSLMode: "managed"}
	if !sc.IsSubdomainMode() {
		t.Error("IsSubdomainMode() = false, want true")
	}
	if !sc.HasManagedSSL() {
		t.Error("HasManagedSSL() = false, want true")
	}
	if !sc.useSubdomain() {
		t.Error("useSubdomain() = false, want true")
	}

	sc = SetupConfig{HostBases: []string{"s3.example.com"}, SSLMode: "none"}
	if !sc.IsSubdomainMode() {
		t.Error("IsSubdomainMode() = false, want true")
	}
	if sc.HasManagedSSL() {
		t.Error("HasManagedSSL() = true, want false")
	}
	if sc.useSubdomain() {
		t.Error("useSubdomain() = true, want false")
	}

	sc = SetupConfig{SSLMode: "managed"}
	if sc.IsSubdomainMode() {
		t.Error("IsSubdomainMode() = true, want false")
	}
	if sc.useSubdomain() {
		t.Error("useSubdomain() = true, want false")
	}
}

func TestSetupConfig_IsHTTPS(t *testing.T) {
	sc := SetupConfig{Endpoint: "https://s3.example.com"}
	if !sc.IsHTTPS() {
		t.Error("IsHTTPS() = false, want true")
	}

	sc = SetupConfig{Endpoint: "http://localhost:9000"}
	if sc.IsHTTPS() {
		t.Error("IsHTTPS() = true, want false")
	}
}

func TestSetupConfig_Tabs(t *testing.T) {
	sc := SetupConfig{
		Endpoint:   "http://localhost:9000",
		BucketName: "test-bucket",
		AccessKey:  testAccessKey(),
	}
	tabs := sc.Tabs()
	if len(tabs) != len(configRegistry) {
		t.Fatalf("Tabs() returned %d tabs, want %d", len(tabs), len(configRegistry))
	}
	for i, tab := range tabs {
		if tab.Label != configRegistry[i].Label {
			t.Errorf("tab[%d].Label = %q, want %q", i, tab.Label, configRegistry[i].Label)
		}
		if tab.Content == "" {
			t.Errorf("tab[%d].Content is empty", i)
		}
	}
}

func TestConfigRegistry_Order(t *testing.T) {
	want := []string{"AWS CLI", "s3cmd", "rclone", "cURL"}
	if len(configRegistry) != len(want) {
		t.Fatalf("configRegistry has %d entries, want %d", len(configRegistry), len(want))
	}
	for i, entry := range configRegistry {
		if entry.Label != want[i] {
			t.Errorf("configRegistry[%d].Label = %q, want %q", i, entry.Label, want[i])
		}
		if entry.Render == nil {
			t.Errorf("configRegistry[%d].Render is nil", i)
		}
	}
}

// TestConfigRendering verifies that each config renderer produces
// output containing expected substrings.
func TestConfigRendering(t *testing.T) {
	ak := testAccessKey()
	if ak == "" {
		t.Skip("TEST_ACCESS_KEY not set")
	}
	sc := SetupConfig{
		Endpoint:   "http://localhost:9000",
		BucketName: "test-bucket",
		AccessKey:  ak,
	}

	cases := []struct {
		name     string
		render   func(SetupConfig) string
		contains []string
	}{
		{"AWS CLI", configRegistry[0].Render, []string{"endpoint_url", "addressing_style", ak, "test-bucket"}},
		{"s3cmd", configRegistry[1].Render, []string{"host_base", "access_key", ak, "test-bucket"}},
		{"rclone", configRegistry[2].Render, []string{"endpoint", "force_path_style", ak, "test-bucket"}},
		{"cURL", configRegistry[3].Render, []string{"curl", ak, "test-bucket"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := tc.render(sc)
			for _, want := range tc.contains {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, output)
				}
			}
		})
	}
}

// TestS3cmdHostBucket_SinglePercent regression test for the double-percent bug.
// Kody identified that %%(bucket)s rendered literally because text/template
// does not re-process fmt format directives. The output must contain exactly
// one percent sign before (bucket)s.
func TestS3cmdHostBucket_SinglePercent(t *testing.T) {
	sc := SetupConfig{
		Endpoint:   "https://s3.example.com",
		HostBases:  []string{"s3.example.com"},
		SSLMode:    "managed",
		BucketName: "test-bucket",
	}
	got := sc.s3cmdHostBucket()
	if strings.Contains(got, "%%") {
		t.Errorf("s3cmdHostBucket() contains double percent: %q", got)
	}
	if !strings.Contains(got, "%(bucket)s") {
		t.Errorf("s3cmdHostBucket() missing single-percent bucket placeholder: %q", got)
	}
}

// TestS3cmdHostBucket_PathStyle when not in subdomain mode, host_bucket
// should be just the host base with no %(bucket)s prefix.
func TestS3cmdHostBucket_PathStyle(t *testing.T) {
	sc := SetupConfig{
		Endpoint:   "http://localhost:9000",
		HostBases:  nil,
		SSLMode:    "none",
		BucketName: "test-bucket",
	}
	got := sc.s3cmdHostBucket()
	if strings.Contains(got, "%(bucket)s") {
		t.Errorf("s3cmdHostBucket() should not contain bucket placeholder in path mode: %q", got)
	}
	if got != "localhost:9000" {
		t.Errorf("s3cmdHostBucket() = %q, want %q", got, "localhost:9000")
	}
}

// TestSubdomainMode_AWSCLIAddressingStyle verifies that the AWS CLI template
// uses "virtual" addressing in subdomain mode and "path" in path mode.
func TestSubdomainMode_AWSCLIAddressingStyle(t *testing.T) {
	// Subdomain mode
	sc := SetupConfig{
		Endpoint:   "https://s3.example.com",
		HostBases:  []string{"s3.example.com"},
		SSLMode:    "managed",
		BucketName: "test-bucket",
	}
	output := configRegistry[0].Render(sc) // AWS CLI
	if !strings.Contains(output, "addressing_style = virtual") {
		t.Errorf("AWS CLI config should use virtual addressing in subdomain mode\ngot:\n%s", output)
	}

	// Path mode
	sc = SetupConfig{
		Endpoint:   "http://localhost:9000",
		HostBases:  nil,
		SSLMode:    "none",
		BucketName: "test-bucket",
	}
	output = configRegistry[0].Render(sc)
	if !strings.Contains(output, "addressing_style = path") {
		t.Errorf("AWS CLI config should use path addressing in path mode\ngot:\n%s", output)
	}
}

// TestSubdomainMode_CurlURL verifies that the cURL template uses
// subdomain-style URL (bucket.host) in subdomain mode and path-style
// URL (host/bucket) in path mode.
func TestSubdomainMode_CurlURL(t *testing.T) {
	// Subdomain mode: https://test-bucket.s3.example.com
	sc := SetupConfig{
		Endpoint:   "https://s3.example.com",
		HostBases:  []string{"s3.example.com"},
		SSLMode:    "managed",
		BucketName: "test-bucket",
	}
	output := configRegistry[3].Render(sc) // cURL
	if !strings.Contains(output, "test-bucket.s3.example.com") {
		t.Errorf("cURL config should use subdomain URL in subdomain mode\ngot:\n%s", output)
	}
	if strings.Contains(output, "s3.example.com/test-bucket") {
		t.Errorf("cURL config should NOT use path-style URL in subdomain mode\ngot:\n%s", output)
	}

	// Path mode: http://localhost:9000/test-bucket
	sc = SetupConfig{
		Endpoint:   "http://localhost:9000",
		HostBases:  nil,
		SSLMode:    "none",
		BucketName: "test-bucket",
	}
	output = configRegistry[3].Render(sc)
	if !strings.Contains(output, "localhost:9000/test-bucket") {
		t.Errorf("cURL config should use path-style URL in path mode\ngot:\n%s", output)
	}
}

// TestSubdomainMode_RclonePathStyle verifies rclone adapts force_path_style
// correctly between subdomain and path modes.
func TestSubdomainMode_RclonePathStyle(t *testing.T) {
	// Subdomain mode: force_path_style = false
	sc := SetupConfig{
		Endpoint:   "https://s3.example.com",
		HostBases:  []string{"s3.example.com"},
		SSLMode:    "managed",
		BucketName: "test-bucket",
	}
	output := configRegistry[2].Render(sc) // rclone
	if !strings.Contains(output, "force_path_style = false") {
		t.Errorf("rclone config should use force_path_style=false in subdomain mode\ngot:\n%s", output)
	}

	// Path mode: force_path_style = true
	sc = SetupConfig{
		Endpoint:   "http://localhost:9000",
		HostBases:  nil,
		SSLMode:    "none",
		BucketName: "test-bucket",
	}
	output = configRegistry[2].Render(sc)
	if !strings.Contains(output, "force_path_style = true") {
		t.Errorf("rclone config should use force_path_style=true in path mode\ngot:\n%s", output)
	}
}

// TestSubdomainMode_S3cmdHostBucket verifies s3cmd host_bucket adapts
// between subdomain and path modes.
func TestSubdomainMode_S3cmdHostBucket(t *testing.T) {
	// Subdomain mode: %(bucket)s.s3.example.com
	sc := SetupConfig{
		Endpoint:   "https://s3.example.com",
		HostBases:  []string{"s3.example.com"},
		SSLMode:    "managed",
		BucketName: "test-bucket",
	}
	output := configRegistry[1].Render(sc) // s3cmd
	if !strings.Contains(output, "host_bucket = %(bucket)s.s3.example.com") {
		t.Errorf("s3cmd config should use subdomain host_bucket in subdomain mode\ngot:\n%s", output)
	}

	// Path mode: host_bucket = localhost:9000
	sc = SetupConfig{
		Endpoint:   "http://localhost:9000",
		HostBases:  nil,
		SSLMode:    "none",
		BucketName: "test-bucket",
	}
	output = configRegistry[1].Render(sc)
	if !strings.Contains(output, "host_bucket = localhost:9000") {
		t.Errorf("s3cmd config should use plain host_bucket in path mode\ngot:\n%s", output)
	}
}

// TestSubdomainMode_CurlURL_MalformedEndpoint regression test for the panic
// when Endpoint does not contain "://". The scheme prefix is now derived
// from IsHTTPS() instead of fragile index arithmetic.
func TestSubdomainMode_CurlURL_MalformedEndpoint(t *testing.T) {
	sc := SetupConfig{
		Endpoint:   "", // no "://" — would have panicked with strings.Index
		HostBases:  []string{"s3.example.com"},
		SSLMode:    "managed",
		BucketName: "test-bucket",
	}
	// Should not panic
	output := configRegistry[3].Render(sc) // cURL
	if !strings.Contains(output, "test-bucket.s3.example.com") {
		t.Errorf("cURL config should contain subdomain URL\ngot:\n%s", output)
	}
}

func TestPrometheusScrapeConfig_Rendering(t *testing.T) {
	pc := PrometheusConfig{Scheme: "https", Host: "s3.example.com:8080"}
	output := PrometheusScrapeConfig(pc)
	for _, want := range []string{"scrape_configs", "s3-server", "https", "s3.example.com:8080", "basic_auth", "prometheus"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
}
