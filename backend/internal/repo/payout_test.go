package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration test for the payout repo SQL (0013). Skipped unless
// TEST_DATABASE_URL is set. Run locally:
//
//	TEST_DATABASE_URL="postgres://chl:chl@127.0.0.1:5432/chl?sslmode=disable" go test ./internal/repo/ -run Payout -v
func TestPayoutRepo(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close) // registered first so it runs after the row cleanup below
	r := New(pool)

	tag :=fmt.Sprintf("%d", time.Now().UnixNano())
	uid, err := r.CreateUser(ctx, "payout_test_"+tag+"@test.com", "x")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	var logID int64
	notifyNo := "NT_test_" + tag
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM payout_order_events WHERE order_id IN (SELECT id FROM payout_orders WHERE user_id=$1)`, uid)
		pool.Exec(ctx, `DELETE FROM payout_orders WHERE user_id=$1`, uid)
		pool.Exec(ctx, `DELETE FROM payout_recipients WHERE user_id=$1`, uid)
		pool.Exec(ctx, `DELETE FROM payout_payers WHERE user_id=$1`, uid)
		pool.Exec(ctx, `DELETE FROM payout_webhook_events WHERE notify_no=$1`, notifyNo)
		pool.Exec(ctx, `DELETE FROM webhook_raw_log WHERE id=$1`, logID)
		pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, uid)
	})

	t.Run("whitelist", func(t *testing.T) {
		if ok, err := r.IsPayoutEnabled(ctx, uid); err != nil || ok {
			t.Fatalf("default: ok=%v err=%v", ok, err)
		}
		if ok, err := r.IsPayoutEnabled(ctx, -1); err != nil || ok {
			t.Fatalf("unknown user: ok=%v err=%v", ok, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET payout_enabled=true WHERE id=$1`, uid); err != nil {
			t.Fatal(err)
		}
		if ok, err := r.IsPayoutEnabled(ctx, uid); err != nil || !ok {
			t.Fatalf("enabled: ok=%v err=%v", ok, err)
		}
	})

	t.Run("trade password", func(t *testing.T) {
		if set, err := r.SetTradePasswordIfUnset(ctx, uid, "h1"); err != nil || !set {
			t.Fatalf("first set: set=%v err=%v", set, err)
		}
		if set, err := r.SetTradePasswordIfUnset(ctx, uid, "h2"); err != nil || set {
			t.Fatalf("second set must not overwrite: set=%v err=%v", set, err)
		}
		lockUntil := time.Now().Add(30 * time.Minute).Truncate(time.Microsecond)
		for i := 1; i <= 4; i++ {
			n, err := r.RecordTradePasswordFailure(ctx, uid, 5, lockUntil)
			if err != nil || n != i {
				t.Fatalf("failure %d: n=%d err=%v", i, n, err)
			}
		}
		tp, err := r.GetTradePassword(ctx, uid)
		if err != nil || tp.LockedUntil != nil || tp.FailCount != 4 {
			t.Fatalf("before lock: %+v err=%v", tp, err)
		}
		if n, err := r.RecordTradePasswordFailure(ctx, uid, 5, lockUntil); err != nil || n != 0 {
			t.Fatalf("5th failure should lock and reset: n=%d err=%v", n, err)
		}
		tp, err = r.GetTradePassword(ctx, uid)
		if err != nil || tp.Hash == nil || *tp.Hash != "h1" || tp.FailCount != 0 ||
			tp.LockedUntil == nil || !tp.LockedUntil.Equal(lockUntil) {
			t.Fatalf("locked: %+v err=%v", tp, err)
		}
		if err := r.ResetTradePasswordFailures(ctx, uid); err != nil {
			t.Fatal(err)
		}
		if tp, _ = r.GetTradePassword(ctx, uid); tp.LockedUntil != nil || tp.FailCount != 0 {
			t.Fatalf("after reset: %+v", tp)
		}
	})

	t.Run("payer", func(t *testing.T) {
		if _, err := r.GetPayoutPayer(ctx, uid); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing payer: %v", err)
		}
		if err := r.CreatePayoutPayer(ctx, uid, "PY_test_"+tag, "Test Payer"); err != nil {
			t.Fatal(err)
		}
		if err := r.CreatePayoutPayer(ctx, uid, "PY_test2_"+tag, "Test Payer"); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate payer: %v", err)
		}
		p, err := r.GetPayoutPayer(ctx, uid)
		if err != nil || p.PayerNo != "PY_test_"+tag || p.Name != "Test Payer" {
			t.Fatalf("payer: %+v err=%v", p, err)
		}
	})

	var recipientID int64
	t.Run("recipients", func(t *testing.T) {
		recipientID, err = r.CreatePayoutRecipient(ctx, PayoutRecipient{
			UserID: uid, GivenName: "San", FamilyName: "Zhang", BankName: "Bank A",
			SwiftCode: "AAAAUS33", AccountLast4: "1111", Currency: "USD", BankCountry: "US",
		})
		if err != nil {
			t.Fatal(err)
		}
		if list, err := r.ListPayoutRecipients(ctx, uid); err != nil || len(list) != 0 {
			t.Fatalf("recipient without beneficiary must be hidden: %d err=%v", len(list), err)
		}
		if err := r.SetRecipientError(ctx, recipientID, "step 1 failed"); err != nil {
			t.Fatal(err)
		}
		if err := r.SetRecipientBeneficiary(ctx, recipientID, "BF_test_"+tag); err != nil {
			t.Fatal(err)
		}
		p, err := r.GetPayoutRecipient(ctx, uid, recipientID)
		if err != nil || p.Complete() || p.LastError != nil || p.Name() != "San Zhang" {
			t.Fatalf("after step 1: %+v err=%v", p, err)
		}
		bank := "BA_test_" + tag
		p.BankAccountNo, p.BankName, p.AccountLast4 = &bank, "Bank B", "2222"
		if err := r.SetRecipientBank(ctx, p); err != nil {
			t.Fatal(err)
		}
		list, err := r.ListPayoutRecipients(ctx, uid)
		if err != nil || len(list) != 1 || !list[0].Complete() || list[0].BankName != "Bank B" || list[0].AccountLast4 != "2222" {
			t.Fatalf("after step 2: %+v err=%v", list, err)
		}
		if _, err := r.GetPayoutRecipient(ctx, uid+1_000_000, recipientID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other user's recipient: %v", err)
		}
	})

	var orderIDs []int64
	t.Run("orders", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			id, err := r.CreatePayoutOrder(ctx, PayoutOrder{
				UserID: uid, RecipientID: recipientID, ThirdOrderNo: fmt.Sprintf("TO_test_%s_%d", tag, i),
				Amount: "68.06", Currency: "USD", DebitCoin: "USDT", Usage: "tuition", Note: "tuition",
			})
			if err != nil {
				t.Fatal(err)
			}
			orderIDs = append(orderIDs, id)
		}
		// Fresh order: NULL order_no / quote / last_info must scan cleanly.
		o, err := r.GetPayoutOrder(ctx, uid, orderIDs[0])
		if err != nil || o.OrderNo != nil || o.Quote != nil || o.LastInfo != nil || o.Amount != "68.06" {
			t.Fatalf("fresh order: %+v err=%v", o, err)
		}
		if list, _, err := r.ListPayoutOrders(ctx, uid, 0, 10); err != nil || len(list) != 0 {
			t.Fatalf("orders without order_no must be hidden: %d err=%v", len(list), err)
		}
		for i, id := range orderIDs {
			if err := r.SetPayoutOrderCreated(ctx, id, fmt.Sprintf("PO_test_%s_%d", tag, i)); err != nil {
				t.Fatal(err)
			}
		}

		page1, more, err := r.ListPayoutOrders(ctx, uid, 0, 2)
		if err != nil || !more || len(page1) != 2 || page1[0].ID != orderIDs[2] || page1[1].ID != orderIDs[1] {
			t.Fatalf("page 1: more=%v err=%v %+v", more, err, page1)
		}
		page2, more, err := r.ListPayoutOrders(ctx, uid, page1[1].ID, 2)
		if err != nil || more || len(page2) != 1 || page2[0].ID != orderIDs[0] {
			t.Fatalf("page 2: more=%v err=%v %+v", more, err, page2)
		}

		if err := r.SetPayoutOrderError(ctx, orderIDs[0], "quote failed"); err != nil {
			t.Fatal(err)
		}
		if err := r.SetPayoutOrderConfirmed(ctx, orderIDs[0], []byte(`{"debitAmount":"68.06"}`)); err != nil {
			t.Fatal(err)
		}
		if err := r.SetPayoutOrderInfo(ctx, orderIDs[0], []byte(`{"status":3}`)); err != nil {
			t.Fatal(err)
		}
		o, err = r.GetPayoutOrderByOrderNo(ctx, "PO_test_"+tag+"_0")
		if err != nil || o.ID != orderIDs[0] || o.LastError != nil || o.ConfirmedAt == nil || o.SyncedAt == nil ||
			len(o.Quote) == 0 || len(o.LastInfo) == 0 {
			t.Fatalf("confirmed order: %+v err=%v", o, err)
		}
		if _, err := r.GetPayoutOrderByOrderNo(ctx, "PO_missing_"+tag); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing order: %v", err)
		}
	})

	t.Run("status events", func(t *testing.T) {
		id := orderIDs[0]
		t0 := time.Now().Add(-time.Minute)
		steps := []struct {
			status  int
			changed bool
		}{{2, true}, {2, false}, {3, true}, {2, true}}
		for i, s := range steps {
			changed, err := r.ApplyPayoutOrderStatus(ctx, id, s.status, t0.Add(time.Duration(i)*time.Second), "poll")
			if err != nil || changed != s.changed {
				t.Fatalf("step %d status %d: changed=%v err=%v", i, s.status, changed, err)
			}
		}
		// UNIQUE(order_id,status): a status seen again keeps its first observation.
		events, err := r.ListPayoutOrderEvents(ctx, id)
		if err != nil || len(events) != 2 || events[0].Status != 2 || events[1].Status != 3 {
			t.Fatalf("events: %+v err=%v", events, err)
		}

		if _, err := r.ApplyPayoutOrderStatus(ctx, id, 3, time.Now(), "webhook"); err != nil {
			t.Fatal(err)
		}
		sync, err := r.ListPayoutOrdersToSync(ctx, 1000)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range sync {
			found = found || o.ID == id
		}
		if !found {
			t.Fatalf("status-3 order %d missing from sync list (%d rows)", id, len(sync))
		}
	})

	t.Run("webhook dedupe", func(t *testing.T) {
		logID, err = r.SaveRawWebhook(ctx, []byte(`{}`), `{}`, "upay-busi-payout")
		if err != nil {
			t.Fatal(err)
		}
		pushedAt := time.Now().Add(-2 * time.Minute)
		if ok, err := r.InsertPayoutWebhookEvent(ctx, notifyNo, logID, "PO_test_"+tag+"_0", 3, pushedAt); err != nil || !ok {
			t.Fatalf("first insert: ok=%v err=%v", ok, err)
		}
		if ok, err := r.InsertPayoutWebhookEvent(ctx, notifyNo, logID, "PO_test_"+tag+"_0", 3, pushedAt); err != nil || ok {
			t.Fatalf("duplicate insert: ok=%v err=%v", ok, err)
		}

		pending := func(olderThan time.Duration) bool {
			t.Helper()
			list, err := r.ListUnprocessedPayoutWebhooks(ctx, olderThan, 1000)
			if err != nil {
				t.Fatalf("list unprocessed: %v", err)
			}
			for _, p := range list {
				if p.NotifyNo == notifyNo {
					return true
				}
			}
			return false
		}
		if pending(time.Minute) {
			t.Fatal("just-received push must wait for olderThan")
		}
		if !pending(0) {
			t.Fatal("unprocessed push missing")
		}
		if err := r.MarkPayoutWebhookProcessed(ctx, notifyNo); err != nil {
			t.Fatal(err)
		}
		if pending(0) {
			t.Fatal("processed push still listed")
		}
	})
}
