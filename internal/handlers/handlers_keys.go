package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/SiaFoundation/s3d/sia"
	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.uber.org/zap"
)

func (s *Services) listKeys(c *echo.Context) error {
	keys := s.listAccessKeys()
	resp := ListAccessKeysResponse{
		Keys: lo.Map(keys, func(kp backend.AccessKeyInfo, _ int) AccessKeyResponse {
			return AccessKeyResponse{
				AccessKey: kp.AccessKeyID,
				SecretKey: kp.SecretKey,
				UserName:  kp.UserName,
			}
		}),
	}
	return c.JSON(http.StatusOK, resp)
}

func (s *Services) addKey(c *echo.Context) error {
	var req AddAccessKeyRequest
	if err := bindJSON(c, s.log, &req); err != nil {
		return err
	}

	generated := false
	if req.AccessKey == "" && req.SecretKey == "" {
		ak, sk := generateAccessKey()
		req.AccessKey = ak
		req.SecretKey = sk
		generated = true
	} else {
		if req.AccessKey == "" || req.SecretKey == "" {
			return api.SendValidation(c, api.TypeAccessKeyMissing, "both access_key and secret_key required")
		}
		if len(req.AccessKey) < 16 || len(req.AccessKey) > 128 {
			return api.SendValidation(c, api.TypeAccessKeyLength, "access_key must be 16-128 characters")
		}
		if len(req.SecretKey) < 32 || len(req.SecretKey) > 128 {
			return api.SendValidation(c, api.TypeSecretKeyLength, "secret_key must be 32-128 characters")
		}
	}

	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}

	s.keyMu.Lock()

	keys, err := ks.ListAccessKeys(nil)
	if err != nil {
		s.keyMu.Unlock()
		return api.SendInternal(c, api.TypeAccessKeyListFailed, "failed to list access keys", err)
	}

	// check for duplicate
	if lo.SomeBy(keys, func(kp backend.AccessKeyInfo) bool {
		return kp.AccessKeyID == req.AccessKey
	}) {
		s.keyMu.Unlock()
		return api.SendConflict(c, api.TypeAccessKeyCreateFailed, "access key already exists")
	}

	targetUser := req.UserName
	if targetUser == "" {
		return api.SendValidation(c, api.TypeAccessKeyMissing, "user_name is required")
	}

	// ensure the target user exists; ignore already-exists errors.
	if err := ks.CreateUser(targetUser); err != nil && !errors.Is(err, sia.ErrUserAlreadyExists) {
		s.keyMu.Unlock()
		return api.SendInternal(c, api.TypeUserCreateFailed, "failed to create user", err)
	}

	if err := ks.CreateAccessKey(targetUser, req.AccessKey, req.SecretKey); err != nil {
		s.keyMu.Unlock()
		return api.SendInternal(c, api.TypeAccessKeyCreateFailed, "failed to create access key", err)
	}

	updatedCount := len(keys) + 1
	s.keyMu.Unlock()

	// restart backend so s3d picks up the new credential (outside the lock)
	if s.restarter != nil {
		if err := s.restarter.Restart(c.Request().Context()); err != nil {
			s.log.Error("failed to restart backend after adding access key", zap.Error(err))
		}
	}

	// notify SSE clients of the key change
	if s.sseBroker != nil {
		s.sseBroker.NotifyKeyChange("created", updatedCount)
	}

	return c.JSON(http.StatusCreated, AddAccessKeyResponse{
		AccessKey: req.AccessKey,
		SecretKey: req.SecretKey,
		Generated: generated,
	})
}

func (s *Services) deleteKey(c *echo.Context) error {
	accessKey, err := s.requireParam(c, "accessKey")
	if err != nil {
		return err
	}

	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}

	s.keyMu.Lock()

	keys, err := ks.ListAccessKeys(nil)
	if err != nil {
		s.keyMu.Unlock()
		return api.SendInternal(c, api.TypeAccessKeyListFailed, "failed to list access keys", err)
	}

	if !lo.SomeBy(keys, func(kp backend.AccessKeyInfo) bool {
		return kp.AccessKeyID == accessKey
	}) {
		s.keyMu.Unlock()
		return api.SendNotFound(c, api.TypeAccessKeyNotFound, "access key not found")
	}

	if len(keys) == 1 {
		s.keyMu.Unlock()
		return api.SendValidation(c, api.TypeCannotDeleteLastKey, "cannot delete the last access key")
	}

	if err := ks.DeleteAccessKey(accessKey); err != nil {
		s.keyMu.Unlock()
		return api.SendInternal(c, api.TypeAccessKeyDeleteFailed, "failed to delete access key", err)
	}

	remainingCount := len(keys) - 1
	s.keyMu.Unlock()

	// restart backend so s3d drops the removed credential (outside the lock)
	if s.restarter != nil {
		if err := s.restarter.Restart(c.Request().Context()); err != nil {
			s.log.Error("failed to restart backend after deleting access key", zap.Error(err))
		}
	}

	// notify SSE clients of the key change
	if s.sseBroker != nil {
		s.sseBroker.NotifyKeyChange("deleted", remainingCount)
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Services) listUsers(c *echo.Context) error {
	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}
	users, err := ks.ListUsers()
	if err != nil {
		return api.SendInternal(c, api.TypeUserListFailed, "failed to list users", err)
	}
	return c.JSON(http.StatusOK, UserListResponse{Users: users})
}

func (s *Services) createUser(c *echo.Context) error {
	var req UserResponse
	if err := bindJSON(c, s.log, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return api.SendValidation(c, api.TypeNameRequired, "name is required")
	}
	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}
	if err := ks.CreateUser(name); err != nil {
		if errors.Is(err, sia.ErrUserAlreadyExists) {
			return api.SendConflict(c, api.TypeUserCreateFailed, "user already exists")
		}
		return api.SendInternal(c, api.TypeUserCreateFailed, "failed to create user", err)
	}
	return c.JSON(http.StatusCreated, UserResponse{Name: name})
}

func (s *Services) deleteUser(c *echo.Context) error {
	name, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}

	s.keyMu.Lock()
	defer s.keyMu.Unlock()

	if err := ks.DeleteUser(name); err != nil {
		return api.SendInternal(c, api.TypeUserDeleteFailed, "failed to delete user", err)
	}

	// No backend restart needed: s3d reads credentials from the SQLite store
	// on every request via LoadSecret/UserNameForAccessKey. Deleted users and
	// their access keys are immediately invalid: no in-memory cache to flush.

	// notify SSE clients of the key change
	if s.sseBroker != nil {
		remaining := s.listAccessKeysLocked()
		s.sseBroker.NotifyKeyChange("deleted", len(remaining))
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Services) listUserKeys(c *echo.Context) error {
	name, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}
	keys, err := ks.ListAccessKeys(&name)
	if err != nil {
		return api.SendInternal(c, api.TypeAccessKeyListFailed, "failed to list access keys", err)
	}
	resp := ListAccessKeysResponse{
		Keys: lo.Map(keys, func(kp backend.AccessKeyInfo, _ int) AccessKeyResponse {
			return AccessKeyResponse{
				AccessKey: kp.AccessKeyID,
				SecretKey: kp.SecretKey,
				UserName:  kp.UserName,
			}
		}),
	}
	return c.JSON(http.StatusOK, resp)
}
