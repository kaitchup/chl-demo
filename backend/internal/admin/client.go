package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client calls the UPay admin BFF using Dolos credentials.
// It manages the access token lifecycle (auto-refresh, re-login on failure).
type Client struct {
	baseURL  string
	email    string
	password string

	mu           sync.Mutex
	accessToken  string
	tokenExpires time.Time
	refreshCookie string // value of the upay_refresh cookie

	http *http.Client
}

func NewClient(baseURL, email, password string) *Client {
	return &Client{
		baseURL:  baseURL,
		email:    email,
		password: password,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// --- token management ---

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Audience string `json:"audience"`
}

type loginResp struct {
	AccessToken      string `json:"access_token"`
	AccessExpiresAt  string `json:"access_expires_at"`
}

func (c *Client) login() error {
	body, _ := json.Marshal(loginReq{Email: c.email, Password: c.password, Audience: "platform"})
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("admin login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("admin login %d: %s", resp.StatusCode, raw)
	}

	var lr loginResp
	if err := json.Unmarshal(raw, &lr); err != nil {
		return fmt.Errorf("admin login decode: %w", err)
	}

	exp, _ := time.Parse(time.RFC3339Nano, lr.AccessExpiresAt)
	c.accessToken = lr.AccessToken
	c.tokenExpires = exp

	// extract refresh cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == "upay_refresh" {
			c.refreshCookie = ck.Value
			break
		}
	}
	return nil
}

func (c *Client) refresh() error {
	if c.refreshCookie == "" {
		return c.login()
	}
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "upay_refresh", Value: c.refreshCookie})

	resp, err := c.http.Do(req)
	if err != nil {
		return c.login() // fall back to full login
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return c.login()
	}

	var lr loginResp
	if err := json.Unmarshal(raw, &lr); err != nil {
		return c.login()
	}

	exp, _ := time.Parse(time.RFC3339Nano, lr.AccessExpiresAt)
	c.accessToken = lr.AccessToken
	c.tokenExpires = exp
	return nil
}

// token returns a valid access token, refreshing/re-logging-in as needed.
func (c *Client) token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken == "" || time.Until(c.tokenExpires) < 5*time.Minute {
		if err := c.refresh(); err != nil {
			return "", err
		}
	}
	return c.accessToken, nil
}

// --- HTTP helper ---

func (c *Client) do(method, path string, body any) (int, []byte, error) {
	tok, err := c.token()
	if err != nil {
		return 0, nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}

	req, _ := http.NewRequest(method, c.baseURL+path, bodyReader)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// on 401, invalidate token and retry once
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.accessToken = ""
		c.mu.Unlock()

		tok, err = c.token()
		if err != nil {
			return resp.StatusCode, raw, err
		}
		if body != nil {
			b, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(b)
		}
		req2, _ := http.NewRequest(method, c.baseURL+path, bodyReader)
		req2.Header.Set("Authorization", "Bearer "+tok)
		if body != nil {
			req2.Header.Set("Content-Type", "application/json")
		}
		resp2, err := c.http.Do(req2)
		if err != nil {
			return 0, nil, err
		}
		defer resp2.Body.Close()
		raw, _ = io.ReadAll(resp2.Body)
		return resp2.StatusCode, raw, nil
	}

	return resp.StatusCode, raw, nil
}

// --- Simulate settlement ---

type SimEvent struct {
	Amount  string `json:"amount"`
	Status  string `json:"status"`
	DelayMs int    `json:"delay_ms"`
	Note    string `json:"note"`
}

type SimulateReq struct {
	Scenario string     `json:"scenario"`
	Events   []SimEvent `json:"events"`
}

type SimulateResp struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (c *Client) SimulateSettlement(paymentRequestID string, req SimulateReq) (*SimulateResp, error) {
	code, raw, err := c.do(http.MethodPost,
		"/platform/payment-requests/"+paymentRequestID+"/simulate-settlement", req)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("simulate %d: %s", code, raw)
	}
	var r SimulateResp
	_ = json.Unmarshal(raw, &r)
	return &r, nil
}

