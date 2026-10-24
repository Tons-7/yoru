-- +goose Up

-- Where the ending started when the ending rule ticked the episode, seconds: finished from there on.
ALTER TABLE playback ADD COLUMN done_s REAL;

-- +goose Down
ALTER TABLE playback DROP COLUMN done_s;
