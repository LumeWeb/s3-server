// Package updater provides environment detection and flag-file communication
// with the update sidecar via the shared /state volume.
//
// The panel never touches the Docker socket or uses os/exec.  All control
// flows through simple flag files on the /state volume:
//
//	/state/autoupdate.enabled   : presence => auto-update on (default)
//	/state/autoupdate.disabled  : presence => auto-update off (takes precedence)
//	/state/update.trigger       : presence => one-shot manual update trigger
//	/state/updater.log          : append-only log (panel reads for display)
//	/state/last-digest          : last-applied image digest
package updater

import (
	"os"
	"strings"
)

// StateDir is the shared volume mount-point for sidecar communication.
const StateDir = "/state"

// Flag file paths on the /state volume.
const (
	FlagAutoEnabled  = StateDir + "/autoupdate.enabled"
	FlagAutoDisabled = StateDir + "/autoupdate.disabled"
	FlagTrigger      = StateDir + "/update.trigger"
	FlagUpdaterLog   = StateDir + "/updater.log"
	FlagLastDigest   = StateDir + "/last-digest"
)

// UpdateStatus describes the current update state for the panel UI.
type UpdateStatus struct {
	// SidecarMode is true when /state is mounted (auto-update sidecar present).
	SidecarMode bool `json:"sidecar_mode"`
	// AutoUpdateOn reports whether auto-update is currently enabled.
	AutoUpdateOn bool `json:"auto_update_on"`
	// LastDigest is the last-applied image digest (empty if unknown).
	LastDigest string `json:"last_digest,omitempty"`
	// UpdaterLog holds the last N lines of the updater log (max 50).
	UpdaterLog []string `json:"updater_log,omitempty"`
}

// Manager handles flag-file operations against the /state volume.
// When the /state directory does not exist, all operations degrade to
// no-ops and SidecarMode returns false.
type Manager struct {
	stateDir string
}

// New creates a Manager that uses the real /state path.
func New() *Manager {
	return &Manager{stateDir: StateDir}
}

// NewWithDir creates a Manager with a custom state directory (for testing).
func NewWithDir(dir string) *Manager {
	return &Manager{stateDir: dir}
}

// IsAvailable returns true when the /state volume is mounted and accessible.
func (m *Manager) IsAvailable() bool {
	info, err := os.Stat(m.stateDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// rewritePaths returns the flag paths adjusted for a custom state dir
// (used in tests).  When stateDir is the default "/state", these const
// values are returned directly.
func (m *Manager) flagPath(name string) string {
	if m.stateDir == StateDir {
		return name
	}
	// Replace the "/state" prefix with the custom dir.
	return m.stateDir + strings.TrimPrefix(name, StateDir)
}

// IsAutoUpdateOn reports whether auto-update is enabled.
// Default is ON (when neither flag file exists).  The disabled flag
// takes precedence over the enabled flag.
func (m *Manager) IsAutoUpdateOn() bool {
	if _, err := os.Stat(m.flagPath(FlagAutoDisabled)); err == nil {
		return false
	}
	return true
}

// EnableAutoUpdate removes the disabled flag and writes the enabled flag.
func (m *Manager) EnableAutoUpdate() error {
	if !m.IsAvailable() {
		return nil
	}
	dir := m.stateDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := m.flagPath(FlagAutoDisabled)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	en := m.flagPath(FlagAutoEnabled)
	f, err := os.OpenFile(en, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// DisableAutoUpdate writes the disabled flag and removes the enabled flag.
func (m *Manager) DisableAutoUpdate() error {
	if !m.IsAvailable() {
		return nil
	}
	dir := m.stateDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	en := m.flagPath(FlagAutoEnabled)
	if err := os.Remove(en); err != nil && !os.IsNotExist(err) {
		return err
	}
	path := m.flagPath(FlagAutoDisabled)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// TriggerUpdate creates the update.trigger flag file to request an
// immediate update from the sidecar.
func (m *Manager) TriggerUpdate() error {
	if !m.IsAvailable() {
		return nil
	}
	dir := m.stateDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := m.flagPath(FlagTrigger)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// LastDigest returns the last-applied image digest, or empty string.
func (m *Manager) LastDigest() string {
	data, err := os.ReadFile(m.flagPath(FlagLastDigest))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// UpdaterLog returns the last `maxLines` lines of the updater log.
// If the log file doesn't exist, an empty slice is returned.
func (m *Manager) UpdaterLog(maxLines int) []string {
	data, err := os.ReadFile(m.flagPath(FlagUpdaterLog))
	if err != nil {
		return nil
	}
	if maxLines <= 0 {
		maxLines = 50
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// Status returns a complete snapshot of the current update state.
func (m *Manager) Status() UpdateStatus {
	if !m.IsAvailable() {
		return UpdateStatus{SidecarMode: false}
	}
	return UpdateStatus{
		SidecarMode:  true,
		AutoUpdateOn: m.IsAutoUpdateOn(),
		LastDigest:   m.LastDigest(),
		UpdaterLog:   m.UpdaterLog(50),
	}
}
