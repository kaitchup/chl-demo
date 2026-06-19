package job

import (
	"context"
	"log"
	"time"

	"chldemo/internal/admin"
	"chldemo/internal/repo"
)

// AMLPoller polls pending RISK simulations and auto-approves their AML tickets.
// It runs until ctx is cancelled.
type AMLPoller struct {
	repo     *repo.Repo
	admin    *admin.Client
	interval time.Duration
}

func NewAMLPoller(r *repo.Repo, ac *admin.Client, interval time.Duration) *AMLPoller {
	return &AMLPoller{repo: r, admin: ac, interval: interval}
}

func (p *AMLPoller) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	log.Printf("aml poller started (every %s)", p.interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *AMLPoller) tick(ctx context.Context) {
	sims, err := p.repo.ListPendingSimulations(ctx)
	if err != nil {
		log.Printf("aml poller: list pending: %v", err)
		return
	}

	for _, s := range sims {
		p.process(ctx, s)
	}
}

func (p *AMLPoller) process(ctx context.Context, s repo.Simulation) {
	if s.ToAddress == nil || *s.ToAddress == "" {
		_ = p.repo.MarkSimulationFailed(ctx, s.ID, "missing to_address for RISK simulation")
		return
	}

	// Consume one retry slot for this tick.
	if err := p.repo.IncrSimulationRetry(ctx, s.ID); err != nil {
		log.Printf("aml poller: incr retry sim %d: %v", s.ID, err)
		return
	}

	// Step 1: query deposits by to_address → discover inspection_ids.
	deposits, err := p.admin.ListDeposits(*s.ToAddress)
	if err != nil {
		log.Printf("aml poller: list deposits sim %d: %v", s.ID, err)
		return
	}

	for _, dep := range deposits {
		if dep.InspectionID == "" {
			continue
		}
		if err := p.repo.UpsertAMLTicket(ctx, s.ID, dep.InspectionID); err != nil {
			log.Printf("aml poller: upsert ticket sim %d inspection %s: %v", s.ID, dep.InspectionID, err)
		}
	}

	// Step 2: for each ticket without a UUID, query the AML API.
	tickets, err := p.repo.ListAMLTickets(ctx, s.ID)
	if err != nil {
		log.Printf("aml poller: list tickets sim %d: %v", s.ID, err)
		return
	}

	if len(tickets) == 0 {
		// No deposits/tickets discovered yet — keep SIMULATING, wait for next tick.
		return
	}

	// We have at least one ticket; transition to AML_PENDING if still in SIMULATING.
	if s.Status == "SIMULATING" {
		_ = p.repo.SetSimulationStatus(ctx, s.ID, "AML_PENDING")
	}

	for _, t := range tickets {
		if t.TicketUUID == nil {
			amlTicket, err := p.admin.GetAMLTicketByInspectionID(t.InspectionID)
			if err != nil {
				log.Printf("aml poller: get ticket sim %d inspection %s: %v", s.ID, t.InspectionID, err)
				continue
			}
			if amlTicket == nil {
				continue // not yet available
			}
			if err := p.repo.SetAMLTicketUUID(ctx, t.InspectionID, amlTicket.TicketNo); err != nil {
				log.Printf("aml poller: set uuid sim %d: %v", s.ID, err)
				continue
			}
			t.TicketUUID = &amlTicket.TicketNo
		}

		// Step 3: submit review for unapproved tickets.
		if t.ApprovedAt != nil {
			continue
		}
		if err := p.admin.SubmitAMLReview(*t.TicketUUID); err != nil {
			log.Printf("aml poller: submit review sim %d ticket %s: %v", s.ID, *t.TicketUUID, err)
			continue
		}
		if err := p.repo.MarkAMLTicketApproved(ctx, t.InspectionID); err != nil {
			log.Printf("aml poller: mark approved sim %d: %v", s.ID, err)
		}
	}

	// Step 4: check if all tickets are approved → advance status.
	allDone, err := p.repo.AllAMLTicketsApproved(ctx, s.ID)
	if err != nil {
		log.Printf("aml poller: check all approved sim %d: %v", s.ID, err)
		return
	}
	if allDone {
		_ = p.repo.SetSimulationStatus(ctx, s.ID, "AML_APPROVED")
		log.Printf("aml poller: sim %d all AML tickets approved", s.ID)
	}
}
