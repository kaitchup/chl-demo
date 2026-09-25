package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- payout: whitelist + trade password ----

func (r *Repo) IsPayoutEnabled(ctx context.Context, userID int64) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT payout_enabled FROM users WHERE id=$1`, userID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

type TradePassword struct {
	Hash        *string
	FailCount   int
	LockedUntil *time.Time
}

func (r *Repo) GetTradePassword(ctx context.Context, userID int64) (TradePassword, error) {
	var tp TradePassword
	err := r.pool.QueryRow(ctx,
		`SELECT trade_password_hash, trade_password_fail_count, trade_password_locked_until FROM users WHERE id=$1`,
		userID).Scan(&tp.Hash, &tp.FailCount, &tp.LockedUntil)
	return tp, err
}

// SetTradePasswordIfUnset stores the hash only when none exists; returns
// false if a password was already set.
func (r *Repo) SetTradePasswordIfUnset(ctx context.Context, userID int64, hash string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET trade_password_hash=$2, trade_password_fail_count=0, trade_password_locked_until=NULL
		 WHERE id=$1 AND trade_password_hash IS NULL`, userID, hash)
	return tag.RowsAffected() == 1, err
}

// RecordTradePasswordFailure increments the counter; on reaching maxFails it
// locks until lockUntil and resets the counter. Returns the failures so far
// (0 means this failure triggered the lock).
func (r *Repo) RecordTradePasswordFailure(ctx context.Context, userID int64, maxFails int, lockUntil time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		UPDATE users SET
		  trade_password_locked_until = CASE WHEN trade_password_fail_count + 1 >= $2 THEN $3 ELSE trade_password_locked_until END,
		  trade_password_fail_count   = CASE WHEN trade_password_fail_count + 1 >= $2 THEN 0 ELSE trade_password_fail_count + 1 END
		WHERE id=$1 RETURNING trade_password_fail_count`,
		userID, maxFails, lockUntil).Scan(&n)
	return n, err
}

func (r *Repo) ResetTradePasswordFailures(ctx context.Context, userID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET trade_password_fail_count=0, trade_password_locked_until=NULL WHERE id=$1`, userID)
	return err
}

// ---- payout: payers ----

type PayoutPayer struct {
	UserID    int64
	PayerNo   string
	Name      string
	CreatedAt time.Time
}

