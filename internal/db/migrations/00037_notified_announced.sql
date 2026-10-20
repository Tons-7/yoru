-- +goose Up
-- A release fetched for auto-download but not announced (notifications ask for a dub, it was a sub) must not
-- mark its episode as announced, or the dub that follows is never told.
ALTER TABLE notified_release ADD COLUMN announced INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE notified_release DROP COLUMN announced;
