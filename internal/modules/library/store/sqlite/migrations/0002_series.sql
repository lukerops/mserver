-- Series and seasons are logical: they only group episodes, and are
-- removed with their last episode. A series is matched by its normalized
-- title (key) and year within its library.
CREATE TABLE series (
    id         TEXT PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    sort_title TEXT NOT NULL,
    key        TEXT NOT NULL,
    year       INTEGER NOT NULL, -- 0 when unknown
    created_at INTEGER NOT NULL,
    UNIQUE (library_id, key, year)
);

CREATE INDEX series_library_sort_title ON series (library_id, sort_title);

CREATE TABLE seasons (
    id         TEXT PRIMARY KEY,
    series_id  TEXT NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    library_id TEXT NOT NULL,
    number     INTEGER NOT NULL, -- 0 for the specials
    created_at INTEGER NOT NULL,
    UNIQUE (series_id, number)
);

-- Episodes identified from the files of the storage. Like the movies, the
-- id is derived from the file id.
CREATE TABLE episodes (
    id          TEXT PRIMARY KEY,
    library_id  TEXT NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    series_id   TEXT NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    season_id   TEXT NOT NULL REFERENCES seasons (id) ON DELETE CASCADE,
    file_id     TEXT NOT NULL UNIQUE,
    location_id TEXT NOT NULL,
    path        TEXT NOT NULL,
    title       TEXT NOT NULL,
    sort_key    TEXT NOT NULL,
    number      INTEGER,          -- NULL when unknown
    end_number  INTEGER,          -- NULL unless the file holds several episodes
    media_info  BLOB,             -- JSON; NULL until the file is analyzed
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX episodes_series ON episodes (series_id);
CREATE INDEX episodes_season ON episodes (season_id, number, sort_key);
CREATE INDEX episodes_library_created_at ON episodes (library_id, created_at);
