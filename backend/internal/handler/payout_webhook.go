package handler

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"chldemo/internal/service"
	"chldemo/internal/upayopen"

	"github.com/labstack/echo/v4"
)

// PayoutWebhook receives UPay OpenAPI payout pushes (PO_ORDER_STATUS_CHANGE):
// raw log -> decrypt -> verify -> dedupe on the notification id -> plain-text
// SUCCESS, then re-read the order from UPay in the background (detail.status is
// not trusted: statuses are not monotonic and retries may arrive out of order).
// See TECH-DESIGN-汇款功能.md §6.4. svc may be nil (webhook-only config): the
// push is then only logged; the sync job replays it once payout is enabled.
//
// The body is decrypted before verifying because the signed string starts with
// the event name, which lives inside the ciphertext. Any correctly signed event
// is acknowledged, so UPay's address check passes whatever event it sends.
func (h *Handler) PayoutWebhook(secret string, key *rsa.PrivateKey, svc *service.PayoutService) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		ctx := c.Request().Context()

		hdrJSON, _ := json.Marshal(c.Request().Header)
		logID, err := h.repo.SaveRawWebhook(ctx, hdrJSON, string(raw), "upay-busi-payout")
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}
		finalize := func(status int, eventType, outcome string) error {
			_ = h.repo.UpdateWebhookLog(ctx, logID, eventType, outcome)
			if status == http.StatusOK {
				return c.String(http.StatusOK, "SUCCESS") // UPay requires exactly this body
			}
			return c.NoContent(status)
		}

		if secret == "" || key == nil {
			return finalize(http.StatusServiceUnavailable, "", "not_configured")
		}

		w, err := upayopen.DecodeWebhook(raw, key)
		if err != nil {
			log.Printf("payout_webhook: log=%d decrypt failed: %v", logID, err)
			return finalize(http.StatusBadRequest, "", "decrypt_failed")
		}

		hdr := c.Request().Header
		if err := upayopen.VerifyWebhook(secret, w.Event,
			hdr.Get("X-UPA-TIMESTAMP"), hdr.Get("X-UPA-SIGN"), raw, time.Now()); err != nil {
			log.Printf("payout_webhook: log=%d event=%s verify failed: %v", logID, w.Event, err)
			outcome := "sig_failed"
			if errors.Is(err, upayopen.ErrStale) {
				outcome = "stale"
			}
			return finalize(http.StatusUnauthorized, w.Event, outcome)
		}

		if w.Event != upayopen.EventPayoutOrderStatusChange {
			log.Printf("payout_webhook: log=%d notify=%s event=%s (acknowledged, not handled)",
				logID, w.OrderNo, w.Event)
			return finalize(http.StatusOK, w.Event, "ok")
		}
		d, err := w.PayoutDetail()
		if err != nil {
			log.Printf("payout_webhook: log=%d notify=%s bad detail: %v", logID, w.OrderNo, err)
			return finalize(http.StatusOK, w.Event, "bad_detail") // retrying won't fix it
		}
		log.Printf("payout_webhook: log=%d notify=%s trace=%s order=%s status=%d",
			logID, w.OrderNo, w.Trace, d.OrderNo, d.Status)

		pushedAt := time.UnixMilli(w.Timestamp)
		if w.Timestamp == 0 {
			pushedAt = time.Now()
		}
		inserted, err := h.repo.InsertPayoutWebhookEvent(ctx, w.OrderNo, logID, d.OrderNo, d.Status, pushedAt)
		if err != nil {
			return finalize(http.StatusInternalServerError, w.Event, "db_error")
		}
		if !inserted {
			return finalize(http.StatusOK, w.Event, "duplicate")
		}
		if svc != nil {
			// UPay needs SUCCESS within 5s; the order/info round trip happens after.
			go func(notifyNo, orderNo string) {
				c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := svc.SyncByOrderNo(c, orderNo, pushedAt, "webhook"); err != nil {
					log.Printf("payout_webhook: notify=%s sync failed (job will retry): %v", notifyNo, err)
					return
				}
				_ = h.repo.MarkPayoutWebhookProcessed(c, notifyNo)
			}(w.OrderNo, d.OrderNo)
		}
		return finalize(http.StatusOK, w.Event, "ok")
	}
}
