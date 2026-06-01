package handler

import (
	"io"
	"net/http"
	"time"

	"chldemo/internal/upay"

	"github.com/labstack/echo/v4"
)

// UpayWebhook receives UPay events. Order: read raw body -> verify signature ->
// dedupe by event.id -> on paid, re-fetch authoritative state and activate.
// Always returns 2xx quickly on success (UPay only reads the status code).
func (h *Handler) UpayWebhook(c echo.Context) error {
	raw, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	if h.payment == nil {
		return c.NoContent(http.StatusServiceUnavailable) // UPay not configured
	}

	// 1) Verify signature over the RAW bytes.
	if err := upay.VerifyWebhook(
		c.Request().Header.Get("UPay-Signature"), raw, h.cfg.WebhookSecrets(), time.Now(),
	); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// 2) Parse event.
	evt, err := upay.ParseEvent(raw)
	if err != nil || evt.ID == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	ctx := c.Request().Context()

	// 3) Idempotent dedupe on event.id (delivery is at-least-once).
	inserted, err := h.repo.InsertWebhookEvent(ctx, evt.ID, evt.Data.ID, evt.Event, raw)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	if !inserted {
		return c.NoContent(http.StatusOK) // already processed
	}

	// 4) Only full payment activates membership; re-fetch authoritative state.
	if evt.Event == "payment_order.paid" {
		if err := h.payment.ConfirmAndActivate(ctx, evt.Data.ID); err != nil {
			// Return non-2xx so UPay retries; reconcile job also backs this up.
			return c.NoContent(http.StatusInternalServerError)
		}
	}
	// partially_paid / others: recorded above, order stays PENDING.
	return c.NoContent(http.StatusOK)
}
