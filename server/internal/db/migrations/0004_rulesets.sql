-- A list is either plain text (one site or address per line) or a sing-box
-- rule-set, kept as the JSON array of its rules.
ALTER TABLE route_lists ADD COLUMN format TEXT NOT NULL DEFAULT 'text' CHECK (format IN ('text', 'ruleset'));
