-- Libraries group locations of the storage by media type. A location
-- belongs to at most one library. position keeps the order the locations
-- were given in.
CREATE TABLE libraries (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    media_type TEXT NOT NULL,
    created_at BIGINT NOT NULL
);

CREATE TABLE library_locations (
    library_id  UUID NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    location_id UUID NOT NULL UNIQUE,
    position    BIGINT NOT NULL,
    PRIMARY KEY (library_id, location_id)
);

-- Movies identified from the files of the storage. The id is derived from
-- the file id, so that the same file always gets the same movie.
-- audio_languages and subtitle_languages are derived from media_info, to
-- filter by language with a plain LIKE: lowercase tags, sorted, delimited
-- by commas (",eng,por,"); empty when there are none.
CREATE TABLE movies (
    id                 UUID PRIMARY KEY,
    library_id         UUID NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    file_id            UUID NOT NULL UNIQUE,
    location_id        UUID NOT NULL,
    path               TEXT NOT NULL,
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL,
    year               BIGINT NOT NULL, -- 0 when unknown
    media_info         TEXT,           -- JSON; NULL until the file is analyzed
    audio_languages    TEXT NOT NULL DEFAULT '',
    subtitle_languages TEXT NOT NULL DEFAULT '',
    created_at         BIGINT NOT NULL,
    updated_at         BIGINT NOT NULL
);

CREATE INDEX movies_library_sort_title ON movies (library_id, sort_title);
CREATE INDEX movies_library_created_at ON movies (library_id, created_at);

-- Series and seasons are logical: they only group episodes, and are
-- removed with their last episode. A series is matched by its normalized
-- title (key) and year within its library.
CREATE TABLE series (
    id         UUID PRIMARY KEY,
    library_id UUID NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    sort_title TEXT NOT NULL,
    key        TEXT NOT NULL,
    year       BIGINT NOT NULL, -- 0 when unknown
    created_at BIGINT NOT NULL,
    UNIQUE (library_id, key, year)
);

CREATE INDEX series_library_sort_title ON series (library_id, sort_title);

CREATE TABLE seasons (
    id         UUID PRIMARY KEY,
    series_id  UUID NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    library_id UUID NOT NULL,
    number     BIGINT NOT NULL, -- 0 for the specials
    created_at BIGINT NOT NULL,
    UNIQUE (series_id, number)
);

-- Episodes identified from the files of the storage. Like the movies, the
-- id is derived from the file id.
CREATE TABLE episodes (
    id                 UUID PRIMARY KEY,
    library_id         UUID NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    series_id          UUID NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    season_id          UUID NOT NULL REFERENCES seasons (id) ON DELETE CASCADE,
    file_id            UUID NOT NULL UNIQUE,
    location_id        UUID NOT NULL,
    path               TEXT NOT NULL,
    title              TEXT NOT NULL,
    sort_key           TEXT NOT NULL,
    number             BIGINT,          -- NULL when unknown
    end_number         BIGINT,          -- NULL unless the file holds several episodes
    media_info         TEXT,           -- JSON; NULL until the file is analyzed
    audio_languages    TEXT NOT NULL DEFAULT '',
    subtitle_languages TEXT NOT NULL DEFAULT '',
    created_at         BIGINT NOT NULL,
    updated_at         BIGINT NOT NULL
);

CREATE INDEX episodes_series ON episodes (series_id);
CREATE INDEX episodes_season ON episodes (season_id, number, sort_key);
CREATE INDEX episodes_library_created_at ON episodes (library_id, created_at);
