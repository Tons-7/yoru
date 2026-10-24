-- +goose Up

-- Skip times found from two episodes' shared sound, or marked by the viewer; by show, as AniSkip's need a MAL id.
CREATE TABLE skip_own (
    anime_id INTEGER NOT NULL,
    episode  INTEGER NOT NULL,
    kind     TEXT    NOT NULL,
    start_s  REAL    NOT NULL,
    end_s    REAL    NOT NULL,
    source   TEXT    NOT NULL CHECK (source IN ('found', 'marked')),
    PRIMARY KEY (anime_id, episode, kind, source)
) STRICT;

-- A show's opening or ending as chromaprint items, so later episodes match it once earlier files are gone.
CREATE TABLE skip_print (
    anime_id INTEGER NOT NULL,
    kind     TEXT    NOT NULL,
    print    BLOB    NOT NULL,
    found_at INTEGER NOT NULL,
    PRIMARY KEY (anime_id, kind)
) STRICT;

-- What was sent to AniSkip (a submission or a vote), so each goes once.
CREATE TABLE skip_shared (
    key     TEXT    PRIMARY KEY,
    sent_at INTEGER NOT NULL
) STRICT;

-- +goose Down
DROP TABLE IF EXISTS skip_shared;
DROP TABLE IF EXISTS skip_print;
DROP TABLE IF EXISTS skip_own;
