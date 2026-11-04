-- 服务器与入站支持多个对外地址（如同一台服务器的 IPv4 与 IPv6），订阅中每个地址生成一个节点。
-- public_hosts_json 为 [{host, label}]；link_addrs_json 为 [{host, port, label}]，host 为空表示沿用服务器地址、仅覆盖端口。
ALTER TABLE nodes ADD COLUMN public_hosts_json TEXT NOT NULL DEFAULT '[]';
UPDATE nodes SET public_hosts_json = json_array(json_object('host', public_host, 'label', '')) WHERE public_host != '';
ALTER TABLE nodes DROP COLUMN public_host;

ALTER TABLE inbounds ADD COLUMN link_addrs_json TEXT NOT NULL DEFAULT '[]';
UPDATE inbounds SET link_addrs_json = json_array(json_object('host', link_host, 'port', link_port, 'label', ''))
    WHERE link_host != '' OR link_port != 0;
ALTER TABLE inbounds DROP COLUMN link_host;
ALTER TABLE inbounds DROP COLUMN link_port;
