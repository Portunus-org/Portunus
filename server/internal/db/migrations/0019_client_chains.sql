-- Clients can belong to multiple chains (choosing between them on the
-- client side), while traffic inside each chain shares one credential and
-- fails over automatically across the chain's exits on the server.
CREATE TABLE client_chains (
    client_id INTEGER NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    chain_id  INTEGER NOT NULL REFERENCES chains  (id) ON DELETE CASCADE,
    PRIMARY KEY (client_id, chain_id)
);

INSERT INTO client_chains (client_id, chain_id)
SELECT id, chain_id FROM clients WHERE chain_id IS NOT NULL;
