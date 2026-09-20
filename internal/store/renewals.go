package store

import (
	"context"
	"database/sql"
	"errors"
)

// Renewal request statuses.
const (
	RenewalPending   = "pending"
	RenewalConfirmed = "confirmed"
	RenewalRejected  = "rejected"
	RenewalFailed    = "failed"
)

// RenewalRequest is a client's "I have paid" claim and what the app did about it.
type RenewalRequest struct {
	ID           int64
	SubID        string
	Email        string
	PlanID       string
	Days         int
	Amount       int
	Currency     string
	Status       string
	AppliedDays  int
	ExpiryBefore int64
	ExpiryAfter  int64
	CreatedAt    int64
	ResolvedAt   int64
	ResolvedBy   string
	Error        string
}

const renewalColumns = `id, sub_id, email, plan_id, days, amount, currency, status, applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error`

func scanRenewal(sc interface{ Scan(...any) error }) (*RenewalRequest, error) {
	var r RenewalRequest
	err := sc.Scan(&r.ID, &r.SubID, &r.Email, &r.PlanID, &r.Days, &r.Amount, &r.Currency, &r.Status,
		&r.AppliedDays, &r.ExpiryBefore, &r.ExpiryAfter, &r.CreatedAt, &r.ResolvedAt, &r.ResolvedBy, &r.Error)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateRenewal inserts a request and sets r.ID. A second pending request for
// the same subscription returns ErrPendingExists.
func (s *Store) CreateRenewal(ctx context.Context, r *RenewalRequest) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO renewal_requests (sub_id, email, plan_id, days, amount, currency, status, applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', '')`,
		r.SubID, r.Email, r.PlanID, r.Days, r.Amount, r.Currency, r.Status, r.AppliedDays, r.ExpiryBefore, r.ExpiryAfter, r.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrPendingExists
		}
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

// GetRenewal returns a request by id or ErrNotFound.
func (s *Store) GetRenewal(ctx context.Context, id int64) (*RenewalRequest, error) {
	r, err := scanRenewal(s.db.QueryRowContext(ctx, `SELECT `+renewalColumns+` FROM renewal_requests WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// ListRenewals returns the newest requests, optionally filtered by status ("" = all).
func (s *Store) ListRenewals(ctx context.Context, status string, limit int) ([]RenewalRequest, error) {
	q := `SELECT ` + renewalColumns + ` FROM renewal_requests`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	return s.queryRenewals(ctx, q, args...)
}

// ListRenewalsBySubID returns the newest requests of one subscription.
func (s *Store) ListRenewalsBySubID(ctx context.Context, subID string, limit int) ([]RenewalRequest, error) {
	return s.queryRenewals(ctx, `SELECT `+renewalColumns+` FROM renewal_requests WHERE sub_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, subID, limit)
}

// ListPendingRenewalsBefore returns pending requests created before the given time.
func (s *Store) ListPendingRenewalsBefore(ctx context.Context, createdBefore int64) ([]RenewalRequest, error) {
	return s.queryRenewals(ctx, `SELECT `+renewalColumns+` FROM renewal_requests WHERE status = ? AND created_at < ? ORDER BY created_at`, RenewalPending, createdBefore)
}

func (s *Store) queryRenewals(ctx context.Context, q string, args ...any) ([]RenewalRequest, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RenewalRequest
	for rows.Next() {
		r, err := scanRenewal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// HasPendingRenewal reports whether the subscription has an unconfirmed request.
func (s *Store) HasPendingRenewal(ctx context.Context, subID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM renewal_requests WHERE sub_id = ? AND status = ?`, subID, RenewalPending).Scan(&n)
	return n > 0, err
}

// SetRenewalOutcome records the panel write result right after the request was created.
func (s *Store) SetRenewalOutcome(ctx context.Context, id int64, status string, expiryAfter int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE renewal_requests SET status = ?, expiry_after = ?, error = ? WHERE id = ?`, status, expiryAfter, errMsg, id)
	return err
}

// ResolveRenewal moves a pending request to confirmed/rejected. Returns
// ErrNotPending if the request was already resolved or does not exist.
func (s *Store) ResolveRenewal(ctx context.Context, id int64, status, resolvedBy string, at int64, expiryAfter int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE renewal_requests SET status = ?, resolved_at = ?, resolved_by = ?, expiry_after = CASE WHEN ? > 0 THEN ? ELSE expiry_after END WHERE id = ? AND status = ?`,
		status, at, resolvedBy, expiryAfter, expiryAfter, id, RenewalPending)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotPending
	}
	return nil
}

// CountRenewals returns counts per status.
func (s *Store) CountRenewals(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM renewal_requests GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}
