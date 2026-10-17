-- +goose Up
-- The Up next list: episodes to play in order, grouped by show. A show's episodes share its place and play in
-- episode order; moving a show moves them all.
CREATE TABLE up_next (
    anime_id INTEGER NOT NULL REFERENCES anime(id) ON DELETE CASCADE,
    episode  INTEGER NOT NULL,
    show_pos INTEGER NOT NULL,
    added_at INTEGER NOT NULL,
    PRIMARY KEY (anime_id, episode)
) STRICT;

CREATE INDEX up_next_order ON up_next (show_pos, episode);

-- Watched episodes leave by themselves, however progress rose: played here, ticked, or pulled from a tracker.
-- Only the episodes the rise passed, so one added again to rewatch stays.
-- +goose StatementBegin
CREATE TRIGGER up_next_progress AFTER UPDATE OF progress ON list_entry
WHEN new.progress > old.progress BEGIN
    DELETE FROM up_next WHERE anime_id = new.anime_id AND episode > old.progress AND episode <= new.progress;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER up_next_entry AFTER INSERT ON list_entry
WHEN new.progress > 0 BEGIN
    DELETE FROM up_next WHERE anime_id = new.anime_id AND episode <= new.progress;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS up_next_entry;
DROP TRIGGER IF EXISTS up_next_progress;
DROP TABLE IF EXISTS up_next;
