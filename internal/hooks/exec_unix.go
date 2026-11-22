//go:build unix

package hooks

import (
	"context"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, run string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", run)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// The negative pid is the process group: sh and its children.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd
}
