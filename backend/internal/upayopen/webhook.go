package upayopen

import (
	"crypto/hmac"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// EventPayoutOrderStatusChange is the only event UPay pushes for payouts.
const EventPayoutOrderStatusChange = "PO_ORDER_STATUS_CHANGE"

var (
	ErrBadSignature = errors.New("upayopen: webhook signature mismatch")
	ErrStale        = errors.New("upayopen: webhook timestamp outside tolerance")
)

// WebhookTolerance bounds |now - X-UPA-TIMESTAMP|. UPay reuses the first push
// time on every retry (WebhookConsumer.post), so the window must be wider than
// its queueing delay; duplicates are caught by the notification id instead.
const WebhookTolerance = 30 * time.Minute

// Webhook is the decrypted envelope. OrderNo here is the *notification* id
// (unique per push, use it for dedupe); the payout order number is in Detail.
type Webhook struct {
	Event     string `json:"event"`
	Trace     string `json:"trace"`
	OrderNo   string `json:"orderNo"`
	Timestamp int64  `json:"timestamp"`
	Detail    string `json:"detail"` // JSON string, see PayoutStatusDetail
}

// PayoutStatusDetail is Webhook.Detail for PO_ORDER_STATUS_CHANGE.
type PayoutStatusDetail struct {
	PayerNo       string `json:"payerNo"`
	BankAccountNo string `json:"bankAccountNo"`
	OrderNo       string `json:"orderNo"` // payout order number (payout_order.order_no)
	Status        int    `json:"status"`
}

// VerifyWebhook checks X-UPA-SIGN before anything is decrypted. Per UPay's
// sender (upay-broker-server WebhookConsumer.post) the signed string is
// event + "|" + X-UPA-TIMESTAMP + "|" + <raw JWE body>, keyed with the same
// SecretKey used for API request signing.
func VerifyWebhook(secret, event, tsHeader, signHeader string, rawBody []byte, now time.Time) error {
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return fmt.Errorf("upayopen: bad X-UPA-TIMESTAMP %q", tsHeader)
	}
	if d := now.Sub(time.UnixMilli(ts)); d > WebhookTolerance || d < -WebhookTolerance {
		return ErrStale
	}
	want := Sign(secret, event+"|"+tsHeader+"|"+string(rawBody))
	if !hmac.Equal([]byte(want), []byte(signHeader)) {
		return ErrBadSignature
	}
	return nil
}

// DecodeWebhook decrypts the body into its envelope.
func DecodeWebhook(rawBody []byte, key *rsa.PrivateKey) (Webhook, error) {
	var w Webhook
	plain, err := Decrypt(string(rawBody), key)
	if err != nil {
		return w, err
	}
	if err := json.Unmarshal(plain, &w); err != nil {
		return w, fmt.Errorf("upayopen: decode webhook: %w", err)
	}
	return w, nil
}

// PayoutDetail parses Detail for PO_ORDER_STATUS_CHANGE.
func (w Webhook) PayoutDetail() (PayoutStatusDetail, error) {
	var d PayoutStatusDetail
	err := json.Unmarshal([]byte(w.Detail), &d)
	return d, err
}
