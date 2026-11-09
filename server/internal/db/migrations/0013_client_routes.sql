-- A client may travel through several chains at once: each route gets its
-- own credential, so an entry node can tell which chain a connection wants.
CREATE TABLE client_routes (
  client_id INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  chain_id INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  uuid TEXT NOT NULL,
  PRIMARY KEY (client_id, chain_id)
);
