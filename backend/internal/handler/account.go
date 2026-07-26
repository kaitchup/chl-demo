package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"chldemo/internal/middleware"
	"chldemo/internal/repo"
	"chldemo/internal/upay"

	"github.com/labstack/echo/v4"
)

// memberView computes membership status from a subscription.
func memberView(s repo.Subscription, found bool) map[string]any {
	status := "none"
	var expires *time.Time
	daysLeft := 0
	if found && s.ExpiresAt != nil {
		expires = s.ExpiresAt
		if s.ExpiresAt.After(time.Now()) {
			status = "active"
			daysLeft = int(time.Until(*s.ExpiresAt).Hours() / 24)
		} else {
			status = "expired"
		}
	}
	return map[string]any{
		"tier":       "super",
		"status":     status,
		"expires_at": expires,
		"days_left":  daysLeft,
	}
}

func (h *Handler) Me(c echo.Context) error {
	uid := middleware.UserID(c)
	u, err := h.repo.GetUserByID(c.Request().Context(), uid)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "unauthorized", "user not found")
	}
	sub, found, _ := h.repo.GetSubscription(c.Request().Context(), uid)
	return ok(c, map[string]any{
		"email":      u.Email,
		"created_at": u.CreatedAt,
		"member":     memberView(sub, found),
	})
}

func (h *Handler) Subscription(c echo.Context) error {
	uid := middleware.UserID(c)
	sub, found, err := h.repo.GetSubscription(c.Request().Context(), uid)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	return ok(c, memberView(sub, found))
}

func (h *Handler) ListPlans(c echo.Context) error {
	plans, err := h.repo.ListPlans(c.Request().Context())
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	out := make([]map[string]any, 0, len(plans))
	for _, p := range plans {
		out = append(out, map[string]any{
			"code": p.Code, "name": p.Name, "amount": p.Amount,
			"currency": p.Currency, "duration_days": p.DurationDays,
		})
	}
	return ok(c, out)
}

// displayStatus derives a read-only status for the UI. UPay no longer has an
// EXPIRED status (chain arrival stays authoritative, so the row stays PENDING and
// reconcile keeps polling — a late on-chain payment can still activate it). For
// display only, an unpaid order past its deadline is surfaced as EXPIRED without
// mutating the DB.
func displayStatus(o repo.Order) string {
	if o.Status == "PENDING" && o.ExpiresAt != nil && o.ExpiresAt.Before(time.Now()) {
		if amt, err := strconv.ParseFloat(o.ReceivedAmount, 64); err == nil && amt == 0 {
			return "EXPIRED"
		}
	}
	return o.Status
}

func orderView(o repo.Order) map[string]any {
	return map[string]any{
		"row_id":          o.ID,
		"order_id":        o.MerchantOrderID,
		"upay_payment_id": o.UpayPaymentID,
		"uid":             o.UpayUID,
		"plan_code":       o.PlanCode,
		"amount":          o.Amount,
		"currency":        o.Currency,
		"status":          displayStatus(o),
		"payment_status":  o.PaymentStatus,
		"kyc_status":      o.KycStatus,
		"kyc_first_name":  o.KycFirstName,
		"kyc_last_name":   o.KycLastName,
		"received_amount": o.ReceivedAmount,
		"failure_code":    o.FailureCode,
		"checkout_url":    o.CheckoutURL,
		"created_at":      o.CreatedAt,
		"paid_at":         o.PaidAt,
		"expires_at":      o.ExpiresAt,
	}
}

