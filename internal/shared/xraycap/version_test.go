package xraycap

import "testing"

func TestParseAndCompare(t *testing.T) {
	a, ok := ParseVersion("v26.7.28")
	if !ok || a != (Version{26, 7, 28}) {
		t.Fatalf("parse v26.7.28: %v %v", a, ok)
	}
	b, _ := ParseVersion("26.10.21")
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Fatal("compare mismatch")
	}
	old, _ := ParseVersion("v1.8.24")
	if old.Compare(a) != -1 {
		t.Fatal("legacy version should be older")
	}
	if _, ok := ParseVersion("v26.7"); ok {
		t.Fatal("two-part version must be rejected")
	}
	if IsValidTag("26.7.28") || IsValidTag("v26.7.28/../x") || !IsValidTag("v26.7.28") {
		t.Fatal("tag validation mismatch")
	}
}
