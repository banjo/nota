package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
)

const (
	testRPID   = "nota.example.com"
	testOrigin = "https://nota.example.com"
)

// clock is a settable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Now()} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// authEnv is a server with passkeys on, and the clock it runs on.
func authEnv(t *testing.T, cfg Config) (*env, *clock) {
	t.Helper()
	c := newClock()
	cfg.Now = c.Now
	if cfg.RPID == "" {
		cfg.RPID = testRPID
	}
	if cfg.WebOrigins == nil {
		cfg.WebOrigins = []string{testOrigin}
	}
	cfg.CookieInsecure = true // httptest is plain http
	if cfg.AuthRateLimit == 0 {
		cfg.AuthRateLimit = -1
	}
	return newEnv(t, cfg), c
}

// browser is a cookie-keeping web client with a virtual passkey.
type browser struct {
	t      *testing.T
	e      *env
	client *http.Client
	origin string
	auth   virtualwebauthn.Authenticator
	cred   virtualwebauthn.Credential
}

func (e *env) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: e.t, e: e, client: &http.Client{Jar: jar}, origin: testOrigin,
		auth: virtualwebauthn.NewAuthenticator(), cred: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)}
}

// call sends a JSON request like the web app does: protocol and client
// headers plus the Origin.
func (b *browser) call(method, path string, body any, out any) (int, map[string]any) {
	b.t.Helper()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			b.t.Fatal(err)
		}
		rd = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, b.e.ts.URL+path, rd)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Nota-Protocol", "1")
	req.Header.Set(HeaderClient, "web")
	req.Header.Set("Origin", b.origin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if out != nil && res.StatusCode/100 == 2 {
		if err := json.Unmarshal(data, out); err != nil {
			b.t.Fatalf("%s %s: %v: %s", method, path, err, data)
		}
	}
	return res.StatusCode, m
}

func (b *browser) rp() virtualwebauthn.RelyingParty {
	return virtualwebauthn.RelyingParty{ID: testRPID, Name: "Nota", Origin: b.origin}
}

func optionsJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// register signs up with the browser's virtual passkey.
func (b *browser) register(email, invite string) (int, map[string]any, RegisterResponse) {
	b.t.Helper()
	var begin BeginResponse
	st, m := b.call("POST", "/v1/auth/register/begin", BeginRequest{Email: email, Invite: invite}, &begin)
	if st != 200 {
		return st, m, RegisterResponse{}
	}
	att, err := virtualwebauthn.ParseAttestationOptions(optionsJSON(b.t, begin.Options))
	if err != nil {
		b.t.Fatal(err)
	}
	b.auth.Options.UserHandle = []byte(att.UserID)
	resp := virtualwebauthn.CreateAttestationResponse(b.rp(), b.auth, b.cred, *att)
	b.auth.AddCredential(b.cred)
	var out RegisterResponse
	st, m = b.call("POST", "/v1/auth/register/finish", FinishRequest{Ceremony: begin.Ceremony, Credential: json.RawMessage(resp), Name: "test key"}, &out)
	return st, m, out
}

// login signs in with the browser's passkey; email may be empty for a
// usernameless login.
func (b *browser) login(email string) (int, map[string]any) {
	b.t.Helper()
	var begin BeginResponse
	st, m := b.call("POST", "/v1/auth/login/begin", BeginRequest{Email: email}, &begin)
	if st != 200 {
		return st, m
	}
	as, err := virtualwebauthn.ParseAssertionOptions(optionsJSON(b.t, begin.Options))
	if err != nil {
		b.t.Fatal(err)
	}
	b.cred.Counter++
	resp := virtualwebauthn.CreateAssertionResponse(b.rp(), b.auth, b.cred, *as)
	return b.call("POST", "/v1/auth/login/finish", FinishRequest{Ceremony: begin.Ceremony, Credential: json.RawMessage(resp)}, nil)
}

func code(m map[string]any) string { s, _ := m["code"].(string); return s }

