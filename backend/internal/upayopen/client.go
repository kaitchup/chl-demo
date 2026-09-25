package upayopen

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// APIError is a non-zero UPay business code. UPay answers HTTP 200 even on
// failure, and its `success` flag is unreliable, so only `code` matters.
type APIError struct {
	Code      int
	Msg       string
	URI       string
	RequestID string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("upayopen %s: code=%d msg=%s (requestId=%s)", e.URI, e.Code, e.Msg, e.RequestID)
}

// Client calls the UPay OpenAPI. Every request is signed; endpoints marked as
// encrypted send {"payload": JWE(plaintext)} and return data.payload as JWE.
type Client struct {
	host         string
	apiKey       string
	secret       string
	platformPub  *rsa.PublicKey
	platformKID  string
	merchantPriv *rsa.PrivateKey
	http         *http.Client
}

// New builds a client. Keys may be bare base64 DER (as UPay issues them) or PEM.
func New(host, apiKey, secret, platformPublicKey, merchantPrivateKey string) (*Client, error) {
	pubDER, err := derBytes(platformPublicKey)
	if err != nil {
		return nil, fmt.Errorf("platform public key: %w", err)
	}
	pk, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, fmt.Errorf("parse platform public key: %w", err)
	}
	rpk, ok := pk.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("platform public key is not RSA")
	}
	priv, err := ParsePrivateKey(merchantPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("merchant private key: %w", err)
	}
	// kid = sha256 of the PEM text (64-column body), matching UPay's samples.
	kid := sha256.Sum256([]byte(wrapPEM("PUBLIC KEY", base64.StdEncoding.EncodeToString(pubDER))))
	return &Client{
		host:         strings.TrimRight(host, "/"),
		apiKey:       apiKey,
		secret:       secret,
		platformPub:  rpk,
		platformKID:  hex.EncodeToString(kid[:]),
		merchantPriv: priv,
		http:         &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// PrivateKey exposes the merchant key for webhook decryption.
func (c *Client) PrivateKey() *rsa.PrivateKey { return c.merchantPriv }

func wrapPEM(typ, b64 string) string {
	var sb strings.Builder
	sb.WriteString("-----BEGIN " + typ + "-----\n")
	for i := 0; i < len(b64); i += 64 {
		j := min(i+64, len(b64))
		sb.WriteString(b64[i:j] + "\n")
	}
	sb.WriteString("-----END " + typ + "-----\n")
	return sb.String()
}

// SignParam renders params as sorted k=v pairs joined by '&'. Strings and
// json.Number are used verbatim; anything else is compact JSON.
func SignParam(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var s string
		switch v := params[k].(type) {
		case string:
			s = v
		case json.Number:
			s = v.String()
		default:
			b, _ := json.Marshal(v)
			s = string(b)
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, "&")
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) encrypt(plain []byte) (string, error) {
	enc, err := jose.NewEncrypter(jose.A128GCM,
		jose.Recipient{Algorithm: jose.RSA_OAEP_256, Key: c.platformPub, KeyID: c.platformKID},
		(&jose.EncrypterOptions{Compression: jose.DEFLATE}).WithType("JOSE"))
	if err != nil {
		return "", err
	}
	obj, err := enc.Encrypt(plain)
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// call signs the plaintext params (verified against the test environment:
// signing the encrypted payload fails with 1005) and decodes data into out.
func (c *Client) call(ctx context.Context, uri string, params map[string]any, encrypted bool, out any) error {
	var body []byte
	if encrypted {
		plain, err := json.Marshal(params)
		if err != nil {
			return err
		}
		jwe, err := c.encrypt(plain)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", uri, err)
		}
		body, _ = json.Marshal(map[string]string{"payload": jwe})
	} else if len(params) > 0 {
		body, _ = json.Marshal(params)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+uri, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	rid := c.sign(req, uri, SignParam(params))
	return c.do(req, uri, rid, encrypted, out)
}

func (c *Client) sign(req *http.Request, uri, signParam string) string {
	rid := newRequestID()
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req.Header.Set("X-UPA-REQUESTID", rid)
	req.Header.Set("X-UPA-TIMESTAMP", ts)
	req.Header.Set("X-UPA-APIKEY", c.apiKey)
	req.Header.Set("X-UPA-SIGN", Sign(c.secret, "POST|"+uri+"|"+ts+"|"+rid+"|"+signParam))
	return rid
}

func (c *Client) do(req *http.Request, uri, rid string, encrypted bool, out any) error {
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("upayopen %s rid=%s err=%v", uri, rid, err)
		return fmt.Errorf("upayopen %s: %w", uri, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("upayopen %s rid=%s http=%d non-JSON response", uri, rid, resp.StatusCode)
		return fmt.Errorf("upayopen %s: http %d: %s", uri, resp.StatusCode, truncate(raw, 200))
	}
	// Params are deliberately not logged: they carry personal data.
	log.Printf("upayopen %s rid=%s code=%d %s", uri, rid, env.Code, time.Since(start).Round(time.Millisecond))
	if env.Code != 0 {
		return &APIError{Code: env.Code, Msg: env.Msg, URI: uri, RequestID: rid}
	}
	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	data := []byte(env.Data)
	if encrypted {
		var p struct {
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(env.Data, &p); err == nil && p.Payload != "" {
			if data, err = Decrypt(p.Payload, c.merchantPriv); err != nil {
				return fmt.Errorf("upayopen %s: %w", uri, err)
			}
		}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("upayopen %s: decode data: %w", uri, err)
	}
	return nil
}

// Upload posts a file as multipart. Per UPay docs the form fields are not part
// of the signature, so SIGN_PARAM is empty.
func (c *Client) Upload(ctx context.Context, r io.Reader, filename, contentType string, fileType int) (string, error) {
	const uri = "/api/v1/common/file/upload"
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", contentType) // UPay checks the part type, not just the name
	part, err := w.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, r); err != nil {
		return "", err
	}
	_ = w.WriteField("fileType", strconv.Itoa(fileType))
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+uri, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	rid := c.sign(req, uri, "")
	var out struct {
		FileID string `json:"fileId"`
	}
	if err := c.do(req, uri, rid, false, &out); err != nil {
		return "", err
	}
	return out.FileID, nil
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
