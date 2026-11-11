-- Results of the file analyzers. A row describes the file state it was
-- produced from (size and mod_time): when the file changes, or the
-- analyzer version is raised, the file is analyzed again.
CREATE TABLE file_analyses (
    file_id     UUID NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    analyzer    TEXT NOT NULL,
    version     INTEGER NOT NULL,
    size        BIGINT NOT NULL,
    mod_time    BIGINT NOT NULL,
    result      BYTEA,         -- JSON; NULL when the analysis failed
    error       TEXT NOT NULL, -- empty when the analysis succeeded
    analyzed_at BIGINT NOT NULL,
    PRIMARY KEY (file_id, analyzer)
);
