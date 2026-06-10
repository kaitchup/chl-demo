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

// APIError is returned by client methods when UPay responds with a non-2xx
// status code. Callers can type-assert to inspect the raw response.
type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string { return fmt.Sprintf("upay %d: %s", e.StatusCode, e.Body) }

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
// It returns the raw request and response bodies alongside the parsed order so
// callers can persist them for debugging.
func (c *Client) CreateOrder(r CreateOrderReq, idempotencyKey string) (reqBody, respBody []byte, order *Order, err error) {
	reqBody, _ = json.Marshal(r)
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/v1/payment/request", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	statusCode, respBody, err := c.do(req)
	if err != nil {
		return reqBody, respBody, nil, err
	}
	if statusCode/100 != 2 {
		return reqBody, respBody, nil, fmt.Errorf("upay %d: %s", statusCode, respBody)
	}
	var o Order
	if err = json.Unmarshal(respBody, &o); err != nil {
		return reqBody, respBody, nil, fmt.Errorf("decode upay response: %w", err)
	}
	return reqBody, respBody, &o, nil
}

// GetOrder fetches the current authoritative state of an order (pay_<ULID>).
func (c *Client) GetOrder(payID string) (*Order, error) {
	req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/v1/payment/request/"+payID, nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	code, raw, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, &APIError{StatusCode: code, Body: raw}
	}
	var o Order
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("decode upay response: %w", err)
	}
	return &o, nil
}

// CreateRefundReq is the POST /v1/payment/refund body.
type CreateRefundReq struct {
	PaymentRequestID string `json:"payment_request_id"`
	Amount           string `json:"amount"`
	Coin             string `json:"coin"`
	Chain            string `json:"chain"`
	ToAddress        string `json:"to_address"`
	MerchantRefundID string `json:"merchant_refund_id"`
	Reason           string `json:"reason,omitempty"`
}

// Refund is the refund resource returned by the gateway.
type Refund struct {
	ID               string `json:"id"`
	Object           string `json:"object"`
	PaymentRequestID string `json:"payment_request_id"`
	MerchantID       string `json:"merchant_id"`
	MerchantRefundID string `json:"merchant_refund_id"`
	Amount           string `json:"amount"`
	Fee              string `json:"fee"`
	TotalDebit       string `json:"total_debit"`
	Currency         string `json:"currency"`
	Coin             string `json:"coin"`
	SettleAmount     string `json:"settle_amount"`
	FxRateID         string `json:"fx_rate_id"`
	Chain            string `json:"chain"`
	ToAddress        string `json:"to_address"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
}

// CreateRefund submits a refund request to UPay. idempotencyKey is required.
func (c *Client) CreateRefund(r CreateRefundReq, idempotencyKey string) (*Refund, error) {
	body, _ := json.Marshal(r)
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/v1/payment/refund", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	code, raw, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("upay %d: %s", code, raw)
	}
	var rf Refund
	if err := json.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("decode upay refund: %w", err)
	}
	return &rf, nil
}

// GetRefund fetches the current state of a refund by its UPay id (rfd_<ULID>).
func (c *Client) GetRefund(refundID string) (*Refund, error) {
	req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/v1/payment/refund/"+refundID, nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	code, raw, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("upay %d: %s", code, raw)
	}
	var rf Refund
	if err := json.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("decode upay refund: %w", err)
	}
	return &rf, nil
}

type listRefundsResp struct {
	Object string   `json:"object"`
	Data   []Refund `json:"data"`
}

// ListRefunds fetches all refunds for a given payment_request_id.
func (c *Client) ListRefunds(paymentRequestID string) ([]Refund, error) {
	req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/v1/payment/refund", nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	q := req.URL.Query()
	q.Set("payment_request_id", paymentRequestID)
	req.URL.RawQuery = q.Encode()
	code, raw, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("upay %d: %s", code, raw)
	}
	var resp listRefundsResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode upay refunds: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) do(req *http.Request) (statusCode int, body []byte, err error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}
