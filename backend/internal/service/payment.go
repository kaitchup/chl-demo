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

// action is the local consequence of a UPay order state.
type action int

const (
	actWait     action = iota // stay PENDING, record partial arrival if any
	actActivate               // fully paid — activate membership
	actTerminal               // terminal without activation
)

// classify maps a UPay order (2026-05-04 doc) to a local action.
// Fulfillment follows payment_status (PAID/OVERPAID both count as paid in full);
// status COMPLETED/PAID is the fallback for responses predating payment_status.
// Expiry is not a status: an "expired" local order can still settle on-chain,
// so anything non-terminal keeps polling.
func classify(o *upay.Order) (act action, reason string) {
	paid := o.PaymentStatus == "PAID" || o.PaymentStatus == "OVERPAID"
	switch o.Status {
	case "COMPLETED", "PAID":
		return actActivate, ""
	case "CLOSED": // settlement archive — activate only if it closed fully paid
		if paid {
			return actActivate, ""
		}
		return actTerminal, "closed_unpaid"
	case "CANCELED":
		return actTerminal, o.CancelReason
	case "REJECTED": // payer failed KYC / compliance rejection
		return actTerminal, "kyc_rejected"
	default: // INITED / PENDING_APPROVAL / REJECTING / PROCESSING
		if paid {
			return actActivate, ""
		}
		return actWait, ""
	}
}

func (s *PaymentService) apply(ctx context.Context, o *upay.Order) error {
	// Always persist the KYC / payment_status snapshot first — it is the evidence
	// trail for the uid-vs-KYC scenarios and is filled in regardless of lifecycle.
	if err := s.repo.SaveUpayOrderState(ctx, o.ID, o.PaymentStatus, o.KycStatus,
		o.CustomerKycFirstName, o.CustomerKycLastName); err != nil {
		return err
	}

	act, reason := classify(o)
	switch act {
	case actActivate:
		return s.repo.ActivateMembership(ctx, o.ID, parseTime(o.PaidAt))
	case actTerminal:
		// Local terminal vocabulary is CANCELED (+failure_code); UPay's
		// CLOSED/REJECTED specifics live in the reason.
		return s.repo.MarkOrderTerminal(ctx, o.ID, "CANCELED", reason)
	default:
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
