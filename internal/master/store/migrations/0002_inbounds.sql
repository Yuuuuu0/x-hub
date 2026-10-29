ALTER TABLE nodes ADD COLUMN desired_rev INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN applied_rev INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN applied_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';

CREATE TABLE inbounds (
    id         INTEGER PRIMARY KEY,
    node_id    INTEGER NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    remark     TEXT    NOT NULL DEFAULT '',
    sort       INTEGER NOT NULL DEFAULT 0,
    enabled    INTEGER NOT NULL DEFAULT 1,
    listen     TEXT    NOT NULL DEFAULT '',
    port       INTEGER NOT NULL,
    protocol   TEXT    NOT NULL,
    spec_json  TEXT    NOT NULL,
    link_host  TEXT    NOT NULL DEFAULT '',
    link_port  INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (node_id, port)
);

CREATE TABLE subscriptions (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL,
    note            TEXT    NOT NULL DEFAULT '',
    sort            INTEGER NOT NULL DEFAULT 0,
    token           TEXT    NOT NULL UNIQUE,
    secret          TEXT    NOT NULL,
    uuid            TEXT    NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    state           TEXT    NOT NULL DEFAULT 'active',
    all_inbounds    INTEGER NOT NULL DEFAULT 1,
    limit_bytes     INTEGER NOT NULL DEFAULT 0,
    expire_at       INTEGER NOT NULL DEFAULT 0,
    reset_day       INTEGER NOT NULL DEFAULT 0,
    period_start_at INTEGER NOT NULL DEFAULT 0,
    used_up         INTEGER NOT NULL DEFAULT 0,
    used_down       INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL
);

CREATE TABLE subscription_inbounds (
    sub_id     INTEGER NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    inbound_id INTEGER NOT NULL REFERENCES inbounds (id) ON DELETE CASCADE,
    PRIMARY KEY (sub_id, inbound_id)
);
