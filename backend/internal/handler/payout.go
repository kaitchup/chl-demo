package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"chldemo/internal/middleware"
	"chldemo/internal/repo"
	"chldemo/internal/service"
	"chldemo/internal/upayopen"

	"github.com/labstack/echo/v4"
)

// PayoutHandler serves /api/payout/* (TECH-DESIGN-汇款功能.md §7). All routes
// sit behind JWT + the payout_enabled whitelist.
type PayoutHandler struct {
	svc  *service.PayoutService
	repo *repo.Repo
}

func NewPayoutHandler(svc *service.PayoutService, r *repo.Repo) *PayoutHandler {
	return &PayoutHandler{svc: svc, repo: r}
}

type payoutErrBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// payoutFail renders service errors with their status/code; anything else is 500.
func payoutFail(c echo.Context, err error) error {
	var pe *service.PayoutError
	if errors.As(err, &pe) {
		return c.JSON(pe.HTTP, map[string]any{"data": nil,
			"error": payoutErrBody{Code: pe.Code, Message: pe.Msg, Details: pe.Extra}})
	}
	if errors.Is(err, repo.ErrNotFound) {
		return fail(c, http.StatusNotFound, "NOT_FOUND", "not found")
	}
	log.Printf("payout: %s %s: %v", c.Request().Method, c.Path(), err)
	return fail(c, http.StatusInternalServerError, "INTERNAL", "internal error")
}

func pathID(c echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &service.PayoutError{HTTP: http.StatusNotFound, Code: "NOT_FOUND", Msg: "not found"}
	}
	return id, nil
}

// ---- bootstrap / options ----

func (h *PayoutHandler) Bootstrap(c echo.Context) error {
	ctx, uid := c.Request().Context(), middleware.UserID(c)
	payer, err := h.repo.GetPayoutPayer(ctx, uid)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return payoutFail(c, err)
	}
	hasPwd, err := h.svc.HasTradePassword(ctx, uid)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{
		"has_payer":          payer.PayerNo != "",
		"payer_name":         payer.Name,
		"has_trade_password": hasPwd,
		"debit_symbol":       h.svc.DebitSymbol(),
		"min_amount":         service.MinPayoutAmount,
		"max_amount":         service.MaxPayoutAmount,
		"currency":           "USD",
	})
}

func (h *PayoutHandler) Options(c echo.Context) error {
	areas, err := h.svc.Countries(c.Request().Context())
	if err != nil {
		return payoutFail(c, err)
	}
	countries := make([]map[string]string, 0, len(areas))
	for _, a := range areas {
		if a.Alpha2 != "" {
			countries = append(countries, map[string]string{"code": a.Alpha2, "name_zh": a.NameZh, "name_en": a.NameEn})
		}
	}
	return ok(c, map[string]any{
		"countries":         countries,
		"genders":           service.Genders,
		"doc_types":         service.DocTypes,
		"expiry_types":      service.ExpiryTypes,
		"employment_status": service.EmploymentStatus,
		"sources_of_income": service.SourcesOfIncome,
	})
}

// ---- files ----

func (h *PayoutHandler) Upload(c echo.Context) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return payoutFail(c, &service.PayoutError{HTTP: http.StatusUnprocessableEntity, Code: "VALIDATION", Msg: "file: required"})
	}
	if fh.Size > service.MaxUploadBytes {
		return payoutFail(c, &service.PayoutError{HTTP: http.StatusRequestEntityTooLarge, Code: "FILE_TOO_LARGE",
			Msg: "file must be at most 10 MB"})
	}
	f, err := fh.Open()
	if err != nil {
		return payoutFail(c, err)
	}
	defer f.Close()
	id, err := h.svc.UploadFile(c.Request().Context(), f, fh.Filename)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{"file_id": id, "file_name": fh.Filename})
}

// ---- payer ----

func (h *PayoutHandler) GetPayer(c echo.Context) error {
	p, err := h.repo.GetPayoutPayer(c.Request().Context(), middleware.UserID(c))
	if errors.Is(err, repo.ErrNotFound) {
		return ok(c, map[string]any{"exists": false})
	} else if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{"exists": true, "name": p.Name})
}

func (h *PayoutHandler) CreatePayer(c echo.Context) error {
	var in service.PayerInput
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
	}
	p, err := h.svc.CreatePayer(c.Request().Context(), middleware.UserID(c), in)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{"name": p.Name})
}

// ---- recipients ----

func recipientView(p repo.PayoutRecipient) map[string]any {
	return map[string]any{
		"id": p.ID, "name": p.Name(), "given_name": p.GivenName, "family_name": p.FamilyName,
		"bank_name": p.BankName, "swift_code": p.SwiftCode, "account_last4": p.AccountLast4,
		"currency": p.Currency, "bank_country": p.BankCountry, "complete": p.Complete(),
		"last_error": p.LastError, "created_at": p.CreatedAt,
	}
}

func (h *PayoutHandler) ListRecipients(c echo.Context) error {
	list, err := h.repo.ListPayoutRecipients(c.Request().Context(), middleware.UserID(c))
	if err != nil {
		return payoutFail(c, err)
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, recipientView(p))
	}
	return ok(c, out)
}

func (h *PayoutHandler) CreateRecipient(c echo.Context) error {
	var in service.RecipientInput
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
	}
	p, err := h.svc.CreateRecipient(c.Request().Context(), middleware.UserID(c), in)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, recipientView(p))
}

