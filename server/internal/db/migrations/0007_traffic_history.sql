-- Traffic of all clients together, by the hour: the history behind the
-- chart on the overview. Hours older than three months are dropped.
CREATE TABLE traffic_hours (
    hour     INTEGER PRIMARY KEY, -- Unix time of the start of the hour
    upload   INTEGER NOT NULL DEFAULT 0,
    download INTEGER NOT NULL DEFAULT 0
);