// --- Deposits ---

type depositListReq struct {
	ToAddress string `json:"to_address"`
	Limit     string `json:"limit"`
	Offset    string `json:"offset"`
}

// Deposit matches the DepositItem shape returned by POST /platform/deposits/list.
type Deposit struct {
	ID           string `json:"id"`
	ToAddress    string `json:"to_address"`
	Amount       string `json:"amount"`
	CoinSymbol   string `json:"coin_symbol"`
	InspectionID string `json:"inspection_id"`
	Status       string `json:"status"`
	CreatedAt    string `json:"create_at"`
}

type depositListResp struct {
	Items   []Deposit `json:"items"`
	HasMore string    `json:"has_more"`
}

func (c *Client) ListDeposits(toAddress string) ([]Deposit, error) {
	code, raw, err := c.do(http.MethodPost, "/platform/deposits/list", depositListReq{
		ToAddress: toAddress,
		Limit:     "20",
		Offset:    "0",
	})
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("list deposits %d: %s", code, raw)
	}
	var resp depositListResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode deposits: %w", err)
	}
	return resp.Items, nil
}

// --- AML tickets ---

// AMLTicket matches the ReviewTicket shape from GET /platform/compliance/aml/review-tickets.
// TicketNo is the identifier used in the submit-review path parameter.
type AMLTicket struct {
	TicketNo     string `json:"ticket_no"`
	InspectionID string `json:"inspection_id"`
	Status       string `json:"status"`
}

type amlTicketListResp struct {
	Tickets []AMLTicket `json:"tickets"`
	Total   int         `json:"total"`
}

func (c *Client) GetAMLTicketByInspectionID(inspectionID string) (*AMLTicket, error) {
	code, raw, err := c.do(http.MethodGet,
		"/platform/compliance/aml/review-tickets?inspection_id="+inspectionID+"&page=1&size=20", nil)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("get aml ticket %d: %s", code, raw)
	}
	var resp amlTicketListResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode aml tickets: %w", err)
	}
	if len(resp.Tickets) == 0 {
		return nil, nil
	}
	return &resp.Tickets[0], nil
}

// SubmitAMLReview approves the AML review ticket identified by its ticket_no.
func (c *Client) SubmitAMLReview(ticketNo string) error {
	code, raw, err := c.do(http.MethodPost,
		"/platform/compliance/aml/review-tickets/"+ticketNo+"/submit-review",
		map[string]any{"status": "APPROVED"})
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fmt.Errorf("submit aml review %d: %s", code, raw)
	}
	return nil
}

// --- Webhook delivery records ---

// WebhookDelivery matches the WebhookDeliveryRow shape from
// GET /platform/payment-requests/:id/webhooks.
type WebhookDelivery struct {
	ID             string `json:"id"`
	EventType      string `json:"event_type"`
	Status         string `json:"status"`
	LastStatusCode int    `json:"last_status_code"`
	Attempts       int    `json:"attempts"`
	DeliveredAt    any    `json:"delivered_at_unix_micro"`
	CreatedAt      any    `json:"created_at_unix_micro"`
}

type webhookListResp struct {
	Items []WebhookDelivery `json:"items"`
	Total int               `json:"total"`
}

func (c *Client) ListWebhookDeliveries(paymentRequestID string, page, size int) ([]WebhookDelivery, int, error) {
	path := fmt.Sprintf("/platform/payment-requests/%s/webhooks?page=%d&size=%d",
		paymentRequestID, page, size)
	code, raw, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, err
	}
	if code/100 != 2 {
		return nil, 0, fmt.Errorf("list webhooks %d: %s", code, raw)
	}
	var resp webhookListResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode webhooks: %w", err)
	}
	return resp.Items, resp.Total, nil
}