func (h *PayoutHandler) RetryRecipient(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return payoutFail(c, err)
	}
	var in service.BankInput
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
	}
	p, err := h.svc.RetryRecipientBank(c.Request().Context(), middleware.UserID(c), id, in)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, recipientView(p))
}

// ---- trade password ----

func (h *PayoutHandler) SetTradePassword(c echo.Context) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
	}
	if err := h.svc.SetTradePassword(c.Request().Context(), middleware.UserID(c), in.Password); err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{})
}

// ---- orders ----

func (h *PayoutHandler) CreateOrder(c echo.Context) error {
	var in struct {
		RecipientID int64  `json:"recipient_id"`
		Amount      string `json:"amount"`
		Usage       string `json:"usage"`
		Note        string `json:"note"`
	}
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
	}
	res, err := h.svc.CreateOrder(c.Request().Context(), middleware.UserID(c), in.RecipientID, in.Amount, in.Usage, in.Note)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, createdView(res))
}

func createdView(res service.CreatedOrder) map[string]any {
	return map[string]any{"id": res.Order.ID, "status": res.Order.Status, "stage": service.Stage(res.Order.Status),
		"information_to_improve": res.Improve}
}

func (h *PayoutHandler) Requote(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return payoutFail(c, err)
	}
	res, err := h.svc.Requote(c.Request().Context(), middleware.UserID(c), id)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, createdView(res))
}

func (h *PayoutHandler) Quote(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return payoutFail(c, err)
	}
	st, err := h.svc.Quote(c.Request().Context(), middleware.UserID(c), id)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, st)
}

func (h *PayoutHandler) Confirm(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return payoutFail(c, err)
	}
	var in struct {
		QuoteNo       string `json:"quote_no"`
		TradePassword string `json:"trade_password"`
	}
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
	}
	status, err := h.svc.Confirm(c.Request().Context(), middleware.UserID(c), id, in.QuoteNo, in.TradePassword)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{"status": status, "stage": service.Stage(status)})
}

func (h *PayoutHandler) Cancel(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return payoutFail(c, err)
	}
	status, err := h.svc.Cancel(c.Request().Context(), middleware.UserID(c), id)
	if err != nil {
		return payoutFail(c, err)
	}
	return ok(c, map[string]any{"status": status, "stage": service.Stage(status)})
}

func (h *PayoutHandler) ListOrders(c echo.Context) error {
	ctx, uid := c.Request().Context(), middleware.UserID(c)
	before, _ := strconv.ParseInt(c.QueryParam("before_id"), 10, 64)
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	orders, more, err := h.repo.ListPayoutOrders(ctx, uid, before, limit)
	if err != nil {
		return payoutFail(c, err)
	}
	names := map[int64]string{}
	if recs, err := h.repo.ListPayoutRecipients(ctx, uid); err == nil {
		for _, r := range recs {
			names[r.ID] = r.Name()
		}
	}
	out := make([]map[string]any, 0, len(orders))
	for _, o := range orders {
		out = append(out, map[string]any{
			"id": o.ID, "amount": o.Amount, "currency": o.Currency, "debit_coin": o.DebitCoin,
			"debit_total": snapshot(o).DebitTotal, "recipient_name": names[o.RecipientID],
			"status": o.Status, "stage": service.Stage(o.Status), "created_at": o.CreatedAt,
		})
	}
	return ok(c, map[string]any{"orders": out, "has_more": more})
}

func snapshot(o repo.PayoutOrder) service.QuoteView {
	var q service.QuoteView
	if len(o.Quote) > 0 {
		_ = json.Unmarshal(o.Quote, &q)
	}
	return q
}

type timelineEntry struct {
	Status int       `json:"status"`
	Stage  string    `json:"stage"`
	At     time.Time `json:"at"`
	Source string    `json:"source"`
}

func (h *PayoutHandler) GetOrder(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return payoutFail(c, err)
	}
	ctx, uid := c.Request().Context(), middleware.UserID(c)
	o, err := h.repo.GetPayoutOrder(ctx, uid, id)
	if err != nil {
		return payoutFail(c, err)
	}
	rec, err := h.repo.GetPayoutRecipient(ctx, uid, o.RecipientID)
	if err != nil {
		return payoutFail(c, err)
	}
	events, err := h.repo.ListPayoutOrderEvents(ctx, o.ID)
	if err != nil {
		return payoutFail(c, err)
	}
	timeline := make([]timelineEntry, 0, len(events))
	for _, e := range events {
		timeline = append(timeline, timelineEntry{Status: e.Status, Stage: service.Stage(e.Status), At: e.ObservedAt, Source: e.Source})
	}
	var info struct {
		RemitInfo struct {
			ExchangeRate upayopen.Str `json:"exchangeRate"`
		} `json:"remitInfo"`
	}
	if len(o.LastInfo) > 0 {
		_ = json.Unmarshal(o.LastInfo, &info)
	}
	var quote any
	if len(o.Quote) > 0 {
		quote = snapshot(o)
	}
	return ok(c, map[string]any{
		"id": o.ID, "third_order_no": o.ThirdOrderNo, "order_no": o.OrderNo,
		"amount": o.Amount, "currency": o.Currency, "debit_coin": o.DebitCoin,
		"usage": o.Usage, "note": o.Note, "remit_method": "swift",
		"status": o.Status, "stage": service.Stage(o.Status),
		"quote": quote, "exchange_rate": info.RemitInfo.ExchangeRate.String(),
		"recipient": recipientView(rec), "timeline": timeline,
		"last_error": o.LastError, "created_at": o.CreatedAt, "confirmed_at": o.ConfirmedAt,
	})
}