func (r *Repo) GetPayoutPayer(ctx context.Context, userID int64) (PayoutPayer, error) {
	var p PayoutPayer
	err := r.pool.QueryRow(ctx,
		`SELECT user_id, payer_no, name, created_at FROM payout_payers WHERE user_id=$1`, userID,
	).Scan(&p.UserID, &p.PayerNo, &p.Name, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (r *Repo) CreatePayoutPayer(ctx context.Context, userID int64, payerNo, name string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO payout_payers(user_id, payer_no, name) VALUES($1,$2,$3)`, userID, payerNo, name)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// ---- payout: recipients ----

type PayoutRecipient struct {
	ID            int64
	UserID        int64
	GivenName     string
	FamilyName    string
	BeneficiaryNo *string
	BankAccountNo *string
	BankName      string
	SwiftCode     string
	AccountLast4  string
	Currency      string
	BankCountry   string
	LastError     *string
	CreatedAt     time.Time
}

func (p PayoutRecipient) Name() string { return p.GivenName + " " + p.FamilyName }

// Complete reports whether both UPay steps succeeded.
func (p PayoutRecipient) Complete() bool { return p.BeneficiaryNo != nil && p.BankAccountNo != nil }

const recipientCols = `id, user_id, given_name, family_name, beneficiary_no, bank_account_no, bank_name,
	swift_code, account_last4, currency, bank_country, last_error, created_at`

func scanRecipient(row pgx.Row) (PayoutRecipient, error) {
	var p PayoutRecipient
	err := row.Scan(&p.ID, &p.UserID, &p.GivenName, &p.FamilyName, &p.BeneficiaryNo, &p.BankAccountNo,
		&p.BankName, &p.SwiftCode, &p.AccountLast4, &p.Currency, &p.BankCountry, &p.LastError, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (r *Repo) CreatePayoutRecipient(ctx context.Context, p PayoutRecipient) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO payout_recipients(user_id, given_name, family_name, bank_name, swift_code, account_last4, currency, bank_country)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		p.UserID, p.GivenName, p.FamilyName, p.BankName, p.SwiftCode, p.AccountLast4, p.Currency, p.BankCountry,
	).Scan(&id)
	return id, err
}

func (r *Repo) SetRecipientBeneficiary(ctx context.Context, id int64, beneficiaryNo string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE payout_recipients SET beneficiary_no=$2, last_error=NULL WHERE id=$1`, id, beneficiaryNo)
	return err
}

// SetRecipientBank records step 2; bank fields are refreshed because a retry
// resubmits the bank section.
func (r *Repo) SetRecipientBank(ctx context.Context, p PayoutRecipient) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE payout_recipients SET bank_account_no=$2, bank_name=$3, swift_code=$4, account_last4=$5,
		  bank_country=$6, last_error=NULL WHERE id=$1`,
		p.ID, p.BankAccountNo, p.BankName, p.SwiftCode, p.AccountLast4, p.BankCountry)
	return err
}

func (r *Repo) SetRecipientError(ctx context.Context, id int64, msg string) error {
	_, err := r.pool.Exec(ctx, `UPDATE payout_recipients SET last_error=$2 WHERE id=$1`, id, msg)
	return err
}

func (r *Repo) GetPayoutRecipient(ctx context.Context, userID, id int64) (PayoutRecipient, error) {
	return scanRecipient(r.pool.QueryRow(ctx,
		`SELECT `+recipientCols+` FROM payout_recipients WHERE id=$1 AND user_id=$2`, id, userID))
}

// ListPayoutRecipients hides records whose first UPay step never succeeded.
func (r *Repo) ListPayoutRecipients(ctx context.Context, userID int64) ([]PayoutRecipient, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+recipientCols+` FROM payout_recipients
		 WHERE user_id=$1 AND beneficiary_no IS NOT NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayoutRecipient
	for rows.Next() {
		p, err := scanRecipient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- payout: orders ----

type PayoutOrder struct {
	ID           int64
	UserID       int64
	RecipientID  int64
	ThirdOrderNo string
	OrderNo      *string
	Amount       string
	Currency     string
	DebitCoin    string
	Usage        string
	Note         string
	Status       int
	Quote        json.RawMessage
	LastInfo     json.RawMessage
	LastError    *string
	ConfirmedAt  *time.Time
	SyncedAt     *time.Time
	CreatedAt    time.Time
}

const orderCols = `id, user_id, recipient_id, third_order_no, order_no, amount, currency, debit_coin, usage, note,
	status, quote, last_info, last_error, confirmed_at, synced_at, created_at`

func scanPayoutOrder(row pgx.Row) (PayoutOrder, error) {
	var o PayoutOrder
	err := row.Scan(&o.ID, &o.UserID, &o.RecipientID, &o.ThirdOrderNo, &o.OrderNo, &o.Amount, &o.Currency,
		&o.DebitCoin, &o.Usage, &o.Note, &o.Status, &o.Quote, &o.LastInfo, &o.LastError, &o.ConfirmedAt,
		&o.SyncedAt, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (r *Repo) CreatePayoutOrder(ctx context.Context, o PayoutOrder) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO payout_orders(user_id, recipient_id, third_order_no, amount, currency, debit_coin, usage, note)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		o.UserID, o.RecipientID, o.ThirdOrderNo, o.Amount, o.Currency, o.DebitCoin, o.Usage, o.Note,
	).Scan(&id)
	return id, err
}

func (r *Repo) SetPayoutOrderCreated(ctx context.Context, id int64, orderNo string) error {
	_, err := r.pool.Exec(ctx, `UPDATE payout_orders SET order_no=$2 WHERE id=$1`, id, orderNo)
	return err
}

func (r *Repo) SetPayoutOrderError(ctx context.Context, id int64, msg string) error {
	_, err := r.pool.Exec(ctx, `UPDATE payout_orders SET last_error=$2 WHERE id=$1`, id, msg)
	return err
}

func (r *Repo) SetPayoutOrderConfirmed(ctx context.Context, id int64, quote []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE payout_orders SET quote=$2, confirmed_at=now(), last_error=NULL WHERE id=$1`, id, quote)
	return err
}

func (r *Repo) SetPayoutOrderInfo(ctx context.Context, id int64, info []byte) error {
	_, err := r.pool.Exec(ctx, `UPDATE payout_orders SET last_info=$2, synced_at=now() WHERE id=$1`, id, info)
	return err
}

// ApplyPayoutOrderStatus sets the status and records the first time it was
// observed. Returns true if the status changed.
func (r *Repo) ApplyPayoutOrderStatus(ctx context.Context, id int64, status int, observedAt time.Time, source string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`UPDATE payout_orders SET status=$2 WHERE id=$1 AND status <> $2`, id, status)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payout_order_events(order_id, status, observed_at, source) VALUES($1,$2,$3,$4)
		ON CONFLICT (order_id, status) DO NOTHING`, id, status, observedAt, source); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, tx.Commit(ctx)
}

func (r *Repo) GetPayoutOrder(ctx context.Context, userID, id int64) (PayoutOrder, error) {
	return scanPayoutOrder(r.pool.QueryRow(ctx,
		`SELECT `+orderCols+` FROM payout_orders WHERE id=$1 AND user_id=$2`, id, userID))
}

func (r *Repo) GetPayoutOrderByOrderNo(ctx context.Context, orderNo string) (PayoutOrder, error) {
	return scanPayoutOrder(r.pool.QueryRow(ctx,
		`SELECT `+orderCols+` FROM payout_orders WHERE order_no=$1`, orderNo))
}

// ListPayoutOrders pages by id DESC; beforeID=0 starts from the newest.
func (r *Repo) ListPayoutOrders(ctx context.Context, userID, beforeID int64, limit int) ([]PayoutOrder, bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+orderCols+` FROM payout_orders
		 WHERE user_id=$1 AND ($2 = 0 OR id < $2) AND order_no IS NOT NULL
		 ORDER BY id DESC LIMIT $3`, userID, beforeID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []PayoutOrder
	for rows.Next() {
		o, err := scanPayoutOrder(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, o)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, rows.Err()
}

// ListPayoutOrdersToSync returns confirmed, non-terminal orders from the last
// 7 days — the only ones the background job polls.
func (r *Repo) ListPayoutOrdersToSync(ctx context.Context, limit int) ([]PayoutOrder, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+orderCols+` FROM payout_orders
		 WHERE status IN (3, 5, 6, 10) AND order_no IS NOT NULL AND created_at > now() - interval '7 days'
		 ORDER BY synced_at NULLS FIRST LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayoutOrder
	for rows.Next() {
		o, err := scanPayoutOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type PayoutOrderEvent struct {
	Status     int
	ObservedAt time.Time
	Source     string
}

func (r *Repo) ListPayoutOrderEvents(ctx context.Context, orderID int64) ([]PayoutOrderEvent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT status, observed_at, source FROM payout_order_events WHERE order_id=$1 ORDER BY observed_at, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayoutOrderEvent
	for rows.Next() {
		var e PayoutOrderEvent
		if err := rows.Scan(&e.Status, &e.ObservedAt, &e.Source); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- payout: webhook dedupe ----

// InsertPayoutWebhookEvent returns false when notifyNo was already recorded.
func (r *Repo) InsertPayoutWebhookEvent(ctx context.Context, notifyNo string, rawLogID int64, orderNo string, status int, pushedAt time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO payout_webhook_events(notify_no, raw_log_id, order_no, status, pushed_at)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT (notify_no) DO NOTHING`,
		notifyNo, rawLogID, orderNo, status, pushedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repo) MarkPayoutWebhookProcessed(ctx context.Context, notifyNo string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE payout_webhook_events SET processed_at=now() WHERE notify_no=$1`, notifyNo)
	return err
}

type PendingPayoutWebhook struct {
	NotifyNo string
	OrderNo  string
	PushedAt time.Time
}

// ListUnprocessedPayoutWebhooks returns pushes whose async sync never finished.
func (r *Repo) ListUnprocessedPayoutWebhooks(ctx context.Context, olderThan time.Duration, limit int) ([]PendingPayoutWebhook, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT notify_no, COALESCE(order_no, ''), COALESCE(pushed_at, received_at) FROM payout_webhook_events
		WHERE processed_at IS NULL AND received_at < now() - make_interval(secs => $1)
		ORDER BY received_at LIMIT $2`, olderThan.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingPayoutWebhook
	for rows.Next() {
		var p PendingPayoutWebhook
		if err := rows.Scan(&p.NotifyNo, &p.OrderNo, &p.PushedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
