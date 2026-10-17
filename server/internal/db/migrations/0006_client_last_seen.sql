-- When traffic of a client was last counted: "last online" in the panel.
ALTER TABLE clients ADD COLUMN last_seen_at INTEGER;
