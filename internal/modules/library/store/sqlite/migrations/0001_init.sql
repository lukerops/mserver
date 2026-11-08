-- Libraries group locations of the storage by media type. A location
-- belongs to at most one library.
CREATE TABLE libraries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    media_type TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE library_locations (
    library_id  TEXT NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    location_id TEXT NOT NULL UNIQUE,
    PRIMARY KEY (library_id, location_id)
);

-- Movies identified from the files of the storage. The id is derived from
-- the file id, so that the same file always gets the same movie.
CREATE TABLE movies (
    id          TEXT PRIMARY KEY,
    library_id  TEXT NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    file_id     TEXT NOT NULL UNIQUE,
    location_id TEXT NOT NULL,
    path        TEXT NOT NULL,
    title       TEXT NOT NULL,
    sort_title  TEXT NOT NULL,
    year        INTEGER NOT NULL, -- 0 when unknown
    media_info  BLOB,             -- JSON; NULL until the file is analyzed
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX movies_library_sort_title ON movies (library_id, sort_title);
CREATE INDEX movies_library_created_at ON movies (library_id, created_at);
