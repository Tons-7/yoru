-- +goose Up
-- AniList's answer for a finished show as it gave it: its page is served from it, studio links and all.
ALTER TABLE anime ADD COLUMN media TEXT;

-- +goose Down
ALTER TABLE anime DROP COLUMN media;
