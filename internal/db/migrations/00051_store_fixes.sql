-- +goose Up
ALTER TABLE list_entry ADD COLUMN completed_from_repeat INTEGER NOT NULL DEFAULT 0;

DROP TRIGGER IF EXISTS anime_fts_au;
-- +goose StatementBegin
CREATE TRIGGER anime_fts_au AFTER UPDATE OF title_romaji, title_english, title_native, synonyms ON anime
WHEN old.title_romaji IS NOT new.title_romaji OR old.title_english IS NOT new.title_english
  OR old.title_native IS NOT new.title_native OR old.synonyms IS NOT new.synonyms
BEGIN
    INSERT INTO anime_fts(anime_fts, rowid, romaji, english, native, synonyms)
    VALUES ('delete', old.id, old.title_romaji, old.title_english, old.title_native, old.synonyms);
    INSERT INTO anime_fts(rowid, romaji, english, native, synonyms)
    VALUES (new.id, new.title_romaji, new.title_english, new.title_native, new.synonyms);
END;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS http_cache_fetched ON http_cache(fetched_at, url);

CREATE TABLE notified_release_new (
    info_hash  TEXT    NOT NULL,
    anime_id   INTEGER NOT NULL DEFAULT 0,
    episode    INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    announced  INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (info_hash, anime_id, episode)
) STRICT;
INSERT OR IGNORE INTO notified_release_new (info_hash, anime_id, episode, created_at, announced)
SELECT info_hash, coalesce(anime_id, 0), coalesce(episode, 0), created_at, announced FROM notified_release;
DROP TABLE notified_release;
ALTER TABLE notified_release_new RENAME TO notified_release;
CREATE INDEX IF NOT EXISTS notified_episode ON notified_release(anime_id, episode);

-- +goose Down
CREATE TABLE notified_release_old (
    info_hash  TEXT PRIMARY KEY,
    anime_id   INTEGER,
    episode    INTEGER,
    created_at INTEGER NOT NULL,
    announced  INTEGER NOT NULL DEFAULT 1
) STRICT;
INSERT OR IGNORE INTO notified_release_old (info_hash, anime_id, episode, created_at, announced)
SELECT info_hash, nullif(anime_id, 0), nullif(episode, 0), created_at, announced FROM notified_release
ORDER BY created_at;
DROP TABLE notified_release;
ALTER TABLE notified_release_old RENAME TO notified_release;
CREATE INDEX IF NOT EXISTS notified_episode ON notified_release(anime_id, episode);

DROP INDEX IF EXISTS http_cache_fetched;

DROP TRIGGER IF EXISTS anime_fts_au;
-- +goose StatementBegin
CREATE TRIGGER anime_fts_au AFTER UPDATE OF title_romaji, title_english, title_native, synonyms ON anime BEGIN
    INSERT INTO anime_fts(anime_fts, rowid, romaji, english, native, synonyms)
    VALUES ('delete', old.id, old.title_romaji, old.title_english, old.title_native, old.synonyms);
    INSERT INTO anime_fts(rowid, romaji, english, native, synonyms)
    VALUES (new.id, new.title_romaji, new.title_english, new.title_native, new.synonyms);
END;
-- +goose StatementEnd

ALTER TABLE list_entry DROP COLUMN completed_from_repeat;
