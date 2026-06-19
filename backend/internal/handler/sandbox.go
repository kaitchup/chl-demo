package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"chldemo/internal/admin"
	"chldemo/internal/repo"

	"github.com/labstack/echo/v4"
)

// SandboxHandler handles the /api/sandbox/* routes.
// It is only registered when cfg.SandboxEnabled() is true.
type SandboxHandler struct {
	repo         *repo.Repo
	adminClient  *admin.Client
	merchantAuth *admin.MerchantAuth
	maxRetries   int
}

func NewSandboxHandler(r *repo.Repo, ac *admin.Client, ma *admin.MerchantAuth, maxRetries int) *SandboxHandler {
	return &SandboxHandler{repo: r, adminClient: ac, merchantAuth: ma, maxRetries: maxRetries}
}

// POST /api/sandbox/login
func (sh *SandboxHandler) Login(c echo.Context) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil || in.Email == "" || in.Password == "" {
		return fail(c, http.StatusBadRequest, "invalid_request", "email and password required")
	}
	sessionKey, profile, err := sh.merchantAuth.Login(in.Email, in.Password)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "login_failed", err.Error())
	}
	return ok(c, map[string]any{
		"session_key":   sessionKey,
		"merchant_id":   profile.MerchantID,
		"merchant_name": profile.MerchantName,
	})
}

// merchantProfile extracts and resolves the X-Merchant-Token session key.
// Returns the merchant profile or writes a 401 and returns nil.
func (sh *SandboxHandler) merchantProfile(c echo.Context) *admin.MerchantProfile {
	key := c.Request().Header.Get("X-Merchant-Token")
	if key == "" {
		_ = fail(c, http.StatusUnauthorized, "missing_token", "X-Merchant-Token header required")
		return nil
	}
	profile, ok := sh.merchantAuth.GetProfile(key)
	if !ok {
		_ = fail(c, http.StatusUnauthorized, "invalid_session", "session not found, please log in again")
		return nil
	}
	return profile
}

// POST /api/sandbox/simulations
func (sh *SandboxHandler) CreateSimulation(c echo.Context) error {
	profile := sh.merchantProfile(c)
	if profile == nil {
		return nil
	}

	var in struct {
		PaymentRequestID string             `json:"payment_request_id"`
		Scenario         string             `json:"scenario"`
		ToAddress        string             `json:"to_address"`
		Amount           string             `json:"amount"`
		Coin             string             `json:"coin"`
		Events           []admin.SimEvent   `json:"events"`
	}
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "invalid_request", "malformed body")
	}
	if in.PaymentRequestID == "" {
		return fail(c, http.StatusBadRequest, "invalid_request", "payment_request_id required")
	}
	if in.Scenario == "" {
		return fail(c, http.StatusBadRequest, "invalid_request", "scenario required")
	}
	if in.ToAddress == "" {
		return fail(c, http.StatusBadRequest, "invalid_request", "to_address required")
	}
	if len(in.Events) == 0 {
		return fail(c, http.StatusBadRequest, "invalid_request", "events required")
	}

	eventsJSON, _ := json.Marshal(in.Events)

	var toAddr *string
	if in.ToAddress != "" {
		toAddr = &in.ToAddress
	}
	var coin *string
	if in.Coin != "" {
		coin = &in.Coin
	}

	ctx := c.Request().Context()

	simID, err := sh.repo.CreateSimulation(ctx, repo.Simulation{
		MerchantID:       profile.MerchantID,
		PaymentRequestID: in.PaymentRequestID,
		Scenario:         in.Scenario,
		ToAddress:        toAddr,
		Amount:           in.Amount,
		Coin:             coin,
		SimEvents:        eventsJSON,
		MaxRetries:       sh.maxRetries,
	})
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "create simulation failed")
	}

	// Verify the payment request belongs to the logged-in merchant.
	ownerID, err := sh.adminClient.GetPaymentRequestMerchantID(in.PaymentRequestID)
	if err != nil {
		_ = sh.repo.MarkSimulationFailed(ctx, simID, err.Error())
		return fail(c, http.StatusBadRequest, "invalid_payment_request", err.Error())
	}
	if ownerID != profile.MerchantID {
		_ = sh.repo.MarkSimulationFailed(ctx, simID, "payment request belongs to another merchant")
		return fail(c, http.StatusForbidden, "forbidden", "payment request does not belong to your merchant account")
	}

	// Call admin API to trigger the simulation.
	_, err = sh.adminClient.SimulateSettlement(in.PaymentRequestID, admin.SimulateReq{
		Scenario: in.Scenario,
		Events:   in.Events,
	})
	if err != nil {
		_ = sh.repo.MarkSimulationFailed(ctx, simID, err.Error())
		return fail(c, http.StatusBadGateway, "simulate_failed", err.Error())
	}

	sim, _ := sh.repo.GetSimulation(ctx, simID, profile.MerchantID)
	return ok(c, simulationView(sim, nil, nil))
}

// GET /api/sandbox/simulations
func (sh *SandboxHandler) ListSimulations(c echo.Context) error {
	profile := sh.merchantProfile(c)
	if profile == nil {
		return nil
	}

	limit := 20
	offset := 0
	if s := c.QueryParam("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if s := c.QueryParam("offset"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			offset = v
		}
	}

	sims, err := sh.repo.ListSimulations(c.Request().Context(), profile.MerchantID, limit, offset)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "query failed")
	}

	out := make([]map[string]any, 0, len(sims))
	for _, s := range sims {
		out = append(out, simulationView(s, nil, nil))
	}
	return ok(c, out)
}

// GET /api/sandbox/simulations/:id
func (sh *SandboxHandler) GetSimulation(c echo.Context) error {
	profile := sh.merchantProfile(c)
	if profile == nil {
		return nil
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "invalid_id", "id must be an integer")
	}

	ctx := c.Request().Context()
	sim, err := sh.repo.GetSimulation(ctx, id, profile.MerchantID)
	if err != nil {
		return fail(c, http.StatusNotFound, "not_found", "simulation not found")
	}

	tickets, _ := sh.repo.ListAMLTickets(ctx, sim.ID)

	// Fetch live webhook deliveries from admin API.
	var webhooks []admin.WebhookDelivery
	wh, _, whErr := sh.adminClient.ListWebhookDeliveries(sim.PaymentRequestID, 1, 20)
	if whErr == nil {
		webhooks = wh
	}

	return ok(c, simulationView(sim, tickets, webhooks))
}

// ---- view helpers ----

func simulationView(s repo.Simulation, tickets []repo.AMLTicket, webhooks []admin.WebhookDelivery) map[string]any {
	v := map[string]any{
		"id":                  s.ID,
		"payment_request_id": s.PaymentRequestID,
		"scenario":           s.Scenario,
		"amount":             s.Amount,
		"coin":               s.Coin,
		"to_address":         s.ToAddress,
		"status":             s.Status,
		"retry_count":        s.RetryCount,
		"max_retries":        s.MaxRetries,
		"error_detail":       s.ErrorDetail,
		"created_at":         s.CreatedAt,
		"updated_at":         s.UpdatedAt,
	}
	if tickets != nil {
		tv := make([]map[string]any, 0, len(tickets))
		for _, t := range tickets {
			tv = append(tv, map[string]any{
				"inspection_id": t.InspectionID,
				"ticket_uuid":   t.TicketUUID,
				"approved_at":   t.ApprovedAt,
			})
		}
		v["aml_tickets"] = tv
	}
	if webhooks != nil {
		v["webhooks"] = webhooks
	}
	return v
}
