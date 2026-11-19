package api

import (
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

func TestCheckXrayVersion(t *testing.T) {
	hy := &store.Inbound{Protocol: model.ProtoHysteria}
	for ver, ok := range map[string]bool{"": true, "v26.3.27": true, "26.11.9": true, "v26.2.6": false, "v25.12.8": false} {
		if err := checkXrayVersion(&store.Node{XrayVersion: ver}, hy); (err == nil) != ok {
			t.Errorf("version %q: err=%v", ver, err)
		}
	}
	if err := checkXrayVersion(&store.Node{XrayVersion: "v25.1.1"}, &store.Inbound{Protocol: model.ProtoVLESS}); err != nil {
		t.Errorf("non-hysteria inbound must not be restricted: %v", err)
	}
	if portNetwork(model.ProtoHysteria) != "udp" || portNetwork(model.ProtoTrojan) != "tcp" {
		t.Error("port network")
	}
}
