package xray

import (
	"fmt"
	"slices"
	"testing"
)

const baseCfg = `{"inbounds":[{"tag":"in-1","protocol":"vless","port":443,"settings":{"decryption":"none","clients":[%s]}}],"outbounds":[]}`

func cfg(clients string) []byte { return []byte(fmt.Sprintf(baseCfg, clients)) }

func TestUsersOnlyDiff(t *testing.T) {
	a := `{"email":"s1.i1","id":"u1","flow":"xtls-rprx-vision"}`
	b := `{"email":"s2.i1","id":"u2","flow":"xtls-rprx-vision"}`
	b2 := `{"email":"s2.i1","id":"u2-new","flow":"xtls-rprx-vision"}`

	ops, ok := usersOnlyDiff(cfg(a), cfg(a+","+b))
	if !ok || len(ops) != 1 || ops[0].Add == nil || ops[0].Add.Email != "s2.i1" {
		t.Fatalf("add: %v %+v", ok, ops)
	}
	ops, ok = usersOnlyDiff(cfg(a+","+b), cfg(a))
	if !ok || len(ops) != 1 || ops[0].Remove != "s2.i1" {
		t.Fatalf("remove: %v %+v", ok, ops)
	}
	ops, ok = usersOnlyDiff(cfg(a+","+b), cfg(a+","+b2))
	var kinds []string
	for _, op := range ops {
		if op.Add != nil {
			kinds = append(kinds, "add:"+op.Add.ID)
		} else {
			kinds = append(kinds, "rm:"+op.Remove)
		}
	}
	slices.Sort(kinds)
	if !ok || !slices.Equal(kinds, []string{"add:u2-new", "rm:s2.i1"}) {
		t.Fatalf("change: %v %v", ok, kinds)
	}
	if ops, ok = usersOnlyDiff(cfg(a), cfg(a)); !ok || len(ops) != 0 {
		t.Fatalf("identical: %v %+v", ok, ops)
	}

	changedPort := []byte(`{"inbounds":[{"tag":"in-1","protocol":"vless","port":8443,"settings":{"decryption":"none","clients":[]}}],"outbounds":[]}`)
	if _, ok = usersOnlyDiff(cfg(a), changedPort); ok {
		t.Fatal("port change must require restart")
	}
	added := []byte(`{"inbounds":[{"tag":"in-1","protocol":"vless","port":443,"settings":{"decryption":"none","clients":[]}},{"tag":"in-2","protocol":"vless","port":2,"settings":{"clients":[]}}],"outbounds":[]}`)
	if _, ok = usersOnlyDiff(cfg(""), added); ok {
		t.Fatal("new inbound must require restart")
	}
}

func TestUsersOnlyDiffHysteria(t *testing.T) {
	hy := func(clients string) []byte {
		return []byte(`{"inbounds":[{"tag":"in-3","protocol":"hysteria","port":443,"settings":{"version":2,"clients":[` + clients + `]}}]}`)
	}
	ops, ok := usersOnlyDiff(hy(`{"email":"s1.i3","auth":"a1"}`), hy(`{"email":"s1.i3","auth":"a2"}`))
	if !ok || len(ops) != 2 {
		t.Fatalf("hysteria auth change must be applied online: %v %+v", ok, ops)
	}
	for _, op := range ops {
		if op.Add != nil && (op.Add.Auth != "a2" || account(op.Protocol, op.Add) == nil) {
			t.Fatalf("hysteria add op: %+v", op.Add)
		}
	}
}
