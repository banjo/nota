//go:build !darwin

package importer

import (
	"io/fs"
	"time"
)

// birth is a file's creation time: Go reads none here, so it is zero.
func birth(fs.FileInfo) time.Time { return time.Time{} }
