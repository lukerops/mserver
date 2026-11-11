-- Locations never overlap: root is the directory of the location, kept apart
-- from the driver options so that the overlap can be checked.
CREATE TABLE locations (
    id         UUID PRIMARY KEY,
    driver     TEXT NOT NULL,
    options    TEXT NOT NULL, -- JSON
    root       TEXT NOT NULL UNIQUE,
    created_at BIGINT NOT NULL
);

CREATE TABLE files (
    id          UUID PRIMARY KEY,
    location_id UUID NOT NULL REFERENCES locations (id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    dir         TEXT NOT NULL,
    size        BIGINT NOT NULL,
    mod_time    BIGINT NOT NULL,
    inode       BIGINT NOT NULL, -- 0 when unsupported
    UNIQUE (location_id, path)
);

CREATE INDEX files_dir ON files (location_id, dir);
