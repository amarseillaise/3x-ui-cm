package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/amarseillaise/3x-ui-cm/internal/store/queries"
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
	res, err := s.db.ExecContext(ctx, queries.Q.RenewalInsert,
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
	r, err := scanRenewal(s.db.QueryRowContext(ctx, queries.Q.RenewalGet, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// ListRenewals returns the newest requests, optionally filtered by status ("" = all).
func (s *Store) ListRenewals(ctx context.Context, status string, limit int) ([]RenewalRequest, error) {
	if status == "" {
		return s.queryRenewals(ctx, queries.Q.RenewalList, limit)
	}
	return s.queryRenewals(ctx, queries.Q.RenewalListByStatus, status, limit)
}

// ListRenewalsBySubID returns the newest requests of one subscription.
func (s *Store) ListRenewalsBySubID(ctx context.Context, subID string, limit int) ([]RenewalRequest, error) {
	return s.queryRenewals(ctx, queries.Q.RenewalListBySub, subID, limit)
}

// ListPendingRenewalsBefore returns pending requests created before the given time.
func (s *Store) ListPendingRenewalsBefore(ctx context.Context, createdBefore int64) ([]RenewalRequest, error) {
	return s.queryRenewals(ctx, queries.Q.RenewalListPendingBefore, RenewalPending, createdBefore)
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
	err := s.db.QueryRowContext(ctx, queries.Q.RenewalCountPendingForSub, subID, RenewalPending).Scan(&n)
	return n > 0, err
}

// SetRenewalOutcome records the panel write result right after the request was created.
func (s *Store) SetRenewalOutcome(ctx context.Context, id int64, status string, expiryAfter int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, queries.Q.RenewalSetOutcome, status, expiryAfter, errMsg, id)
	return err
}

// ResolveRenewal moves a pending request to confirmed/rejected. Returns
// ErrNotPending if the request was already resolved or does not exist.
func (s *Store) ResolveRenewal(ctx context.Context, id int64, status, resolvedBy string, at int64, expiryAfter int64) error {
	res, err := s.db.ExecContext(ctx, queries.Q.RenewalResolve,
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
	rows, err := s.db.QueryContext(ctx, queries.Q.RenewalCountByStatus)
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
