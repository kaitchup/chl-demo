package upay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// replayTolerance: reject webhooks whose timestamp drifts more than this.
const replayTolerance = 5 * time.Minute

// Event is the webhook payload (payment_order.paid / .partially_paid).
type Event struct {
	ID        string    `json:"id"`    // evt_<ULID>, dedupe key
	Event     string    `json:"event"` // payment_order.paid | payment_order.partially_paid
	CreatedAt string    `json:"created_at"`
	Data      EventData `json:"data"`
}

type EventData struct {
	ID              string `json:"id"` // pay_<ULID>
	MerchantOrderID string `json:"merchant_order_id"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	ReceivedAmount  string `json:"received_amount"`
	SettlementFee   string `json:"settlement_fee"`
	SettlementAmt   string `json:"settlement_amount"`
	ExpiresAt       string `json:"expires_at"`
	CreatedAt       string `json:"created_at"`
}

// VerifyWebhook validates the UPay-Signature header against the raw request body.
//
//   - rawBody MUST be the exact bytes received (do not re-marshal).
//   - secrets may contain more than one value: during a 24h key rotation window
//     UPay double-signs and the header carries two v= values; any match passes.
//
// Signed data = "<t>." + rawBody ; signature = lowercase_hex(HMAC-SHA256(secret, signed)).
func VerifyWebhook(sigHeader string, rawBody []byte, secrets []string, now time.Time) error {
	if len(secrets) == 0 {
		return errors.New("no webhook secret configured")
	}
	ts, vs, err := parseSigHeader(sigHeader)
	if err != nil {
		return err
	}

	if d := now.Sub(time.Unix(ts, 0)); d > replayTolerance || d < -replayTolerance {
		return errors.New("signature timestamp outside tolerance")
	}

	signed := append([]byte(strconv.FormatInt(ts, 10)+"."), rawBody...)
	for _, secret := range secrets {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(signed)
		want := []byte(hex.EncodeToString(mac.Sum(nil)))
		for _, v := range vs {
			if hmac.Equal(want, []byte(v)) {
				return nil
			}
		}
	}
	return errors.New("no matching signature")
}

func parseSigHeader(h string) (ts int64, vs []string, err error) {
	for _, part := range strings.Split(h, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts, err = strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return 0, nil, errors.New("bad timestamp in signature")
			}
		case "v":
			vs = append(vs, kv[1])
		}
	}
	if ts == 0 || len(vs) == 0 {
		return 0, nil, errors.New("malformed UPay-Signature header")
	}
	return ts, vs, nil
}

// ParseEvent unmarshals a webhook body into an Event.
func ParseEvent(rawBody []byte) (Event, error) {
	var e Event
	err := json.Unmarshal(rawBody, &e)
	return e, err
}
