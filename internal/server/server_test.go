package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

type env struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	log *lockedWriter
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	buf := &lockedWriter{w: &bytes.Buffer{}}
	cfg.Logger = slog.New(slog.NewJSONHandler(buf, nil))
	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		srv.broker.close()
		ts.Close()
		srv.Close()
	})
	return &env{t: t, srv: srv, ts: ts, log: buf}
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

// user creates a user with a token and a vault named "main".
func (e *env) user(email string) string {
	e.t.Helper()
	ctx := context.Background()
	u, err := e.srv.Store().AddUser(ctx, email)
	if err != nil {
		e.t.Fatal(err)
	}
	tok, _, err := e.srv.Store().CreateToken(ctx, u.ID, "dev-"+strings.Split(email, "@")[0])
	if err != nil {
		e.t.Fatal(err)
	}
	if res, body := e.do(tok, "POST", protocol.VaultsPath(), nil, `{"name":"main"}`); res.StatusCode != 201 {
		e.t.Fatalf("create vault: %d %s", res.StatusCode, body)
	}
	return tok
}

func (e *env) do(tok, method, path string, hdr map[string]string, body string) (*http.Response, []byte) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set(protocol.HeaderProtocol, "1")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func (e *env) put(tok, p string, base int64, content string) (int, protocol.Entry, protocol.Error) {
	e.t.Helper()
	res, body := e.do(tok, "PUT", protocol.FilePath("main", p), map[string]string{protocol.HeaderBaseVersion: strconv.FormatInt(base, 10)}, content)
	return decode(e.t, res, body)
}

func decode(t *testing.T, res *http.Response, body []byte) (int, protocol.Entry, protocol.Error) {
	t.Helper()
	var ent protocol.Entry
	var perr protocol.Error
	if res.StatusCode == 200 {
		if err := json.Unmarshal(body, &ent); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
	} else if err := json.Unmarshal(body, &perr); err != nil {
		t.Fatalf("%d %s: %v", res.StatusCode, body, err)
	}
	return res.StatusCode, ent, perr
}

