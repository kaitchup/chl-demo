package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"chldemo/internal/upay"

	"github.com/labstack/echo/v4"
)

// LogOnlyWebhook returns a handler that logs every inbound UPay event for the
// given merchant secrets without triggering any business logic. The secrets
// slice is injected at route-registration time so the same handler covers any
// number of merchants without code duplication.
func (h *Handler) LogOnlyWebhook(secrets []string) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		ctx := c.Request().Context()

		hdrJSON, _ := json.Marshal(c.Request().Header)
		logID, err := h.repo.SaveRawWebhook(ctx, hdrJSON, raw)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		finalize := func(status int, eventType, respBody string) error {
			_ = h.repo.UpdateWebhookLog(ctx, logID, eventType, respBody)
			return c.NoContent(status)
		}

		if err := upay.VerifyWebhook(
			c.Request().Header.Get("UPay-Signature"), raw, secrets, time.Now(),
		); err != nil {
			return finalize(http.StatusBadRequest, "", "sig_failed")
		}

		evt, err := upay.ParseEvent(raw)
		if err != nil || evt.ID == "" {
			return finalize(http.StatusBadRequest, "", "parse_error")
		}

		return finalize(http.StatusOK, evt.Event, "ok")
	}
}

// UpayWebhook receives UPay events. Order: read raw body -> save raw log ->
// verify signature -> dedupe by event.id -> on paid, re-fetch authoritative
// state and activate. Always returns 2xx quickly on success.
func (h *Handler) UpayWebhook(c echo.Context) error {
	raw, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	ctx := c.Request().Context()

	// 0) Persist raw headers + body immediately, before any validation that
	//    might short-circuit the handler (bad sig, parse error, etc.).
	hdrJSON, _ := json.Marshal(c.Request().Header)
	rawLogID, err := h.repo.SaveRawWebhook(ctx, hdrJSON, raw)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
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

	// 3) Idempotent dedupe on event.id (delivery is at-least-once).
	inserted, err := h.repo.InsertWebhookEvent(ctx, evt.ID, evt.Data.ID, evt.Event, raw, rawLogID)
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

	_ = h.repo.MarkWebhookProcessed(ctx, evt.ID)
	return c.NoContent(http.StatusOK)
}
