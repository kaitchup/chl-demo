package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"chldemo/internal/config"
	"chldemo/internal/repo"
	"chldemo/internal/service"
	"chldemo/internal/upay"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	cfg     config.Config
	repo    *repo.Repo
	upay    *upay.Client            // nil in placeholder mode
	payment *service.PaymentService // nil in placeholder mode
}

// New builds the handler. upayClient/payment may be nil when UPay is not
// configured — CreateOrder then falls back to a placeholder checkout.
func New(cfg config.Config, r *repo.Repo, upayClient *upay.Client, payment *service.PaymentService) *Handler {
	return &Handler{cfg: cfg, repo: r, upay: upayClient, payment: payment}
}

// envelope is the unified response shape: { "data": ..., "error": ... }.
type envelope struct {
	Data  any     `json:"data"`
	Error *apiErr `json:"error"`
}

type apiErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, envelope{Data: data})
}

func fail(c echo.Context, status int, code, msg string) error {
	return c.JSON(status, envelope{Error: &apiErr{Code: code, Message: msg}})
}

func (h *Handler) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"ok": true, "version": "0.0.2", "upay_enabled": h.cfg.UpayEnabled(),
	})
}

// randID returns a prefixed random identifier, e.g. sub_<32 hex chars>.
func randID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}
