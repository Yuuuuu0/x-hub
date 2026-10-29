package xray

import "syscall"

// procAttr 让 Xray 在被控端进程意外退出时收到 SIGTERM，避免遗留孤儿进程占用端口。
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
