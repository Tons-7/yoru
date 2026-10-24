-- +goose Up
-- The curator's notes and comparisons for the release picker, and each release's files, which say its
-- resolution and episodes before its own metadata arrives.
ALTER TABLE seadex ADD COLUMN notes TEXT;
ALTER TABLE seadex ADD COLUMN comparison TEXT NOT NULL DEFAULT '[]';
ALTER TABLE seadex ADD COLUMN incomplete INTEGER NOT NULL DEFAULT 0;
ALTER TABLE seadex ADD COLUMN files TEXT NOT NULL DEFAULT '[]';

-- Forces a re-mirror so the new columns fill.
DELETE FROM corpus_source WHERE name = 'seadex';

-- +goose Down
ALTER TABLE seadex DROP COLUMN files;
ALTER TABLE seadex DROP COLUMN incomplete;
ALTER TABLE seadex DROP COLUMN comparison;
ALTER TABLE seadex DROP COLUMN notes;
