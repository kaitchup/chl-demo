// Package upayopen talks to the UPay OpenAPI (https://openapi.upay-test.best):
// X-UPA-* header signing, JWE payload encryption, and the payout webhook.
// It is deliberately separate from internal/upay, which covers the older
// acquiring API (Bearer auth, whsec_ webhooks).
package upayopen

import (
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	jose "github.com/go-jose/go-jose/v4"
)

// Sign returns Base64(HMAC-SHA256(secret, data)), the X-UPA-SIGN value used both
// for outbound API requests and inbound webhooks.
func Sign(secret, data string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// ParsePrivateKey accepts a PKCS#8 RSA private key either as a PEM block or as
// the bare base64 DER that UPay hands out (no BEGIN/END lines).
func ParsePrivateKey(s string) (*rsa.PrivateKey, error) {
	der, err := derBytes(s)
	if err != nil {
		return nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rk, nil
}

func derBytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty key")
	}
	if strings.HasPrefix(s, "-----BEGIN") {
		blk, _ := pem.Decode([]byte(s))
		if blk == nil {
			return nil, errors.New("invalid PEM")
		}
		return blk.Bytes, nil
	}
	der, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode base64 key: %w", err)
	}
	return der, nil
}

// Decrypt opens a compact JWE (RSA-OAEP-256 + A128GCM, optionally DEFLATE) with
// the merchant private key. UPay encrypts API responses and webhook bodies this way.
func Decrypt(jwe string, key *rsa.PrivateKey) ([]byte, error) {
	obj, err := jose.ParseEncrypted(strings.TrimSpace(jwe),
		[]jose.KeyAlgorithm{jose.RSA_OAEP_256}, []jose.ContentEncryption{jose.A128GCM})
	if err != nil {
		return nil, fmt.Errorf("parse JWE: %w", err)
	}
	out, err := obj.Decrypt(key)
	if err != nil {
		return nil, fmt.Errorf("decrypt JWE: %w", err)
	}
	return out, nil
}
