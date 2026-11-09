-- +goose Up
UPDATE filler SET source = 'mal' WHERE source = 'jikan';
UPDATE filler SET recap_source = 'mal' WHERE recap_source = 'jikan';

-- +goose Down
UPDATE filler SET source = 'jikan' WHERE source = 'mal';
UPDATE filler SET recap_source = 'jikan' WHERE recap_source = 'mal';