func (e *env) changes(tok string, since int64, limit int) protocol.Changes {
	e.t.Helper()
	res, body := e.do(tok, "GET", protocol.ChangesPath("main")+"?since="+strconv.FormatInt(since, 10)+"&limit="+strconv.Itoa(limit), nil, "")
	if res.StatusCode != 200 {
		e.t.Fatalf("changes: %d %s", res.StatusCode, body)
	}
	var c protocol.Changes
	if err := json.Unmarshal(body, &c); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func TestOptimisticConcurrency(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("anton@example.com")

	st, v1, _ := e.put(tok, "Plan.md", 0, "one\n")
	if st != 200 || v1.Version != 1 || v1.Hash != protocol.Hash([]byte("one\n")) || v1.Kind != protocol.KindMD || v1.DeviceID != "dev-anton" {
		t.Fatalf("create: %d %+v", st, v1)
	}
	st, v2, _ := e.put(tok, "Plan.md", 1, "two\n")
	if st != 200 || v2.Version != 2 {
		t.Fatalf("edit: %d %+v", st, v2)
	}
	// A stale base is refused with the current state.
	st, _, perr := e.put(tok, "Plan.md", 1, "stale\n")
	if st != 409 || perr.Code != protocol.CodeVersionMismatch || perr.Current == nil || perr.Current.Version != 2 {
		t.Fatalf("stale: %d %+v", st, perr)
	}
	// Creating what exists is a mismatch too.
	if st, _, perr = e.put(tok, "Plan.md", 0, "again\n"); st != 409 || perr.Code != protocol.CodeVersionMismatch {
		t.Fatalf("create existing: %d %+v", st, perr)
	}
	// The same content again is an idempotent no-op.
	if st, v, _ := e.put(tok, "Plan.md", 2, "two\n"); st != 200 || v.Version != 2 {
		t.Fatalf("retry: %d %+v", st, v)
	}
	// GET returns the content and the version headers.
	res, body := e.do(tok, "GET", protocol.FilePath("main", "Plan.md"), nil, "")
	if res.StatusCode != 200 || string(body) != "two\n" || res.Header.Get(protocol.HeaderVersion) != "2" ||
		res.Header.Get(protocol.HeaderHash) != v2.Hash || res.Header.Get(protocol.HeaderProtocol) != "1" {
		t.Fatalf("get: %d %q %v", res.StatusCode, body, res.Header)
	}
	// Missing Base-Version is a bad request.
	res, body = e.do(tok, "PUT", protocol.FilePath("main", "x.md"), nil, "x")
	if res.StatusCode != 400 || !strings.Contains(string(body), "Base-Version") {
		t.Fatalf("no base: %d %s", res.StatusCode, body)
	}
	// A wrong Nota-Hash is refused.
	res, body = e.do(tok, "PUT", protocol.FilePath("main", "x.md"), map[string]string{
		protocol.HeaderBaseVersion: "0", protocol.HeaderHash: protocol.Hash([]byte("other"))}, "x")
	if res.StatusCode != 400 || !strings.Contains(string(body), protocol.CodeHashMismatch) {
		t.Fatalf("hash: %d %s", res.StatusCode, body)
	}
}

func TestTombstonesAndHistory(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("a@example.com")
	e.put(tok, "Möte.md", 0, "v1\n")
	e.put(tok, "Möte.md", 1, "v2\n")

	del := func(base string) (*http.Response, []byte) {
		return e.do(tok, "DELETE", protocol.FilePath("main", "Möte.md"), map[string]string{protocol.HeaderBaseVersion: base}, "")
	}
	if res, body := del("1"); res.StatusCode != 409 {
		t.Fatalf("stale delete: %d %s", res.StatusCode, body)
	}
	res, body := del("2")
	st, tomb, _ := decode(t, res, body)
	if st != 200 || !tomb.Deleted || tomb.Version != 3 || tomb.Hash != protocol.Hash([]byte("v2\n")) {
		t.Fatalf("delete: %d %+v", st, tomb)
	}
	if res, body := del("3"); res.StatusCode != 410 {
		t.Fatalf("delete twice: %d %s", res.StatusCode, body)
	}
	res, body = e.do(tok, "GET", protocol.FilePath("main", "Möte.md"), nil, "")
	if _, _, perr := decode(t, res, body); res.StatusCode != 410 || perr.Code != protocol.CodeDeleted || perr.Current.Version != 3 {
		t.Fatalf("get deleted: %d %s", res.StatusCode, body)
	}
	// Old content stays reachable.
	res, body = e.do(tok, "GET", protocol.FilePath("main", "Möte.md")+"?version=1", nil, "")
	if res.StatusCode != 200 || string(body) != "v1\n" {
		t.Fatalf("get v1: %d %s", res.StatusCode, body)
	}
	// Recreate after delete with base 0 (or the tombstone's version).
	if st, v, _ := e.put(tok, "Möte.md", 0, "v3\n"); st != 200 || v.Version != 4 {
		t.Fatalf("recreate: %d %+v", st, v)
	}

	// A move carries the history along.
	res, body = e.do(tok, "POST", protocol.MovesPath("main"), nil, `{"from":"Möte.md","to":"Arkiv/Möte.md","base_version":4}`)
	if res.StatusCode != 200 {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	var mv protocol.MoveResult
	json.Unmarshal(body, &mv)
	if !mv.From.Deleted || mv.To.MovedFrom != "Möte.md" || mv.To.Version != 6 {
		t.Fatalf("move result: %+v", mv)
	}
	res, body = e.do(tok, "GET", protocol.HistoryPath("main", "Arkiv/Möte.md"), nil, "")
	var h protocol.History
	json.Unmarshal(body, &h)
	var got []string
	for _, v := range h.Versions {
		got = append(got, v.Path+"@"+strconv.FormatInt(v.Version, 10)+map[bool]string{true: "x", false: ""}[v.Deleted])
	}
	if want := "Arkiv/Möte.md@6 Möte.md@5x Möte.md@4 Möte.md@3x Möte.md@2 Möte.md@1"; strings.Join(got, " ") != want {
		t.Fatalf("history = %v, want %s (%d %s)", got, want, res.StatusCode, body)
	}
	// Moving onto an existing file is refused.
	e.put(tok, "b.md", 0, "b")
	res, body = e.do(tok, "POST", protocol.MovesPath("main"), nil, `{"from":"b.md","to":"Arkiv/Möte.md","base_version":7}`)
	if res.StatusCode != 409 || !strings.Contains(string(body), protocol.CodeExists) {
		t.Fatalf("move onto existing: %d %s", res.StatusCode, body)
	}
}

func TestChangesPagination(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("a@example.com")
	for i := range 7 {
		e.put(tok, "n"+strconv.Itoa(i)+".md", 0, "x"+strconv.Itoa(i))
	}
	e.put(tok, "n0.md", 1, "edited") // n0 moves to the end with version 8

	var all []string
	cursor := int64(0)
	pages := 0
	for {
		c := e.changes(tok, cursor, 3)
		pages++
		for _, en := range c.Entries {
			all = append(all, en.Path)
		}
		cursor = c.Cursor
		if !c.More {
			break
		}
	}
	if pages != 3 || cursor != 8 || strings.Join(all, ",") != "n1.md,n2.md,n3.md,n4.md,n5.md,n6.md,n0.md" {
		t.Fatalf("pages=%d cursor=%d %v", pages, cursor, all)
	}
	if c := e.changes(tok, 8, 3); len(c.Entries) != 0 || c.Cursor != 8 || c.More {
		t.Fatalf("caught up: %+v", c)
	}
	if c := e.changes(tok, 6, 100); len(c.Entries) != 2 || c.Entries[0].Path != "n6.md" {
		t.Fatalf("since 6: %+v", c)
	}
}

func TestIsolation(t *testing.T) {
	e := newEnv(t, Config{})
	a := e.user("a@example.com")
	b := e.user("b@example.com") // also has a vault named "main"
	e.put(a, "secret.md", 0, "a's secret")

	// B's "main" is B's own: empty, and A's file is not there.
	if c := e.changes(b, 0, 100); len(c.Entries) != 0 {
		t.Fatalf("b sees %+v", c.Entries)
	}
	res, _ := e.do(b, "GET", protocol.FilePath("main", "secret.md"), nil, "")
	if res.StatusCode != 404 {
		t.Fatalf("b reads a's file: %d", res.StatusCode)
	}
	// A vault only A has is invisible to B on every route.
	e.do(a, "POST", protocol.VaultsPath(), nil, `{"name":"private"}`)
	e.do(a, "PUT", protocol.FilePath("private", "x.md"), map[string]string{protocol.HeaderBaseVersion: "0"}, "x")
	for _, r := range []struct{ method, path, body string }{
		{"GET", protocol.ChangesPath("private"), ""},
		{"GET", protocol.FilePath("private", "x.md"), ""},
		{"PUT", protocol.FilePath("private", "x.md"), "overwrite"},
		{"DELETE", protocol.FilePath("private", "x.md"), ""},
		{"GET", protocol.HistoryPath("private", "x.md"), ""},
		{"POST", protocol.MovesPath("private"), `{"from":"x.md","to":"y.md","base_version":1}`},
		{"GET", protocol.EventsPath("private"), ""},
	} {
		res, body := e.do(b, r.method, r.path, map[string]string{protocol.HeaderBaseVersion: "1"}, r.body)
		if res.StatusCode != 404 {
			t.Errorf("b %s %s: %d %s", r.method, r.path, res.StatusCode, body)
		}
	}
	res, body := e.do(b, "GET", protocol.VaultsPath(), nil, "")
	if strings.Contains(string(body), "private") || res.StatusCode != 200 {
		t.Fatalf("b lists %s", body)
	}
	// A still has the file untouched.
	res, body = e.do(a, "GET", protocol.FilePath("private", "x.md"), nil, "")
	if res.StatusCode != 200 || string(body) != "x" {
		t.Fatalf("a: %d %s", res.StatusCode, body)
	}
}

func TestAuth(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("a@example.com")
	for _, h := range []string{"", "Bearer ", "Bearer nota_nope", "Basic x"} {
		res, body := e.do("", "GET", protocol.VaultsPath(), map[string]string{"Authorization": h}, "")
		if res.StatusCode != 401 || !strings.Contains(string(body), protocol.CodeUnauthorized) {
			t.Errorf("auth %q: %d %s", h, res.StatusCode, body)
		}
	}
	ts, _ := e.srv.Store().Tokens(context.Background(), 0)
	if len(ts) != 1 || ts[0].LastUsedAt == nil {
		t.Fatalf("tokens: %+v", ts)
	}
	if err := e.srv.Store().RevokeToken(context.Background(), ts[0].ID); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.do(tok, "GET", protocol.VaultsPath(), nil, ""); res.StatusCode != 401 {
		t.Fatalf("revoked token: %d", res.StatusCode)
	}
}

func TestProtocolVersion(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("a@example.com")
	for _, v := range []string{"", "2", "0", "banana"} {
		res, body := e.do(tok, "GET", protocol.VaultsPath(), map[string]string{protocol.HeaderProtocol: v}, "")
		var perr protocol.Error
		json.Unmarshal(body, &perr)
		if res.StatusCode != http.StatusUpgradeRequired || perr.Code != protocol.CodeUnsupportedProtocol ||
			len(perr.Supported) != 1 || perr.Supported[0] != 1 || !strings.Contains(perr.Message, "protocol") {
			t.Errorf("version %q: %d %s", v, res.StatusCode, body)
		}
	}
	if res, _ := e.do(tok, "GET", protocol.VaultsPath(), map[string]string{protocol.HeaderProtocol: "1.3"}, ""); res.StatusCode != 200 {
		t.Fatalf("1.3: %d", res.StatusCode)
	}
	// The query parameter works for clients that can't set headers.
	if res, _ := e.do(tok, "GET", protocol.VaultsPath()+"?protocol=1", map[string]string{protocol.HeaderProtocol: ""}, ""); res.StatusCode != 200 {
		t.Fatalf("?protocol=1: %d", res.StatusCode)
	}
}

func TestCaseCollision(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("a@example.com")
	e.put(tok, "Projekt/Plan.md", 0, "x")
	for _, p := range []string{"Projekt/plan.md", "projekt/Other.md", "PROJEKT/x/y.md"} {
		st, _, perr := e.put(tok, p, 0, "y")
		if st != 409 || perr.Code != protocol.CodeCaseCollision || perr.Existing != "Projekt/Plan.md" {
			t.Errorf("%s: %d %+v", p, st, perr)
		}
	}
	// NFD in the URL is the same path as NFC.
	e.put(tok, "Möte.md", 0, "nfc")
	st, _, perr := e.put(tok, "Mo\u0308te.md", 0, "nfd")
	if st != 409 || perr.Code != protocol.CodeVersionMismatch {
		t.Fatalf("NFD put: %d %+v", st, perr)
	}
	// Other files in the same folder are fine; so is a case-only rename.
	if st, _, _ := e.put(tok, "Projekt/Other.md", 0, "z"); st != 200 {
		t.Fatalf("sibling: %d", st)
	}
	res, body := e.do(tok, "POST", protocol.MovesPath("main"), nil, `{"from":"Projekt/Plan.md","to":"Projekt/PLAN.md","base_version":1}`)
	if res.StatusCode != 200 {
		t.Fatalf("case rename: %d %s", res.StatusCode, body)
	}
	// After a delete the name is free in any case.
	e.put(tok, "a.md", 0, "a")
	c := e.changes(tok, 0, 100)
	base := c.Entries[len(c.Entries)-1].Version
	e.do(tok, "DELETE", protocol.FilePath("main", "a.md"), map[string]string{protocol.HeaderBaseVersion: strconv.FormatInt(base, 10)}, "")
	if st, _, perr := e.put(tok, "A.md", 0, "A"); st != 200 {
		t.Fatalf("after delete: %d %+v", st, perr)
	}
	// Bad paths are refused (ServeMux already redirects "a/../b" away).
	res, body = e.do(tok, "PUT", "/v1/vaults/main/files/a%5Cb.md", map[string]string{protocol.HeaderBaseVersion: "0"}, "x")
	if res.StatusCode != 400 || !strings.Contains(string(body), protocol.CodeInvalidPath) {
		t.Fatalf("backslash: %d %s", res.StatusCode, body)
	}
}

func TestBlobDedupeAndSizeLimit(t *testing.T) {
	dir := t.TempDir()
	e := newEnv(t, Config{DataDir: dir, MaxFileSize: 1000})
	tok := e.user("a@example.com")
	img := strings.Repeat("\x89PNG", 100)
	for _, p := range []string{"attachments/a.png", "attachments/b.png", "copy.png"} {
		if st, en, perr := e.put(tok, p, 0, img); st != 200 || en.Kind != protocol.KindBlob {
			t.Fatalf("%s: %d %+v", p, st, perr)
		}
	}
	var n int
	if err := e.srv.blobs.Walk(context.Background(), func(BlobInfo) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d blobs stored, want 1", n)
	}
	st, _, perr := e.put(tok, "big.png", 0, strings.Repeat("x", 1001))
	if st != 413 || perr.Code != protocol.CodeTooLarge {
		t.Fatalf("too large: %d %+v", st, perr)
	}
	if st, _, _ := e.put(tok, "edge.png", 0, strings.Repeat("x", 1000)); st != 200 {
		t.Fatalf("at the limit: %d", st)
	}
	// Chunked bodies (no Content-Length) are limited too.
	req, _ := http.NewRequest("PUT", e.ts.URL+protocol.FilePath("main", "chunked.png"), io.MultiReader(strings.NewReader(strings.Repeat("y", 2000))))
	req.ContentLength = -1
	req.Header.Set(protocol.HeaderProtocol, "1")
	req.Header.Set(protocol.HeaderBaseVersion, "0")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 413 {
		t.Fatalf("chunked: %d", res.StatusCode)
	}
}

func TestSSE(t *testing.T) {
	e := newEnv(t, Config{KeepAlive: 50 * time.Millisecond})
	tok := e.user("a@example.com")
	e.put(tok, "a.md", 0, "a")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.ts.URL+protocol.EventsPath("main")+"?protocol=1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: %d %v", res.StatusCode, res.Header)
	}
	sc := bufio.NewScanner(res.Body)
	next := func() (string, protocol.ChangeEvent) {
		var ev string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				var c protocol.ChangeEvent
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &c)
				return ev, c
			case line == ": keepalive":
				return "keepalive", protocol.ChangeEvent{}
			}
		}
		t.Fatalf("stream ended: %v", sc.Err())
		return "", protocol.ChangeEvent{}
	}
	if ev, c := next(); ev != "change" || c.Cursor != 1 {
		t.Fatalf("hello: %s %+v", ev, c)
	}
	go e.put(tok, "b.md", 0, "b")
	for {
		ev, c := next()
		if ev == "keepalive" {
			continue
		}
		if ev != "change" || c.Cursor != 2 {
			t.Fatalf("nudge: %s %+v", ev, c)
		}
		break
	}
	if ev, _ := next(); ev != "keepalive" {
		t.Fatalf("want keepalive, got %s", ev)
	}
}

