package importer

import (
	"io/fs"
	"syscall"
	"time"
)

// birth is a file's creation time; zero when the file system has none.
func birth(info fs.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Birthtimespec.Sec > 0 {
		return time.Unix(st.Birthtimespec.Unix())
	}
	return time.Time{}
}
