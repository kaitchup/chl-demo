package job

import (
	"context"
	"log"
	"time"

	"chldemo/internal/repo"
	"chldemo/internal/service"
)

// PayoutSync is the safety net behind the webhook: UPay stops pushing after
// 5 failed retries, so confirmed non-terminal orders are polled, and pushes
// whose async processing never finished are replayed.
type PayoutSync struct {
	repo     *repo.Repo
	payout   *service.PayoutService
	interval time.Duration
}

func NewPayoutSync(r *repo.Repo, p *service.PayoutService, interval time.Duration) *PayoutSync {
	return &PayoutSync{repo: r, payout: p, interval: interval}
}

func (j *PayoutSync) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	log.Printf("payout sync job started (every %s)", j.interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.tick(ctx)
		}
	}
}

const payoutSyncBatch = 50

func (j *PayoutSync) tick(ctx context.Context) {
	orders, err := j.repo.ListPayoutOrdersToSync(ctx, payoutSyncBatch)
	if err != nil {
		log.Printf("payout sync: list orders: %v", err)
		return
	}
	for _, o := range orders {
		if err := j.payout.SyncOrder(ctx, o, time.Now(), "poll"); err != nil {
			log.Printf("payout sync: order %s: %v", o.ThirdOrderNo, err)
		}
		time.Sleep(200 * time.Millisecond) // stay clear of UPay's 900 rate limit
	}

	pending, err := j.repo.ListUnprocessedPayoutWebhooks(ctx, time.Minute, payoutSyncBatch)
	if err != nil {
		log.Printf("payout sync: list webhooks: %v", err)
		return
	}
	for _, p := range pending {
		if err := j.payout.SyncByOrderNo(ctx, p.OrderNo, p.PushedAt, "webhook"); err != nil {
			log.Printf("payout sync: replay webhook %s: %v", p.NotifyNo, err)
			continue
		}
		_ = j.repo.MarkPayoutWebhookProcessed(ctx, p.NotifyNo)
	}
}
