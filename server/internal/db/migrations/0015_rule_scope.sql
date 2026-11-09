-- 0015: a rule may belong to one chain instead of to every client. A chain
-- is filtered where its traffic enters, so a rule of a chain applies only at
-- that chain's entry points and only to the clients that ride it.
-- chain_id NULL keeps the rule global, as it always was.
ALTER TABLE route_rules ADD COLUMN chain_id INTEGER REFERENCES chains (id) ON DELETE CASCADE;