// CreateOrder persists a PENDING order, then creates the UPay order (real mode)
// or a placeholder checkout (no UPay configured). send_uid (default true) controls
// whether the UPay order carries our stable per-user uid — with uid, KYC is done
// once per user; without, UPay requires KYC per merchant_order_id (test scenario).
func (h *Handler) CreateOrder(c echo.Context) error {
	uid := middleware.UserID(c)
	var in struct {
		PlanCode string `json:"plan_code"`
		SendUID  *bool  `json:"send_uid"`
	}
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "invalid_request", "malformed body")
	}
	withUID := in.SendUID == nil || *in.SendUID

	plan, err := h.repo.GetPlan(c.Request().Context(), in.PlanCode)
	if errors.Is(err, repo.ErrNotFound) {
		return fail(c, http.StatusBadRequest, "plan_not_found", "unknown plan_code")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}

	ctx := c.Request().Context()

	// Reuse an existing open UPay order to avoid duplicate checkouts (real mode only).
	if h.upay != nil {
		if ord, err := h.repo.ReuseOpenOrder(ctx, uid, plan.Code, withUID); err == nil && ord.CheckoutURL != nil {
			return ok(c, map[string]any{
				"order_id": ord.MerchantOrderID, "checkout_url": *ord.CheckoutURL,
				"expires_at": ord.ExpiresAt, "reused": true,
			})
		}
	}

	moid := randID("sub_")
	idemKey := moid + ":create"

	var upayUID *string
	if withUID {
		v := fmt.Sprintf("user-%d", uid) // stable, no PII — never email
		upayUID = &v
	}

	// Persist the local order first (PENDING) so we never lose track of a payment.
	o := repo.Order{
		MerchantOrderID: moid,
		UpayUID:         upayUID,
		PlanCode:        plan.Code,
		Amount:          plan.Amount,
		Currency:        plan.Currency,
		Status:          "PENDING",
	}
	if err := h.repo.CreateOrder(ctx, o, uid, idemKey); err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "create order failed")
	}

	// Placeholder mode: no UPay configured (keeps local dev working without NetBird).
	if h.upay == nil {
		expires := time.Now().Add(30 * time.Minute)
		checkout := h.cfg.PublicBaseURL + "/checkout/" + moid
		_ = h.repo.AttachUpayResult(ctx, moid, "", checkout, &expires)
		return ok(c, map[string]any{
			"order_id": moid, "checkout_url": checkout, "expires_at": expires,
			"note": "占位收银台（未配置 UPay）；配置 UPAY_API_KEY 后启用真实支付",
		})
	}

	// Real mode: create the UPay order and attach its checkout URL.
	resultURL := h.cfg.PublicBaseURL + "/checkout/" + moid + "/result"
	var uidValue string
	if upayUID != nil {
		uidValue = *upayUID
	}
	reqBody, respBody, up, err := h.upay.CreateOrder(upay.CreateOrderReq{
		MerchantOrderID:  moid,
		UID:              uidValue,
		Amount:           plan.Amount, // already "20.00" form from numeric::text
		Currency:         plan.Currency,
		Description:      plan.Name,
		ExpiresInSeconds: 1800,
		SuccessURL:       resultURL,
		Metadata:         map[string]string{"user_id": fmt.Sprint(uid), "plan_code": plan.Code},
	}, idemKey)
	_ = h.repo.SaveUpayBodies(ctx, moid, reqBody, respBody)
	if err != nil {
		_ = h.repo.MarkOrderFailedByMOID(ctx, moid)
		return fail(c, http.StatusBadGateway, "upstream_error", "create UPay order failed: "+err.Error())
	}

	expiresAt := parseRFC3339(up.ExpiresAt)
	if err := h.repo.AttachUpayResult(ctx, moid, up.ID, up.CheckoutURL, expiresAt); err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "attach upay result failed")
	}

	return ok(c, map[string]any{
		"order_id": moid, "checkout_url": up.CheckoutURL, "expires_at": up.ExpiresAt,
	})
}

// parseRFC3339 returns a *time.Time or nil for empty/invalid input.
func parseRFC3339(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}

func (h *Handler) ListOrders(c echo.Context) error {
	uid := middleware.UserID(c)
	ctx := c.Request().Context()

	var beforeID int64
	if s := c.QueryParam("before_id"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			beforeID = v
		}
	}
	includeExpired := c.QueryParam("include_expired") == "true"
	limit := 20
	if s := c.QueryParam("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	orders, hasMore, err := h.repo.ListOrdersPage(ctx, uid, beforeID, includeExpired, limit)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	out := make([]map[string]any, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderView(o))
	}
	return ok(c, map[string]any{
		"orders":   out,
		"has_more": hasMore,
	})
}

func (h *Handler) GetOrder(c echo.Context) error {
	uid := middleware.UserID(c)
	o, err := h.repo.GetOrder(c.Request().Context(), uid, c.Param("id"))
	if errors.Is(err, repo.ErrNotFound) {
		return fail(c, http.StatusNotFound, "not_found", "order not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	return ok(c, orderView(o))
}
