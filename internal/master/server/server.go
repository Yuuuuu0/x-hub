package server

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/settings"
)

// Server 持有当前监听与 http.Server。TLS 模式在每个连接 Accept 时按最新设置决定，
// 因此切换 TLS 模式无需重启；仅监听地址变化时才换绑端口。
type Server struct {
	settings *settings.Service
	handler  http.Handler
	tlsCfg   *tls.Config
	log      *slog.Logger

	mu   sync.Mutex
	addr string
	srv  *http.Server
}

// New 创建服务器。
func New(s *settings.Service, certs *Certs, handler http.Handler, log *slog.Logger) *Server {
	return &Server{
		settings: s,
		handler:  handler,
		log:      log,
		tlsCfg: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: certs.GetCertificate,
			NextProtos:     []string{"http/1.1"},
		},
	}
}

// Start 按当前设置绑定端口并开始服务。
func (s *Server) Start() error {
	addr := s.settings.Access().Listen
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.serve(addr, ln)
	return nil
}

// Addr 返回当前监听地址。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Listen 预先绑定新地址，用于保存设置前校验端口可用。
func (s *Server) Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// SwitchTo 在新监听上开始服务，并在 delay 后平滑关闭旧监听（让触发切换的请求先完成响应）。
func (s *Server) SwitchTo(addr string, ln net.Listener, delay time.Duration) {
	s.mu.Lock()
	old := s.srv
	s.mu.Unlock()
	s.serve(addr, ln)
	go func() {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if old != nil {
			old.Shutdown(ctx)
		}
	}()
}

// Shutdown 平滑关闭当前服务。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) serve(addr string, ln net.Listener) {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
	s.mu.Lock()
	s.addr, s.srv = addr, srv
	s.mu.Unlock()
	dl := newDynamicListener(ln, s.tlsCfg, s.settings)
	s.log.Info("listening", "addr", addr)
	go func() {
		if err := srv.Serve(dl); err != nil && err != http.ErrServerClosed {
			s.log.Error("serve", "addr", addr, "err", err)
		}
	}()
}
