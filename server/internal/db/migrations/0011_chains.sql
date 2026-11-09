-- 0010: chains of nodes a client's traffic can travel through.

-- A chain is an ordered path of nodes: the client's traffic enters at the
-- first node and leaves from the last one, travelling hop by hop between
-- the nodes in between. The path is stored as a JSON array of node IDs.
CREATE TABLE chains (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    nodes      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- One link of a chain: the hop from one node to the next. The material
-- lives on the receiving node (to_node_id): a VLESS + REALITY inbound on
-- hop_port with its own key pair, provisioned like an exit's hop but per
-- link. from_node_id is 0 for the first link, which every panel server
-- (the main one and the backup entry points) uses to send clients into
-- the chain; it never references a node on its own.
CREATE TABLE chain_links (
    id           INTEGER PRIMARY KEY,
    chain_id     INTEGER NOT NULL REFERENCES chains (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    from_node_id INTEGER NOT NULL DEFAULT 0,
    to_node_id   INTEGER NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    hop_port     INTEGER NOT NULL,
    uuid         TEXT    NOT NULL DEFAULT '',
    settings     TEXT    NOT NULL DEFAULT '{}',
    secret       TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);

CREATE UNIQUE INDEX chain_links_pair ON chain_links (chain_id, position);
CREATE INDEX chain_links_to ON chain_links (to_node_id);

-- A client is bound to a chain instead of a single exit; the two are
-- mutually exclusive. Losing the chain unbinds the client.
ALTER TABLE clients ADD COLUMN chain_id INTEGER REFERENCES chains (id) ON DELETE SET NULL;
