-- The exit a client's traffic leaves through when a rule says "through the
-- client's exit". It is kept until that exit stays unreachable, so the address
-- sites see for a client does not change on its own.
ALTER TABLE clients ADD COLUMN exit_node_id INTEGER REFERENCES nodes (id) ON DELETE SET NULL;
