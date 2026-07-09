package handlers

import (
	"github.com/labstack/echo/v5"
)

func (s *Services) getStats(c *echo.Context) error {
	return s.proxyToAdmin(c, "/stats/uploads")
}

func (s *Services) getPrometheus(c *echo.Context) error {
	return s.proxyToAdmin(c, "/prometheus")
}
