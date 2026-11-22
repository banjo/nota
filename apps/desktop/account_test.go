package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/banjo/nota/internal/protocol"
	nsync "github.com/banjo/nota/internal/sync"
)

func TestPKCE(t *testing.T) {
	p := newPKCE()
	if len(p.Verifier) < 43 || len(p.Verifier) > 128 || !regexp.MustCompile(`^[A-Za-z0-9._~-]+$`).MatchString(p.Verifier) {
		t.Fatalf("verifier %q is not a valid PKCE verifier", p.Verifier)
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	if p.Challenge != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatal("challenge is not S256 of the verifier")
	}
	if q := newPKCE(); q.Verifier == p.Verifier || q.State == p.State {
		t.Fatal("verifiers or states repeat")
	}
	u, err := url.Parse(connectURL("https://nota.banjoanton.com/", p, "Antons Mac"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "nota.banjoanton.com" || u.Path != "/app/connect" || q.Get("challenge") != p.Challenge ||
		q.Get("device") != "Antons Mac" || q.Get("redirect") != "nota://auth" || q.Get("state") != p.State {
		t.Fatalf("connect URL = %s", u)
	}
}

func TestParseCallback(t *testing.T) {
	code, state, err := parseCallback("nota://auth?code=abc&state=xyz")
	if err != nil || code != "abc" || state != "xyz" {
		t.Fatalf("= %q %q %v", code, state, err)
	}
	for _, bad := range []string{"nota://auth?state=x", "https://auth?code=a", "nota://other?code=a", "nota://auth?error=access_denied"} {
		if _, _, err := parseCallback(bad); err == nil {
			t.Errorf("parseCallback(%q) accepted it", bad)
		}
	}
}

// fakeServer is the part of nota serve that desktop sign-in talks to:
// the token endpoint (checking PKCE like internal/server/authflow.go)
// and the vault list.
type fakeServer struct {
	mu        sync.Mutex
	challenge string
	code      string
	used      bool
	vaults    []protocol.VaultInfo
	created   []string
	revoked   bool
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get(protocol.HeaderProtocol) != "1" {
		http.Error(w, `{"code":"unsupported_protocol","message":"protocol"}`, http.StatusUpgradeRequired)
		return
	}
	authed := r.Header.Get("Authorization") == "Bearer nota_devicetoken"
	fail := func(status int, code string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(protocol.Error{Code: code, Message: code})
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/auth/token":
		var req protocol.TokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GrantType != protocol.GrantAuthorizationCode {
			fail(400, "bad_request")
			return
		}
		sum := sha256.Sum256([]byte(req.CodeVerifier))
		ok := !f.used && req.Code == f.code && base64.RawURLEncoding.EncodeToString(sum[:]) == f.challenge
		f.used = true // a code works once, a wrong verifier burns it
		if !ok {
			fail(400, "invalid_grant")
			return
		}
		_ = json.NewEncoder(w).Encode(protocol.TokenResponse{Token: "nota_devicetoken", TokenType: "bearer", TokenID: 7, Device: "Antons Mac", Email: "anton@example.com"})
	case r.Method == "GET" && r.URL.Path == "/v1/vaults" && authed:
		_ = json.NewEncoder(w).Encode(protocol.VaultList{Vaults: f.vaults})
	case r.Method == "POST" && r.URL.Path == "/v1/vaults" && authed:
		var req protocol.CreateVault
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.created = append(f.created, req.Name)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(protocol.VaultInfo{Name: req.Name})
	case r.Method == "DELETE" && r.URL.Path == "/v1/devices/current" && authed:
		f.revoked = true
		w.WriteHeader(204)
	default:
		fail(401, "unauthorized")
	}
}

// testAccount is an Account against srv with an in-memory token store
// and a browser that records the URL it opens.
func testAccount(t *testing.T, srv *httptest.Server) (*Account, *[]string, map[string]string, *int) {
	t.Helper()
	opened := &[]string{}
	tokens := map[string]string{}
	restarts := new(int)
	a := &Account{
		metaDir:   t.TempDir(),
		vaultName: "Anteckningar",
		web:       "https://nota.banjoanton.com",
		openURL:   func(u string) error { *opened = append(*opened, u); return nil },
		saveToken: func(account, token string) (string, error) { tokens[account] = token; return storeApp, nil },
		deleteToken: func(account, store string) error {
			delete(tokens, account)
			return nil
		},
		loadToken: func(r nsync.Remote) (string, error) {
			if tok, ok := tokens[r.Account()]; ok {
				return tok, nil
			}
			return "", nsync.ErrNoToken
		},
		restart: func() { *restarts++ },
	}
	t.Setenv("NOTA_SERVER_URL", srv.URL)
	a.state = a.initial()
	return a, opened, tokens, restarts
}

func TestSignInExchangesTheCodeAndLinksTheVault(t *testing.T) {
	f := &fakeServer{code: "one-time-code", vaults: []protocol.VaultInfo{{Name: "Jobb", Cursor: 12}}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a, opened, tokens, restarts := testAccount(t, srv)
	if st := a.State(); st.State != "signed-out" || st.Server != srv.URL {
		t.Fatalf("initial state = %+v", st)
	}
	u, err := a.SignIn("")
	if err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 1 || (*opened)[0] != u || a.State().State != "pending" {
		t.Fatalf("opened %v, state %+v", *opened, a.State())
	}
	cu, _ := url.Parse(u)
	f.mu.Lock()
	f.challenge = cu.Query().Get("challenge") // what /v1/auth/connect stores
	f.mu.Unlock()
	state := cu.Query().Get("state")

	// A link from another sign-in is refused before the code is spent.
	if err := a.CompleteSignIn("nota://auth?code=one-time-code&state=other"); err == nil || a.State().State != "error" {
		t.Fatalf("wrong state accepted: %v", err)
	}
	if err := a.CompleteSignIn("nota://auth?code=one-time-code&state=" + url.QueryEscape(state)); err != nil {
		t.Fatal(err)
	}
	st := a.State()
	if st.State != "choose-vault" || st.Email != "anton@example.com" || st.Device != "Antons Mac" ||
		len(st.Vaults) != 1 || st.Vaults[0].Name != "Jobb" || st.Suggested != "Anteckningar" {
		t.Fatalf("after exchange = %+v", st)
	}
	account := nsync.Remote{URL: srv.URL, Device: "Antons Mac"}.Account()
	if tokens[account] != "nota_devicetoken" {
		t.Fatalf("tokens = %v", tokens)
	}
	// Upload this vault as a new synced vault.
	if err := a.ChooseVault("Anteckningar", true); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 || f.created[0] != "Anteckningar" || *restarts != 1 {
		t.Fatalf("created %v, restarts %d", f.created, *restarts)
	}
	r, ok, err := nsync.LoadRemote(a.metaDir)
	if err != nil || !ok || r.URL != srv.URL || r.Vault != "Anteckningar" || r.Device != "Antons Mac" || r.TokenStore != storeApp {
		t.Fatalf("sync.toml = %+v %v %v", r, ok, err)
	}
	if st := a.State(); st.State != "signed-in" || st.Vault != "Anteckningar" {
		t.Fatalf("signed in state = %+v", st)
	}
	// Signing out revokes the token, forgets it and removes the remote.
	if err := a.SignOut(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !f.revoked || len(tokens) != 0 || *restarts != 2 {
		t.Fatalf("revoked %v tokens %v restarts %d", f.revoked, tokens, *restarts)
	}
	if _, ok, _ := nsync.LoadRemote(a.metaDir); ok || a.State().State != "signed-out" {
		t.Fatal("still signed in after signing out")
	}
}

func TestSignInWithAWrongVerifierFails(t *testing.T) {
	f := &fakeServer{code: "one-time-code"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a, _, tokens, _ := testAccount(t, srv)
	u, err := a.SignIn(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cu, _ := url.Parse(u)
	f.challenge = "not-the-challenge"
	err = a.CompleteSignIn("nota://auth?code=one-time-code&state=" + url.QueryEscape(cu.Query().Get("state")))
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("err = %v", err)
	}
	if st := a.State(); st.State != "error" || len(tokens) != 0 {
		t.Fatalf("state %+v tokens %v", st, tokens)
	}
	if err := a.ChooseVault("x", true); err == nil {
		t.Fatal("chose a vault without signing in")
	}
}
