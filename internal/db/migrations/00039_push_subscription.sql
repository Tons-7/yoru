-- +goose Up
-- Devices that asked for alerts while Yoru is closed: the browser's push endpoint and the keys its messages are
-- encrypted to. origin is the address the device used, so a tapped alert opens Yoru there.
-- device (its approval id, if any) and paired (it came in with the pairing token) let a declined, removed or
-- unpaired device's alerts stop with its access.
CREATE TABLE push_subscription (
    endpoint   TEXT PRIMARY KEY,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    origin     TEXT NOT NULL,
    device     TEXT NOT NULL DEFAULT '',
    paired     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
) STRICT;

-- +goose Down
DROP TABLE IF EXISTS push_subscription;
