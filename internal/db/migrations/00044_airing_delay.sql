-- +goose Up
-- An episode AniList moved more than a day later, or whose future date it dropped, recorded however the
-- row was updated; the release watch announces it for shows on the list.
CREATE TABLE airing_delay (
    anime_id  INTEGER NOT NULL,
    episode   INTEGER NOT NULL,
    was_at    INTEGER NOT NULL,
    now_at    INTEGER,
    seen_at   INTEGER NOT NULL,
    announced INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (anime_id, episode)
) STRICT;

-- A date dropped after it passed is a finale AniList hasn't marked finished yet, not a delay.
-- +goose StatementBegin
CREATE TRIGGER anime_airing_delayed AFTER UPDATE OF next_airing_at ON anime
WHEN old.next_episode IS NOT NULL AND old.next_airing_at IS NOT NULL AND (
    (new.next_episode = old.next_episode AND new.next_airing_at > old.next_airing_at + 86400)
    OR (new.next_airing_at IS NULL AND new.status = 'RELEASING' AND old.next_airing_at > unixepoch()))
BEGIN
    INSERT INTO airing_delay (anime_id, episode, was_at, now_at, seen_at)
    VALUES (new.id, old.next_episode, old.next_airing_at, new.next_airing_at, unixepoch())
    ON CONFLICT(anime_id, episode) DO UPDATE SET
        now_at = excluded.now_at, seen_at = excluded.seen_at, announced = 0;
END;
-- +goose StatementEnd

-- An episode left without a date gets one: told again, with the date.
-- +goose StatementBegin
CREATE TRIGGER anime_airing_redated AFTER UPDATE OF next_airing_at ON anime
WHEN old.next_airing_at IS NULL AND new.next_airing_at IS NOT NULL
BEGIN
    UPDATE airing_delay SET now_at = new.next_airing_at, seen_at = unixepoch(), announced = 0
    WHERE anime_id = new.id AND episode = new.next_episode AND now_at IS NULL;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS anime_airing_redated;
DROP TRIGGER IF EXISTS anime_airing_delayed;
DROP TABLE IF EXISTS airing_delay;
