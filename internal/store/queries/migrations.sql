-- name: migration.create_table
-- Bookkeeping for the migration runner itself; the schema it applies lives in
-- internal/store/migrations.
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

-- name: migration.list_applied
SELECT version FROM schema_migrations;

-- name: migration.record
INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?);
