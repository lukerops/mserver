-- Usernames and emails are unique ignoring case: the *_key columns hold
-- their lowercase form.
CREATE TABLE accounts (
    id            UUID PRIMARY KEY,
    username      TEXT NOT NULL,
    username_key  TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL,
    email_key     TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL, -- argon2id, PHC string format
    is_admin      BOOLEAN NOT NULL,
    disabled      BOOLEAN NOT NULL,
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL
);

CREATE TABLE profiles (
    id           UUID PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    handle       TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE (account_id, handle)
);

CREATE TABLE devices (
    id           UUID PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    identifier   TEXT NOT NULL, -- chosen by the client
    name         TEXT NOT NULL,
    client       TEXT NOT NULL,
    version      TEXT NOT NULL,
    created_at   BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    UNIQUE (account_id, identifier)
);

CREATE TABLE sessions (
    id         UUID PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE, -- SHA-256 of the access token
    account_id UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    device_id  UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    profile_id UUID REFERENCES profiles (id) ON DELETE SET NULL, -- NULL until selected
    created_at BIGINT NOT NULL
);

CREATE INDEX sessions_device ON sessions (device_id);
