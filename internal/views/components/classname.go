package components

import (
	"fmt"
	"strings"
)

// Cls merges a list of class strings, ignoring empty/falsy values.
// Inspired by clsx from the React ecosystem — conditional class composition
// without conflict resolution (Tailwind v4 @utility CSS cascade handles overrides).
//
// Usage in templ:
//
//	class={ components.Cls("btn-primary", isActive && "font-bold", "text-sm") }
//
// Boolean false values are silently dropped, true values are skipped (they
// were used as conditional guards by the caller). Non-string types are
// stringified via fmt.Sprint, with "false" and "" filtered out.
func Cls(parts ...any) string {
	var b strings.Builder
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			if v != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(v)
			}
		case bool:
			// skip — caller passed a condition that evaluated false
			continue
		default:
			s := fmt.Sprint(v)
			if s != "" && s != "false" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(s)
			}
		}
	}
	return b.String()
}
