-- +goose Up
CREATE INDEX IF NOT EXISTS torrent_hash_lower ON torrent(lower(info_hash));
CREATE INDEX IF NOT EXISTS torrent_file_hash_lower ON torrent_file(lower(info_hash));
CREATE INDEX IF NOT EXISTS cache_entry_hash_lower ON cache_entry(lower(info_hash));

-- +goose Down
DROP INDEX IF EXISTS cache_entry_hash_lower;
DROP INDEX IF EXISTS torrent_file_hash_lower;
DROP INDEX IF EXISTS torrent_hash_lower;
