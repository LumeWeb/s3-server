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
	"go.lumeweb.com/s3-server/internal/config"
	handlerMocks "go.lumeweb.com/s3-server/internal/handlers/mocks"
)

// --- getLogConfig ---

func TestServices_GetLogConfig(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("LogConfig").Return(config.LogConfig{Level: "debug", Format: "json"})

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/log-config")

	err := svc.getLogConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp LogConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "debug", resp.Level)
	assert.Equal(t, "json", resp.Format)
}

// --- setLogConfig ---

func TestServices_SetLogConfig_Success(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("SetLogConfig", config.LogConfig{Level: "info", Format: "json"}).Return(nil)

	body := `{"level":"info","format":"json"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPut, "/api/log-config", strings.NewReader(body))

	err := svc.setLogConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_SetLogConfig_EmptyLevel(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `{"level":"","format":"json"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPut, "/api/log-config", strings.NewReader(body))

	err := svc.setLogConfig(c)
	assert.NoError(t, err) // error already written via JSON
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_SetLogConfig_InvalidLevel(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `{"level":"trace"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPut, "/api/log-config", strings.NewReader(body))

	err := svc.setLogConfig(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_SetLogConfig_InvalidFormat(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `{"level":"info","format":"xml"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPut, "/api/log-config", strings.NewReader(body))

	err := svc.setLogConfig(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_SetLogConfig_StoreError(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("SetLogConfig", mock.Anything).Return(assert.AnError)

	body := `{"level":"warn"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPut, "/api/log-config", strings.NewReader(body))

	err := svc.setLogConfig(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServices_SetLogConfig_HotReload(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("SetLogConfig", mock.Anything).Return(nil)

	var captured config.LogConfig
	svc.logLevelUpdater = &stubLogLevelUpdater{onSet: func(lc config.LogConfig) { captured = lc }}

	body := `{"level":"error","format":"human"}`
	e := echo.New()
	c, _ := testJSONContext(e, http.MethodPut, "/api/log-config", strings.NewReader(body))

	err := svc.setLogConfig(c)
	require.NoError(t, err)
	assert.Equal(t, "error", captured.Level)
	assert.Equal(t, "human", captured.Format)
}

type stubLogLevelUpdater struct {
	onSet func(config.LogConfig)
}

func (s *stubLogLevelUpdater) SetLevel(level config.LogConfig) {
	if s.onSet != nil {
		s.onSet(level)
	}
}

// --- changePassword ---

func TestServices_ChangePassword_Success(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("ValidateAdminPassword", "oldpass").Return(true)
	mockStore.On("SetAdminPassword", "newpass123").Return(nil)

	body := `{"current_password":"oldpass","new_password":"newpass123"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_ChangePassword_EmptyCurrent(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `{"current_password":"","new_password":"newpass123"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_ChangePassword_EmptyNew(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `{"current_password":"oldpass","new_password":""}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_ChangePassword_TooShort(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `{"current_password":"oldpass","new_password":"short"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_ChangePassword_WrongCurrent(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("ValidateAdminPassword", "wrongpass").Return(false)

	body := `{"current_password":"wrongpass","new_password":"newpass123"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServices_ChangePassword_StoreError(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.On("ValidateAdminPassword", "oldpass").Return(true)
	mockStore.On("SetAdminPassword", "newpass123").Return(assert.AnError)

	body := `{"current_password":"oldpass","new_password":"newpass123"}`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServices_ChangePassword_InvalidJSON(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	body := `not json`
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/password/change", strings.NewReader(body))

	err := svc.changePassword(c)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- sseEvents ---

func TestServices_SSEEvents_NoBroker(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	svc.sseBroker = nil

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/events")

	err := svc.sseEvents(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServices_SSEEvents_Delegates(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	broker := &handlerMocks.MockSSEBroker{}
	broker.Test(t)
	broker.On("ServeHTTP", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		w := args.Get(0).(http.ResponseWriter)
		w.WriteHeader(http.StatusOK)
	}).Maybe()
	svc.sseBroker = broker

	_ = mockStore // suppress unused
	e := echo.New()
	c, _ := testContext(e, http.MethodGet, "/api/events")

	err := svc.sseEvents(c)
	require.NoError(t, err)
}
