-- Other servers of the installation and third-party proxies.
CREATE TABLE nodes (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    -- 'agent': a server of ours running `portunus agent`, configured by the
    -- panel. 'link': somebody else's proxy imported from a share link.
    kind       TEXT    NOT NULL CHECK (kind IN ('agent', 'link')),
    -- Host name or IP address clients and the panel connect to.
    address    TEXT    NOT NULL,
    -- Roles. is_exit: traffic can leave through it. is_entry: it accepts
    -- the same clients as the main server (a backup entry point).
    is_exit    INTEGER NOT NULL DEFAULT 1,
    is_entry   INTEGER NOT NULL DEFAULT 0,
    enabled    INTEGER NOT NULL DEFAULT 1,
    -- Public parameters (JSON): ports, REALITY public key…
    settings   TEXT    NOT NULL DEFAULT '{}',
    -- Private material (JSON), encrypted with the master key.
    secret     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Named sets of domains and addresses that routing rules refer to.
CREATE TABLE route_lists (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    -- 'manual': entries typed in the panel. 'url': downloaded on a schedule.
    kind       TEXT    NOT NULL CHECK (kind IN ('manual', 'url')),
    url        TEXT    NOT NULL DEFAULT '',
    -- Manual entries, or the last downloaded text list, one per line.
    content    TEXT    NOT NULL DEFAULT '',
    -- For url lists: when it was last downloaded and what went wrong.
    fetched_at INTEGER,
    fetch_error TEXT   NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Ordered rules: the first one whose list matches decides.
CREATE TABLE route_rules (
    id         INTEGER PRIMARY KEY,
    position   INTEGER NOT NULL,
    list_id    INTEGER NOT NULL REFERENCES route_lists (id) ON DELETE CASCADE,
    -- 'direct', 'block', 'auto' (the fastest working exit) or 'node'.
    action     TEXT    NOT NULL CHECK (action IN ('direct', 'block', 'auto', 'node')),
    node_id    INTEGER REFERENCES nodes (id) ON DELETE CASCADE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX route_rules_position ON route_rules (position, id);
