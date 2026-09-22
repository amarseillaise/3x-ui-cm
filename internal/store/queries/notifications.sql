-- name: notification.insert
-- Ignored on conflict: (sub_id, kind, ref) is what makes a notification unique,
-- so a repeated pass never notifies twice.
INSERT OR IGNORE INTO notifications (sub_id, kind, ref, title, sent_at)
VALUES (?, ?, ?, ?, ?);

-- name: notification.count
SELECT COUNT(*) FROM notifications;

-- name: notification.count_for_ref
SELECT COUNT(*) FROM notifications WHERE sub_id = ? AND kind = ? AND ref = ?;
