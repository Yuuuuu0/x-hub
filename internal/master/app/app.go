// Package app 组装主控各组件，并实现主控的命令行子命令。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Yuuuuu0/x-hub/deploy"
	"github.com/Yuuuuu0/x-hub/internal/master/accounting"
	"github.com/Yuuuuu0/x-hub/internal/master/api"
	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/server"
	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/syncer"
	"github.com/Yuuuuu0/x-hub/internal/master/xrayrel"
	"github.com/Yuuuuu0/x-hub/internal/version"
	"github.com/Yuuuuu0/x-hub/web"
)

const defaultUsername = "admin"

// Options 为主控启动参数。
type Options struct {
	DataDir  string
	AgentDir string
}

func newPanelPath() string { return auth.RandomString(12) }

func openStore(dataDir string) (*store.Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(dataDir, "xhub.db"))
}

// Run 启动主控服务，直到收到退出信号。
func Run(opts Options) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := openStore(opts.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	cfg, _, err := settings.Load(st, newPanelPath)
	if err != nil {
		return err
	}
	password, err := ensureAdmin(st)
	if err != nil {
		return err
	}
	if password != "" { // 首次启动：为个人自用预置一个订阅
		if _, err := api.CreateSubscription(st, "自己", ""); err != nil {
			return err
		}
	}

	certs := server.NewCerts(filepath.Join(opts.DataDir, "tls"), cfg)
	if cfg.Access().TLSMode == settings.TLSSelfSigned {
		if _, err := certs.SelfSigned(); err != nil {
			return err
		}
	}

	h := &api.Handler{
		Store:         st,
		Settings:      cfg,
		Certs:         certs,
		Releases:      xrayrel.New(filepath.Join(opts.DataDir, "cache", "xray")),
		Log:           log,
		Web:           web.Dist(),
		DataDir:       opts.DataDir,
		InContainer:   inContainer(),
		AgentDir:      opts.AgentDir,
		InstallScript: deploy.InstallScript,
	}
	h.Hub = hub.New(st, log, hub.Hooks{Endpoint: h.Endpoint, OnHello: h.OnAgentHello})
	h.Syncer = syncer.New(st, h.Hub, log)
	h.Syncer.SetACME(func() (string, string) { c := cfg.Certs(); return c.ACMEEmail, c.CloudflareToken })
	h.Accounting = accounting.New(st, cfg, h.Syncer, log)
	h.Hub.SetTrafficHooks(h.Accounting.LastSeq, h.Accounting.Ingest)
	h.Init()
	h.Server = server.New(cfg, certs, h, log)
	if err := h.Server.Start(); err != nil {
		return fmt.Errorf("监听 %s 失败：%w（可执行 settings reset-access 恢复默认访问配置）", cfg.Access().Listen, err)
	}
	printBanner(cfg.Access(), password, h.InContainer)
	h.Syncer.TriggerAll() // 预先编译各服务器期望配置，用于同步状态展示
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go cleanupLoop(ctx, st, log)
	go h.Accounting.Run(ctx)
	go h.RunExternalRefresh(ctx)
	<-ctx.Done()

	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.Server.Shutdown(sctx)
}

// ensureAdmin 首次启动时创建管理员，返回生成的初始密码（已存在则为空）。
func ensureAdmin(st *store.Store) (string, error) {
	n, err := st.CountAdmins()
	if err != nil || n > 0 {
		return "", err
	}
	pw := auth.RandomString(16)
	return pw, st.CreateAdmin(defaultUsername, auth.HashPassword(pw))
}

// inContainer 判断是否运行在 Docker 容器中。
func inContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func printBanner(a settings.Access, password string, container bool) {
	scheme := "https"
	if a.TLSMode == settings.TLSProxy {
		scheme = "http"
	}
	base := a.PublicURL
	if base == "" {
		host, port, _ := net.SplitHostPort(a.Listen)
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "<服务器IP>"
		}
		if container {
			// 容器内监听端口不等于对外端口，以 compose 中映射的宿主机端口为准
			host, port = "<服务器IP>", "<映射的宿主机端口>"
		}
		base = fmt.Sprintf("%s://%s:%s", scheme, host, port)
	}
	fmt.Println("────────────────────────────────────────────")
	fmt.Printf(" x-hub %s\n", version.Version)
	fmt.Printf(" 面板地址：%s/%s/\n", base, a.PanelPath)
	if password != "" {
		fmt.Printf(" 初始账号：%s\n 初始密码：%s\n", defaultUsername, password)
		fmt.Println(" 请登录后在「设置 → 账户」中修改密码")
	}
	fmt.Println("────────────────────────────────────────────")
}

func cleanupLoop(ctx context.Context, st *store.Store, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := st.DeleteExpiredSessions(); err != nil {
				log.Warn("cleanup sessions", "err", err)
			}
		}
	}
}

