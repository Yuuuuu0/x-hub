//go:build !linux

package xray

import "syscall"

func procAttr() *syscall.SysProcAttr { return nil }
