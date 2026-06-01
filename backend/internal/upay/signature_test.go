package upay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

// sign mimics how UPay signs a webhook: lowercase_hex(HMAC-SHA256(secret, "<t>."+body)).
func sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook(t *testing.T) {
	secret := "whsec_test_0123456789abcdefghijklmnopqrstuvwxyzABCDE"
	body := []byte(`{"id":"evt_x","event":"payment_order.paid","data":{"id":"pay_x"}}`)
	now := time.Now()
	ts := now.Unix()
	good := "t=" + strconv.FormatInt(ts, 10) + ",v=" + sign(secret, ts, body)

	t.Run("valid", func(t *testing.T) {
		if err := VerifyWebhook(good, body, []string{secret}, now); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("tampered body", func(t *testing.T) {
		if err := VerifyWebhook(good, []byte(`{"id":"evt_y"}`), []string{secret}, now); err == nil {
			t.Fatal("expected failure on tampered body")
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		if err := VerifyWebhook(good, body, []string{"whsec_other"}, now); err == nil {
			t.Fatal("expected failure on wrong secret")
		}
	})

	t.Run("replay too old", func(t *testing.T) {
		old := now.Add(-10 * time.Minute)
		oldTs := old.Unix()
		h := "t=" + strconv.FormatInt(oldTs, 10) + ",v=" + sign(secret, oldTs, body)
		if err := VerifyWebhook(h, body, []string{secret}, now); err == nil {
			t.Fatal("expected failure on replay outside tolerance")
		}
	})

	t.Run("rotation dual sign", func(t *testing.T) {
		newSecret, oldSecret := "whsec_new", "whsec_old"
		// header carries both v= (new first, old second); we only know the old one
		h := "t=" + strconv.FormatInt(ts, 10) +
			",v=" + sign(newSecret, ts, body) +
			",v=" + sign(oldSecret, ts, body)
		if err := VerifyWebhook(h, body, []string{oldSecret}, now); err != nil {
			t.Fatalf("expected old secret to verify during rotation, got %v", err)
		}
	})
}
