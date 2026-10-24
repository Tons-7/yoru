-- +goose Up
-- New episodes auto-download holds for a preferred group or a higher resolution, and since when, so a
-- restart doesn't start their wait over.
CREATE TABLE release_wait (
    anime_id   INTEGER NOT NULL,
    episode    INTEGER NOT NULL,
    first_seen INTEGER NOT NULL,
    PRIMARY KEY (anime_id, episode)
) STRICT;

-- +goose Down
DROP TABLE IF EXISTS release_wait;
