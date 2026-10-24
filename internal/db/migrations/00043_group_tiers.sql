-- +goose Up
-- TRaSH Guides' anime release-group grades: BD and WEB tiers, and the low-quality list.
CREATE TABLE group_tier (
    kind    TEXT    NOT NULL,
    level   INTEGER NOT NULL,
    name    TEXT    NOT NULL,
    pattern TEXT    NOT NULL
) STRICT;

-- +goose Down
DROP TABLE IF EXISTS group_tier;
