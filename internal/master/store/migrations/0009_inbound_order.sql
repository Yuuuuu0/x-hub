-- 入站改为全局排序：初始顺序与原订阅输出一致（按服务器排序，同一服务器内按创建先后）。
UPDATE inbounds SET sort = (
    SELECT r FROM (
        SELECT i.id AS id, ROW_NUMBER() OVER (ORDER BY n.sort, n.id, i.id) AS r
        FROM inbounds i JOIN nodes n ON n.id = i.node_id
    ) WHERE id = inbounds.id
);
