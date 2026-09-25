package handler

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"chldemo/internal/upayopen"

	"github.com/labstack/echo/v4"
)

// PayoutWebhook receives UPay OpenAPI payout pushes (PO_ORDER_STATUS_CHANGE).
// Minimal version: raw log -> decrypt -> verify -> log detail -> plain-text
// SUCCESS. It does not touch order state yet; that arrives with the payout
// module (TECH-DESIGN-汇款功能.md §6.4).
//
// The body is decrypted before verifying because the signed string starts with
// the event name, which lives inside the ciphertext. Any correctly signed event
// is acknowledged, so UPay's address check passes whatever event it sends.
func (h *Handler) PayoutWebhook(secret string, key *rsa.PrivateKey) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		ctx := c.Request().Context()

		hdrJSON, _ := json.Marshal(c.Request().Header)
		logID, err := h.repo.SaveRawWebhook(ctx, hdrJSON, string(raw), "upay-payout")
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

		if w.Event == upayopen.EventPayoutOrderStatusChange {
			d, err := w.PayoutDetail()
			if err != nil {
				log.Printf("payout_webhook: log=%d notify=%s bad detail: %v", logID, w.OrderNo, err)
			} else {
				log.Printf("payout_webhook: log=%d notify=%s trace=%s order=%s status=%d",
					logID, w.OrderNo, w.Trace, d.OrderNo, d.Status)
			}
		} else {
			log.Printf("payout_webhook: log=%d notify=%s event=%s (acknowledged, not handled)",
				logID, w.OrderNo, w.Event)
		}
		return finalize(http.StatusOK, w.Event, "ok")
	}
}
