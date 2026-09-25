package upayopen

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Str decodes a JSON string or number into a string. UPay is inconsistent
// (e.g. quote destinationAmount is documented as number, returned as "20").
type Str string

func (s *Str) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = Str(v)
		return nil
	}
	*s = Str(strings.TrimSpace(string(b)))
	return nil
}

func (s Str) String() string { return string(s) }

// Millis parses a millisecond-timestamp string (UPay returns times this way,
// not as the ISO strings the docs show). Returns 0 when unparsable.
func (s Str) Millis() int64 {
	n, _ := strconv.ParseInt(string(s), 10, 64)
	return n
}

// ---- order status (payout_order.status) ----

const (
	StatusCreated          = 0 // undocumented; returned right after creation
	StatusQuoting          = 1
	StatusAwaitingConfirm  = 2
	StatusQuoteConfirmed   = 3
	StatusQuoteFailed      = 4
	StatusReviewing        = 5
	StatusProcessing       = 6
	StatusSucceeded        = 7
	StatusFailed           = 8
	StatusCanceled         = 9
	StatusRefunding        = 10
	StatusRefunded         = 11
	StatusAwaitingNewQuote = 12
)

// QuoteAvailable is quoteList[].status for a usable quote.
const QuoteAvailable = 3

// IsTerminal reports whether an order status will not change any more.
func IsTerminal(status int) bool {
	switch status {
	case StatusQuoteFailed, StatusSucceeded, StatusFailed, StatusCanceled, StatusRefunded:
		return true
	}
	return false
}

// ---- endpoints ----

type DebitCoin struct {
	CoinID string `json:"coinId"`
	Symbol string `json:"symbol"`
}

func (c *Client) DebitCoins(ctx context.Context) ([]DebitCoin, error) {
	var out []DebitCoin
	err := c.call(ctx, "/api/v1/payout/order/debit/coin/list", nil, false, &out)
	return out, err
}

// Area is a country from common/area/list (children are provinces/cities).
type Area struct {
	ID     string `json:"id"`
	NameZh string `json:"nameZh"`
	NameEn string `json:"nameEn"`
	Alpha2 string `json:"alpha2"`
	Alpha3 string `json:"alpha3"`
}

func (c *Client) Countries(ctx context.Context) ([]Area, error) {
	var out []Area
	err := c.call(ctx, "/api/v1/common/area/list", nil, false, &out)
	return out, err
}

func (c *Client) AddPayer(ctx context.Context, subject, formData string) (string, error) {
	var out struct {
		PayerNo string `json:"payerNo"`
	}
	err := c.call(ctx, "/api/v1/payout/payer/add",
		map[string]any{"subject": subject, "formData": formData}, true, &out)
	return out.PayerNo, err
}

func (c *Client) AddBeneficiary(ctx context.Context, subject, formData string) (string, error) {
	var out struct {
		BeneficiaryNo string `json:"beneficiaryNo"`
	}
	err := c.call(ctx, "/api/v1/payout/beneficiary/add",
		map[string]any{"subject": subject, "formData": formData}, true, &out)
	return out.BeneficiaryNo, err
}

func (c *Client) AddBankAccount(ctx context.Context, beneficiaryNo, formData string) (string, error) {
	var out struct {
		BankAccountNo string `json:"bankAccountNo"`
	}
	err := c.call(ctx, "/api/v1/payout/bank/account/add",
		map[string]any{"subject": "bankAccount", "beneficiaryNo": beneficiaryNo, "formData": formData}, true, &out)
	return out.BankAccountNo, err
}

type EntityInfo struct {
	Info               Info `json:"info"`
	DataCompleteStatus int  `json:"dataCompleteStatus"`
}

func (c *Client) BeneficiaryInfo(ctx context.Context, beneficiaryNo string) (EntityInfo, error) {
	var out EntityInfo
	err := c.call(ctx, "/api/v1/payout/beneficiary/info",
		map[string]any{"beneficiaryNo": beneficiaryNo}, true, &out)
	return out, err
}

func (c *Client) BankAccountInfo(ctx context.Context, bankAccountNo string) (EntityInfo, error) {
	var out EntityInfo
	err := c.call(ctx, "/api/v1/payout/bank/account/info",
		map[string]any{"bankAccountNo": bankAccountNo}, true, &out)
	return out, err
}

type CreateOrderReq struct {
	DebitCoinID   string
	ThirdOrderNo  string
	PayerNo       string
	BankAccountNo string
	Amount        string // decimal string, ≤ 2 dp
	RemitMethod   string // swift | local
	RemitUsage    string
	RemitNote     string
}

type ImproveGroup struct {
	GroupCode string `json:"groupCode"`
	GroupName string `json:"groupName"`
	Fields    []struct {
		FieldKey  string `json:"fieldKey"`
		FieldName string `json:"fieldName"`
	} `json:"fields"`
}

