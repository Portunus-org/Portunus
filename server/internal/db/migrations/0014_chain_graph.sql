-- 0014: chains become directed graphs of edges over the nodes. The client's
-- traffic enters at a source (a node nothing points to, or the main server
-- when it has an outgoing edge), travels along the edges, and leaves from a
-- sink (a node with no outgoing edge). A source marked in chain_entry_flags
-- is a backup entry: drawn dashed, tried after the primary ones.

CREATE TABLE chain_edges (
    id           INTEGER PRIMARY KEY,
    chain_id     INTEGER NOT NULL REFERENCES chains (id) ON DELETE CASCADE,
    from_node_id INTEGER NOT NULL DEFAULT 0,
    to_node_id   INTEGER NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    hop_port     INTEGER NOT NULL,
    uuid         TEXT    NOT NULL DEFAULT '',
    settings     TEXT    NOT NULL DEFAULT '{}',
    secret       TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);

CREATE UNIQUE INDEX chain_edges_pair ON chain_edges (chain_id, from_node_id, to_node_id);
CREATE INDEX chain_edges_to ON chain_edges (to_node_id);

-- Backup entry markers: source nodes of a chain the operator drew as
-- fallbacks.
CREATE TABLE chain_entry_flags (
    chain_id INTEGER NOT NULL REFERENCES chains (id) ON DELETE CASCADE,
    node_id  INTEGER NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    PRIMARY KEY (chain_id, node_id)
);

-- Convert the old ordered paths into edges: the first hop always comes from
-- every panel server (0), each following one from its predecessor. A
-- leading 0 in the stored path is the main-server marker and takes no edge
-- of its own.
-- json_each exposes the array index as key.
INSERT INTO chain_edges (chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at)
SELECT c.id,
       CASE WHEN j.key - (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) = 0
            THEN 0
            ELSE json_extract(c.nodes, '$[' || (j.key - 1) || ']')
       END,
       json_extract(c.nodes, '$[' || j.key || ']'),
       l.hop_port, l.uuid, l.settings, l.secret, l.created_at
FROM chains c
CROSS JOIN json_each(c.nodes) j
JOIN chain_links l ON l.chain_id = c.id
                   AND l.position = j.key - (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END);

-- The old backup entry becomes a source that feeds the second node of the
-- path — skipping the first, so it keeps working when that one is down —
-- or, for a one-node path, the only node itself. It is marked as a backup.
INSERT INTO chain_edges (chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at)
SELECT c.id, c.backup_entry_node_id,
       json_extract(c.nodes, '$[' || (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) + 1 || ']'),
       0, '', '{}', '', c.updated_at
FROM chains c
WHERE c.backup_entry_node_id IS NOT NULL
      AND json_array_length(c.nodes) - (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) >= 2;

INSERT INTO chain_edges (chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at)
SELECT c.id, c.backup_entry_node_id,
       json_extract(c.nodes, '$[' || (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) || ']'),
       0, '', '{}', '', c.updated_at
FROM chains c
WHERE c.backup_entry_node_id IS NOT NULL
      AND json_array_length(c.nodes) - (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) = 1;

INSERT INTO chain_entry_flags (chain_id, node_id)
SELECT c.id, c.backup_entry_node_id
FROM chains c
WHERE c.backup_entry_node_id IS NOT NULL;

-- The old backup exit becomes an extra sink fed by the node before the last
-- one (or by the main server for a one-node path).
INSERT INTO chain_edges (chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at)
SELECT c.id,
       json_extract(c.nodes, '$[' || (json_array_length(c.nodes) - 2) || ']'),
       c.backup_exit_node_id, 0, '', '{}', '', c.updated_at
FROM chains c
WHERE c.backup_exit_node_id IS NOT NULL
      AND json_array_length(c.nodes) - (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) >= 2;

INSERT INTO chain_edges (chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at)
SELECT c.id, 0, c.backup_exit_node_id, 0, '', '{}', '', c.updated_at
FROM chains c
WHERE c.backup_exit_node_id IS NOT NULL
      AND json_array_length(c.nodes) - (CASE WHEN json_extract(c.nodes, '$[0]') = 0 THEN 1 ELSE 0 END) = 1;

-- The ordered-path machinery is gone: the graph lives in chain_edges now.
ALTER TABLE chains DROP COLUMN nodes;
ALTER TABLE chains DROP COLUMN backup_entry_node_id;
ALTER TABLE chains DROP COLUMN backup_exit_node_id;
DROP TABLE chain_links;

-- One chain per client again: its exits are the sinks of its graph, and the
-- per-route credentials were derived from the client's own uuid anyway.
DROP TABLE client_routes;
