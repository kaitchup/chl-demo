package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- sandbox types ----

type Simulation struct {
	ID                int64
	MerchantID        string
	PaymentRequestID  string
	Scenario          string
	ToAddress         *string
	Amount            string
	Coin              *string
	SimEvents         json.RawMessage
	Status            string
	RetryCount        int
	MaxRetries        int
	ErrorDetail       *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type AMLTicket struct {
	ID           int64
	SimulationID int64
	InspectionID string
	TicketUUID   *string
	ApprovedAt   *time.Time
	CreatedAt    time.Time
}

// ---- simulations ----

func (r *Repo) CreateSimulation(ctx context.Context, s Simulation) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sandbox_simulations
		  (merchant_id, payment_request_id, scenario, to_address, amount, coin, sim_events, max_retries)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id`,
		s.MerchantID, s.PaymentRequestID, s.Scenario, s.ToAddress,
		s.Amount, s.Coin, s.SimEvents, s.MaxRetries,
	).Scan(&id)
	return id, err
}

func (r *Repo) GetSimulation(ctx context.Context, id int64, merchantID string) (Simulation, error) {
	var s Simulation
	err := r.pool.QueryRow(ctx, `
		SELECT id, merchant_id, payment_request_id, scenario, to_address,
		       amount, coin, sim_events, status, retry_count, max_retries,
		       error_detail, created_at, updated_at
		  FROM sandbox_simulations
		 WHERE id=$1 AND merchant_id=$2`,
		id, merchantID,
	).Scan(
		&s.ID, &s.MerchantID, &s.PaymentRequestID, &s.Scenario, &s.ToAddress,
		&s.Amount, &s.Coin, &s.SimEvents, &s.Status, &s.RetryCount, &s.MaxRetries,
		&s.ErrorDetail, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func (r *Repo) ListSimulations(ctx context.Context, merchantID string, limit, offset int) ([]Simulation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, merchant_id, payment_request_id, scenario, to_address,
		       amount, coin, sim_events, status, retry_count, max_retries,
		       error_detail, created_at, updated_at
		  FROM sandbox_simulations
		 WHERE merchant_id=$1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		merchantID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Simulation
	for rows.Next() {
		var s Simulation
		if err := rows.Scan(
			&s.ID, &s.MerchantID, &s.PaymentRequestID, &s.Scenario, &s.ToAddress,
			&s.Amount, &s.Coin, &s.SimEvents, &s.Status, &s.RetryCount, &s.MaxRetries,
			&s.ErrorDetail, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListPendingSimulations returns all RISK simulations in SIMULATING or AML_PENDING state
// that have not yet exceeded max_retries.
func (r *Repo) ListPendingSimulations(ctx context.Context) ([]Simulation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, merchant_id, payment_request_id, scenario, to_address,
		       amount, coin, sim_events, status, retry_count, max_retries,
		       error_detail, created_at, updated_at
		  FROM sandbox_simulations
		 WHERE scenario = 'RISK'
		   AND status IN ('SIMULATING','AML_PENDING')
		   AND retry_count < max_retries
		 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Simulation
	for rows.Next() {
		var s Simulation
		if err := rows.Scan(
			&s.ID, &s.MerchantID, &s.PaymentRequestID, &s.Scenario, &s.ToAddress,
			&s.Amount, &s.Coin, &s.SimEvents, &s.Status, &s.RetryCount, &s.MaxRetries,
			&s.ErrorDetail, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) IncrSimulationRetry(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sandbox_simulations SET retry_count=retry_count+1, updated_at=NOW() WHERE id=$1`, id)
	return err
}

func (r *Repo) SetSimulationStatus(ctx context.Context, id int64, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sandbox_simulations SET status=$1, updated_at=NOW() WHERE id=$2`, status, id)
	return err
}

func (r *Repo) MarkSimulationFailed(ctx context.Context, id int64, detail string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sandbox_simulations SET status='FAILED', error_detail=$1, updated_at=NOW() WHERE id=$2`,
		detail, id)
	return err
}

func (r *Repo) MarkSimulationDone(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sandbox_simulations SET status='DONE', updated_at=NOW() WHERE id=$1`, id)
	return err
}

// ---- AML tickets ----

// UpsertAMLTicket inserts a ticket row for the given inspection_id, or does nothing
// if the row already exists (idempotent discovery).
func (r *Repo) UpsertAMLTicket(ctx context.Context, simulationID int64, inspectionID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO sandbox_aml_tickets (simulation_id, inspection_id)
		VALUES ($1,$2)
		ON CONFLICT (inspection_id) DO NOTHING`,
		simulationID, inspectionID,
	)
	return err
}

// SetAMLTicketUUID fills in the ticket UUID discovered from the AML API.
func (r *Repo) SetAMLTicketUUID(ctx context.Context, inspectionID, uuid string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sandbox_aml_tickets SET ticket_uuid=$1 WHERE inspection_id=$2`, uuid, inspectionID)
	return err
}

// MarkAMLTicketApproved records approval time.
func (r *Repo) MarkAMLTicketApproved(ctx context.Context, inspectionID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sandbox_aml_tickets SET approved_at=NOW() WHERE inspection_id=$1`, inspectionID)
	return err
}

// ListAMLTickets returns all AML tickets for a simulation.
func (r *Repo) ListAMLTickets(ctx context.Context, simulationID int64) ([]AMLTicket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, simulation_id, inspection_id, ticket_uuid, approved_at, created_at
		  FROM sandbox_aml_tickets
		 WHERE simulation_id=$1
		 ORDER BY created_at`,
		simulationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AMLTicket
	for rows.Next() {
		var t AMLTicket
		if err := rows.Scan(
			&t.ID, &t.SimulationID, &t.InspectionID, &t.TicketUUID, &t.ApprovedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AllAMLTicketsApproved returns true when at least one ticket exists and all are approved.
func (r *Repo) AllAMLTicketsApproved(ctx context.Context, simulationID int64) (bool, error) {
	var total, approved int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(approved_at)
		  FROM sandbox_aml_tickets
		 WHERE simulation_id=$1`,
		simulationID,
	).Scan(&total, &approved)
	if err != nil {
		return false, err
	}
	return total > 0 && total == approved, nil
}
