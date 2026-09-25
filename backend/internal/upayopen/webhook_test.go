package upayopen

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Fixed vector from https://upay-api.readme.io/reference/鉴权签名-数据加密 (鉴权签名示例).
func TestSignDocVector(t *testing.T) {
	secret := `g3)&~8P9S+ZhqO@G8b9Ate%xa5Jh-.E%`
	raw := `POST|/api/v1/customer/add|1774573955994|366dde77c57a4e7e988c074a21a4f04c|` +
		`address={"address":"535 Mission St, Floor 12","countryId":12,"name":"Demo given name","postalCode":"94105","provinceId":475}` +
		`&annualIncome=100.5&company=Demo company&email=sing_demo@upb.com&givenName=Demo given name&globalCode=+1` +
		`&groupNo=101680210001&lastName=Demo last name&occupation=Demo occupation&phone=127897890` +
		`&position=Demo position&remark=This is sample data for adding customers&workYear=1`
	if got, want := Sign(secret, raw), "pRbX0ikM/4j/E3tBxNOO/m3Hra+4d2/r+xtRTJ6N5pk="; got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}

// pushLikeUPay mirrors upay-broker-server WebhookConsumer.post: JWE with the
// merchant public key, then sign event|timestamp|<ciphertext>.
func pushLikeUPay(t *testing.T, pub *rsa.PublicKey, secret, plain string, ts int64) (body []byte, tsHeader, sign string) {
	t.Helper()
	enc, err := jose.NewEncrypter(jose.A128GCM,
		jose.Recipient{Algorithm: jose.RSA_OAEP_256, Key: pub},
		(&jose.EncrypterOptions{Compression: jose.DEFLATE}).WithType("JOSE"))
	if err != nil {
		t.Fatal(err)
	}
	obj, err := enc.Encrypt([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := obj.CompactSerialize()
	tsHeader = strconv.FormatInt(ts, 10)
	return []byte(s), tsHeader, Sign(secret, EventPayoutOrderStatusChange+"|"+tsHeader+"|"+s)
}

func TestWebhookRoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	key, err := ParsePrivateKey(base64.StdEncoding.EncodeToString(der)) // bare base64, as UPay issues it
	if err != nil {
		t.Fatal(err)
	}

	const secret = "T0p$ecret"
	now := time.UnixMilli(1790328000000)
	plain := `{"event":"PO_ORDER_STATUS_CHANGE","trace":"TR1","orderNo":"WH1","timestamp":1790328000000,` +
		`"detail":"{\"payerNo\":\"P1\",\"bankAccountNo\":\"B1\",\"orderNo\":\"ORD1\",\"status\":7}"}`
	body, ts, sign := pushLikeUPay(t, &priv.PublicKey, secret, plain, now.UnixMilli())

	if err := VerifyWebhook(secret, EventPayoutOrderStatusChange, ts, sign, body, now); err != nil {
		t.Fatalf("verify: %v", err)
	}
	w, err := DecodeWebhook(body, key)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if w.Event != EventPayoutOrderStatusChange || w.OrderNo != "WH1" {
		t.Fatalf("envelope = %+v", w)
	}
	d, err := w.PayoutDetail()
	if err != nil || d.OrderNo != "ORD1" || d.Status != 7 {
		t.Fatalf("detail = %+v, err %v", d, err)
	}

	t.Run("tampered body", func(t *testing.T) {
		bad := append([]byte{}, body...)
		bad[len(bad)-2] ^= 1
		if err := VerifyWebhook(secret, EventPayoutOrderStatusChange, ts, sign, bad, now); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("wrong secret", func(t *testing.T) {
		if err := VerifyWebhook("other", EventPayoutOrderStatusChange, ts, sign, body, now); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("retry within tolerance", func(t *testing.T) {
		if err := VerifyWebhook(secret, EventPayoutOrderStatusChange, ts, sign, body, now.Add(10*time.Minute)); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stale", func(t *testing.T) {
		if err := VerifyWebhook(secret, EventPayoutOrderStatusChange, ts, sign, body, now.Add(31*time.Minute)); !errors.Is(err, ErrStale) {
			t.Fatalf("err = %v", err)
		}
	})
}
