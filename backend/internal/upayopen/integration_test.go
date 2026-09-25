//go:build integration

// Read-only checks against the real UPay test environment:
//
//	UPA_HOST=… UPA_API_KEY=… UPA_SECRET_KEY=… UPA_PLATFORM_PUBLIC_KEY=… UPA_MERCHANT_PRIVATE_KEY=… \
//	UPA_TEST_THIRD_ORDER_NO=PO1790318127PROBE UPA_TEST_BENEFICIARY_NO=2103372347110658048 \
//	go test -tags integration ./internal/upayopen/ -run Integration -v
package upayopen

import (
	"context"
	"os"
	"testing"
)

func integrationClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(os.Getenv("UPA_HOST"), os.Getenv("UPA_API_KEY"), os.Getenv("UPA_SECRET_KEY"),
		os.Getenv("UPA_PLATFORM_PUBLIC_KEY"), os.Getenv("UPA_MERCHANT_PRIVATE_KEY"))
	if err != nil {
		t.Skipf("UPA_* not configured: %v", err)
	}
	return c
}

func TestIntegrationReadOnly(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	coins, err := c.DebitCoins(ctx)
	if err != nil || len(coins) == 0 {
		t.Fatalf("DebitCoins = %v, %v", coins, err)
	}
	t.Logf("debit coins: %+v", coins)

	countries, err := c.Countries(ctx)
	if err != nil || len(countries) == 0 {
		t.Fatalf("Countries: %d, %v", len(countries), err)
	}
	t.Logf("countries: %d, first %+v", len(countries), countries[0])

	if no := os.Getenv("UPA_TEST_THIRD_ORDER_NO"); no != "" {
		info, raw, err := c.OrderInfo(ctx, no)
		if err != nil {
			t.Fatalf("OrderInfo: %v", err)
		}
		t.Logf("order %s status=%d remit=%s %s raw=%d bytes", info.OrderNo, info.Status,
			info.RemitInfo.RemitAmount, info.RemitInfo.RemitCurrency, len(raw))
		q, err := c.QuoteInfo(ctx, no)
		if err != nil {
			t.Fatalf("QuoteInfo: %v", err)
		}
		t.Logf("quote status=%d quotes=%d", q.Status, len(q.QuoteList))
	}
	if no := os.Getenv("UPA_TEST_BENEFICIARY_NO"); no != "" {
		b, err := c.BeneficiaryInfo(ctx, no)
		if err != nil {
			t.Fatalf("BeneficiaryInfo: %v", err)
		}
		m := b.Info.Flatten()
		if m["residentialAddress.country"] == "" {
			t.Fatalf("flatten missing residentialAddress.country: %v", m)
		}
		t.Logf("beneficiary address country=%s city=%s", m["residentialAddress.country"], m["residentialAddress.city"])
	}
}
