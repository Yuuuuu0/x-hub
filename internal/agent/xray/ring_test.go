package xray

import (
	"slices"
	"testing"
)

func TestRingTail(t *testing.T) {
	r := newRing(3)
	r.Write([]byte("a\nb\nc"))
	if got := r.Tail(10); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("partial line must wait for newline: %v", got)
	}
	r.Write([]byte("\nd\n"))
	if got := r.Tail(10); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Fatalf("wraparound: %v", got)
	}
	if got := r.Tail(2); !slices.Equal(got, []string{"c", "d"}) {
		t.Fatalf("tail 2: %v", got)
	}
}
