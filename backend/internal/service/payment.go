package service

import (
	"context"
	"time"

	"chldemo/internal/repo"
	"chldemo/internal/upay"
)

// PaymentService coordinates UPay lookups with local order/subscription state.
type PaymentService struct {
	upay *upay.Client
	repo *repo.Repo
}

func NewPayment(c *upay.Client, r *repo.Repo) *PaymentService {
	return &PaymentService{upay: c, repo: r}
}

// ConfirmAndActivate fetches the authoritative order state from UPay and, if it
// is fully paid, activates membership. Otherwise it syncs partial/terminal state.
// Shared by the webhook handler and the reconcile job; idempotent.
func (s *PaymentService) ConfirmAndActivate(ctx context.Context, payID string) error {
	o, err := s.upay.GetOrder(payID)
	if err != nil {
		return err
	}
	return s.apply(ctx, o)
}

// Sync aligns local state with an already-fetched order (used by reconcile).
func (s *PaymentService) Sync(ctx context.Context, o *upay.Order) error {
	return s.apply(ctx, o)
}

func (s *PaymentService) apply(ctx context.Context, o *upay.Order) error {
	// UPay lifecycle status (per gateway docs): INITED | PROCESSING | SUCCEEDED | CANCELED.
	// Funding/refund progress is NOT carried by status — judge it from received_amount.
	// Expiry is no longer a status: an "expired" local order can still settle on-chain,
	// so we keep polling until UPay reports SUCCEEDED or CANCELED.
	switch o.Status {
	case "SUCCEEDED":
		return s.repo.ActivateMembership(ctx, o.ID, parseTime(o.PaidAt))
	case "CANCELED":
		return s.repo.MarkOrderTerminal(ctx, o.ID, "CANCELED", o.CancelReason)
	default: // INITED / PROCESSING — record any partial arrival, stay PENDING
		if o.ReceivedAmount != "" {
			return s.repo.UpdateReceived(ctx, o.ID, o.ReceivedAmount)
		}
		return nil
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now()
}
