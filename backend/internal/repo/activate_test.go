package repo

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration test for ActivateMembership. Skipped unless TEST_DATABASE_URL is
// set (so it never breaks CI without a database). Run locally:
//   TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/chl?sslmode=disable" go test ./internal/repo/ -run Activate -v
func TestActivateMembership(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	r := New(pool)

	email := fmt.Sprintf("activate_test_%d@test.com", time.Now().UnixNano())
	uid, err := r.CreateUser(ctx, email, "x")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM orders WHERE user_id=$1`, uid)
		pool.Exec(ctx, `DELETE FROM subscriptions WHERE user_id=$1`, uid)
		pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, uid)
	})

	// First paid order (monthly = 30 days).
	moid1 := fmt.Sprintf("sub_test_%d", time.Now().UnixNano())
	pay1 := "pay_test_" + moid1
	mustOrder(t, ctx, r, moid1, uid, "monthly")
	if err := r.AttachUpayResult(ctx, moid1, pay1, "http://x", nil); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := r.ActivateMembership(ctx, pay1, now); err != nil {
		t.Fatalf("activate 1: %v", err)
	}
	sub, found, _ := r.GetSubscription(ctx, uid)
	if !found || sub.ExpiresAt == nil {
		t.Fatal("expected active subscription")
	}
	exp1 := *sub.ExpiresAt
	if d := exp1.Sub(now).Hours(); d < 29*24 || d > 31*24 {
		t.Fatalf("expected ~30d, got %.1fh", d)
	}

	// Idempotent: re-activating the same order must NOT extend again.
	if err := r.ActivateMembership(ctx, pay1, now); err != nil {
		t.Fatalf("activate 1 repeat: %v", err)
	}
	sub, _, _ = r.GetSubscription(ctx, uid)
	if !sub.ExpiresAt.Equal(exp1) {
		t.Fatalf("idempotency broken: %v != %v", *sub.ExpiresAt, exp1)
	}

	// Second order (quarterly = 90 days) stacks on top of remaining time.
	moid2 := fmt.Sprintf("sub_test_%d_b", time.Now().UnixNano())
	pay2 := "pay_test_" + moid2
	mustOrder(t, ctx, r, moid2, uid, "quarterly")
	if err := r.AttachUpayResult(ctx, moid2, pay2, "http://x", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ActivateMembership(ctx, pay2, now); err != nil {
		t.Fatalf("activate 2: %v", err)
	}
	sub, _, _ = r.GetSubscription(ctx, uid)
	if d := sub.ExpiresAt.Sub(exp1).Hours(); d < 89*24 || d > 91*24 {
		t.Fatalf("expected +90d stacked, got +%.1fh", d)
	}
}

func mustOrder(t *testing.T, ctx context.Context, r *Repo, moid string, uid int64, plan string) {
	t.Helper()
	p, err := r.GetPlan(ctx, plan)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	o := Order{MerchantOrderID: moid, PlanCode: p.Code, Amount: p.Amount, Currency: p.Currency, Status: "PENDING"}
	if err := r.CreateOrder(ctx, o, uid, moid+":create"); err != nil {
		t.Fatalf("create order: %v", err)
	}
}
