-- 外部节点来源：手动粘贴（content 为原文）或外部订阅（content 为 URL，按 interval 秒拉取）。
-- nodes_json 为解析结果（external.Proxy 列表）；chain 为前置：空、inbound:<入站 ID> 或 group:<分组名>。
CREATE TABLE external_sources (
    id          INTEGER PRIMARY KEY,
    kind        TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    content     TEXT    NOT NULL DEFAULT '',
    user_agent  TEXT    NOT NULL DEFAULT '',
    interval    INTEGER NOT NULL DEFAULT 0,
    prefix      TEXT    NOT NULL DEFAULT '',
    include     TEXT    NOT NULL DEFAULT '',
    exclude     TEXT    NOT NULL DEFAULT '',
    chain       TEXT    NOT NULL DEFAULT '',
    all_subs    INTEGER NOT NULL DEFAULT 0,
    sub_ids     TEXT    NOT NULL DEFAULT '[]',
    nodes_json  TEXT    NOT NULL DEFAULT '[]',
    fetched_at  INTEGER NOT NULL DEFAULT 0,
    fetch_error TEXT    NOT NULL DEFAULT '',
    user_info   TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

INSERT INTO id_seq (name, last) VALUES ('external_sources', 0);
