-- ID 高水位：SQLite 会复用被删除的最大 ID，而流量历史与 Xray 统计标识按 ID 关联，
-- 复用会把已删除对象的历史算到新对象上。新建记录时 ID 取高水位 + 1，永不复用。
CREATE TABLE id_seq (
    name TEXT PRIMARY KEY,
    last INTEGER NOT NULL
);

INSERT INTO id_seq (name, last) VALUES
    ('nodes', MAX(
        COALESCE((SELECT MAX(id) FROM nodes), 0),
        COALESCE((SELECT MAX(node_id) FROM traffic_hourly), 0),
        COALESCE((SELECT MAX(node_id) FROM traffic_daily), 0),
        COALESCE((SELECT MAX(node_id) FROM node_traffic_hourly), 0),
        COALESCE((SELECT MAX(node_id) FROM node_traffic_daily), 0))),
    ('inbounds', MAX(
        COALESCE((SELECT MAX(id) FROM inbounds), 0),
        COALESCE((SELECT MAX(inbound_id) FROM traffic_hourly), 0),
        COALESCE((SELECT MAX(inbound_id) FROM traffic_daily), 0))),
    ('subscriptions', MAX(
        COALESCE((SELECT MAX(id) FROM subscriptions), 0),
        COALESCE((SELECT MAX(sub_id) FROM traffic_hourly), 0),
        COALESCE((SELECT MAX(sub_id) FROM traffic_daily), 0)));
