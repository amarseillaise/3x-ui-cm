package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/amarseillaise/3x-ui-cm/internal/store/queries"
)

// PushSubscription is a browser Web Push subscription bound to a session role.
type PushSubscription struct {
	Endpoint  string
	Role      string
	SubID     string
	P256dh    string
	Auth      string
	UserAgent string
	CreatedAt int64
	LastOkAt  int64
	FailCount int
}

// UpsertPushSubscription inserts or refreshes a subscription keyed by endpoint.
func (s *Store) UpsertPushSubscription(ctx context.Context, p PushSubscription) error {
	_, err := s.db.ExecContext(ctx, queries.Q.PushUpsert,
		p.Endpoint, p.Role, p.SubID, p.P256dh, p.Auth, p.UserAgent, p.CreatedAt)
	return err
}

// DeletePushSubscription removes a subscription and reports whether it existed.
func (s *Store) DeletePushSubscription(ctx context.Context, endpoint string) (bool, error) {
	res, err := s.db.ExecContext(ctx, queries.Q.PushDelete, endpoint)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListPushSubscriptions returns subscriptions filtered by role ("" = any) and
// subscription ids (nil = any).
func (s *Store) ListPushSubscriptions(ctx context.Context, role string, subIDs []string) ([]PushSubscription, error) {
	query, args, err := pushListQuery(role, subIDs)
	if err != nil || query == "" {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.Endpoint, &p.Role, &p.SubID, &p.P256dh, &p.Auth, &p.UserAgent, &p.CreatedAt, &p.LastOkAt, &p.FailCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PushSilence is how long an endpoint has gone without confirming delivery.
type PushSilence struct {
	Unconfirmed int   // messages the push service accepted since the last receipt
	Since       int64 // when the first of them was accepted; 0 when Unconfirmed is 0
}

// RecordPushAccepted notes that the push service accepted a message for the
// endpoint and returns the silence that follows. ErrNotFound means the
// endpoint was deleted meanwhile.
func (s *Store) RecordPushAccepted(ctx context.Context, endpoint string, at int64) (PushSilence, error) {
	var silence PushSilence
	err := s.db.QueryRowContext(ctx, queries.Q.PushRecordAccepted, at, at, endpoint).
		Scan(&silence.Unconfirmed, &silence.Since)
	if errors.Is(err, sql.ErrNoRows) {
		return silence, ErrNotFound
	}
	return silence, err
}

// RecordPushFailure counts a delivery the push service refused.
func (s *Store) RecordPushFailure(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, queries.Q.PushMarkFailed, endpoint)
	return err
}

// RecordPushReceipt notes that the device's service worker received a message,
// ending any silence. It reports whether the endpoint is known.
func (s *Store) RecordPushReceipt(ctx context.Context, endpoint string, at int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, queries.Q.PushRecordReceipt, at, endpoint)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// HasPushSubscription reports whether a subscription id has at least one endpoint.
func (s *Store) HasPushSubscription(ctx context.Context, subID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, queries.Q.PushCountForSub, subID).Scan(&n)
	return n > 0, err
}

// CountPushSubscriptions returns counts per role.
func (s *Store) CountPushSubscriptions(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, queries.Q.PushCountByRole)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var role string
		var n int64
		if err := rows.Scan(&role, &n); err != nil {
			return nil, err
		}
		out[role] = n
	}
	return out, rows.Err()
}

// pushListQuery picks the statement matching the filters and binds their
// values. An empty query means the filters cannot match anything.
func pushListQuery(role string, subIDs []string) (string, []any, error) {
	if subIDs != nil && len(subIDs) == 0 {
		return "", nil, nil
	}
	ids := make([]any, 0, len(subIDs)+1)
	switch {
	case role == "" && subIDs == nil:
		return queries.Q.PushList, nil, nil
	case subIDs == nil:
		return queries.Q.PushListByRole, []any{role}, nil
	case role == "":
		for _, id := range subIDs {
			ids = append(ids, id)
		}
		return fmt.Sprintf(queries.Q.PushListBySubIDs, placeholders(len(subIDs))), ids, nil
	default:
		ids = append(ids, role)
		for _, id := range subIDs {
			ids = append(ids, id)
		}
		return fmt.Sprintf(queries.Q.PushListByRoleAndSubs, placeholders(len(subIDs))), ids, nil
	}
}
