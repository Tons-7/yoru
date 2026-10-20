-- +goose Up
-- Titles only: every sync restamp and airing refresh also updates anime, and re-indexed unchanged titles each
-- time on the one writer connection that playback reports wait on.
DROP TRIGGER IF EXISTS anime_fts_au;
-- +goose StatementBegin
CREATE TRIGGER anime_fts_au AFTER UPDATE OF title_romaji, title_english, title_native, synonyms ON anime BEGIN
    INSERT INTO anime_fts(anime_fts, rowid, romaji, english, native, synonyms)
    VALUES ('delete', old.id, old.title_romaji, old.title_english, old.title_native, old.synonyms);
    INSERT INTO anime_fts(rowid, romaji, english, native, synonyms)
    VALUES (new.id, new.title_romaji, new.title_english, new.title_native, new.synonyms);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS anime_fts_au;
-- +goose StatementBegin
CREATE TRIGGER anime_fts_au AFTER UPDATE ON anime BEGIN
    INSERT INTO anime_fts(anime_fts, rowid, romaji, english, native, synonyms)
    VALUES ('delete', old.id, old.title_romaji, old.title_english, old.title_native, old.synonyms);
    INSERT INTO anime_fts(rowid, romaji, english, native, synonyms)
    VALUES (new.id, new.title_romaji, new.title_english, new.title_native, new.synonyms);
END;
-- +goose StatementEnd
