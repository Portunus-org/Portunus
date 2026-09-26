CREATE TABLE inbounds (
    id         INTEGER PRIMARY KEY,
    -- sing-box inbound tag; stable, derived from protocol and port.
    tag        TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL DEFAULT '',
    protocol   TEXT    NOT NULL CHECK (protocol IN ('vless', 'hysteria2', 'trojan', 'shadowsocks', 'tuic')),
    port       INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    enabled    INTEGER NOT NULL DEFAULT 1,
    -- Public, protocol-specific parameters (JSON): SNI, REALITY public key, certificate…
    settings   TEXT    NOT NULL DEFAULT '{}',
    -- Private key material (JSON), encrypted with the master key.
    secret     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE clients (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    enabled       INTEGER NOT NULL DEFAULT 1,
    -- Credentials: uuid for VLESS/TUIC, password for Hysteria2/Trojan/TUIC/
    -- Shadowsocks (base64 of 16 bytes, valid as a 2022-blake3 user key).
    uuid          TEXT    NOT NULL UNIQUE,
    password      TEXT    NOT NULL,
    -- Secret part of the subscription URL.
    sub_token     TEXT    NOT NULL UNIQUE,
    -- 0 means unlimited.
    traffic_limit INTEGER NOT NULL DEFAULT 0 CHECK (traffic_limit >= 0),
    expires_at    INTEGER,
    upload        INTEGER NOT NULL DEFAULT 0,
    download      INTEGER NOT NULL DEFAULT 0,
    note          TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
