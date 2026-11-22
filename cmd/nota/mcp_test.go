package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMCPStdio runs `nota mcp` as a subprocess and talks JSON-RPC to it
// over its stdin and stdout, as an agent would.
func TestMCPStdio(t *testing.T) {
	nota, err := exec.LookPath("nota") // testscript.Main puts it on $PATH
	if err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(t.TempDir(), "vault")
	cmd := exec.Command(nota, "mcp")
	cmd.Env = append(os.Environ(), "NOTA_VAULT="+vault, "NOTA_NOW=2026-11-22T10:42:00+01:00",
		"XDG_CONFIG_HOME="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	send := func(msg string) {
		t.Helper()
		if _, err := stdin.Write([]byte(msg + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	// recv reads the response to request id and decodes its result.
	recv := func(id int, result any) {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("stdout closed; stderr: %s", stderr.String())
				}
				var resp struct {
					ID     int             `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if err := json.Unmarshal([]byte(l), &resp); err != nil {
					t.Fatalf("bad JSON-RPC line %q: %v", l, err)
				}
				if resp.ID != id {
					continue // a notification
				}
				if resp.Error != nil {
					t.Fatalf("request %d failed: %s", id, resp.Error)
				}
				if err := json.Unmarshal(resp.Result, result); err != nil {
					t.Fatal(err)
				}
				return
			case <-time.After(10 * time.Second):
				t.Fatalf("no response to request %d; stderr: %s", id, stderr.String())
			}
		}
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"go-test","version":"1"}}}`)
	var init struct {
		ServerInfo struct{ Name string } `json:"serverInfo"`
	}
	recv(1, &init)
	if init.ServerInfo.Name != "nota" {
		t.Fatalf("serverInfo %+v", init)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)

	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	var list struct {
		Tools []struct{ Name string } `json:"tools"`
	}
	recv(2, &list)
	var names []string
	for _, tl := range list.Tools {
		names = append(names, tl.Name)
	}
	for _, want := range []string{"search_notes", "read_note", "list_notes", "create_note", "append_to_note",
		"update_note", "update_frontmatter", "move_note", "list_tags", "list_views", "get_backlinks"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools/list lacks %s: %q", want, names)
		}
	}

	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_note","arguments":{"title":"Från MCP","body":"Ett mcpord här.","tags":["mcp"]}}}`)
	var created struct {
		IsError    bool `json:"isError"`
		Structured struct {
			Path string `json:"path"`
		} `json:"structuredContent"`
	}
	recv(3, &created)
	if created.IsError || created.Structured.Path != "Inbox/Från MCP.md" {
		t.Fatalf("create_note %+v", created)
	}
	b, err := os.ReadFile(filepath.Join(vault, "Inbox", "Från MCP.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntags: [mcp]\ncreated: \"2026-11-22T10:42:00+01:00\"\n---\n# Från MCP\n\nEtt mcpord här.\n"
	if string(b) != want {
		t.Fatalf("file %q, want %q", b, want)
	}

	send(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_notes","arguments":{"query":"mcpord"}}}`)
	var found struct {
		Structured struct {
			Count int `json:"count"`
		} `json:"structuredContent"`
	}
	recv(4, &found)
	if found.Structured.Count != 1 {
		t.Fatalf("search_notes %+v", found)
	}

	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("nota mcp exited with %v; stderr: %s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
}
