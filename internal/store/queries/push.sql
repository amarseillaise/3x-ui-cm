-- name: push.upsert
INSERT INTO push_subscriptions
    (endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count)
VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0)
ON CONFLICT(endpoint) DO UPDATE SET
    role       = excluded.role,
    sub_id     = excluded.sub_id,
    p256dh     = excluded.p256dh,
    auth       = excluded.auth,
    user_agent = excluded.user_agent,
    fail_count = 0,
    -- subscribing again is itself proof that the device is alive
    unconfirmed       = 0,
    unconfirmed_since = 0;

-- name: push.delete
DELETE FROM push_subscriptions WHERE endpoint = ?;

-- name: push.list
SELECT endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count
FROM push_subscriptions
ORDER BY created_at;

-- name: push.list_by_role
SELECT endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count
FROM push_subscriptions
WHERE role = ?
ORDER BY created_at;

-- name: push.list_by_sub_ids
-- The placeholder list is built by the caller, one question mark per
-- subscription id. The ids are always bound as parameters, never interpolated.
SELECT endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count
FROM push_subscriptions
WHERE sub_id IN (%s)
ORDER BY created_at;

-- name: push.list_by_role_and_sub_ids
SELECT endpoint, role, sub_id, p256dh, auth, user_agent, created_at, last_ok_at, fail_count
FROM push_subscriptions
WHERE role = ? AND sub_id IN (%s)
ORDER BY created_at;

-- name: push.record_accepted
-- The push service took the message, which is not the same as the device
-- showing it. unconfirmed counts such messages since the last receipt, and
-- unconfirmed_since marks when that silence began. SET expressions read the
-- old row, so the CASE sees the count before the increment.
UPDATE push_subscriptions
SET last_ok_at        = ?,
    fail_count        = 0,
    unconfirmed_since = CASE WHEN unconfirmed = 0 THEN ? ELSE unconfirmed_since END,
    unconfirmed       = unconfirmed + 1
WHERE endpoint = ?
RETURNING unconfirmed, unconfirmed_since;

-- name: push.record_receipt
UPDATE push_subscriptions
SET last_receipt_at   = ?,
    unconfirmed       = 0,
    unconfirmed_since = 0
WHERE endpoint = ?;

-- name: push.mark_failed
UPDATE push_subscriptions SET fail_count = fail_count + 1 WHERE endpoint = ?;

-- name: push.count_for_sub
SELECT COUNT(*) FROM push_subscriptions WHERE sub_id = ?;

-- name: push.count_by_role
SELECT role, COUNT(*) FROM push_subscriptions GROUP BY role;
