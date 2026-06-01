package middleware

import (
	"net/http"
	"strings"

	"chldemo/internal/auth"

	"github.com/labstack/echo/v4"
)

const userIDKey = "user_id"

// JWT validates the Bearer token and stores the user id in the echo context.
func JWT(secret string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Request().Header.Get(echo.HeaderAuthorization)
			parts := strings.SplitN(h, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
			}
			uid, err := auth.Parse(secret, parts[1])
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}
			c.Set(userIDKey, uid)
			return next(c)
		}
	}
}

// UserID returns the authenticated user id set by the JWT middleware.
func UserID(c echo.Context) int64 {
	if v, ok := c.Get(userIDKey).(int64); ok {
		return v
	}
	return 0
}
