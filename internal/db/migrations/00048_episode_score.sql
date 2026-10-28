-- +goose Up
-- MyAnimeList's score for each episode (1 to 5) and its discussion thread, from the filler flags' listing.
CREATE TABLE episode_score (
    mal_id    INTEGER NOT NULL,
    number    INTEGER NOT NULL,
    score     REAL,
    forum_url TEXT,
    PRIMARY KEY (mal_id, number)
) STRICT;

-- +goose Down
DROP TABLE IF EXISTS episode_score;
