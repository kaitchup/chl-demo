package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrConflict indicates a unique constraint violation (e.g. duplicate email).
var ErrConflict = errors.New("conflict")

// ErrNotFound indicates no row matched.
var ErrNotFound = errors.New("not found")

type Repo struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

type Plan struct {
	Code         string
	Name         string
	DurationDays int
	Amount       string // numeric rendered as text to avoid float
	Currency     string
}

type Subscription struct {
	Tier      string
	ExpiresAt *time.Time
}

type Order struct {
	MerchantOrderID string
	UpayPaymentID   *string
	PlanCode        string
	Amount          string
	Currency        string
	Status          string
	ReceivedAmount  string
	FailureCode     *string
	CheckoutURL     *string
	CreatedAt       time.Time
	PaidAt          *time.Time
	ExpiresAt       *time.Time
}

type Refund struct {
	ID               int64
	MerchantRefundID string
	UpayRefundID     *string
	MerchantOrderID  string
	UserID           int64
	Amount           string
	Coin             string
	Chain            string
	ToAddress        string
	Status           string
	Reason           string
	CreatedAt        time.Time
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ---- users ----

func (r *Repo) CreateUser(ctx context.Context, email, hash string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users(email, password_hash) VALUES($1,$2) RETURNING id`,
		email, hash,
	).Scan(&id)
	if isUnique(err) {
		return 0, ErrConflict
	}
	return id, err
}

func (r *Repo) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (r *Repo) GetUserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// ---- plans ----

func (r *Repo) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT code, name, duration_days, amount::text, currency
		   FROM membership_plans WHERE active ORDER BY duration_days`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.Code, &p.Name, &p.DurationDays, &p.Amount, &p.Currency); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) GetPlan(ctx context.Context, code string) (Plan, error) {
	var p Plan
	err := r.pool.QueryRow(ctx,
		`SELECT code, name, duration_days, amount::text, currency
		   FROM membership_plans WHERE code=$1 AND active`, code,
	).Scan(&p.Code, &p.Name, &p.DurationDays, &p.Amount, &p.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ---- subscriptions ----

// GetSubscription returns the user's subscription. If none exists it returns a
// zero-value Subscription with found=false (treated as no membership).
func (r *Repo) GetSubscription(ctx context.Context, userID int64) (Subscription, bool, error) {
	var s Subscription
	err := r.pool.QueryRow(ctx,
		`SELECT tier, expires_at FROM subscriptions WHERE user_id=$1`, userID,
	).Scan(&s.Tier, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, false, nil
	}
	if err != nil {
		return s, false, err
	}
	return s, true, nil
}

// ---- orders ----

func (r *Repo) CreateOrder(ctx context.Context, o Order, userID int64, idemKey string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO orders(merchant_order_id, upay_payment_id, user_id, plan_code,
		                    amount, currency, status, checkout_url, idempotency_key, expires_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		o.MerchantOrderID, o.UpayPaymentID, userID, o.PlanCode,
		o.Amount, o.Currency, o.Status, o.CheckoutURL, idemKey, o.ExpiresAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func (r *Repo) ListOrders(ctx context.Context, userID int64) ([]Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT merchant_order_id, upay_payment_id, plan_code, amount::text, currency,
		        status, received_amount::text, failure_code, checkout_url,
		        created_at, paid_at, expires_at
		   FROM orders WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *Repo) GetOrder(ctx context.Context, userID int64, moid string) (Order, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT merchant_order_id, upay_payment_id, plan_code, amount::text, currency,
		        status, received_amount::text, failure_code, checkout_url,
		        created_at, paid_at, expires_at
		   FROM orders WHERE user_id=$1 AND merchant_order_id=$2`, userID, moid)
	o, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOrder(s scanner) (Order, error) {
	var o Order
	err := s.Scan(&o.MerchantOrderID, &o.UpayPaymentID, &o.PlanCode, &o.Amount, &o.Currency,
		&o.Status, &o.ReceivedAmount, &o.FailureCode, &o.CheckoutURL,
		&o.CreatedAt, &o.PaidAt, &o.ExpiresAt)
	return o, err
}

// ---- v0.0.2: UPay integration ----

// SaveUpayBodies stores the raw request and response JSON sent to / received from UPay.
// Called after CreateOrder regardless of success; req/resp may be nil if the call
// never reached the wire.
func (r *Repo) SaveUpayBodies(ctx context.Context, moid string, req, resp []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE orders SET upay_req_body=$2, upay_resp_body=$3 WHERE merchant_order_id=$1`,
		moid, req, resp)
	return err
}

// AttachUpayResult stores the UPay order id / checkout url / expiry on a local order.
func (r *Repo) AttachUpayResult(ctx context.Context, moid, payID, checkoutURL string, expiresAt *time.Time) error {
	// NULLIF keeps placeholder-mode orders (empty payID) out of the reconcile set,
	// which filters on upay_payment_id IS NOT NULL.
	_, err := r.pool.Exec(ctx,
		`UPDATE orders SET upay_payment_id=NULLIF($2,''), checkout_url=$3, expires_at=$4
		   WHERE merchant_order_id=$1`,
		moid, payID, checkoutURL, expiresAt)
	return err
}

// ReuseOpenOrder returns the user's most recent reusable PENDING order (not expired),
// or ErrNotFound. Used to avoid creating duplicate UPay orders.
func (r *Repo) ReuseOpenOrder(ctx context.Context, userID int64) (Order, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT merchant_order_id, upay_payment_id, plan_code, amount::text, currency,
		        status, received_amount::text, failure_code, checkout_url,
		        created_at, paid_at, expires_at
		   FROM orders
		  WHERE user_id=$1 AND status='PENDING' AND upay_payment_id IS NOT NULL
		        AND expires_at > now()
		  ORDER BY created_at DESC LIMIT 1`, userID)
	o, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

// ListPendingOrders returns PENDING orders that have a UPay id (for reconcile).
func (r *Repo) ListPendingOrders(ctx context.Context) ([]Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT merchant_order_id, upay_payment_id, plan_code, amount::text, currency,
		        status, received_amount::text, failure_code, checkout_url,
		        created_at, paid_at, expires_at
		   FROM orders WHERE status='PENDING' AND upay_payment_id IS NOT NULL AND upay_payment_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SaveRawWebhook persists raw request headers and body before any processing.
// Returns the generated log id, which should be threaded into InsertWebhookEvent.
func (r *Repo) SaveRawWebhook(ctx context.Context, headers, rawBody []byte) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO webhook_raw_log(req_headers, raw_body) VALUES($1, $2) RETURNING id`,
		headers, rawBody,
	).Scan(&id)
	return id, err
}

// InsertWebhookEvent records an event; returns inserted=false if the event id was
// already seen (idempotent dedupe via UNIQUE(upay_event_id)).
func (r *Repo) InsertWebhookEvent(ctx context.Context, eventID, payID, eventType string, payload []byte, rawLogID int64) (inserted bool, err error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO webhook_events(upay_event_id, upay_payment_id, event_type, payload, signature_valid, raw_log_id)
		 VALUES($1,$2,$3,$4,true,$5) ON CONFLICT (upay_event_id) DO NOTHING`,
		eventID, payID, eventType, payload, rawLogID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// MarkWebhookProcessed flips processed=true once the event has been fully handled.
func (r *Repo) MarkWebhookProcessed(ctx context.Context, eventID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE webhook_events SET processed=true WHERE upay_event_id=$1`, eventID)
	return err
}

// MarkOrderExpiredWithError transitions a PENDING order to EXPIRED and records
// the HTTP status code and raw response body from the failed UPay reconcile call.
func (r *Repo) MarkOrderExpiredWithError(ctx context.Context, payID string, code int, body []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE orders
		    SET status='EXPIRED', reconcile_err_code=$2, reconcile_err_body=$3
		  WHERE upay_payment_id=$1 AND status='PENDING'`,
		payID, code, body)
	return err
}

// MarkOrderTerminal sets a non-paid terminal status (CANCELED) found by reconcile;
// reason carries UPay's cancel_reason (stored in failure_code).
func (r *Repo) MarkOrderTerminal(ctx context.Context, payID, status, reason string) error {
	var fc *string
	if reason != "" {
		fc = &reason
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE orders SET status=$2, failure_code=$3
		   WHERE upay_payment_id=$1 AND status='PENDING'`,
		payID, status, fc)
	return err
}

