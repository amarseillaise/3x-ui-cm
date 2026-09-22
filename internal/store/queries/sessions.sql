-- name: session.insert
INSERT INTO sessions (id, role, sub_id, created_at, last_seen_at, user_agent)
VALUES (?, ?, ?, ?, ?, ?);

-- name: session.get
SELECT id, role, sub_id, created_at, last_seen_at, user_agent
FROM sessions
WHERE id = ?;

-- name: session.touch
UPDATE sessions SET last_seen_at = ? WHERE id = ?;

-- name: session.delete
DELETE FROM sessions WHERE id = ?;

-- name: session.delete_idle
DELETE FROM sessions WHERE last_seen_at < ?;

-- name: session.count
SELECT COUNT(*) FROM sessions;
