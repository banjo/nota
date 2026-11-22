package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// maxOutput caps what is kept of a hook's stdout and stderr.
const maxOutput = 1 << 20

// ShellExec runs h.Run through the shell (sh -c, or cmd /C on Windows)
// in dir. When ctx ends, the whole process group is killed, so a hook
// that started children cannot outlive its timeout. A non-zero exit is
// an error that includes the end of stderr.
func ShellExec(ctx context.Context, h Hook, dir string, env []string, stdin []byte) ([]byte, error) {
	cmd := shellCommand(ctx, h.Run)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	stdout := &capped{max: maxOutput}
	stderr := &capped{max: maxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second // don't hang on pipes held open by orphans
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if msg := strings.TrimSpace(stderr.String()); msg != "" && errors.As(err, &ee) {
			if len(msg) > 500 {
				msg = "…" + msg[len(msg)-500:]
			}
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

// capped is a buffer that drops what goes beyond max bytes.
type capped struct {
	bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
