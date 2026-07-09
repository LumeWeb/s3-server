package js

import "fmt"

// CopyToClipboard returns a hyperscript directive that copies text to the
// clipboard and shows a toast notification.
func CopyToClipboard(text string, label string) string {
	// Escape single quotes for safe embedding in hyperscript strings.
	escaped := fmt.Sprintf("%q", text)
	labelEscaped := fmt.Sprintf("%q", label)
	return fmt.Sprintf(`on click call navigator.clipboard.writeText(%s) then window.__sseToast('Copied '+%s, 'success')`, escaped, labelEscaped)
}
