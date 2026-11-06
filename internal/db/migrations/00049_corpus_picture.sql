-- +goose Up
ALTER TABLE corpus_anime ADD COLUMN picture TEXT;
DELETE FROM corpus_source WHERE name = 'manami';

-- +goose Down
ALTER TABLE corpus_anime DROP COLUMN picture;
