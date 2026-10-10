-- Delivery receipts. A push service answering 2xx does not mean the device
-- showed anything: Apple keeps accepting messages for a web app that was
-- removed from the home screen. The service worker now reports each message
-- it receives; endpoints that stay silent are dropped.
ALTER TABLE push_subscriptions ADD COLUMN last_receipt_at   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE push_subscriptions ADD COLUMN unconfirmed       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE push_subscriptions ADD COLUMN unconfirmed_since INTEGER NOT NULL DEFAULT 0;
