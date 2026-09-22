package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/amarseillaise/3x-ui-cm/internal/store/queries"
)

// Session roles.
const (
	RoleClient = "client"
	RoleAdmin  = "admin"
)

// Session is a logged-in browser. Client sessions carry the subscription id.
type Session struct {
	ID         string
	Role       string
	SubID      string
	CreatedAt  int64
	LastSeenAt int64
	UserAgent  string
}

// CreateSession inserts a new session.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, queries.Q.SessionInsert,
		sess.ID, sess.Role, sess.SubID, sess.CreatedAt, sess.LastSeenAt, sess.UserAgent)
	return err
}

// GetSession returns a session by id or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx, queries.Q.SessionGet, id).
		Scan(&sess.ID, &sess.Role, &sess.SubID, &sess.CreatedAt, &sess.LastSeenAt, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// TouchSession updates last_seen_at.
func (s *Store) TouchSession(ctx context.Context, id string, at int64) error {
	_, err := s.db.ExecContext(ctx, queries.Q.SessionTouch, at, id)
	return err
}

// DeleteSession removes a session; deleting a missing session is not an error.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, queries.Q.SessionDelete, id)
	return err
}

// DeleteSessionsIdleBefore removes sessions not seen since the given time.
func (s *Store) DeleteSessionsIdleBefore(ctx context.Context, lastSeenBefore int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, queries.Q.SessionDeleteIdle, lastSeenBefore)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountSessions returns the number of sessions.
func (s *Store) CountSessions(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, queries.Q.SessionCount).Scan(&n)
	return n, err
}
