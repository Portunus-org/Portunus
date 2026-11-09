-- The display color of a chain on the map and in the chains list. Empty
-- means the panel's default (the foreground).
ALTER TABLE chains ADD COLUMN color TEXT NOT NULL DEFAULT '';
