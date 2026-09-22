package store

import (
	"context"

	"github.com/amarseillaise/3x-ui-cm/internal/store/queries"
)

// Notification is a sent-notification log row used for de-duplication.
type Notification struct {
	ID     int64
	SubID  string
	Kind   string
	Ref    string
	Title  string
	SentAt int64
}

// RecordNotification inserts the row unless (sub_id, kind, ref) was already
// recorded. It reports whether the row was inserted.
func (s *Store) RecordNotification(ctx context.Context, n Notification) (bool, error) {
	res, err := s.db.ExecContext(ctx, queries.Q.NotificationInsert,
		n.SubID, n.Kind, n.Ref, n.Title, n.SentAt)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}

// CountNotifications returns the number of logged notifications.
func (s *Store) CountNotifications(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, queries.Q.NotificationCount).Scan(&n)
	return n, err
}

// NotificationSent reports whether (sub_id, kind, ref) was already logged.
func (s *Store) NotificationSent(ctx context.Context, subID, kind, ref string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, queries.Q.NotificationCountForRef, subID, kind, ref).Scan(&n)
	return n > 0, err
}
