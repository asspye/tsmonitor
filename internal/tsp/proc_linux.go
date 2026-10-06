package tsp

import (
	"os/exec"
	"syscall"
)

// setProcAttr: tsp получает SIGKILL, если tsmonitor умрёт, — без осиротевших процессов
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
