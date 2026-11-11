CREATE TABLE locations (
    id         TEXT PRIMARY KEY,
    driver     TEXT NOT NULL,
    options    TEXT NOT NULL, -- JSON
    root       TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE files (
    id          TEXT PRIMARY KEY,
    location_id TEXT NOT NULL REFERENCES locations (id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    dir         TEXT NOT NULL,
    size        INTEGER NOT NULL,
    mod_time    INTEGER NOT NULL,
    inode       INTEGER NOT NULL, -- 0 when unsupported
    UNIQUE (location_id, path)
);

CREATE INDEX files_dir ON files (location_id, dir);
