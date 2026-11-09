-- 0012: optional backup entry and exit for a chain. When the primary one
-- fails, traffic takes the backup hop instead; both are 0 when unset.
ALTER TABLE chains ADD COLUMN backup_entry_node_id INTEGER REFERENCES nodes (id) ON DELETE SET NULL;
ALTER TABLE chains ADD COLUMN backup_exit_node_id INTEGER REFERENCES nodes (id) ON DELETE SET NULL;
