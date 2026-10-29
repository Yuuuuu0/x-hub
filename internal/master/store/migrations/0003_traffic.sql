ALTER TABLE nodes ADD COLUMN last_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN net_reset_day INTEGER NOT NULL DEFAULT 1;
ALTER TABLE nodes ADD COLUMN net_quota_bytes INTEGER NOT NULL DEFAULT 0;

UPDATE subscriptions SET period_start_at = created_at WHERE period_start_at = 0;

-- 订阅 × 入站 × 小时的流量明细。不建外键，删除订阅或入站后历史仍保留。
CREATE TABLE traffic_hourly (
    hour       INTEGER NOT NULL,
    sub_id     INTEGER NOT NULL,
    inbound_id INTEGER NOT NULL,
    node_id    INTEGER NOT NULL,
    up         INTEGER NOT NULL DEFAULT 0,
    down       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (hour, sub_id, inbound_id)
);
CREATE INDEX traffic_hourly_sub ON traffic_hourly (sub_id, hour);

-- 超过保留期的小时明细按统计时区汇总为天。
CREATE TABLE traffic_daily (
    day        INTEGER NOT NULL,
    sub_id     INTEGER NOT NULL,
    inbound_id INTEGER NOT NULL,
    node_id    INTEGER NOT NULL,
    up         INTEGER NOT NULL DEFAULT 0,
    down       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, sub_id, inbound_id)
);
CREATE INDEX traffic_daily_sub ON traffic_daily (sub_id, day);

-- 服务器物理网卡总流量。
CREATE TABLE node_traffic_hourly (
    hour    INTEGER NOT NULL,
    node_id INTEGER NOT NULL,
    rx      INTEGER NOT NULL DEFAULT 0,
    tx      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (hour, node_id)
);

CREATE TABLE node_traffic_daily (
    day     INTEGER NOT NULL,
    node_id INTEGER NOT NULL,
    rx      INTEGER NOT NULL DEFAULT 0,
    tx      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, node_id)
);
