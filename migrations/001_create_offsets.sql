CREATE TABLE IF NOT EXISTS offsets (
    machine_id  TEXT        NOT NULL,
    file_path   TEXT        NOT NULL,
    "offset"    BIGINT      NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (machine_id, file_path)
);
