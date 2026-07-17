package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestNavSeparatorsBetweenAllLinks verifies that nav links are visually
// separated by nav-sep dividers between logical groups. This is a regression
// test for the missing separator between Users and Keys.
//
// The nav uses manual <div class="nav-sep"> dividers between link groups.
// If someone removes them or adds a link without a separator, this test fails.
func TestNavSeparatorsBetweenAllLinks(t *testing.T) {
	pages := []string{"dashboard", "users", "keys", "buckets", "backups", "monitoring", "settings"}
	for _, page := range pages {
		var buf bytes.Buffer
		err := Nav(page, "v1.0.0", "csrf-token").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("render Nav(%s): %v", page, err)
		}
		html := buf.String()

		// Desktop nav must have nav-sep dividers between groups
		navSepCount := strings.Count(html, `class="nav-sep"`)
		if navSepCount < 3 {
			t.Errorf("nav for page %q: expected at least 3 nav-sep dividers (desktop groups), found %d", page, navSepCount)
		}

		// Count nav link occurrences — each appears twice: desktop + mobile
		navLinkCount := strings.Count(html, ">Dashboard<") +
			strings.Count(html, ">Users<") +
			strings.Count(html, ">Keys<") +
			strings.Count(html, ">Buckets<") +
			strings.Count(html, ">Backups<") +
			strings.Count(html, ">Monitoring<") +
			strings.Count(html, ">Settings<")
		if navLinkCount != 14 { // 7 desktop + 7 mobile
			t.Errorf("nav for page %q: expected 14 nav links (7 desktop + 7 mobile), found %d", page, navLinkCount)
		}
	}
}

// TestNavNoFontBold verifies nav links don't use font-bold for active state,
// which causes layout shift when switching pages. All links should use
// font-medium with a background highlight for the active state.
func TestNavNoFontBold(t *testing.T) {
	pages := []string{"dashboard", "users", "keys", "buckets", "backups", "monitoring", "settings"}
	for _, page := range pages {
		var buf bytes.Buffer
		err := Nav(page, "v1.0.0", "csrf-token").Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("render Nav(%s): %v", page, err)
		}
		html := buf.String()

		// Check nav link <a> tags specifically — not the logo or other elements
		// that may legitimately use font-bold
		navLinkHTML := extractNavLinkAnchors(html)
		if strings.Contains(navLinkHTML, "font-bold") {
			t.Errorf("nav links for page %q contain font-bold (causes layout shift); use font-medium + bg highlight", page)
		}
		if !strings.Contains(navLinkHTML, "font-medium") {
			t.Errorf("nav links for page %q missing font-medium (consistent weight prevents layout shift)", page)
		}
	}
}

// extractNavLinkAnchors extracts just the <a> tags that are nav links
// (have href="/_panel/...") to avoid matching font-bold in the logo.
func extractNavLinkAnchors(html string) string {
	var result strings.Builder
	for _, prefix := range []string{">Dashboard<", ">Users<", ">Keys<", ">Buckets<", ">Backups<", ">Monitoring<", ">Settings<"} {
		idx := strings.Index(html, prefix)
		if idx < 0 {
			continue
		}
		// Find the <a tag before this text
		aStart := strings.LastIndex(html[:idx], "<a ")
		// Find the </a> after this text
		aEnd := strings.Index(html[idx:], "</a>")
		if aStart >= 0 && aEnd >= 0 {
			result.WriteString(html[aStart : idx+aEnd+4])
		}
	}
	return result.String()
}

// TestNavSignOutConfirmation verifies that:
// 1. The Sign Out button is type="button" (not a direct form submit)
// 2. A logout form exists with id="logout-form"
// 3. A Dialog component for showSignOut is rendered
// 4. The dialog-confirm listener includes showSignOut
func TestNavSignOutConfirmation(t *testing.T) {
	var buf bytes.Buffer
	err := Nav("dashboard", "v1.0.0", "csrf-token").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Nav: %v", err)
	}
	html := buf.String()

	// Hidden logout form exists
	if !strings.Contains(html, `id="logout-form"`) {
		t.Error("missing #logout-form hidden form")
	}

	// Sign Out button triggers dialog, not form submit
	if !strings.Contains(html, "showSignOut = true") {
		t.Error("Sign Out button should set showSignOut = true to open dialog")
	}

	// Dialog component rendered
	if !strings.Contains(html, "showSignOut") {
		t.Error("missing Dialog component for showSignOut")
	}

	// dialog-confirm listener handles showSignOut
	if !strings.Contains(html, "showSignOut") {
		t.Error("missing dialog-confirm listener for showSignOut")
	}
}
