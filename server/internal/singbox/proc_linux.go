package singbox

import "syscall"

// sysProcAttr makes the kernel stop sing-box if the panel dies, so an orphan
// never keeps the proxy ports bound with a stale config.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