func TestRegisterLoginWithPasskey(t *testing.T) {
	e, _ := authEnv(t, Config{})
	ctx := context.Background()
	b := e.browser()

	// Invite-only: no invite, no account.
	if st, m, _ := b.register("new@example.com", ""); st != 403 || code(m) != CodeInviteNeeded {
		t.Fatalf("no invite: %d %v", st, m)
	}
	if st, m, _ := b.register("new@example.com", "ninv_bogus"); st != 403 || code(m) != CodeBadInvite {
		t.Fatalf("bad invite: %d %v", st, m)
	}
	inv, _, err := e.srv.Store().CreateInvite(ctx, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	st, m, reg := b.register("New@Example.com", inv)
	if st != 200 {
		t.Fatalf("register: %d %v", st, m)
	}
	if reg.User.Email != "new@example.com" || len(reg.RecoveryCodes) != RecoveryCodeCount {
		t.Fatalf("register result: %+v", reg)
	}
	var me MeResponse
	if st, m := b.call("GET", "/v1/auth/me", nil, &me); st != 200 || me.User.Email != "new@example.com" || me.Via != "session" || me.Passkeys != 1 {
		t.Fatalf("me after sign-up: %d %v %+v", st, m, me)
	}

	// The invite is spent.
	other := e.browser()
	if st, m, _ := other.register("other@example.com", inv); st != 403 || code(m) != CodeBadInvite {
		t.Fatalf("reused invite: %d %v", st, m)
	}

	// Sign out, then in again with the passkey: usernameless and by email.
	if st, m := b.call("POST", "/v1/auth/logout", nil, nil); st != 204 {
		t.Fatalf("logout: %d %v", st, m)
	}
	if st, _ := b.call("GET", "/v1/auth/me", nil, nil); st != 401 {
		t.Fatalf("me after logout: %d", st)
	}
	if st, m := b.login(""); st != 200 {
		t.Fatalf("passkey login: %d %v", st, m)
	}
	if st, _ := b.call("GET", "/v1/auth/me", nil, &me); st != 200 || me.User.Email != "new@example.com" {
		t.Fatalf("me after login: %d %+v", st, me)
	}
	fresh := e.browser()
	fresh.auth, fresh.cred = b.auth, b.cred
	if st, m := fresh.login("new@example.com"); st != 200 {
		t.Fatalf("login by email: %d %v", st, m)
	}
	b.cred = fresh.cred

	// A response collected on another origin is refused.
	evil := e.browser()
	evil.auth, evil.cred = b.auth, b.cred
	evil.cred.Counter += 10
	evil.origin = "https://evil.example.org"
	var begin BeginResponse
	if st, m := b.call("POST", "/v1/auth/login/begin", BeginRequest{}, &begin); st != 200 {
		t.Fatalf("begin: %d %v", st, m)
	}
	as, err := virtualwebauthn.ParseAssertionOptions(optionsJSON(t, begin.Options))
	if err != nil {
		t.Fatal(err)
	}
	resp := virtualwebauthn.CreateAssertionResponse(evil.rp(), evil.auth, evil.cred, *as)
	if st, m := b.call("POST", "/v1/auth/login/finish", FinishRequest{Ceremony: begin.Ceremony, Credential: json.RawMessage(resp)}, nil); st != 400 || code(m) != CodeWebAuthn ||
		!strings.Contains(strings.ToLower(m["message"].(string)), "origin") {
		t.Fatalf("wrong origin: %d %v", st, m)
	}
	// The ceremony is gone after one attempt.
	if st, m := b.call("POST", "/v1/auth/login/finish", FinishRequest{Ceremony: begin.Ceremony, Credential: json.RawMessage(resp)}, nil); st != 400 || code(m) != CodeExpired {
		t.Fatalf("finished twice: %d %v", st, m)
	}

	// An unknown passkey does not sign in.
	stranger := e.browser()
	stranger.auth.Options.UserHandle = []byte("nobody")
	if st, m := stranger.login(""); st != 400 || code(m) != CodeWebAuthn {
		t.Fatalf("unknown passkey: %d %v", st, m)
	}
	// A registration made on another origin is refused too.
	evilReg := e.browser()
	evilReg.origin = "https://evil.example.org"
	inv2, _, _ := e.srv.Store().CreateInvite(ctx, "", "", 0)
	var rb BeginResponse
	if st, m := b.call("POST", "/v1/auth/register/begin", BeginRequest{Email: "x@example.com", Invite: inv2}, &rb); st != 200 {
		t.Fatalf("begin: %d %v", st, m)
	}
	att, _ := virtualwebauthn.ParseAttestationOptions(optionsJSON(t, rb.Options))
	aresp := virtualwebauthn.CreateAttestationResponse(evilReg.rp(), evilReg.auth, evilReg.cred, *att)
	if st, m := b.call("POST", "/v1/auth/register/finish", FinishRequest{Ceremony: rb.Ceremony, Credential: json.RawMessage(aresp)}, nil); st != 400 || code(m) != CodeWebAuthn {
		t.Fatalf("register from another origin: %d %v", st, m)
	}
	if _, err := e.srv.Store().UserByEmail(ctx, "x@example.com"); err == nil {
		t.Fatal("a failed registration created the user")
	}

	// The email is taken now.
	if st, m, _ := e.browser().register("new@example.com", ""); st != 409 {
		t.Fatalf("register twice: %d %v", st, m)
	}
}

func TestRecoveryCodes(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b := e.browser()
	st, m, reg := b.register("anton@example.com", "")
	if st != 200 {
		t.Fatalf("register (open sign-up): %d %v", st, m)
	}
	lost := e.browser() // a new device without the passkey
	var lr LoginResponse
	if st, m := lost.call("POST", "/v1/auth/recovery", RecoveryRequest{Email: "anton@example.com", Code: "WRONG-CODE-0000"}, nil); st != 401 || code(m) != CodeBadCredential {
		t.Fatalf("wrong code: %d %v", st, m)
	}
	// Codes are case- and dash-insensitive.
	typed := strings.ToLower(strings.ReplaceAll(reg.RecoveryCodes[3], "-", " "))
	if st, m := lost.call("POST", "/v1/auth/recovery", RecoveryRequest{Email: "anton@example.com", Code: typed}, &lr); st != 200 || !lr.NeedsPasskey || lr.RecoveryCodesLeft != RecoveryCodeCount-1 {
		t.Fatalf("recovery: %d %v %+v", st, m, lr)
	}
	// The session can only add a passkey until it has done so.
	if st, m := lost.call("GET", "/v1/auth/passkeys", nil, nil); st != 403 || code(m) != CodeNeedsPasskey {
		t.Fatalf("pending session: %d %v", st, m)
	}
	var me MeResponse
	if st, _ := lost.call("GET", "/v1/auth/me", nil, &me); st != 200 || !me.NeedsPasskey {
		t.Fatalf("me: %d %+v", st, me)
	}
	var begin BeginResponse
	if st, m := lost.call("POST", "/v1/auth/passkeys/begin", nil, &begin); st != 200 {
		t.Fatalf("passkey begin: %d %v", st, m)
	}
	att, err := virtualwebauthn.ParseAttestationOptions(optionsJSON(t, begin.Options))
	if err != nil {
		t.Fatal(err)
	}
	if len(att.ExcludeCredentials) != 1 {
		t.Fatalf("exclusions: %v", att.ExcludeCredentials)
	}
	lost.auth.Options.UserHandle = []byte(att.UserID)
	resp := virtualwebauthn.CreateAttestationResponse(lost.rp(), lost.auth, lost.cred, *att)
	lost.auth.AddCredential(lost.cred)
	if st, m := lost.call("POST", "/v1/auth/passkeys/finish", FinishRequest{Ceremony: begin.Ceremony, Credential: json.RawMessage(resp), Name: "new phone"}, nil); st != 200 {
		t.Fatalf("passkey finish: %d %v", st, m)
	}
	var pks struct{ Passkeys []Passkey }
	if st, m := lost.call("GET", "/v1/auth/passkeys", nil, &pks); st != 200 || len(pks.Passkeys) != 2 || pks.Passkeys[1].Name != "new phone" {
		t.Fatalf("passkeys after adding: %d %v %+v", st, m, pks)
	}
	// The new passkey signs in.
	if st, m := lost.login(""); st != 200 {
		t.Fatalf("login with the new passkey: %d %v", st, m)
	}
	// A recovery code works once.
	if st, m := e.browser().call("POST", "/v1/auth/recovery", RecoveryRequest{Email: "anton@example.com", Code: reg.RecoveryCodes[3]}, nil); st != 401 {
		t.Fatalf("used code again: %d %v", st, m)
	}
	// Removing passkeys stops at the last one.
	if st, m := lost.call("DELETE", "/v1/auth/passkeys/"+itoa(pks.Passkeys[0].ID), nil, nil); st != 204 {
		t.Fatalf("delete passkey: %d %v", st, m)
	}
	if st, m := lost.call("DELETE", "/v1/auth/passkeys/"+itoa(pks.Passkeys[1].ID), nil, nil); st != 409 || code(m) != CodeLastPasskey {
		t.Fatalf("delete last passkey: %d %v", st, m)
	}
	// New recovery codes replace the old ones.
	var rc struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if st, m := lost.call("POST", "/v1/auth/recovery-codes", nil, &rc); st != 200 || len(rc.RecoveryCodes) != RecoveryCodeCount {
		t.Fatalf("regenerate: %d %v", st, m)
	}
	if st, _ := e.browser().call("POST", "/v1/auth/recovery", RecoveryRequest{Email: "anton@example.com", Code: reg.RecoveryCodes[0]}, nil); st != 401 {
		t.Fatalf("old code after regenerate: %d", st)
	}
	if st, _ := e.browser().call("POST", "/v1/auth/recovery", RecoveryRequest{Email: "nobody@example.com", Code: rc.RecoveryCodes[0]}, nil); st != 401 {
		t.Fatalf("code with another email: %d", st)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestSignupModesAndClaim(t *testing.T) {
	ctx := context.Background()
	closed, _ := authEnv(t, Config{Signup: SignupClosed})
	if st, m, _ := closed.browser().register("x@example.com", ""); st != 403 || code(m) != CodeSignupClosed {
		t.Fatalf("closed: %d %v", st, m)
	}
	// A user created by the admin claims the account with the invite
	// bound to their email, even when sign-up is closed.
	u, err := closed.srv.Store().AddUser(ctx, "anton@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bound, _, err := closed.srv.Store().CreateInvite(ctx, "anton@example.com", "setup", 0)
	if err != nil {
		t.Fatal(err)
	}
	if st, m, _ := closed.browser().register("anton@example.com", ""); st != 403 || code(m) != CodeInviteNeeded {
		t.Fatalf("claim without invite: %d %v", st, m)
	}
	if st, m, _ := closed.browser().register("someone@example.com", bound); st != 403 {
		t.Fatalf("bound invite, other email: %d %v", st, m)
	}
	st, m, reg := closed.browser().register("anton@example.com", bound)
	if st != 200 || reg.User.ID != u.ID {
		t.Fatalf("claim: %d %v %+v", st, m, reg)
	}

	// Expired invites are refused.
	inv, c := authEnv(t, Config{})
	code1, _, err := inv.srv.Store().CreateInvite(ctx, "", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c.Add(2 * time.Hour)
	if st, m, _ := inv.browser().register("late@example.com", code1); st != 403 || code(m) != CodeBadInvite {
		t.Fatalf("expired invite: %d %v", st, m)
	}
}

func TestSessionExpiry(t *testing.T) {
	e, c := authEnv(t, Config{Signup: SignupOpen, SessionTTL: time.Hour})
	b := e.browser()
	if st, m, _ := b.register("a@example.com", ""); st != 200 {
		t.Fatalf("register: %d %v", st, m)
	}
	// Use rolls the expiry forward.
	for range 3 {
		c.Add(40 * time.Minute)
		if st, _ := b.call("GET", "/v1/auth/me", nil, nil); st != 200 {
			t.Fatalf("active session expired: %d", st)
		}
	}
	c.Add(61 * time.Minute)
	if st, m := b.call("GET", "/v1/auth/me", nil, nil); st != 401 {
		t.Fatalf("idle session: %d %v", st, m)
	}
	// The cookie was cleared, so the next request is just anonymous.
	if st, m := b.call("GET", "/v1/auth/me", nil, nil); st != 401 || code(m) != "unauthorized" {
		t.Fatalf("after expiry: %d %v", st, m)
	}
}

func TestCSRFAndCORS(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b := e.browser()
	if st, m, _ := b.register("a@example.com", ""); st != 200 {
		t.Fatalf("register: %d %v", st, m)
	}
	send := func(method, path, origin string, client bool) (*http.Response, map[string]any) {
		req, _ := http.NewRequest(method, e.ts.URL+path, strings.NewReader(`{}`))
		req.Header.Set("Nota-Protocol", "1")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if client {
			req.Header.Set(HeaderClient, "web")
		}
		u, _ := url.Parse(e.ts.URL)
		for _, ck := range b.client.Jar.Cookies(u) {
			req.AddCookie(ck)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res, m
	}
	// A cookie request without the client header (a cross-site form).
	if res, m := send("POST", "/v1/auth/recovery-codes", "", false); res.StatusCode != 403 || code(m) != CodeCSRF {
		t.Fatalf("no header: %d %v", res.StatusCode, m)
	}
	if res, m := send("POST", "/v1/auth/recovery-codes", "https://evil.example.org", true); res.StatusCode != 403 || code(m) != CodeCSRF {
		t.Fatalf("foreign origin: %d %v", res.StatusCode, m)
	}
	if res, m := send("POST", "/v1/auth/login/begin", "", false); res.StatusCode != 403 || code(m) != CodeCSRF {
		t.Fatalf("login without header: %d %v", res.StatusCode, m)
	}
	if res, m := send("POST", "/v1/auth/recovery-codes", testOrigin, true); res.StatusCode != 200 {
		t.Fatalf("good request: %d %v", res.StatusCode, m)
	}
	// Reads need no header.
	if res, _ := send("GET", "/v1/auth/me", "", false); res.StatusCode != 200 {
		t.Fatalf("GET me: %d", res.StatusCode)
	}

	preflight := func(origin string) *http.Response {
		req, _ := http.NewRequest("OPTIONS", e.ts.URL+"/v1/auth/login/begin", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type,nota-client,nota-protocol")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	res := preflight(testOrigin)
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Origin") != testOrigin ||
		res.Header.Get("Access-Control-Allow-Credentials") != "true" ||
		!strings.Contains(res.Header.Get("Access-Control-Allow-Headers"), HeaderClient) {
		t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
	}
	if res := preflight("https://evil.example.org"); res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("preflight for a foreign origin: %v", res.Header)
	}
	res, _ = send("GET", "/v1/auth/config", testOrigin, false)
	if res.Header.Get("Access-Control-Allow-Origin") != testOrigin || !strings.Contains(res.Header.Get("Access-Control-Expose-Headers"), "Nota-Version") {
		t.Fatalf("CORS on a request: %v", res.Header)
	}
}

func TestAuthRateLimit(t *testing.T) {
	e, _ := authEnv(t, Config{AuthRateLimit: 5})
	b := e.browser()
	got := map[int]int{}
	for range 25 {
		st, _ := b.call("POST", "/v1/auth/login/begin", BeginRequest{}, nil)
		got[st]++
	}
	if got[200] != 20 || got[429] != 5 {
		t.Fatalf("statuses: %v", got)
	}
	// Other routes are not limited.
	if st, _ := b.call("GET", "/v1/auth/config", nil, nil); st != 200 {
		t.Fatalf("config: %d", st)
	}
}

func TestClientIP(t *testing.T) {
	proxies, err := ParseProxies("private, 203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: Config{TrustedProxies: proxies}}
	for _, c := range []struct{ remote, xff, want string }{
		{"198.51.100.1:1234", "1.2.3.4", "198.51.100.1"},       // untrusted peer: header ignored
		{"10.0.1.5:1234", "1.2.3.4", "1.2.3.4"},                // Traefik on the Docker network
		{"10.0.1.5:1234", "6.6.6.6, 1.2.3.4", "1.2.3.4"},       // a spoofed left-most hop is ignored
		{"10.0.1.5:1234", "1.2.3.4, 203.0.113.7", "1.2.3.4"},   // a chain of trusted proxies
		{"10.0.1.5:1234", "", "10.0.1.5"},                      // no header
		{"[::ffff:10.0.1.5]:1234", "1.2.3.4", "1.2.3.4"},       // IPv4-mapped peer
		{"10.0.1.5:1234", "garbage, 1.2.3.4", "1.2.3.4"},       // garbage left of the real hop
		{"10.0.1.5:1234", "10.0.0.9, 192.168.1.1", "10.0.0.9"}, // all trusted: the left-most
	} {
		r := &http.Request{RemoteAddr: c.remote, Header: http.Header{}}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := s.clientIP(r); got != c.want {
			t.Errorf("%s / %q: got %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
	if _, err := ParseProxies("nonsense"); err == nil {
		t.Error("ParseProxies accepted nonsense")
	}
	if !trusted(proxies, netip.MustParseAddr("172.20.0.3")) {
		t.Error("172.20.0.3 should be private")
	}
}
