-- +goose Up
-- A release taken as an episode AniList has since moved into the future was the wrong one (an old episode with
-- the same TheTVDB number): forgotten, or the real one would never be announced or fetched.
DELETE FROM notified_release WHERE EXISTS (
    SELECT 1 FROM airing_delay d
    WHERE d.anime_id = notified_release.anime_id AND d.episode = notified_release.episode
      AND (d.now_at IS NULL OR d.now_at > unixepoch()));

-- +goose Down
SELECT 1;
