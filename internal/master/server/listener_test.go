package server

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// TestDynamicListener 覆盖 TLS 模式下 HTTPS 正常、明文 HTTP 得到中文说明，以及切换为反向代理后直接提供 HTTP。
func TestDynamicListener(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, _, _ := settings.Load(st, func() string { return "panel1234" })
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	certs := NewCerts(dir, cfg)
	tlsCfg := &tls.Config{GetCertificate: certs.GetCertificate, NextProtos: []string{"http/1.1"}}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, map[bool]string{true: "tls", false: "plain"}[r.TLS != nil])
	}), ErrorLog: slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)}
	go srv.Serve(newDynamicListener(ln, tlsCfg, cfg))
	defer srv.Close()

	get := func(url string) (int, string) {
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		resp, err := c.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get("https://" + addr); code != 200 || body != "tls" {
		t.Fatalf("https: %d %s", code, body)
	}
	if code, body := get("http://" + addr); code != 400 || !strings.Contains(body, "settings proxy") {
		t.Fatalf("plain http in tls mode: %d %s", code, body)
	}
	a := cfg.Access()
	a.TLSMode = settings.TLSProxy
	if err := cfg.SaveAccess(a); err != nil {
		t.Fatal(err)
	}
	if code, body := get("http://" + addr); code != 200 || body != "plain" {
		t.Fatalf("proxy mode: %d %s", code, body)
	}
}
