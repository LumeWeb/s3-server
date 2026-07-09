package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/SiaFoundation/s3d/s3"
	"github.com/SiaFoundation/s3d/s3/s3errs"
	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
	"go.lumeweb.com/s3-server/internal/api"
)

// LifecycleRuleJSON is the simplified JSON representation of an S3 lifecycle
// rule used by the panel API. It exposes only the common fields: expiration
// in days and an optional prefix filter.
type LifecycleRuleJSON struct {
	ID             string `json:"id,omitempty"`
	Status         string `json:"status"`
	Prefix         string `json:"prefix,omitempty"`
	ExpirationDays int    `json:"expiration_days"`
}

// LifecycleConfigJSON is the panel API representation of a bucket lifecycle
// configuration.
type LifecycleConfigJSON struct {
	Rules []LifecycleRuleJSON `json:"rules"`
}

func (lc LifecycleConfigJSON) toS3() s3.LifecycleConfiguration {
	return s3.LifecycleConfiguration{
		Rules: lo.Map(lc.Rules, func(r LifecycleRuleJSON, _ int) s3.LifecycleRule {
			rule := s3.LifecycleRule{
				ID:     r.ID,
				Status: r.Status,
			}
			if r.Prefix != "" {
				p := r.Prefix
				rule.Filter = &s3.LifecycleFilter{Prefix: &p}
			} else {
				rule.Filter = &s3.LifecycleFilter{}
			}
			if r.ExpirationDays > 0 {
				rule.Expiration = &s3.LifecycleExpiration{Days: r.ExpirationDays}
			}
			return rule
		}),
	}
}

func lifecycleConfigToJSON(cfg s3.LifecycleConfiguration) LifecycleConfigJSON {
	return LifecycleConfigJSON{
		Rules: lo.Map(cfg.Rules, func(r s3.LifecycleRule, _ int) LifecycleRuleJSON {
			jr := LifecycleRuleJSON{
				ID:     r.ID,
				Status: r.Status,
			}
			jr.Prefix = r.EffectivePrefix()
			if r.Expiration != nil {
				jr.ExpirationDays = r.Expiration.Days
			}
			return jr
		}),
	}
}

func (s *Services) listBuckets(c *echo.Context) error {
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	buckets, err := b.ListBuckets(c.Request().Context(), accessKey)
	if err != nil {
		return api.SendInternal(c, api.TypeBucketListFailed, "failed to list buckets", err)
	}
	resp := ListBucketsResponse{
		Buckets: lo.Map(buckets, func(bucket s3.BucketInfo, _ int) BucketResponse {
			return BucketResponse{
				Name:      bucket.Name,
				CreatedAt: bucket.CreationDate.Time,
			}
		}),
	}
	return c.JSON(http.StatusOK, resp)
}

func (s *Services) createBucket(c *echo.Context) error {
	var req CreateBucketRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return api.SendValidation(c, api.TypeNameRequired, "name is required")
	}
	if err := s3.ValidateBucketName(name); err != nil {
		return api.SendValidation(c, api.TypeBucketCreateFailed, err.Error())
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	if err := b.CreateBucket(c.Request().Context(), accessKey, name); err != nil {
		return api.SendInternal(c, api.TypeBucketCreateFailed, "failed to create bucket", err)
	}

	// notify SSE clients
	if s.sseBroker != nil {
		s.sseBroker.NotifyBucketChange("created", name)
	}

	return c.JSON(http.StatusCreated, BucketResponse{Name: name})
}

func (s *Services) deleteBucket(c *echo.Context) error {
	name, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	if err := b.DeleteBucket(c.Request().Context(), accessKey, name); err != nil {
		return api.SendInternal(c, api.TypeBucketDeleteFailed, "failed to delete bucket", err)
	}

	// notify SSE clients
	if s.sseBroker != nil {
		s.sseBroker.NotifyBucketChange("deleted", name)
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Services) getBucketVersioning(c *echo.Context) error {
	name, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	status, err := b.GetBucketVersioning(c.Request().Context(), accessKey, name)
	if err != nil {
		return api.SendInternal(c, api.TypeBucketListFailed, "failed to get bucket versioning", err)
	}
	return c.JSON(http.StatusOK, BucketVersioningResponse{Status: status})
}

func (s *Services) putBucketVersioning(c *echo.Context) error {
	name, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	var req SetBucketVersioningRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	status := strings.TrimSpace(req.Status)
	if status != s3.VersioningStatusEnabled && status != s3.VersioningStatusSuspended {
		return api.SendValidation(c, api.TypeRuleStatusInvalid, "status must be 'Enabled' or 'Suspended'")
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	if err := b.PutBucketVersioning(c.Request().Context(), accessKey, name, status); err != nil {
		return api.SendInternal(c, api.TypeBucketCreateFailed, "failed to set bucket versioning", err)
	}
	return c.JSON(http.StatusOK, BucketVersioningResponse{Status: status})
}

func (s *Services) getBucketLifecycle(c *echo.Context) error {
	bucket, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	cfg, err := b.GetBucketLifecycleConfiguration(c.Request().Context(), accessKey, bucket)
	if err != nil {
		if errors.Is(err, s3errs.ErrNoSuchLifecycleConfiguration) {
			return c.JSON(http.StatusOK, lifecycleConfigToJSON(s3.LifecycleConfiguration{}))
		}
		return api.SendInternal(c, api.TypeLifecyclePutFailed, "failed to get lifecycle configuration", err)
	}
	return c.JSON(http.StatusOK, lifecycleConfigToJSON(cfg))
}

func (s *Services) putBucketLifecycle(c *echo.Context) error {
	bucket, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	var req LifecycleConfigJSON
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	// validate rules
	for _, r := range req.Rules {
		if r.Status != s3.LifecycleStatusEnabled && r.Status != s3.LifecycleStatusDisabled {
			return api.SendValidation(c, api.TypeRuleStatusInvalid, "rule status must be Enabled or Disabled")
		}
		if r.ExpirationDays <= 0 {
			return api.SendValidation(c, api.TypeExpirationDaysInvalid, "expiration_days must be a positive integer")
		}
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	s3Cfg := req.toS3()
	if err := s3Cfg.Validate(); err != nil {
		return api.SendValidation(c, api.TypeBucketCreateFailed, err.Error())
	}
	if err := b.PutBucketLifecycleConfiguration(c.Request().Context(), accessKey, bucket, s3Cfg); err != nil {
		return api.SendInternal(c, api.TypeLifecyclePutFailed, "failed to put lifecycle configuration", err)
	}
	return c.JSON(http.StatusOK, lifecycleConfigToJSON(s3Cfg))
}

func (s *Services) deleteBucketLifecycle(c *echo.Context) error {
	bucket, err := s.requireParam(c, "name")
	if err != nil {
		return err
	}
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}
	if err := b.DeleteBucketLifecycleConfiguration(c.Request().Context(), accessKey, bucket); err != nil {
		return api.SendInternal(c, api.TypeLifecycleDeleteFailed, "failed to delete lifecycle configuration", err)
	}
	return c.NoContent(http.StatusNoContent)
}
