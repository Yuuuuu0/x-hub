CREATE TABLE admins (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    totp_secret   TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    id         INTEGER PRIMARY KEY,
    admin_id   INTEGER NOT NULL REFERENCES admins (id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    ip         TEXT    NOT NULL DEFAULT '',
    ua         TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expire_at  INTEGER NOT NULL
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL
);

CREATE TABLE nodes (
    id                INTEGER PRIMARY KEY,
    name              TEXT    NOT NULL,
    remark            TEXT    NOT NULL DEFAULT '',
    sort              INTEGER NOT NULL DEFAULT 0,
    public_host       TEXT    NOT NULL DEFAULT '',
    secret_hash       TEXT    NOT NULL DEFAULT '',
    agent_version     TEXT    NOT NULL DEFAULT '',
    xray_version      TEXT    NOT NULL DEFAULT '',
    capabilities_json TEXT    NOT NULL DEFAULT '[]',
    host_info_json    TEXT    NOT NULL DEFAULT '{}',
    last_ip           TEXT    NOT NULL DEFAULT '',
    last_seen_at      INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL
);

CREATE TABLE enroll_tokens (
    id         INTEGER PRIMARY KEY,
    node_id    INTEGER NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    expire_at  INTEGER NOT NULL,
    used_at    INTEGER
);
