//go:build windows

package agent

import (
	"os"
	"os/exec"

	"github.com/pengmide/lumi/internal/sysutil"
)

func configureCommand(cmd *exec.Cmd) {
	sysutil.HideWindow(cmd)
}

func hideWindow(cmd *exec.Cmd) {
	sysutil.HideWindow(cmd)
}

func interruptProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Signal(os.Interrupt)
}

func killProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}