// UpdateReceived updates the partially-paid running total.
func (r *Repo) UpdateReceived(ctx context.Context, payID, received string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE orders SET received_amount=$2::numeric WHERE upay_payment_id=$1 AND status='PENDING'`,
		payID, received)
	return err
}

// ActivateMembership marks the order PAID and extends the user's subscription.
// Safe to call repeatedly (idempotent via applied_to_subscription). Per-user
// advisory lock serializes concurrent activations so expiry stacking is correct.
func (r *Repo) ActivateMembership(ctx context.Context, payID string, paidAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		orderID  int64
		userID   int64
		planCode string
		applied  bool
	)
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, plan_code, applied_to_subscription
		   FROM orders WHERE upay_payment_id=$1 FOR UPDATE`, payID,
	).Scan(&orderID, &userID, &planCode, &applied)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if applied {
		return tx.Commit(ctx) // already activated
	}

	// Serialize membership math per user (subscription row may not exist yet).
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userID); err != nil {
		return err
	}

	var durationDays int
	if err = tx.QueryRow(ctx,
		`SELECT duration_days FROM membership_plans WHERE code=$1`, planCode,
	).Scan(&durationDays); err != nil {
		return err
	}

	var curExpires *time.Time
	_ = tx.QueryRow(ctx,
		`SELECT expires_at FROM subscriptions WHERE user_id=$1`, userID,
	).Scan(&curExpires)

	base := paidAt
	if curExpires != nil && curExpires.After(base) {
		base = *curExpires
	}
	newExpiry := base.AddDate(0, 0, durationDays)

	if _, err = tx.Exec(ctx,
		`INSERT INTO subscriptions(user_id, tier, expires_at)
		 VALUES($1,'super',$2)
		 ON CONFLICT (user_id) DO UPDATE SET expires_at=EXCLUDED.expires_at, updated_at=now()`,
		userID, newExpiry); err != nil {
		return err
	}

	if _, err = tx.Exec(ctx,
		`UPDATE orders
		    SET status='PAID', applied_to_subscription=true, paid_at=$2,
		        received_amount=amount
		  WHERE id=$1`,
		orderID, paidAt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---- refunds ----

// CreateRefund inserts a new refund row. order_id is resolved via merchant_order_id join.
func (r *Repo) CreateRefund(ctx context.Context, rf Refund) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refunds(merchant_refund_id, upay_refund_id, order_id, user_id,
		                     amount, coin, chain, to_address, status, reason)
		 SELECT $1, $2, o.id, $3, $4, $5, $6, $7, $8, $9
		   FROM orders o WHERE o.merchant_order_id=$10`,
		rf.MerchantRefundID, rf.UpayRefundID, rf.UserID,
		rf.Amount, rf.Coin, rf.Chain, rf.ToAddress, rf.Status, rf.Reason,
		rf.MerchantOrderID)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func scanRefund(s scanner, rf *Refund) error {
	return s.Scan(&rf.MerchantRefundID, &rf.UpayRefundID, &rf.MerchantOrderID, &rf.UserID,
		&rf.Amount, &rf.Coin, &rf.Chain, &rf.ToAddress, &rf.Status, &rf.Reason, &rf.CreatedAt)
}

const refundCols = `rf.merchant_refund_id, rf.upay_refund_id, o.merchant_order_id, rf.user_id,
                    rf.amount::text, rf.coin, rf.chain, rf.to_address, rf.status, rf.reason, rf.created_at`

// GetRefund returns the refund identified by merchant_refund_id, scoped to the user.
func (r *Repo) GetRefund(ctx context.Context, userID int64, merchantRefundID string) (Refund, error) {
	var rf Refund
	row := r.pool.QueryRow(ctx,
		`SELECT `+refundCols+`
		   FROM refunds rf
		   JOIN orders o ON o.id = rf.order_id
		  WHERE rf.merchant_refund_id=$1 AND rf.user_id=$2`,
		merchantRefundID, userID)
	if err := scanRefund(row, &rf); errors.Is(err, pgx.ErrNoRows) {
		return rf, ErrNotFound
	} else if err != nil {
		return rf, err
	}
	return rf, nil
}

// ListPendingRefunds returns refunds that have a UPay id but are not yet in a
// terminal state (CONFIRMED or FAILED), for the reconcile job to poll.
func (r *Repo) ListPendingRefunds(ctx context.Context) ([]Refund, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+refundCols+`
		   FROM refunds rf
		   JOIN orders o ON o.id = rf.order_id
		  WHERE rf.upay_refund_id IS NOT NULL
		    AND rf.status NOT IN ('CONFIRMED','FAILED')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Refund
	for rows.Next() {
		var rf Refund
		if err := scanRefund(rows, &rf); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}

// ConfirmRefund updates a refund to CONFIRMED and, if this is the first
// confirmed refund on the order, flips the order status to REFUNDED.
func (r *Repo) ConfirmRefund(ctx context.Context, upayRefundID string) error {
	_, err := r.pool.Exec(ctx,
		`WITH updated AS (
		    UPDATE refunds SET status='CONFIRMED'
		     WHERE upay_refund_id=$1 AND status <> 'CONFIRMED'
		 RETURNING order_id
		)
		UPDATE orders SET status='REFUNDED'
		  FROM updated
		 WHERE orders.id = updated.order_id AND orders.status = 'PAID'`,
		upayRefundID)
	return err
}

// MarkRefundFailed updates a refund to FAILED.
func (r *Repo) MarkRefundFailed(ctx context.Context, upayRefundID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refunds SET status='FAILED' WHERE upay_refund_id=$1`, upayRefundID)
	return err
}

// ListRefundsByOrder returns all refunds for a given order, scoped to the user.
func (r *Repo) ListRefundsByOrder(ctx context.Context, userID int64, merchantOrderID string) ([]Refund, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+refundCols+`
		   FROM refunds rf
		   JOIN orders o ON o.id = rf.order_id
		  WHERE o.merchant_order_id=$1 AND rf.user_id=$2
		  ORDER BY rf.created_at DESC`,
		merchantOrderID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Refund
	for rows.Next() {
		var rf Refund
		if err := scanRefund(rows, &rf); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}
