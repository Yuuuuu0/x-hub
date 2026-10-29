// Command xhub-master 是 x-hub 主控程序。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	_ "time/tzdata" // distroless 镜像不含时区数据，统计时区依赖它

	"github.com/Yuuuuu0/x-hub/internal/master/app"
	"github.com/Yuuuuu0/x-hub/internal/version"
)

const usage = `用法：
  xhub-master [run]               启动主控
  xhub-master settings show       打印当前访问配置
  xhub-master settings reset-access  恢复默认访问配置（:2096 + 自签证书 + 新面板路径）
  xhub-master admin reset         重置管理员密码并关闭两步验证
  xhub-master restore <备份文件>  用备份恢复数据库（需先停止主控）
  xhub-master version             打印版本

通用参数：
  --data <dir>    数据目录（默认 ./data）
  --agents <dir>  被控端二进制目录（默认为可执行文件同级的 agents）
`

func main() {
	args := os.Args[1:]
	var cmd []string
	for len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd = append(cmd, args[0])
		args = args[1:]
	}
	fs := flag.NewFlagSet("xhub-master", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	dataDir := fs.String("data", "./data", "")
	agentDir := fs.String("agents", defaultAgentDir(), "")
	fs.Parse(args)

	var err error
	if len(cmd) == 2 && cmd[0] == "restore" {
		if err := app.Restore(*dataDir, cmd[1]); err != nil {
			fmt.Fprintln(os.Stderr, "错误：", err)
			os.Exit(1)
		}
		return
	}
	switch fmt.Sprint(cmd) {
	case "[]", "[run]":
		err = app.Run(app.Options{DataDir: *dataDir, AgentDir: *agentDir})
	case "[settings show]":
		err = app.SettingsShow(*dataDir)
	case "[settings reset-access]":
		err = app.SettingsResetAccess(*dataDir)
	case "[admin reset]":
		err = app.AdminReset(*dataDir)
	case "[version]":
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

func defaultAgentDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "agents"
	}
	return filepath.Join(filepath.Dir(exe), "agents")
}
