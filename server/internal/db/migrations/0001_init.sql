-- Timestamps are Unix seconds (INTEGER) throughout.

CREATE TABLE users (
    id                   INTEGER PRIMARY KEY,
    username             TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    -- NULL for accounts that can only sign in through OIDC.
    password_hash        TEXT,
    role                 TEXT    NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    must_change_password INTEGER NOT NULL DEFAULT 0,
    disabled             INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE TABLE sessions (
    -- SHA-256 of the cookie token; the raw token is never stored.
    id_hash      BLOB    PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token   TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    ip           TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_id ON sessions (user_id);
CREATE INDEX sessions_expires_at ON sessions (expires_at);

CREATE TABLE audit_log (
    id       INTEGER PRIMARY KEY,
    at       INTEGER NOT NULL,
    actor_id INTEGER REFERENCES users (id) ON DELETE SET NULL,
    actor    TEXT    NOT NULL DEFAULT '',
    action   TEXT    NOT NULL,
    target   TEXT    NOT NULL DEFAULT '',
    ip       TEXT    NOT NULL DEFAULT '',
    details  TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at ON audit_log (at);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
