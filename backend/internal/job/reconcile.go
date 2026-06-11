package job

import (
	"context"
	"errors"
	"log"
	"time"

	"chldemo/internal/repo"
	"chldemo/internal/service"
	"chldemo/internal/upay"
)

// Reconciler polls UPay for PENDING orders. It is the *only* way EXPIRED/FAILED
// orders are discovered (UPay only webhooks on chain arrivals), and it also
// repairs any COMPLETED order that missed its webhook.
type Reconciler struct {
	repo     *repo.Repo
	upay     *upay.Client
	payment  *service.PaymentService
	interval time.Duration
}

func NewReconciler(r *repo.Repo, c *upay.Client, p *service.PaymentService, interval time.Duration) *Reconciler {
	return &Reconciler{repo: r, upay: c, payment: p, interval: interval}
}

func (j *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	log.Printf("reconcile job started (every %s)", j.interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.tick(ctx)
		}
	}
}

func (j *Reconciler) tick(ctx context.Context) {
	j.tickOrders(ctx)
	j.tickRefunds(ctx)
}

func (j *Reconciler) tickOrders(ctx context.Context) {
	pending, err := j.repo.ListPendingOrders(ctx)
	if err != nil {
		log.Printf("reconcile: list pending: %v", err)
		return
	}
	for _, o := range pending {
		if o.UpayPaymentID == nil || *o.UpayPaymentID == "" {
			continue
		}
		up, err := j.upay.GetOrder(*o.UpayPaymentID)
		if err != nil {
			log.Printf("reconcile: get %s: %v", *o.UpayPaymentID, err)
			var apiErr *upay.APIError
			if errors.As(err, &apiErr) {
				if dbErr := j.repo.MarkOrderExpiredWithError(ctx, *o.UpayPaymentID, apiErr.StatusCode, apiErr.Body); dbErr != nil {
					log.Printf("reconcile: mark expired %s: %v", *o.UpayPaymentID, dbErr)
				}
			}
			continue
		}
		if err := j.payment.Sync(ctx, up); err != nil {
			log.Printf("reconcile: sync %s: %v", *o.UpayPaymentID, err)
		}
	}
}

func (j *Reconciler) tickRefunds(ctx context.Context) {
	pending, err := j.repo.ListPendingRefunds(ctx)
	if err != nil {
		log.Printf("reconcile: list pending refunds: %v", err)
		return
	}
	for _, rf := range pending {
		if rf.UpayRefundID == nil {
			continue
		}
		up, err := j.upay.GetRefund(*rf.UpayRefundID)
		if err != nil {
			log.Printf("reconcile: get refund %s: %v", *rf.UpayRefundID, err)
			continue
		}
		switch up.Status {
		case "CONFIRMED":
			if err := j.repo.ConfirmRefund(ctx, *rf.UpayRefundID); err != nil {
				log.Printf("reconcile: confirm refund %s: %v", *rf.UpayRefundID, err)
			}
		case "FAILED":
			if err := j.repo.MarkRefundFailed(ctx, *rf.UpayRefundID); err != nil {
				log.Printf("reconcile: fail refund %s: %v", *rf.UpayRefundID, err)
			}
		}
	}
}
