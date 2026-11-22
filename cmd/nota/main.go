// Command nota is the Nota CLI: local-first markdown notes from the shell.
// See `nota --help`; the commands live in internal/cli.
package main

import (
	"os"

	"github.com/banjo/nota/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
