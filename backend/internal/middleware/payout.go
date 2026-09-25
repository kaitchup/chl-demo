package middleware

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

// PayoutEnabled gates /api/payout/* to whitelisted users (users.payout_enabled).
// Must run after JWT.
func PayoutEnabled(allowed func(ctx context.Context, userID int64) (bool, error)) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ok, err := allowed(c.Request().Context(), UserID(c))
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "whitelist check failed")
			}
			if !ok {
				return c.JSON(http.StatusForbidden, map[string]any{
					"data": nil, "error": map[string]string{"code": "PAYOUT_NOT_ENABLED", "message": "payout is not enabled for this account"},
				})
			}
			return next(c)
		}
	}
}
