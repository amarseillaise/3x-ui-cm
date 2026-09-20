-- Timestamps are Unix milliseconds, matching 3x-ui expiryTime.

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    role         TEXT NOT NULL,                 -- client | admin
    sub_id       TEXT NOT NULL DEFAULT '',      -- empty for admin
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_sub_id ON sessions(sub_id);
CREATE INDEX sessions_last_seen ON sessions(last_seen_at);

CREATE TABLE push_subscriptions (
    endpoint     TEXT PRIMARY KEY,
    role         TEXT NOT NULL,
    sub_id       TEXT NOT NULL DEFAULT '',
    p256dh       TEXT NOT NULL,
    auth         TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    last_ok_at   INTEGER NOT NULL DEFAULT 0,
    fail_count   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX push_subscriptions_sub_id ON push_subscriptions(sub_id);
CREATE INDEX push_subscriptions_role ON push_subscriptions(role);

CREATE TABLE renewal_requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    sub_id        TEXT NOT NULL,
    email         TEXT NOT NULL,
    plan_id       TEXT NOT NULL,
    days          INTEGER NOT NULL,
    amount        INTEGER NOT NULL,
    currency      TEXT NOT NULL,
    status        TEXT NOT NULL,                -- pending | confirmed | rejected | failed
    applied_days  INTEGER NOT NULL DEFAULT 0,
    expiry_before INTEGER NOT NULL DEFAULT 0,
    expiry_after  INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    resolved_at   INTEGER NOT NULL DEFAULT 0,
    resolved_by   TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT ''
);
-- One unconfirmed request per subscription at a time.
CREATE UNIQUE INDEX renewal_requests_pending ON renewal_requests(sub_id) WHERE status = 'pending';
CREATE INDEX renewal_requests_status ON renewal_requests(status, created_at);
CREATE INDEX renewal_requests_sub_id ON renewal_requests(sub_id, created_at);

CREATE TABLE notifications (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    sub_id  TEXT NOT NULL DEFAULT '',           -- empty for admin / global
    kind    TEXT NOT NULL,
    ref     TEXT NOT NULL DEFAULT '',
    title   TEXT NOT NULL DEFAULT '',
    sent_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX notifications_dedup ON notifications(sub_id, kind, ref);
