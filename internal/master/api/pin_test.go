package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
	"github.com/Yuuuuu0/x-hub/internal/shared/tlspin"
)

// TestVerifySelfSignedPin 覆盖公开地址证书与自签公钥一致、不一致（前置代理）以及无法连接三种情况。
func TestVerifySelfSignedPin(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	fp := tlspin.Fingerprint(srv.Certificate())
	ctx := context.Background()

	if err := verifySelfSignedPin(ctx, protocol.Endpoint{URL: srv.URL, FP: fp}); err != nil {
		t.Fatalf("matching pin rejected: %v", err)
	}
	err := verifySelfSignedPin(ctx, protocol.Endpoint{URL: srv.URL, FP: "other"})
	if err == nil || !strings.Contains(err.Error(), "反向代理") {
		t.Fatalf("mismatched pin must be rejected: %v", err)
	}
	if err := verifySelfSignedPin(ctx, protocol.Endpoint{URL: "https://127.0.0.1:1", FP: "other"}); err != nil {
		t.Fatalf("unreachable address must not block: %v", err)
	}
	if err := verifySelfSignedPin(ctx, protocol.Endpoint{URL: srv.URL}); err != nil {
		t.Fatalf("no pin means nothing to verify: %v", err)
	}
}
