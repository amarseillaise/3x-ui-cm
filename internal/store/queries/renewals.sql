-- name: renewal.insert
INSERT INTO renewal_requests
    (sub_id, email, plan_id, days, amount, currency, status,
     applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', '');

-- name: renewal.get
SELECT id, sub_id, email, plan_id, days, amount, currency, status,
       applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error
FROM renewal_requests
WHERE id = ?;

-- name: renewal.list
SELECT id, sub_id, email, plan_id, days, amount, currency, status,
       applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error
FROM renewal_requests
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: renewal.list_by_status
SELECT id, sub_id, email, plan_id, days, amount, currency, status,
       applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error
FROM renewal_requests
WHERE status = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: renewal.list_by_sub
SELECT id, sub_id, email, plan_id, days, amount, currency, status,
       applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error
FROM renewal_requests
WHERE sub_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: renewal.list_pending_before
SELECT id, sub_id, email, plan_id, days, amount, currency, status,
       applied_days, expiry_before, expiry_after, created_at, resolved_at, resolved_by, error
FROM renewal_requests
WHERE status = ? AND created_at < ?
ORDER BY created_at;

-- name: renewal.count_pending_for_sub
SELECT COUNT(*) FROM renewal_requests WHERE sub_id = ? AND status = ?;

-- name: renewal.set_outcome
UPDATE renewal_requests SET status = ?, expiry_after = ?, error = ? WHERE id = ?;

-- name: renewal.resolve
-- Only a pending row is moved, so a double confirm cannot resolve it twice.
-- expiry_after is left alone when the caller passes 0.
UPDATE renewal_requests
SET status      = ?,
    resolved_at = ?,
    resolved_by = ?,
    expiry_after = CASE WHEN ? > 0 THEN ? ELSE expiry_after END
WHERE id = ? AND status = ?;

-- name: renewal.count_by_status
SELECT status, COUNT(*) FROM renewal_requests GROUP BY status;
