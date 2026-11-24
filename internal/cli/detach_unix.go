//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// detach starts c in its own session, so the app it starts outlives the
// terminal nota ran in.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
