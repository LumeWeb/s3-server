package components

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	p "github.com/rickb777/plural"
)

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
		case '\t':
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

// JsEscape is the exported version of jsEscape for use by page templates.
func JsEscape(s string) string {
	return jsEscape(s)
}

// MaskKey masks a secret key, showing only the last 4 characters.
// Prevents credential exposure via browser cache, proxy cache, or logs.
func MaskKey(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

// HumanBytes returns a human-readable byte string using decimal (SI) units,
// matching S3/AWS conventions (KB=1000, MB=1000², etc).
func HumanBytes(b int64) string {
	if b == 0 {
		return "0 B"
	}
	const k = 1000.0
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	fb := float64(b)
	if fb < k {
		return fmt.Sprintf("%d B", b)
	}
	i := int(math.Floor(math.Log(fb) / math.Log(k)))
	if i >= len(sizes) {
		i = len(sizes) - 1
	}
	val := fb / math.Pow(k, float64(i))
	return fmt.Sprintf("%s %s", strconv.FormatFloat(val, 'f', 2, 64), sizes[i])
}

// VersioningButtonLabel returns the label text for the versioning toggle button.
func VersioningButtonLabel(status string) string {
	if status == "Enabled" {
		return "Suspend Versioning"
	}
	return "Enable Versioning"
}

// CountWord formats an integer count with a noun, choosing singular or plural
// using the rickb777/plural package. For zero, the plural form is used.
//
//	CountWord(1, "key")    → "1 key"
//	CountWord(2, "key")    → "2 keys"
func CountWord(n int, noun string) string {
	cases := p.FromOne("%v "+noun, "%v "+noun+"s")
	return cases.FormatInt(n)
}

// SelectOption represents a single <option> in a Select component.
type SelectOption struct {
	Value string
	Label string
}

// SelectOpts configures a Select component.
type SelectOpts struct {
	Model   string            // Alpine x-model expression (e.g. "log.level")
	Options []SelectOption
	Class   string            // CSS class (default: "form-select")
	Extra   templ.Attributes  // Rare extra attributes (id, disabled, etc.)
}

// DynamicSelectOpts configures a DynamicSelect component for selects whose
// options are populated client-side via Alpine x-for (e.g. from JSON data).
type DynamicSelectOpts struct {
	Model           string            // Alpine x-model expression
	ItemsExpr       string            // Alpine expression yielding []string of option values
	ModelValue      string            // Server-side initial value for x-model (pre-selects an option)
	StaticOptions   []SelectOption    // Extra static options appended after dynamic ones
	Class           string            // CSS class (default: "form-select")
	Extra           templ.Attributes  // Rare extra attributes (id, disabled, etc.)
}

// InputFieldOpts configures an InputField component.
type InputFieldOpts struct {
	Label       string
	ID          string
	Hint        string
	Model       string            // Alpine x-model expression (without x-model prefix)
	Type        string            // "text" (default), "number", "email", "url"
	Placeholder string
	Class       string            // CSS class (default: "form-input")
	Extra       templ.Attributes  // Extra HTML attributes (required, min, step, etc.)
}

// ButtonOpts configures a Button component.
type ButtonOpts struct {
	Type         string            // "button" (default), "submit"
	Class        string            // CSS class string
	OnClick      string            // Alpine @click expression
	Disabled     string            // Alpine :disabled expression
	LoadingProp  string            // Alpine property for loading state (empty = no spinner)
	LoadingLabel string            // Label shown when loading
	Extra        templ.Attributes  // Rare extra attributes
}
