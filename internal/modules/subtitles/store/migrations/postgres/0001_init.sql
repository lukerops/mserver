-- What the module knows of the storage and of the library, to match
-- subtitles with items without calling them. path is relative to the
-- location and dir is its directory ("." at the root of the location):
-- subtitles only match items of the same directory.
CREATE TABLE subtitle_files (
    file_id     UUID PRIMARY KEY,
    location_id UUID NOT NULL,
    path        TEXT NOT NULL,
    dir         TEXT NOT NULL,
    format      TEXT NOT NULL
);

CREATE INDEX subtitle_files_dir ON subtitle_files (location_id, dir);

-- Movies and episodes of the library. item_type is "movie" or "episode".
CREATE TABLE media_items (
    item_id     UUID PRIMARY KEY,
    item_type   TEXT NOT NULL,
    file_id     UUID NOT NULL,
    location_id UUID NOT NULL,
    path        TEXT NOT NULL,
    dir         TEXT NOT NULL
);

CREATE INDEX media_items_dir ON media_items (location_id, dir);

-- Subtitles matched with items. The id is derived from the item and the
-- subtitle file. location_id and dir are the ones of the item (and of the
-- file): the matches of a directory are recalculated together.
CREATE TABLE matched_subtitles (
    subtitle_id         UUID PRIMARY KEY,
    item_id             UUID NOT NULL,
    item_type           TEXT NOT NULL,
    file_id             UUID NOT NULL,
    location_id         UUID NOT NULL,
    dir                 TEXT NOT NULL,
    format              TEXT NOT NULL,
    language            TEXT NOT NULL, -- BCP 47; empty when unknown
    title               TEXT NOT NULL,
    is_forced           BOOLEAN NOT NULL,
    is_hearing_impaired BOOLEAN NOT NULL,
    source              TEXT NOT NULL
);

CREATE INDEX matched_subtitles_dir ON matched_subtitles (location_id, dir);
