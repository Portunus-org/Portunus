-- 0018: allow each chain to set its own default route ("all other traffic")
-- independently of the global default. Empty means follow the global default;
-- otherwise 'direct', 'auto' (through the chain), or 'node' with
-- default_node_id naming an exit node.
ALTER TABLE chains ADD COLUMN default_action TEXT NOT NULL DEFAULT '' CHECK (default_action IN ('', 'direct', 'auto', 'node'));
ALTER TABLE chains ADD COLUMN default_node_id INTEGER REFERENCES nodes (id) ON DELETE SET NULL;
