package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chldemo/db"
	"chldemo/internal/admin"
	"chldemo/internal/config"
	"chldemo/internal/handler"
	"chldemo/internal/job"
	"chldemo/internal/middleware"
	"chldemo/internal/repo"
	"chldemo/internal/service"
	"chldemo/internal/upay"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	emw "github.com/labstack/echo/v4/middleware"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	pool, err := connectWithRetry(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied")

	r := repo.New(pool)

	// UPay integration is optional: when no API key is set the app runs in
	// placeholder mode so local dev works without NetBird/UPay.
	var (
		upayClient *upay.Client
		payment    *service.PaymentService
	)
	if cfg.UpayEnabled() {
		upayClient, err = upay.New(cfg.UpayBaseURL, cfg.UpayAPIKey, cfg.UpayCACertPath)
		if err != nil {
			log.Fatalf("upay client: %v", err)
		}
		payment = service.NewPayment(upayClient, r)
		reconciler := job.NewReconciler(r, upayClient, payment, cfg.ReconcileInterval)
		go reconciler.Run(ctx)
		log.Printf("UPay enabled (base=%s)", cfg.UpayBaseURL)
	} else {
		log.Println("UPay NOT configured — placeholder checkout mode")
	}

	h := handler.New(cfg, r, upayClient, payment)

	// Sandbox module: optional, requires Dolos credentials.
	var sandboxHandler *handler.SandboxHandler
	if cfg.SandboxEnabled() {
		adminClient := admin.NewClient(cfg.AdminBaseURL, cfg.DolosEmail, cfg.DolosPassword)
		merchantAuth := admin.NewMerchantAuth(cfg.MerchantAdminBaseURL)
		sandboxHandler = handler.NewSandboxHandler(r, adminClient, merchantAuth, cfg.SandboxMaxAMLRetries)
		amlPoller := job.NewAMLPoller(r, adminClient, cfg.ReconcileInterval)
		go amlPoller.Run(ctx)
		log.Printf("sandbox enabled (admin=%s merchant-admin=%s)", cfg.AdminBaseURL, cfg.MerchantAdminBaseURL)
	} else {
		log.Println("sandbox NOT configured — DOLOS_EMAIL/ADMIN_BASE_URL not set")
	}

	e := echo.New()
	e.HideBanner = true
	e.Use(emw.Logger(), emw.Recover())
	e.Use(emw.CORSWithConfig(emw.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	api := e.Group("/api")
	api.GET("/healthz", h.Health)
	// api.POST("/auth/register", h.Register)
	api.POST("/auth/login", h.Login)
	api.GET("/plans", h.ListPlans)
	api.POST("/webhooks/upay", h.UpayWebhook)                                            // public; authenticated via HMAC signature
	api.POST("/webhooks/yoki", h.LogOnlyWebhook(cfg.YokiWebhookSecrets(), "yoki"))       // public; authenticated via HMAC signature
	api.POST("/webhooks/sudy", h.LogOnlyWebhook(cfg.SudyWebhookSecrets(), "sudy"))       // public; authenticated via HMAC signature
	api.POST("/webhooks/shirly", h.LogOnlyWebhook(cfg.ShirlyWebhookSecrets(), "shirly")) // public; authenticated via HMAC signature
	api.POST("/webhooks/tia", h.LogOnlyWebhook(cfg.TiaWebhookSecrets(), "tia"))          // public; authenticated via HMAC signature
	api.POST("/webhooks/cassiel", h.LogOnlyWebhook(cfg.CassielWebhookSecrets(), "cassiel")) // public; authenticated via HMAC signature
	api.POST("/webhooks/mch_test_merchant", h.ProdWebhook(                               // production; sig-first + dedup
		cfg.TestMerchantWebhookSecrets(), r.ExistsWebhookTestMerchant, r.InsertWebhookTestMerchant,
	))
	api.POST("/webhooks/prod_dummy1", h.ProdWebhook( // production; sig-first + dedup
		cfg.ProdDummy1WebhookSecrets(), r.ExistsWebhookProdDummy, r.InsertWebhookProdDummy,
	))

	auth := api.Group("", middleware.JWT(cfg.JWTSecret))
	auth.GET("/me", h.Me)
	auth.GET("/subscription", h.Subscription)
	auth.POST("/orders", h.CreateOrder)
	auth.GET("/orders", h.ListOrders)
	auth.GET("/orders/:id", h.GetOrder)
	auth.POST("/orders/:id/refunds", h.CreateRefund)
	auth.GET("/orders/:id/refunds", h.ListOrderRefunds)
	auth.GET("/refunds/:id", h.GetRefund)
	auth.POST("/refunds/:id/sync", h.SyncRefund)

	// Sandbox routes — /login is public; simulation routes require X-Merchant-Token session key.
	// Returns 503 when sandbox is not configured.
	sb := api.Group("/sandbox")
	if sandboxHandler != nil {
		sb.POST("/login", sandboxHandler.Login)
		sb.POST("/simulations", sandboxHandler.CreateSimulation)
		sb.GET("/simulations", sandboxHandler.ListSimulations)
		sb.GET("/simulations/:id", sandboxHandler.GetSimulation)
	} else {
		sb.Any("/*", func(c echo.Context) error {
			return c.JSON(http.StatusServiceUnavailable, map[string]any{
				"error": map[string]any{"code": "sandbox_disabled", "message": "sandbox not configured"},
			})
		})
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go func() {
		if err := e.Start(":" + port); err != nil {
			log.Println("server stopped:", err)
		}
	}()
	log.Println("listening on :" + port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = e.Shutdown(shutdownCtx)
}

func connectWithRetry(ctx context.Context, url string) (*pgxpool.Pool, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		log.Printf("waiting for db (%d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}
