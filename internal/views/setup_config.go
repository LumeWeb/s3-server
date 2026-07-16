package views

import (
	"bytes"
	"embed"
	"strings"
	"sync"
	"text/template"

	"go.lumeweb.com/s3-server/internal/views/components"
)

//go:embed setup_templates/*.tmpl
var setupTemplatesFS embed.FS

const secretPlaceholder = "<your-secret-key>"
const adminPasswordPlaceholder = "<your-admin-password>"

// SetupConfig holds the server-side context needed to generate
// S3 client configuration snippets for a specific bucket.
type SetupConfig struct {
	Endpoint   string   // e.g. "https://s3.example.com:8080" or "http://localhost:9000"
	HostBases  []string // from S3Config.HostBases
	SSLMode    string   // from SSLConfig.Mode ("none", "platform", "managed")
	BucketName string   // the bucket these configs are for
	AccessKey  string   // first access key ID for the bucket owner (may be empty)
}

// PrometheusConfig holds the data needed to render a prometheus.yml scrape config.
type PrometheusConfig struct {
	Scheme string // "http" or "https"
	Host   string // host:port
}

// IsSubdomainMode returns true if virtual-hosted-style (subdomain) S3 access
// is available: requires HostBases to be configured.
func (sc SetupConfig) IsSubdomainMode() bool {
	return len(sc.HostBases) > 0
}

// HasManagedSSL returns true if SSL is in managed mode (ACME certs provisioned).
func (sc SetupConfig) HasManagedSSL() bool {
	return sc.SSLMode == "managed"
}

// IsHTTPS returns true if the endpoint uses HTTPS.
func (sc SetupConfig) IsHTTPS() bool {
	return strings.HasPrefix(sc.Endpoint, "https://")
}

// httpsBool returns "True" or "False" for s3cmd config.
func (sc SetupConfig) httpsBool() string {
	if sc.IsHTTPS() {
		return "True"
	}
	return "False"
}

// AccessKeyDisplay returns the access key ID or a placeholder if none is set.
func (sc SetupConfig) AccessKeyDisplay() string {
	if sc.AccessKey != "" {
		return sc.AccessKey
	}
	return "<your-access-key>"
}

// HostBase returns the first configured host base hostname, or the endpoint
// host if none is set.
func (sc SetupConfig) HostBase() string {
	if len(sc.HostBases) > 0 {
		return sc.HostBases[0]
	}
	u := strings.TrimPrefix(sc.Endpoint, "https://")
	u = strings.TrimPrefix(u, "http://")
	parts := strings.SplitN(u, "/", 2)
	return parts[0]
}

// useSubdomain returns true if subdomain-style URLs should be used in configs.
func (sc SetupConfig) useSubdomain() bool {
	return sc.IsSubdomainMode() && sc.HasManagedSSL()
}

// s3cmdHostBucket returns the host_bucket value for s3cmd config.
// Subdomain-style: %(bucket)s.host_base; path-style: host_base.
func (sc SetupConfig) s3cmdHostBucket() string {
	hb := sc.HostBase()
	if sc.useSubdomain() {
		return "%(bucket)s." + hb
	}
	return hb
}

// rclonePathStyle returns "true" or "false" for rclone's force_path_style.
func (sc SetupConfig) rclonePathStyle() string {
	if sc.useSubdomain() {
		return "false"
	}
	return "true"
}

// templateData is the struct passed to text/template execution.
type templateData struct {
	Endpoint                 string
	BucketName               string
	AccessKeyDisplay         string
	HostBase                 string
	S3cmdHostBucket          string
	HTTPSBool                string
	RclonePathStyle          string
	SecretPlaceholder        string
	AdminPasswordPlaceholder string
	Scheme                   string // for Prometheus template
	Host                     string // for Prometheus template
	AWSCLIAddressingStyle    string // "virtual" or "path"
	CurlURL                  string // full URL for cURL example
}

func (sc SetupConfig) templateData() templateData {
	td := templateData{
		Endpoint:                 sc.Endpoint,
		BucketName:               sc.BucketName,
		AccessKeyDisplay:         sc.AccessKeyDisplay(),
		HostBase:                 sc.HostBase(),
		S3cmdHostBucket:          sc.s3cmdHostBucket(),
		HTTPSBool:                sc.httpsBool(),
		RclonePathStyle:           sc.rclonePathStyle(),
		SecretPlaceholder:        secretPlaceholder,
		AdminPasswordPlaceholder: adminPasswordPlaceholder,
	}
	if sc.useSubdomain() {
		td.AWSCLIAddressingStyle = "virtual"
		scheme := "https://"
		if !sc.IsHTTPS() {
			scheme = "http://"
		}
		td.CurlURL = scheme + sc.BucketName + "." + sc.HostBase()
	} else {
		td.AWSCLIAddressingStyle = "path"
		td.CurlURL = sc.Endpoint + "/" + sc.BucketName
	}
	return td
}

var parsedTemplates sync.Map

// renderTemplate executes a text/template from the embedded FS.
// Templates are parsed once and cached via sync.Map; *template.Template
// is concurrency-safe for Execute after parsing.
func renderTemplate(name string, data templateData) string {
	var t *template.Template
	if v, ok := parsedTemplates.Load(name); ok {
		t = v.(*template.Template)
	} else {
		raw, err := setupTemplatesFS.ReadFile("setup_templates/" + name + ".tmpl")
		if err != nil {
			return "" // should never happen with embedded files
		}
		t, err = template.New(name).Parse(string(raw))
		if err != nil {
			return string(raw)
		}
		parsedTemplates.Store(name, t)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return ""
	}
	return buf.String()
}

// ConfigTab is a registry entry mapping a tab label to a string renderer.
type ConfigTab struct {
	Label  string
	Render func(SetupConfig) string
}

// configRegistry is the ordered list of client config tabs shown in the
// setup guide. To add a new client, add a .tmpl file under setup_templates/
// and append an entry here.
var configRegistry = []ConfigTab{
	{Label: "AWS CLI", Render: func(sc SetupConfig) string { return renderTemplate("aws_cli", sc.templateData()) }},
	{Label: "s3cmd", Render: func(sc SetupConfig) string { return renderTemplate("s3cmd", sc.templateData()) }},
	{Label: "rclone", Render: func(sc SetupConfig) string { return renderTemplate("rclone", sc.templateData()) }},
	{Label: "cURL", Render: func(sc SetupConfig) string { return renderTemplate("curl", sc.templateData()) }},
}

// Tabs returns the registry entries as CodeBlockTabs for the given bucket.
func (sc SetupConfig) Tabs() []components.CodeBlockTab {
	tabs := make([]components.CodeBlockTab, len(configRegistry))
	for i, entry := range configRegistry {
		tabs[i] = components.CodeBlockTab{
			Label:   entry.Label,
			Content: entry.Render(sc),
		}
	}
	return tabs
}

// PrometheusScrapeConfig renders a prometheus.yml scrape config string.
func PrometheusScrapeConfig(pc PrometheusConfig) string {
	return renderTemplate("prometheus", templateData{
		Scheme:                   pc.Scheme,
		Host:                     pc.Host,
		AdminPasswordPlaceholder: adminPasswordPlaceholder,
	})
}
