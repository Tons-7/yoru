-- +goose Up
CREATE INDEX IF NOT EXISTS notification_created ON notification(created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS notification_created;