type CreateOrderRes struct {
	OrderNo              string `json:"orderNo"`
	OrderStatus          int    `json:"orderStatus"`
	InformationToImprove []struct {
		Code   string         `json:"code"`
		Name   string         `json:"name"`
		Groups []ImproveGroup `json:"groups"`
	} `json:"informationToImprove"`
}

func (c *Client) CreateOrder(ctx context.Context, r CreateOrderReq) (CreateOrderRes, error) {
	var out CreateOrderRes
	// json.Number keeps "20.00" identical in the body and the signature string.
	err := c.call(ctx, "/api/v1/payout/order/creation", map[string]any{
		"debitCoinId":   r.DebitCoinID,
		"thirdOrderNo":  r.ThirdOrderNo,
		"payerNo":       r.PayerNo,
		"bankAccountNo": r.BankAccountNo,
		"amount":        json.Number(r.Amount),
		"remitMethod":   r.RemitMethod,
		"remitUsage":    r.RemitUsage,
		"remitNote":     r.RemitNote,
	}, true, &out)
	return out, err
}

type Quote struct {
	QuoteNo             string `json:"quoteNo"`
	Status              int    `json:"status"`
	DebitCoin           string `json:"debitCoin"`
	DebitAmount         Str    `json:"debitAmount"` // total debited, fees included
	DestinationCurrency string `json:"destinationCurrency"`
	DestinationAmount   Str    `json:"destinationAmount"`
	FixedFee            Str    `json:"fixedFee"`
	TransactionFee      Str    `json:"transactionFee"`
	ExchangeFee         Str    `json:"exchangeFee"`
	FeeCurrency         string `json:"feeCurrency"`
	ValidUntil          Str    `json:"validUntil"` // ms timestamp
	KycURL              string `json:"kycUrl"`
	RemitMethod         Str    `json:"remitMethod"`
}

type QuoteInfo struct {
	OrderNo   string  `json:"orderNo"`
	Status    int     `json:"status"`
	QuoteList []Quote `json:"quoteList"`
}

func (c *Client) QuoteInfo(ctx context.Context, thirdOrderNo string) (QuoteInfo, error) {
	var out QuoteInfo
	err := c.call(ctx, "/api/v1/payout/order/quote/info",
		map[string]any{"thirdOrderNo": thirdOrderNo}, true, &out)
	return out, err
}

type ConfirmRes struct {
	OrderNo     string `json:"orderNo"`
	QuoteNo     string `json:"quoteNo"`
	OrderStatus int    `json:"orderStatus"`
}

// ConfirmOrder confirms a quote. NOTE: UPay answers orderStatus=3 even when the
// quote has expired and a requote will follow asynchronously (→ 12 or 4), so
// callers must keep syncing; see docs/UPAY-汇款接口问题清单.md #2.
func (c *Client) ConfirmOrder(ctx context.Context, thirdOrderNo, quoteNo string) (ConfirmRes, error) {
	var out ConfirmRes
	err := c.call(ctx, "/api/v1/payout/order/confirm",
		map[string]any{"thirdOrderNo": thirdOrderNo, "quoteNo": quoteNo}, true, &out)
	return out, err
}

func (c *Client) CancelOrder(ctx context.Context, thirdOrderNo string) (int, error) {
	var out struct {
		Status int `json:"status"`
	}
	err := c.call(ctx, "/api/v1/payout/order/cancel",
		map[string]any{"thirdOrderNo": thirdOrderNo}, true, &out)
	return out.Status, err
}

type OrderInfo struct {
	OrderNo   string `json:"orderNo"`
	Status    int    `json:"status"`
	CrateAt   Str    `json:"crateAt"` // sic
	DebitInfo struct {
		DebitAmount      Str    `json:"debitAmount"`
		DebitCoin        string `json:"debitCoin"`
		FixedFee         Str    `json:"fixedFee"`
		TransactionFee   Str    `json:"transactionFee"`
		ExchangeFee      Str    `json:"exchangeFee"`
		FeeCoin          string `json:"feeCoin"`
		DebitTotalAmount Str    `json:"debitTotalAmount"`
	} `json:"debitInfo"`
	RemitInfo struct {
		RemitAmount   Str    `json:"remitAmount"`
		RemitCurrency string `json:"remitCurrency"`
		ExchangeRate  Str    `json:"exchangeRate"`
		RemitMethod   Str    `json:"remitMethod"`
		RemitUsage    string `json:"remitUsage"`
	} `json:"remitInfo"`
}

// OrderInfo returns the parsed detail plus the raw JSON for storage.
func (c *Client) OrderInfo(ctx context.Context, thirdOrderNo string) (OrderInfo, json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.call(ctx, "/api/v1/payout/order/info",
		map[string]any{"thirdOrderNo": thirdOrderNo}, true, &raw); err != nil {
		return OrderInfo{}, nil, err
	}
	var out OrderInfo
	err := json.Unmarshal(raw, &out)
	return out, raw, err
}
