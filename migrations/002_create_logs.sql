-- Partitioned by start_time (range, monthly).
-- Each month's data lives in its own child table for the sharding shown in the diagram.

CREATE TABLE IF NOT EXISTS logs (
    id          BIGSERIAL,
    machine_id  TEXT        NOT NULL,
    file_path   TEXT        NOT NULL,
    start_time  TIMESTAMPTZ NOT NULL,
    level       TEXT        NOT NULL,
    message     TEXT        NOT NULL,

    PRIMARY KEY (id, start_time)
) PARTITION BY RANGE (start_time);

-- Initial partitions — add more as needed.
CREATE TABLE IF NOT EXISTS logs_2026_04
    PARTITION OF logs
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE TABLE IF NOT EXISTS logs_2026_05
    PARTITION OF logs
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

-- Index per partition is inherited automatically in PG14+.
CREATE INDEX IF NOT EXISTS logs_machine_file_idx
    ON logs (machine_id, file_path, start_time DESC);
