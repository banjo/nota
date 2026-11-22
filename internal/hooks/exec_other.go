//go:build !unix

package hooks

import (
	"context"
	"os/exec"
	"runtime"
)

func shellCommand(ctx context.Context, run string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", run)
	}
	return exec.CommandContext(ctx, "sh", "-c", run)
}
