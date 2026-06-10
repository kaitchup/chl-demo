package handler

import (
	"errors"
	"net/http"

	"chldemo/internal/middleware"
	"chldemo/internal/repo"
	"chldemo/internal/upay"

	"github.com/labstack/echo/v4"
)

// CreateRefund: POST /api/orders/:id/refunds
// 仅 PAID 订单可退款；金额默认全额退；coin/chain/to_address 由前端用户填写。
func (h *Handler) CreateRefund(c echo.Context) error {
	uid := middleware.UserID(c)
	orderID := c.Param("id")

	var in struct {
		Coin      string `json:"coin"`
		Chain     string `json:"chain"`
		ToAddress string `json:"to_address"`
		Reason    string `json:"reason"`
	}
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "invalid_request", "malformed body")
	}
	if in.Coin != "USDT" && in.Coin != "USDC" {
		return fail(c, http.StatusBadRequest, "invalid_coin", "coin must be USDT or USDC")
	}
	if in.Chain != "TRON" && in.Chain != "ETH" && in.Chain != "BSC" {
		return fail(c, http.StatusBadRequest, "invalid_chain", "chain must be TRON / ETH / BSC")
	}
	if in.ToAddress == "" {
		return fail(c, http.StatusBadRequest, "to_address_required", "to_address is required")
	}

	ctx := c.Request().Context()
	ord, err := h.repo.GetOrder(ctx, uid, orderID)
	if errors.Is(err, repo.ErrNotFound) {
		return fail(c, http.StatusNotFound, "not_found", "order not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	if ord.Status != "PAID" {
		return fail(c, http.StatusBadRequest, "order_not_paid", "only PAID orders can be refunded")
	}

	mrID := randID("rfd_")

	rf := repo.Refund{
		MerchantRefundID: mrID,
		MerchantOrderID:  ord.MerchantOrderID,
		UserID:           uid,
		Amount:           ord.Amount,
		Coin:             in.Coin,
		Chain:            in.Chain,
		ToAddress:        in.ToAddress,
		Status:           "PENDING",
		Reason:           in.Reason,
	}

	// Real mode: call UPay gateway to initiate the refund.
	if h.upay != nil && ord.UpayPaymentID != nil {
		upayRF, err := h.upay.CreateRefund(upay.CreateRefundReq{
			PaymentRequestID: *ord.UpayPaymentID,
			Amount:           ord.Amount,
			Coin:             in.Coin,
			Chain:            in.Chain,
			ToAddress:        in.ToAddress,
			MerchantRefundID: mrID,
			Reason:           in.Reason,
		}, mrID+":create")
		if err != nil {
			return fail(c, http.StatusBadGateway, "upstream_error", "create refund failed: "+err.Error())
		}
		rf.UpayRefundID = &upayRF.ID
		rf.Status = upayRF.Status
	}

	if err := h.repo.CreateRefund(ctx, rf); err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "save refund failed")
	}
	return ok(c, refundView(rf))
}

// ListOrderRefunds: GET /api/orders/:id/refunds
func (h *Handler) ListOrderRefunds(c echo.Context) error {
	uid := middleware.UserID(c)
	orderID := c.Param("id")

	ctx := c.Request().Context()
	if _, err := h.repo.GetOrder(ctx, uid, orderID); errors.Is(err, repo.ErrNotFound) {
		return fail(c, http.StatusNotFound, "not_found", "order not found")
	} else if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}

	refunds, err := h.repo.ListRefundsByOrder(ctx, uid, orderID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	out := make([]map[string]any, 0, len(refunds))
	for _, rf := range refunds {
		out = append(out, refundView(rf))
	}
	return ok(c, out)
}

// GetRefund: GET /api/refunds/:id
func (h *Handler) GetRefund(c echo.Context) error {
	uid := middleware.UserID(c)
	rf, err := h.repo.GetRefund(c.Request().Context(), uid, c.Param("id"))
	if errors.Is(err, repo.ErrNotFound) {
		return fail(c, http.StatusNotFound, "not_found", "refund not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}
	return ok(c, refundView(rf))
}

func refundView(rf repo.Refund) map[string]any {
	return map[string]any{
		"refund_id":      rf.MerchantRefundID,
		"upay_refund_id": rf.UpayRefundID,
		"order_id":       rf.MerchantOrderID,
		"amount":         rf.Amount,
		"coin":           rf.Coin,
		"chain":          rf.Chain,
		"to_address":     rf.ToAddress,
		"status":         rf.Status,
		"reason":         rf.Reason,
		"created_at":     rf.CreatedAt,
	}
}
