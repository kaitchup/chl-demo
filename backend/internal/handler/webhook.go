package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"chldemo/internal/repo"
	"chldemo/internal/upay"

	"github.com/labstack/echo/v4"
)

// LogOnlyWebhook returns a handler that records every inbound UPay event for the
// given merchant secrets WITHOUT triggering business logic (no membership
// activation — that is UPay main-merchant only). It persists the raw request
// (webhook_raw_log) and, on a valid signed + parseable event, also records it in
// webhook_events (deduped on event.id). The secrets slice and source name are
// injected at route-registration time so the same handler covers any number of
// merchants without code duplication.
func (h *Handler) LogOnlyWebhook(secrets []string, source string) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		ctx := c.Request().Context()

		hdrJSON, _ := json.Marshal(c.Request().Header)
		logID, err := h.repo.SaveRawWebhook(ctx, hdrJSON, string(raw), source)
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

		// Record the event. Delivery is at-least-once, so dedupe on event.id.
		// No activation is performed for these merchants — recording only.
		inserted, err := h.repo.InsertWebhookEvent(ctx, evt.ID, evt.Data.ID, evt.Event, raw, logID, source)
		if err != nil {
			return finalize(http.StatusInternalServerError, evt.Event, "db_error")
		}
		if !inserted {
			return finalize(http.StatusOK, evt.Event, "duplicate")
		}

		_ = h.repo.MarkWebhookProcessed(ctx, evt.ID)
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
	rawLogID, err := h.repo.SaveRawWebhook(ctx, hdrJSON, string(raw), "upay")
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
	inserted, err := h.repo.InsertWebhookEvent(ctx, evt.ID, evt.Data.ID, evt.Event, raw, rawLogID, "upay")
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

// ProdWebhook returns a handler for production merchants. Unlike LogOnlyWebhook it
// verifies the signature FIRST — invalid requests are not persisted. Valid events are
// deduplicated at two layers: application (exists check) and storage (UNIQUE constraint).
// exists and insert are injected so the same handler covers different merchant tables.
func (h *Handler) ProdWebhook(
	secrets []string,
	exists func(ctx context.Context, eventID string) (bool, error),
	insert func(ctx context.Context, eventID, headers, body, eventType string) error,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		ctx := c.Request().Context()

		// 1. Verify signature — bad requests are not persisted.
		if err := upay.VerifyWebhook(
			c.Request().Header.Get("UPay-Signature"), raw, secrets, time.Now(),
		); err != nil {
			log.Printf("prod_webhook: sig failed: %v", err)
			return c.NoContent(http.StatusBadRequest)
		}

		// 2. Parse event to obtain event_id.
		evt, err := upay.ParseEvent(raw)
		if err != nil || evt.ID == "" {
			log.Printf("prod_webhook: parse error: %v", err)
			return c.NoContent(http.StatusBadRequest)
		}

		// 3. Application-layer dedup.
		dup, err := exists(ctx, evt.ID)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}
		if dup {
			log.Printf("prod_webhook: duplicate event_id=%s", evt.ID)
			return c.NoContent(http.StatusOK)
		}

		// 4. Persist — UNIQUE constraint catches race-condition duplicates.
		hdrText, _ := json.Marshal(c.Request().Header)
		if err := insert(ctx, evt.ID, string(hdrText), string(raw), evt.Event); err != nil {
			if errors.Is(err, repo.ErrConflict) {
				log.Printf("prod_webhook: race-dup event_id=%s", evt.ID)
				return c.NoContent(http.StatusOK)
			}
			return c.NoContent(http.StatusInternalServerError)
		}

		return c.NoContent(http.StatusOK)
	}
}
