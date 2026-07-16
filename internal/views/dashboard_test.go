package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestDashboardStatsPanelEqualWidth verifies the stats panel uses flex-1 on
// each cell so they're equal-width, and text-center for centered content.
// Regression test for uneven-width stats cells.
func TestDashboardStatsPanelEqualWidth(t *testing.T) {
	var buf bytes.Buffer
	err := DashboardContent("v1.0.0", nil, 3, 2, "csrf-token").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render DashboardContent: %v", err)
	}
	html := buf.String()

	// All three stat cells must use flex-1 (equal width)
	flexCount := strings.Count(html, "flex-1")
	if flexCount < 3 {
		t.Errorf("expected at least 3 flex-1 classes in stats panel for equal-width cells, got %d", flexCount)
	}

	// Each cell must have text-center
	textCenterCount := strings.Count(html, "text-center")
	if textCenterCount < 3 {
		t.Errorf("expected at least 3 text-center classes in stats panel, got %d", textCenterCount)
	}
}

// TestDashboardKeyDeleteDialog verifies the dashboard page includes a Dialog
// component for key deletion and the dialog-confirm event listener.
// Regression test for the non-functional dashboard key delete.
func TestDashboardKeyDeleteDialog(t *testing.T) {
	var buf bytes.Buffer
	err := DashboardContent("v1.0.0", nil, 0, 0, "csrf-token").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render DashboardContent: %v", err)
	}
	html := buf.String()

	// Must have the dialog-confirm listener wired to confirmDelete
	if !strings.Contains(html, "showDelete") {
		t.Error("dashboard missing showDelete dialog-confirm listener")
	}
	if !strings.Contains(html, "confirmDelete()") {
		t.Error("dashboard missing confirmDelete() call in dialog-confirm listener")
	}

	// Must render the Delete Access Key Dialog component
	if !strings.Contains(html, "Delete Access Key") {
		t.Error("dashboard missing Delete Access Key Dialog component")
	}
}
