package store

import (
	"context"
	"database/sql"
	"strings"
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions (endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0)
		ON CONFLICT(endpoint) DO UPDATE SET
			role = excluded.role, sub_id = excluded.sub_id, p256dh = excluded.p256dh,
			auth = excluded.auth, user_agent = excluded.user_agent, fail_count = 0`,
		p.Endpoint, p.Role, p.SubID, p.P256dh, p.Auth, p.UserAgent, p.CreatedAt)
	return err
}

// DeletePushSubscription removes a subscription and reports whether it existed.
func (s *Store) DeletePushSubscription(ctx context.Context, endpoint string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListPushSubscriptions returns subscriptions filtered by role ("" = any) and
// subscription ids (nil = any).
func (s *Store) ListPushSubscriptions(ctx context.Context, role string, subIDs []string) ([]PushSubscription, error) {
	var where []string
	var args []any
	if role != "" {
		where = append(where, "role = ?")
		args = append(args, role)
	}
	if subIDs != nil {
		if len(subIDs) == 0 {
			return nil, nil
		}
		where = append(where, "sub_id IN ("+placeholders(len(subIDs))+")")
		for _, id := range subIDs {
			args = append(args, id)
		}
	}
	q := `SELECT endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count FROM push_subscriptions`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at"
	rows, err := s.db.QueryContext(ctx, q, args...)
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

// MarkPushResult records a delivery outcome for an endpoint.
func (s *Store) MarkPushResult(ctx context.Context, endpoint string, ok bool, at int64) error {
	var err error
	if ok {
		_, err = s.db.ExecContext(ctx, `UPDATE push_subscriptions SET last_ok_at = ?, fail_count = 0 WHERE endpoint = ?`, at, endpoint)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE push_subscriptions SET fail_count = fail_count + 1 WHERE endpoint = ?`, endpoint)
	}
	return err
}

// HasPushSubscription reports whether a subscription id has at least one endpoint.
func (s *Store) HasPushSubscription(ctx context.Context, subID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE sub_id = ?`, subID).Scan(&n)
	return n > 0, err
}

// CountPushSubscriptions returns counts per role.
func (s *Store) CountPushSubscriptions(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role, COUNT(*) FROM push_subscriptions GROUP BY role`)
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

var _ = sql.ErrNoRows
