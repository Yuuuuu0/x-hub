-- 服务器的地区与 VPS 套餐信息，用于节点命名与剩余流量 / 时间展示。
-- country 为手动指定的国家或地区代码（空表示使用被控端自动检测的结果），region 为自定义的地区名称；
-- expire_at 为 VPS 到期时间；net_count_mode 为流量计费方式：both 双向合计、tx 仅出站、max 取较大值。
ALTER TABLE nodes ADD COLUMN country TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN region TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN expire_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN net_count_mode TEXT NOT NULL DEFAULT 'both';
