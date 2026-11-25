package server

import (
	"bufio"
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

func TestAccountSettings(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")

	// A new account sets nothing.
	var got protocol.AccountSettings
	if st, m := b.call("GET", "/v1/account/settings", nil, &got); st != 200 || got.Rev != 0 || len(got.Settings) != 0 {
		t.Fatalf("empty: %d %v %+v", st, m, got)
	}
	// A device token sets keys; the browser sees them.
	if st, m := e.device(tok, "PATCH", "/v1/account/settings",
		protocol.SettingsPatch{Set: map[string]string{"theme": "dark", "palette": "paper"}}); st != 200 || m["rev"] != 1.0 {
		t.Fatalf("set: %d %v", st, m)
	}
	if st, _ := b.call("GET", "/v1/account/settings", nil, &got); st != 200 || got.Rev != 1 ||
		!reflect.DeepEqual(got.Settings, map[string]string{"theme": "dark", "palette": "paper"}) {
		t.Fatalf("after set: %d %+v", st, got)
	}
	// Keys change one by one: the device's theme is overwritten, its
	// palette removed.
	got = protocol.AccountSettings{}
	if st, m := b.call("PATCH", "/v1/account/settings",
		protocol.SettingsPatch{Set: map[string]string{"theme": "light", "future_key": "x"}, Unset: []string{"palette"}}, &got); st != 200 {
		t.Fatalf("browser patch: %d %v", st, m)
	}
	if got.Rev != 2 || !reflect.DeepEqual(got.Settings, map[string]string{"theme": "light", "future_key": "x"}) {
		t.Fatalf("after browser patch: %+v", got)
	}
	// Changing nothing keeps the revision.
	if b.call("PATCH", "/v1/account/settings", protocol.SettingsPatch{Set: map[string]string{"theme": "light"}}, &got); got.Rev != 2 {
		t.Fatalf("no-op patch: rev %d", got.Rev)
	}

	for _, bad := range []protocol.SettingsPatch{
		{},
		{Set: map[string]string{"Theme": "dark"}},
		{Set: map[string]string{"theme": strings.Repeat("x", 300)}},
		{Unset: []string{"../x"}},
	} {
		if st, m := e.device(tok, "PATCH", "/v1/account/settings", bad); st != 400 {
			t.Errorf("%+v: %d %v, want 400", bad, st, m)
		}
	}
	// The cookie API needs the CSRF header for writes.
	b2 := *b
	req, _ := http.NewRequest("PATCH", e.ts.URL+"/v1/account/settings", strings.NewReader(`{"set":{"theme":"dark"}}`))
	req.Header.Set("Nota-Protocol", "1")
	req.Header.Set("Origin", testOrigin)
	res, err := b2.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Errorf("PATCH without Nota-Client: %d, want 403", res.StatusCode)
	}
	if st, _ := e.device("", "GET", "/v1/account/settings", nil); st != 401 {
		t.Errorf("signed out: %d, want 401", st)
	}

	// Each user has their own settings.
	_, other := webUser(t, e, "eva@example.com")
	if st, m := e.device(other, "GET", "/v1/account/settings", nil); st != 200 || m["rev"] != 0.0 {
		t.Fatalf("other user: %d %v", st, m)
	}

	// Deleting the user deletes their settings.
	if _, err := e.srv.Store().DeleteUser(context.Background(), "anton@example.com"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.srv.Store().db.QueryRow(`SELECT count(*) FROM user_settings`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("settings rows after delete: %d %v", n, err)
	}
}

func TestAccountSettingsLimit(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("a@example.com")
	set := map[string]string{}
	for i := range 65 {
		set["k"+itoa(int64(i))] = "v"
	}
	if st, m := e.device(tok, "PATCH", "/v1/account/settings", protocol.SettingsPatch{Set: set}); st != 400 {
		t.Fatalf("65 keys: %d %v, want 400", st, m)
	}
}

func TestAccountEvents(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen, KeepAlive: 50 * time.Millisecond})
	b, tok := webUser(t, e, "anton@example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Like EventSource: the session cookie, no custom headers.
	req, _ := http.NewRequestWithContext(ctx, "GET", e.ts.URL+"/v1/account/events?protocol=1", nil)
	req.Header.Set("Origin", testOrigin)
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %v", res.StatusCode, res.Header)
	}
	sc := bufio.NewScanner(res.Body)
	next := func() string {
		var ev string
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "event: "):
				ev = strings.TrimPrefix(l, "event: ")
			case strings.HasPrefix(l, "data: "):
				return ev + " " + strings.TrimPrefix(l, "data: ")
			}
		}
		t.Fatalf("stream ended: %v", sc.Err())
		return ""
	}
	if got := next(); got != `settings {"rev":0}` {
		t.Fatalf("first event %q", got)
	}
	// A change from another device reaches the browser.
	e.device(tok, "PATCH", "/v1/account/settings", protocol.SettingsPatch{Set: map[string]string{"theme": "dark"}})
	if got := next(); got != `settings {"rev":1}` {
		t.Fatalf("after change %q", got)
	}
	// Another user's change does not.
	_, other := webUser(t, e, "eva@example.com")
	e.device(other, "PATCH", "/v1/account/settings", protocol.SettingsPatch{Set: map[string]string{"theme": "dark"}})
	e.device(tok, "PATCH", "/v1/account/settings", protocol.SettingsPatch{Set: map[string]string{"theme": "light"}})
	if got := next(); got != `settings {"rev":2}` {
		t.Fatalf("after own second change %q", got)
	}
}
