//go:build !unix

package vault

import "errors"

func mkfifo(string) error { return errors.New("not supported") }
