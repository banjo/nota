//go:build !unix

package cli

import "os/exec"

func detach(*exec.Cmd) {}