func TestWideEventLog(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("anton@example.com")
	e.put(tok, "x.md", 0, "hello")
	// The event is logged right after the response is written.
	for i := 0; i < 100 && !strings.Contains(e.log.String(), `"method":"PUT"`); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	var found bool
	for _, line := range strings.Split(e.log.String(), "\n") {
		if !strings.Contains(line, `"method":"PUT"`) {
			continue
		}
		var m map[string]any
		json.Unmarshal([]byte(line), &m)
		found = m["route"] == "PUT /v1/vaults/{vault}/files/{path...}" && m["user"] == "anton@example.com" &&
			m["vault"] == "main" && m["status"] == float64(200) && m["bytes_in"] == float64(5) && m["duration"] != nil
		if !found {
			t.Fatalf("log line: %s", line)
		}
	}
	if !found {
		t.Fatalf("no PUT log line in %s", e.log.String())
	}
}

func TestGracefulShutdown(t *testing.T) {
	srv, err := New(context.Background(), Config{DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx, "127.0.0.1:0", 2*time.Second, func(a net.Addr) { addr <- a.String() })
	}()
	a := <-addr
	res, err := http.Get("http://" + a + "/healthz")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", res, err)
	}
	res.Body.Close()
	// An open SSE stream must not hold up the shutdown.
	u, _ := srv.Store().AddUser(context.Background(), "a@example.com")
	tok, _, _ := srv.Store().CreateToken(context.Background(), u.ID, "d")
	srv.Store().CreateVault(context.Background(), u.ID, "main")
	req, _ := http.NewRequest("GET", "http://"+a+protocol.EventsPath("main")+"?protocol=1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	sse, err := http.DefaultClient.Do(req)
	if err != nil || sse.StatusCode != 200 {
		t.Fatalf("sse: %v %v", sse, err)
	}
	defer sse.Body.Close()
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown hung")
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("shutdown took %v", time.Since(start))
	}
}
