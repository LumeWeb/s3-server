package handlers
import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	handlerMocks "go.lumeweb.com/s3-server/internal/handlers/mocks"
	"go.lumeweb.com/s3-server/internal/updater"
)

// newTestServicesWithUpdater is like newTestServices but also wires an UpdaterManager.
func newTestServicesWithUpdater(t *testing.T) (*Services, *handlerMocks.MockUpdaterManager) {
	svc, mockStore, _, _ := newTestServices(t)
	mockUpdater := handlerMocks.NewMockUpdaterManager(t)
	svc.updater = mockUpdater
	_ = mockStore
	return svc, mockUpdater
}

// --- updateStatusAPI ---

func TestServices_UpdateStatusAPI_NilUpdater(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	svc.updater = nil

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/update/status")

	err := svc.updateStatusAPI(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp updater.UpdateStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.SidecarMode)
}

func TestServices_UpdateStatusAPI_WithUpdater(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("Status").Return(updater.UpdateStatus{
		SidecarMode:  true,
		AutoUpdateOn: true,
	})

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/update/status")

	err := svc.updateStatusAPI(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp updater.UpdateStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.SidecarMode)
	assert.True(t, resp.AutoUpdateOn)
}

// --- updateToggleAPI ---

func TestServices_UpdateToggleAPI_NilUpdater(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	svc.updater = nil

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/update/toggle")

	err := svc.updateToggleAPI(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestServices_UpdateToggleAPI_NotAvailable(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(false)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/update/toggle")

	err := svc.updateToggleAPI(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestServices_UpdateToggleAPI_Enable(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(true)
	mockUpdater.On("EnableAutoUpdate").Return(nil)

	body := `{"enabled":true}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/update/toggle", strings.NewReader(body))

	err := svc.updateToggleAPI(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "enabled", resp.Status)
}

func TestServices_UpdateToggleAPI_Disable(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(true)
	mockUpdater.On("DisableAutoUpdate").Return(nil)

	body := `{"enabled":false}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/update/toggle", strings.NewReader(body))

	err := svc.updateToggleAPI(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "disabled", resp.Status)
}

func TestServices_UpdateToggleAPI_EnableError(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(true)
	mockUpdater.On("EnableAutoUpdate").Return(assert.AnError)

	body := `{"enabled":true}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/update/toggle", strings.NewReader(body))

	err := svc.updateToggleAPI(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServices_UpdateToggleAPI_InvalidJSON(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(true)

	body := `not json`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/update/toggle", strings.NewReader(body))

	err := svc.updateToggleAPI(c)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- updateTriggerAPI ---

func TestServices_UpdateTriggerAPI_NilUpdater(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	svc.updater = nil

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/update/trigger")

	err := svc.updateTriggerAPI(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestServices_UpdateTriggerAPI_NotAvailable(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(false)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/update/trigger")

	err := svc.updateTriggerAPI(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestServices_UpdateTriggerAPI_Success(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(true)
	mockUpdater.On("TriggerUpdate").Return(nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/update/trigger")

	err := svc.updateTriggerAPI(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "triggered", resp.Status)
}

func TestServices_UpdateTriggerAPI_Error(t *testing.T) {
	svc, mockUpdater := newTestServicesWithUpdater(t)
	mockUpdater.On("IsAvailable").Return(true)
	mockUpdater.On("TriggerUpdate").Return(assert.AnError)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/update/trigger")

	err := svc.updateTriggerAPI(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// Suppress unused import warning for mock package
var _ = mock.Anything
