package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerNotAvailable(t *testing.T) {
	m := NewWithDir("/nonexistent/path/that/does/not/exist")
	if m.IsAvailable() {
		t.Fatal("expected IsAvailable=false for nonexistent dir")
	}
	status := m.Status()
	if status.SidecarMode {
		t.Fatal("expected SidecarMode=false when /state not mounted")
	}
}

func TestManagerAvailable(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)
	if !m.IsAvailable() {
		t.Fatal("expected IsAvailable=true for temp dir")
	}
}

func TestAutoUpdateDefault(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)

	// Default: auto-update is ON when neither flag exists.
	if !m.IsAutoUpdateOn() {
		t.Fatal("expected auto-update on by default")
	}
}

func TestEnableDisableAutoUpdate(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)

	// Disable
	if err := m.DisableAutoUpdate(); err != nil {
		t.Fatalf("DisableAutoUpdate failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "autoupdate.disabled")); err != nil {
		t.Fatal("expected autoupdate.disabled flag to exist")
	}
	if m.IsAutoUpdateOn() {
		t.Fatal("expected auto-update off after disable")
	}

	// Re-enable
	if err := m.EnableAutoUpdate(); err != nil {
		t.Fatalf("EnableAutoUpdate failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "autoupdate.disabled")); !os.IsNotExist(err) {
		t.Fatal("expected autoupdate.disabled flag to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "autoupdate.enabled")); err != nil {
		t.Fatal("expected autoupdate.enabled flag to exist")
	}
	if !m.IsAutoUpdateOn() {
		t.Fatal("expected auto-update on after enable")
	}
}

func TestTriggerUpdate(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)

	if err := m.TriggerUpdate(); err != nil {
		t.Fatalf("TriggerUpdate failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "update.trigger")); err != nil {
		t.Fatal("expected update.trigger flag to exist")
	}
}

func TestLastDigest(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)

	// No digest file
	if d := m.LastDigest(); d != "" {
		t.Fatalf("expected empty digest, got %q", d)
	}

	// Write a digest
	if err := os.WriteFile(filepath.Join(dir, "last-digest"), []byte("sha256:abc123\n"), 0o644); err != nil {
		t.Fatalf("failed to write last-digest: %v", err)
	}
	if d := m.LastDigest(); d != "sha256:abc123" {
		t.Fatalf("expected 'sha256:abc123', got %q", d)
	}
}

func TestUpdaterLog(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)

	// No log file
	log := m.UpdaterLog(50)
	if len(log) != 0 {
		t.Fatalf("expected empty log, got %v", log)
	}

	// Write a log
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "updater.log"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}
	log = m.UpdaterLog(50)
	if len(log) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(log))
	}
	if log[0] != "line1" || log[1] != "line2" || log[2] != "line3" {
		t.Fatalf("unexpected log lines: %v", log)
	}

	// Test truncation
	log = m.UpdaterLog(2)
	if len(log) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(log))
	}
	if log[0] != "line2" || log[1] != "line3" {
		t.Fatalf("unexpected truncated log: %v", log)
	}
}

func TestStatus(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)

	// Write some state
	_ = m.DisableAutoUpdate()
	_ = os.WriteFile(filepath.Join(dir, "last-digest"), []byte("sha256:deadbeef"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "updater.log"), []byte("log line 1\nlog line 2\n"), 0o644)

	status := m.Status()
	if !status.SidecarMode {
		t.Fatal("expected SidecarMode=true")
	}
	if status.AutoUpdateOn {
		t.Fatal("expected AutoUpdateOn=false")
	}
	if status.LastDigest != "sha256:deadbeef" {
		t.Fatalf("expected sha256:deadbeef, got %s", status.LastDigest)
	}
	if len(status.UpdaterLog) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(status.UpdaterLog))
	}
}
