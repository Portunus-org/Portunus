//go:build !linux

package singbox

import "syscall"

func sysProcAttr() *syscall.SysProcAttr { return nil }
