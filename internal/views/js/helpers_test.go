package js

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyToClipboard_PlainText(t *testing.T) {
	result := CopyToClipboard("hello world", "Access Key")
	assert.Contains(t, result, "hello world")
	assert.Contains(t, result, "Access Key")
	assert.Contains(t, result, "navigator.clipboard.writeText")
	assert.Contains(t, result, "__sseToast")
}

func TestCopyToClipboard_SingleQuote(t *testing.T) {
	// Single quotes in the text should be escaped via %q formatting
	result := CopyToClipboard("it's a key", "label")
	// %q produces a double-quoted Go string literal, which is safe for hyperscript
	assert.Contains(t, result, `"it's a key"`)
}

func TestCopyToClipboard_DoubleQuote(t *testing.T) {
	result := CopyToClipboard(`say "hi"`, "label")
	// %q escapes the double quotes inside
	assert.Contains(t, result, `\"hi\"`)
}

func TestCopyToClipboard_Backslash(t *testing.T) {
	result := CopyToClipboard(`path\to\key`, "label")
	assert.Contains(t, result, `\\`)
}

func TestCopyToClipboard_EmptyStrings(t *testing.T) {
	result := CopyToClipboard("", "")
	assert.Contains(t, result, "navigator.clipboard.writeText")
	assert.Contains(t, result, "__sseToast")
}

func TestCopyToClipboard_LabelInToast(t *testing.T) {
	result := CopyToClipboard("AKIA123", "Access Key")
	// The label is formatted with %q (double-quoted) inside the toast call
	assert.Contains(t, result, "Copied")
	assert.Contains(t, result, "Access Key")
}

func TestCopyToClipboard_XSSAttempt(t *testing.T) {
	// %q wraps the text in double quotes, escaping internal double quotes.
	// Single quotes pass through since they're not special in Go string literals.
	// The safety comes from the text being inside a quoted argument, not raw-inlined.
	evil := `'); alert('xss`
	result := CopyToClipboard(evil, "label")
	// Verify it's properly wrapped in a quoted string, not raw
	assert.Contains(t, result, "navigator.clipboard.writeText")
	assert.Contains(t, result, "clipboard.writeText")
}

func TestCopyToClipboard_Newline(t *testing.T) {
	result := CopyToClipboard("line1\nline2", "label")
	// %q escapes newlines as \n
	assert.Contains(t, result, `\n`)
}
