// Command xhub-agent 是 x-hub 被控端程序，负责托管本机 Xray 并与主控保持长连接。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Yuuuuu0/x-hub/internal/agent/config"
	"github.com/Yuuuuu0/x-hub/internal/agent/conn"
	"github.com/Yuuuuu0/x-hub/internal/agent/ledger"
	"github.com/Yuuuuu0/x-hub/internal/agent/xray"
	"github.com/Yuuuuu0/x-hub/internal/version"
)

const (
	defaultConfig = "/etc/xhub-agent/agent.json"
	defaultData   = "/var/lib/xhub-agent"
)

const usage = `用法：
  xhub-agent run    [--config path]                 启动被控端
  xhub-agent enroll --master URL --token T [--fp F] [--config path] [--data dir]
                                                    用一次性令牌接入主控
  xhub-agent version                                打印版本
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cfgPath := fs.String("config", defaultConfig, "")
	var err error
	switch os.Args[1] {
	case "run":
		fs.Parse(os.Args[2:])
		err = run(*cfgPath)
	case "enroll":
		master := fs.String("master", "", "")
		token := fs.String("token", "", "")
		fp := fs.String("fp", "", "")
		data := fs.String("data", defaultData, "")
		fs.Parse(os.Args[2:])
		if *master == "" || *token == "" {
			fs.Usage()
			os.Exit(2)
		}
		err = conn.Enroll(*master, *token, *fp, *cfgPath, *data)
		if err == nil {
			fmt.Println("接入成功")
		}
	case "version":
		fmt.Println(version.Version)
	default:
		fs.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("读取配置失败：%w", err)
	}
	xm, err := xray.New(cfg.DataDir, log)
	if err != nil {
		return err
	}
	led, err := ledger.Open(filepath.Join(cfg.DataDir, "ledger.json"))
	if err != nil {
		return fmt.Errorf("读取流量账本失败：%w", err)
	}
	collector := conn.NewCollector(led, xm, log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("xhub-agent starting", "version", version.Version, "node", cfg.NodeID)
	done := make(chan struct{})
	go func() {
		xm.Supervise(ctx) // 退出时停止 Xray，停止前会触发一次流量采集
		close(done)
	}()
	go collector.Run(ctx)
	conn.New(cfg, xm, led, log).Run(ctx)
	<-done
	return nil
}
