-- 0017: allow chain edges to end on the main server (to_node_id = 0), so an
-- installation whose main server is abroad can use a domestic entry node as a
-- primary or backup hop into the main server.

CREATE TABLE chain_edges_new (
    id           INTEGER PRIMARY KEY,
    chain_id     INTEGER NOT NULL REFERENCES chains (id) ON DELETE CASCADE,
    from_node_id INTEGER NOT NULL DEFAULT 0,
    to_node_id   INTEGER NOT NULL DEFAULT 0,
    hop_port     INTEGER NOT NULL,
    uuid         TEXT    NOT NULL DEFAULT '',
    settings     TEXT    NOT NULL DEFAULT '{}',
    secret       TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);

INSERT INTO chain_edges_new (id, chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at)
SELECT id, chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at
FROM chain_edges;

DROP TABLE chain_edges;
ALTER TABLE chain_edges_new RENAME TO chain_edges;

CREATE UNIQUE INDEX chain_edges_pair ON chain_edges (chain_id, from_node_id, to_node_id);
CREATE INDEX chain_edges_to ON chain_edges (to_node_id);
