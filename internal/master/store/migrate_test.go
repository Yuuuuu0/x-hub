package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// TestMigrateMultiAddrs 覆盖 0010 迁移：旧的单个公网地址与入站链接地址、端口转换为地址列表。
func TestMigrateMultiAddrs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_init", "0002_inbounds", "0003_traffic", "0004_admin", "0005_id_seq",
		"0006_profiles", "0007_external", "0008_node_meta", "0009_inbound_order"} {
		body, err := migrations.ReadFile("migrations/" + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, q := range []string{
		"PRAGMA user_version = 9",
		"INSERT INTO nodes (id, name, public_host, created_at) VALUES (1, 'a', 'jp.example.com', 0), (2, 'b', '', 0)",
		`INSERT INTO inbounds (id, node_id, port, protocol, spec_json, link_host, link_port, created_at, updated_at) VALUES
			(1, 1, 443, 'vless', '{}', '', 0, 0, 0), (2, 1, 8443, 'vless', '{}', 'cdn.example.com', 0, 0, 0),
			(3, 1, 9443, 'vless', '{}', '', 20443, 0, 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n1, _ := s.GetNode(1)
	n2, _ := s.GetNode(2)
	if !reflect.DeepEqual(n1.PublicHosts, []PublicAddr{{Host: "jp.example.com"}}) || len(n2.PublicHosts) != 0 {
		t.Fatalf("nodes: %+v %+v", n1.PublicHosts, n2.PublicHosts)
	}
	want := map[int64][]LinkAddr{1: {}, 2: {{Host: "cdn.example.com"}}, 3: {{Port: 20443}}}
	for id, w := range want {
		in, _ := s.GetInbound(id)
		if !reflect.DeepEqual(in.LinkAddrs, w) {
			t.Errorf("inbound %d: %+v, want %+v", id, in.LinkAddrs, w)
		}
	}

	// 未设置公网地址的服务器在被控端接入后以来源 IP 补全，已设置的保持不变。
	s.UpdateNodeRuntime(2, "dev", "", nil, n2.Host, "5.6.7.8")
	s.UpdateNodeRuntime(1, "dev", "", nil, n1.Host, "5.6.7.8")
	n1, _ = s.GetNode(1)
	n2, _ = s.GetNode(2)
	if !reflect.DeepEqual(n2.PublicHosts, []PublicAddr{{Host: "5.6.7.8"}}) || len(n1.PublicHosts) != 1 {
		t.Fatalf("runtime fill: %+v %+v", n1.PublicHosts, n2.PublicHosts)
	}
}
