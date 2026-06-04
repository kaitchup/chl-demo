package upay

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client talks to the UPay acquiring API (merchant-facing).
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds a client. caPath may be "" to use system roots; for the UPay test
// environment pass the bundled self-signed CA (certs/upay-local-ca.crt).
func New(baseURL, apiKey, caPath string) (*Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("append CA: no certs in %s", caPath)
		}
		tlsCfg.RootCAs = pool
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

// CreateOrderReq is the POST /v1/payment/request body.
type CreateOrderReq struct {
	MerchantOrderID  string            `json:"merchant_order_id"`
	Amount           string            `json:"amount"`   // "20.00"
	Currency         string            `json:"currency"` // fiat code, e.g. "USD"
	Description      string            `json:"description,omitempty"`
	ExpiresInSeconds int               `json:"expires_in_seconds,omitempty"` // 1800..3600
	SuccessURL       string            `json:"success_url,omitempty"`
	CancelURL        string            `json:"cancel_url,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// Order is the payment order resource returned by create/get.
type Order struct {
	ID              string `json:"id"`
	Object          string `json:"object"`
	MerchantID      string `json:"merchant_id"`
	MerchantOrderID string `json:"merchant_order_id"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`        // INITED | PROCESSING | SUCCEEDED | CANCELED
	ReceivedAmount  string `json:"received_amount"`
	CancelReason    string `json:"cancel_reason"` // set only on CANCELED orders
	CheckoutURL     string `json:"checkout_url"`
	PaidAt          string `json:"paid_at"`
	CreatedAt       string `json:"created_at"`
	ExpiresAt       string `json:"expires_at"`
}

// CreateOrder creates a payment order. idempotencyKey is required by UPay.
func (c *Client) CreateOrder(r CreateOrderReq, idempotencyKey string) (*Order, error) {
	body, _ := json.Marshal(r)
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/v1/payment/request", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	return c.do(req)
}

// GetOrder fetches the current authoritative state of an order (pay_<ULID>).
func (c *Client) GetOrder(payID string) (*Order, error) {
	req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/v1/payment/request/"+payID, nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return c.do(req)
}

func (c *Client) do(req *http.Request) (*Order, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("upay %d: %s", resp.StatusCode, raw)
	}
	var o Order
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("decode upay response: %w", err)
	}
	return &o, nil
}
