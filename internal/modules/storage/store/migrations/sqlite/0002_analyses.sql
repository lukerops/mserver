-- Results of the file analyzers. A row describes the file state it was
-- produced from (size and mod_time): when the file changes, or the
-- analyzer version is raised, the file is analyzed again.
CREATE TABLE file_analyses (
    file_id     TEXT NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    analyzer    TEXT NOT NULL,
    version     INTEGER NOT NULL,
    size        INTEGER NOT NULL,
    mod_time    INTEGER NOT NULL,
    result      BLOB,          -- JSON; NULL when the analysis failed
    error       TEXT NOT NULL, -- empty when the analysis succeeded
    analyzed_at INTEGER NOT NULL,
    PRIMARY KEY (file_id, analyzer)
);
