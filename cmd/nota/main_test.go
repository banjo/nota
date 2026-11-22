package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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
			// No OS keyring in tests: tokens go to the credentials file.
			e.Setenv("NOTA_KEYRING", "file")
			return os.MkdirAll(filepath.Join(e.WorkDir, "home"), 0o755)
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"exitcode":  cmdExitcode,
			"diskls":    cmdDiskls,
			"waitaddr":  cmdWaitaddr,
			"envstdout": cmdEnvstdout,
			"waitfor":   cmdWaitfor,
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

// waitaddr file NAME waits up to 10 s for a server to write its address
// to file (nota serve --addr-file) and sets $NAME to http://<addr>.
func cmdWaitaddr(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: waitaddr file NAME")
	}
	p := ts.MkAbs(args[0])
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(p); err == nil && strings.HasSuffix(string(data), "\n") {
			ts.Setenv(args[1], "http://"+strings.TrimSpace(string(data)))
			return
		}
	}
	ts.Fatalf("waitaddr: no address in %s after 10s", args[0])
}

// envstdout NAME sets $NAME to the trimmed stdout of the last command.
func cmdEnvstdout(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: envstdout NAME")
	}
	ts.Setenv(args[0], strings.TrimSpace(ts.ReadFile("stdout")))
}

// waitfor file regexp waits up to 10 s until file exists and matches.
func cmdWaitfor(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: waitfor file regexp")
	}
	re, err := regexp.Compile(args[1])
	if err != nil {
		ts.Fatalf("waitfor: %v", err)
	}
	p := ts.MkAbs(args[0])
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(p); err == nil && re.Match(data) {
			return
		}
	}
	data, _ := os.ReadFile(p)
	ts.Fatalf("waitfor: %s does not match %q after 10s; it has %q", args[0], args[1], data)
}
