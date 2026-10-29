-- 分流方案：分组、规则与各客户端基础配置，spec_json 对应 routing.Profile。
CREATE TABLE profiles (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    note       TEXT    NOT NULL DEFAULT '',
    spec_json  TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 规则集库：托管（content）或外部链接（url），被分流方案按名称引用。
CREATE TABLE rule_sets (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    behavior   TEXT    NOT NULL,
    source     TEXT    NOT NULL,
    url        TEXT    NOT NULL DEFAULT '',
    format     TEXT    NOT NULL DEFAULT '',
    interval   INTEGER NOT NULL DEFAULT 86400,
    content    TEXT    NOT NULL DEFAULT '',
    note       TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);

-- 0 表示使用内置精简方案。
ALTER TABLE subscriptions ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0;

INSERT INTO id_seq (name, last) VALUES ('profiles', 0), ('rule_sets', 0);
