package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/updater"
)

// updateStatusAPI returns the current updater status.  When /state is not
// mounted, the response has sidecar_mode=false, letting the frontend
// fall back to badge-only mode (version check via the version API).
func (s *Services) updateStatusAPI(c *echo.Context) error {
	if s.updater == nil {
		return c.JSON(http.StatusOK, updater.UpdateStatus{SidecarMode: false})
	}
	return c.JSON(http.StatusOK, s.updater.Status())
}

// updateToggleRequest is the JSON body for enabling/disabling auto-update.
type updateToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// updateToggleAPI enables or disables auto-update via flag files.
// Returns 503 (UPDATE_NOT_AVAILABLE) when /state is not mounted.
func (s *Services) updateToggleAPI(c *echo.Context) error {
	if s.updater == nil || !s.updater.IsAvailable() {
		return api.SendNotReady(c, api.TypeUpdateNotAvailable, "update sidecar not available (/state volume not mounted)", nil)
	}

	var req updateToggleRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}

	var action string
	var err error
	if req.Enabled {
		err = s.updater.EnableAutoUpdate()
		action = "enabled"
	} else {
		err = s.updater.DisableAutoUpdate()
		action = "disabled"
	}
	if err != nil {
		return api.SendInternal(c, api.TypeUpdateToggleFailed, "failed to toggle auto-update", err)
	}

	return c.JSON(http.StatusOK, ConfigUpdateResponse{
		Status:  action,
		Message: "Auto-update " + action + ".",
	})
}

// updateTriggerAPI creates a one-shot update.trigger flag file requesting
// an immediate update from the sidecar on its next poll.
// Returns 503 (UPDATE_NOT_AVAILABLE) when /state is not mounted.
func (s *Services) updateTriggerAPI(c *echo.Context) error {
	if s.updater == nil || !s.updater.IsAvailable() {
		return api.SendNotReady(c, api.TypeUpdateNotAvailable, "update sidecar not available (/state volume not mounted)", nil)
	}

	if err := s.updater.TriggerUpdate(); err != nil {
		return api.SendInternal(c, api.TypeUpdateTriggerFailed, "failed to trigger update", err)
	}

	return c.JSON(http.StatusOK, ConfigUpdateResponse{
		Status:  "triggered",
		Message: "Update triggered. The sidecar will apply it on the next poll cycle.",
	})
}
