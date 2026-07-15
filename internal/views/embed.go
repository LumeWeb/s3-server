package views

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
)

//go:embed css/tailwind.css
var TailwindCSS string

//go:embed all:web/dist
var WebFS embed.FS

// AssetVersion is a content hash of all embedded frontend assets.
// Appended as ?v= to asset URLs for cache busting. Since the binary
// embeds the assets, this hash changes exactly when the assets change.
var AssetVersion string

func init() {
	h := sha256.New()
	h.Write([]byte(TailwindCSS))
	if data, err := WebFS.ReadFile("web/dist/panel.js"); err == nil {
		h.Write(data)
	}
	if data, err := WebFS.ReadFile("web/dist/panel.css"); err == nil {
		h.Write(data)
	}
	AssetVersion = hex.EncodeToString(h.Sum(nil))[:12]
}