// SettingsShow 打印当前访问配置。
func SettingsShow(dataDir string) error {
	st, err := openStore(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	cfg, _, err := settings.Load(st, newPanelPath)
	if err != nil {
		return err
	}
	a := cfg.Access()
	fmt.Printf("监听地址：%s\n面板路径：/%s/\n公开地址：%s\nTLS 模式：%s\n", a.Listen, a.PanelPath, orDash(a.PublicURL), a.TLSMode)
	if a.TLSMode == settings.TLSFile {
		fmt.Printf("证书文件：%s\n私钥文件：%s\n", a.CertFile, a.KeyFile)
	}
	if len(a.TrustedProxies) > 0 {
		fmt.Printf("可信代理：%s\n", strings.Join(a.TrustedProxies, ", "))
	}
	return nil
}

// SettingsProxy 把访问配置切换为反向代理模式：主控改为提供 HTTP，由前置 Nginx / Caddy 终结 TLS。
// trusted 为空时信任本机，运行在容器中时另信任 Docker 默认网段（经端口映射访问时的来源地址）。
func SettingsProxy(dataDir, publicURL string, trusted []string) error {
	if publicURL == "" {
		return fmt.Errorf("需要 --public-url，例如 --public-url https://hub.example.com")
	}
	st, err := openStore(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	cfg, _, err := settings.Load(st, newPanelPath)
	if err != nil {
		return err
	}
	if len(trusted) == 0 {
		trusted = []string{"127.0.0.1", "::1"}
		if inContainer() {
			trusted = append(trusted, "172.16.0.0/12")
		}
	}
	a := cfg.Access()
	a.TLSMode, a.PublicURL, a.TrustedProxies = settings.TLSProxy, publicURL, trusted
	a.CertFile, a.KeyFile = "", ""
	if err := cfg.SaveAccess(a); err != nil {
		return err
	}
	a = cfg.Access()
	fmt.Printf("已切换为反向代理模式，重启主控后生效（Docker：docker restart xhub）。\n  面板地址：%s/%s/\n  可信代理：%s\n",
		a.PublicURL, a.PanelPath, strings.Join(a.TrustedProxies, ", "))
	fmt.Println("  反向代理需转发到主控的 HTTP 端口，并支持 WebSocket（被控端接入使用 /agent/ws）。")
	return nil
}

// SettingsResetAccess 把访问配置恢复为 :2096 + 自签证书 + 新随机面板路径，保留公开地址。
func SettingsResetAccess(dataDir string) error {
	st, err := openStore(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	cfg, _, err := settings.Load(st, newPanelPath)
	if err != nil {
		return err
	}
	a := cfg.Access()
	a.Listen, a.TLSMode, a.PanelPath = settings.DefaultListen, settings.TLSSelfSigned, newPanelPath()
	a.CertFile, a.KeyFile, a.TrustedProxies = "", "", nil
	if err := cfg.SaveAccess(a); err != nil {
		return err
	}
	fmt.Printf("已恢复默认访问配置，重启主控后生效：\n  https://<服务器IP>:2096/%s/\n", a.PanelPath)
	return nil
}

// AdminReset 重置管理员密码并清除全部会话。
func AdminReset(dataDir string) error {
	st, err := openStore(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	admin, err := st.FirstAdmin()
	if err != nil {
		return err
	}
	pw := auth.RandomString(16)
	if err := st.UpdateAdminCredentials(admin.ID, admin.Username, auth.HashPassword(pw), 0); err != nil {
		return err
	}
	if err := st.SetAdminTOTP(admin.ID, ""); err != nil {
		return err
	}
	if err := st.DeleteAllSessions(); err != nil {
		return err
	}
	fmt.Printf("管理员密码已重置，两步验证已关闭：\n  账号：%s\n  密码：%s\n", admin.Username, pw)
	return nil
}

// Restore 用备份文件替换当前数据库，原数据库保留为 .bak 副本。需在主控停止时执行。
func Restore(dataDir, file string) error {
	probe, err := store.Open(file)
	if err != nil {
		return fmt.Errorf("备份文件无效：%w", err)
	}
	_, err = probe.FirstAdmin()
	probe.Close()
	if err != nil {
		return fmt.Errorf("备份文件无效：%w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	db := filepath.Join(dataDir, "xhub.db")
	if _, err := os.Stat(db); err == nil {
		bak := db + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.Rename(db, bak); err != nil {
			return err
		}
		fmt.Println("原数据库已保留为", bak)
	}
	os.Remove(db + "-wal")
	os.Remove(db + "-shm")
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := os.WriteFile(db, data, 0o600); err != nil {
		return err
	}
	fmt.Println("恢复完成，启动主控后生效")
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
