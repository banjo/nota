package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/banjo/nota/internal/cli"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"nota": func() { os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) },
	})
}

// TestScripts runs the CLI scripts in testdata/script. Set
// NOTA_UPDATE_SCRIPTS=1 to rewrite `cmp stdout` files in the archives.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		UpdateScripts:       os.Getenv("NOTA_UPDATE_SCRIPTS") != "",
		RequireExplicitExec: false,
		Setup: func(e *testscript.Env) error {
			e.Setenv("NOTA_VAULT", filepath.Join(e.WorkDir, "vault"))
			e.Setenv("HOME", filepath.Join(e.WorkDir, "home"))
			e.Setenv("USERPROFILE", filepath.Join(e.WorkDir, "home"))
			e.Setenv("XDG_CONFIG_HOME", filepath.Join(e.WorkDir, ".config"))
			e.Setenv("TZ", "UTC")
			return os.MkdirAll(filepath.Join(e.WorkDir, "home"), 0o755)
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"exitcode": cmdExitcode,
			"diskls":   cmdDiskls,
		},
	})
}

// exitcode N cmd args... runs a command and checks its exit status; its
// stdout and stderr can be checked afterwards as usual.
func cmdExitcode(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exitcode N command [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exitcode: %v", err)
	}
	err = ts.Exec(args[1], args[2:]...)
	got := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		got = ee.ExitCode()
	case err != nil:
		ts.Fatalf("exitcode: %v", err)
	}
	if got != want {
		ts.Fatalf("exit code %d, want %d", got, want)
	}
}

// diskls dir prints every path under dir as the file system stores it,
// quoted with non-ASCII escaped, so NFC and NFD names can be told apart.
func cmdDiskls(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: diskls dir")
	}
	root := ts.MkAbs(args[0])
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if d.IsDir() && d.Name() == ".nota" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		lines = append(lines, strconv.QuoteToASCII(filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		ts.Fatalf("diskls: %v", err)
	}
	fmt.Fprintln(ts.Stdout(), strings.Join(lines, "\n"))
}
