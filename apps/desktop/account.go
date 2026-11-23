package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/protocol"
	nsync "github.com/banjo/nota/internal/sync"
)

// Sign in to sync (docs/api.md, "Desktop (browser + PKCE)"): the app
// opens the web app's connect page with a PKCE challenge, the user
// signs in there with a passkey, and the browser hands a one-time code
// back through the nota://auth deep link. The app trades code and
// verifier for a device token, keeps it in the system's credential
// store, writes .nota/sync.toml and starts the background sync. The
// local vault works the same with or without an account.

const (
	defaultServer = "https://nota.app.banjoanton.com"
	defaultWeb    = "https://nota.banjoanton.com"
	redirectURI   = "nota://auth"
)

// AccountState is what Settings shows about sync.
type AccountState struct {
	// State is "signed-out", "pending" (the browser is open, waiting for
	// the deep link), "choose-vault" (signed in, pick where this vault
	// syncs), "signed-in" or "error".
	State  string `json:"state"`
	Server string `json:"server"`
	Email  string `json:"email,omitempty"`
	Device string `json:"device"`
	// Vault is the server vault this vault syncs with.
	Vault string `json:"vault,omitempty"`
	// Vaults are the account's vaults, while choosing one.
	Vaults []RemoteVault `json:"vaults"`
	// Suggested is the name to upload this vault as.
	Suggested string `json:"suggested,omitempty"`
	// ConnectURL is the page opened in the browser while pending.
	ConnectURL string `json:"connectUrl,omitempty"`
	// Web is where the browser signs in ($NOTA_WEB_URL): the hint under
	// the button names its host, so a self-hosted pair shows its own.
	Web   string `json:"web,omitempty"`
	Error string `json:"error,omitempty"`
}

// RemoteVault is a vault of the account on the server.
type RemoteVault struct {
	Name    string    `json:"name"`
	Cursor  int64     `json:"cursor"`
	Created time.Time `json:"created"`
}

// Device is one signed-in device of the account.
type Device struct {
	ID         int64      `json:"id"`
	Device     string     `json:"device"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	Current    bool       `json:"current"`
}

// accountEvent tells the frontend the account state changed.
var accountEvent = mygo.NewEvent[AccountState]("account")

// pkce is one sign-in attempt: the verifier stays in the app, the
// challenge goes to the browser.
type pkce struct {
	Verifier  string
	Challenge string
	State     string
}

// randomString returns n random bytes in base64url: only characters a
// PKCE verifier may hold ([A-Za-z0-9-_]).
func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return base64.RawURLEncoding.EncodeToString(b)
}

func newPKCE() pkce {
	v := randomString(48) // 64 characters
	sum := sha256.Sum256([]byte(v))
	return pkce{Verifier: v, Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), State: randomString(16)}
}

// connectURL is the web app page that signs the user in and sends the
// browser back to nota://auth with a one-time code.
func connectURL(web string, p pkce, device string) string {
	q := url.Values{}
	q.Set("challenge", p.Challenge)
	q.Set("device", device)
	q.Set("redirect", redirectURI)
	q.Set("state", p.State)
	return strings.TrimRight(web, "/") + "/app/connect?" + q.Encode()
}

// parseCallback reads code and state from the nota://auth deep link.
func parseCallback(raw string) (code, state string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "nota" || (u.Host != "auth" && strings.Trim(u.Opaque+u.Path, "/") != "auth") {
		return "", "", fmt.Errorf("not a sign-in link: %s", raw)
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		return "", "", fmt.Errorf("sign-in failed: %s", e)
	}
	code, state = q.Get("code"), q.Get("state")
	if code == "" {
		return "", "", errors.New("the sign-in link has no code")
	}
	return code, state, nil
}

// Account manages signing in to sync, the devices of the account and
// signing out. Signing out keeps the local vault as it is.
type Account struct {
	mu      sync.Mutex
	state   AccountState
	pending *pkce
	token   string // between the code exchange and choosing a vault
	store   string // where token is kept

	// Set up by newAccount; tests replace them.
	metaDir     string
	vaultName   string
	web         string
	httpClient  *http.Client
	openURL     func(string) error
	saveToken   func(account, token string) (store string, err error)
	deleteToken func(account, store string) error
	loadToken   func(nsync.Remote) (string, error)
	restart     func()
	publish     func(AccountState)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func deviceName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "Nota desktop"
	}
	return strings.TrimSuffix(h, ".local")
}

// newAccount sets up sign-in for the vault s, with the app's credential
// store and browser.
func newAccount(s *Vault) *Account { return newAccountFor(s.v.MetaDir(), s.Info().Name, s.restartSync) }

// newAccountFor sets up sign-in for the vault whose .nota folder is
// metaDir ("" while the first-run setup has not made a vault yet: the
// sign-in can finish, and attachVault links the vault later). restart
// runs after the sync settings change.
func newAccountFor(metaDir, vaultName string, restart func()) *Account {
	a := &Account{
		metaDir:   metaDir,
		vaultName: vaultName,
		web:       envOr("NOTA_WEB_URL", defaultWeb),
		openURL:   mygo.Shell.OpenExternal,
		saveToken: func(account, token string) (string, error) {
			if err := mygo.Secrets.Set(account, []byte(token)); err == nil {
				return storeApp, nil
			}
			// No credential store (e.g. Linux without a Secret Service):
			// the CLI's keyring entry or credentials file.
			store, _, err := nsync.SaveToken(account, token)
			return store, err
		},
		deleteToken: func(account, store string) error {
			if store == storeApp {
				return mygo.Secrets.Delete(account)
			}
			return nsync.DeleteToken(account, store)
		},
		loadToken: loadToken,
		restart:   restart,
		publish:   func(st AccountState) { _ = accountEvent.Broadcast(st) },
	}
	a.state = a.initial()
	return a
}

// initial is the state at start: signed in when the vault has a remote.
func (a *Account) initial() AccountState {
	st := AccountState{State: "signed-out", Server: envOr("NOTA_SERVER_URL", defaultServer), Web: a.web,
		Device: deviceName(), Vaults: []RemoteVault{}}
	r, ok, err := a.loadRemote()
	switch {
	case err != nil:
		st.State, st.Error = "error", err.Error()
	case ok:
		st.State, st.Server, st.Vault, st.Device = "signed-in", r.URL, r.Vault, r.Device
	}
	return st
}

// loadRemote reads the vault's sync settings; there are none before the
// first-run setup has made the vault.
func (a *Account) loadRemote() (nsync.Remote, bool, error) {
	a.mu.Lock()
	meta := a.metaDir
	a.mu.Unlock()
	if meta == "" {
		return nsync.Remote{}, false, nil
	}
	return nsync.LoadRemote(meta)
}

// refreshEmail looks up the signed-in account's email in the background.
func (a *Account) refreshEmail() {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		me, err := c.Me(ctx)
		if err != nil {
			return
		}
		a.update(func(st *AccountState) { st.Email = me.User.Email })
	}()
}

func (a *Account) update(fn func(*AccountState)) {
	a.mu.Lock()
	fn(&a.state)
	if a.state.Vaults == nil {
		a.state.Vaults = []RemoteVault{}
	}
	st := a.state
	a.mu.Unlock()
	if a.publish != nil {
		a.publish(st)
	}
}

// State returns the sync account state. Changes also arrive as
// "account" events.
func (a *Account) State() AccountState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// SignIn starts signing in to sync on server ("" for the default): it
// opens the web app's connect page in the browser and returns its URL.
// The browser comes back through the nota://auth deep link.
func (a *Account) SignIn(server string) (string, error) {
	if server == "" {
		server = envOr("NOTA_SERVER_URL", defaultServer)
	}
	server, err := nsync.NormalizeURL(server)
	if err != nil {
		return "", err
	}
	p := newPKCE()
	a.mu.Lock()
	device := a.state.Device
	a.pending = &p
	a.mu.Unlock()
	u := connectURL(a.web, p, device)
	a.update(func(st *AccountState) {
		st.State, st.Server, st.ConnectURL, st.Error = "pending", server, u, ""
	})
	if err := a.openURL(u); err != nil {
		a.update(func(st *AccountState) { st.Error = "Couldn't open your browser. Use the button below." })
		return u, nil
	}
	return u, nil
}

// CancelSignIn forgets a sign-in that has not come back.
func (a *Account) CancelSignIn() {
	a.discardPending(context.Background())
	start := a.initial()
	a.update(func(st *AccountState) { *st = start })
}

// CompleteSignIn handles the nota://auth?code=…&state=… deep link: it
// checks the state, trades the code and the PKCE verifier for a device
// token, stores the token and lists the account's vaults to choose from.
func (a *Account) CompleteSignIn(link string) error {
	err := a.completeSignIn(link)
	if err != nil {
		a.update(func(st *AccountState) { st.State, st.Error = "error", err.Error() })
	}
	return err
}

func (a *Account) completeSignIn(link string) error {
	code, state, err := parseCallback(link)
	if err != nil {
		return err
	}
	a.mu.Lock()
	p, server, device := a.pending, a.state.Server, a.state.Device
	a.mu.Unlock()
	if p == nil {
		return errors.New("no sign-in is in progress; start it again from Settings")
	}
	if state != p.State {
		return errors.New("the sign-in link does not belong to this sign-in; start it again from Settings")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := &nsync.Client{BaseURL: server, HTTP: a.httpClient, UserAgent: "nota-desktop"}
	tok, err := c.ExchangeCode(ctx, code, p.Verifier)
	if err != nil {
		return fmt.Errorf("signing in: %w", err)
	}
	if tok.Device != "" {
		device = tok.Device
	}
	account := nsync.Remote{URL: server, Device: device}.Account()
	store, err := a.saveToken(account, tok.Token)
	if err != nil {
		return fmt.Errorf("storing the device token: %w", err)
	}
	c.Token = tok.Token
	vs, err := c.Vaults(ctx)
	if err != nil {
		return fmt.Errorf("listing your vaults: %w", err)
	}
	out := make([]RemoteVault, 0, len(vs))
	for _, v := range vs {
		out = append(out, RemoteVault{Name: v.Name, Cursor: v.Cursor, Created: v.CreatedAt})
	}
	a.mu.Lock()
	a.pending, a.token, a.store = nil, tok.Token, store
	a.mu.Unlock()
	a.update(func(st *AccountState) {
		st.State, st.Email, st.Device, st.Vaults, st.Suggested = "choose-vault", tok.Email, device, out, a.vaultName
		st.ConnectURL, st.Error = "", ""
	})
	return nil
}

// ChooseVault links this vault to the server vault name, creating it
// first when create is set (uploading this vault as a new synced vault),
// writes .nota/sync.toml and starts the background sync. Either way the
// first pass is a normal three-way sync: nothing is overwritten.
func (a *Account) ChooseVault(name string, create bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("give the vault a name")
	}
	a.mu.Lock()
	tok, store, st, meta := a.token, a.store, a.state, a.metaDir
	a.mu.Unlock()
	if tok == "" || st.State != "choose-vault" {
		return errors.New("sign in first")
	}
	if create {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := &nsync.Client{BaseURL: st.Server, Token: tok, HTTP: a.httpClient, UserAgent: "nota-desktop"}
		if _, err := c.CreateVault(ctx, name); err != nil && !nsync.IsCode(err, protocol.CodeExists) {
			return fmt.Errorf("creating the vault: %w", err)
		}
	}
	r := nsync.Remote{URL: st.Server, Vault: name, Device: st.Device, TokenStore: store}
	if meta == "" {
		return errors.New("there is no vault to sync yet")
	}
	if err := nsync.SaveRemote(meta, r); err != nil {
		return err
	}
	a.mu.Lock()
	a.token = ""
	a.mu.Unlock()
	a.update(func(st *AccountState) {
		st.State, st.Vault, st.Vaults, st.Suggested, st.Error = "signed-in", name, nil, "", ""
	})
	if a.restart != nil {
		a.restart()
	}
	return nil
}

// client is a server client with the stored device token; nil when not
// signed in.
func (a *Account) client() *nsync.Client {
	r, ok, err := a.loadRemote()
	if err != nil || !ok {
		return nil
	}
	tok, err := a.loadToken(r)
	if err != nil {
		return nil
	}
	return &nsync.Client{BaseURL: r.URL, Vault: r.Vault, Token: tok, HTTP: a.httpClient, UserAgent: "nota-desktop"}
}

// Devices lists the account's signed-in devices.
func (a *Account) Devices(ctx context.Context) ([]Device, error) {
	c := a.client()
	if c == nil {
		return nil, errors.New("not signed in")
	}
	ds, err := c.Devices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(ds))
	for _, d := range ds {
		out = append(out, Device{ID: d.ID, Device: d.Device, CreatedAt: d.CreatedAt, LastUsedAt: d.LastUsedAt, Current: d.Current})
	}
	return out, nil
}

// RevokeDevice signs another device out of the account.
func (a *Account) RevokeDevice(ctx context.Context, id int64) error {
	c := a.client()
	if c == nil {
		return errors.New("not signed in")
	}
	return c.RevokeDevice(ctx, id)
}

// SignOut stops sync, revokes this device's token and forgets it. The
// local vault stays exactly as it is.
func (a *Account) SignOut(ctx context.Context) error {
	a.discardPending(ctx)
	r, ok, err := a.loadRemote()
	if err != nil {
		return err
	}
	if ok {
		if c := a.client(); c != nil {
			_ = c.RevokeSelf(ctx) // offline: the token stays valid on the server until revoked from another device
		}
		if err := a.deleteToken(r.Account(), r.TokenStore); err != nil {
			return err
		}
		if err := nsync.RemoveRemote(a.metaDir); err != nil {
			return err
		}
	}
	a.update(func(st *AccountState) {
		*st = AccountState{State: "signed-out", Server: st.Server, Device: st.Device}
	})
	if a.restart != nil {
		a.restart()
	}
	return nil
}

// discardPending forgets a sign-in that is not linked to a vault yet:
// one still waiting for the browser, or one that has a device token but
// no vault chosen (the first-run setup, or "choose-vault" in Settings).
// The token is revoked on the server (best effort) and removed from the
// credential store, so abandoning the sign-in leaves nothing behind.
func (a *Account) discardPending(ctx context.Context) {
	a.mu.Lock()
	tok, store, st := a.token, a.store, a.state
	a.pending, a.token, a.store = nil, "", ""
	a.mu.Unlock()
	if tok == "" {
		return
	}
	c := &nsync.Client{BaseURL: st.Server, Token: tok, HTTP: a.httpClient, UserAgent: "nota-desktop"}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = c.RevokeSelf(rctx) // offline: stays valid until revoked from another device
	if a.deleteToken != nil {
		_ = a.deleteToken(nsync.Remote{URL: st.Server, Device: st.Device}.Account(), store)
	}
}

// attachVault points the account at the vault the first-run setup made
// (or the one switched to) and, when the user signed in during setup,
// links it: the vault syncs as name, created on the server when it is
// not there yet. It does nothing for a user who did not sign in.
func (a *Account) attachVault(metaDir, name string) error {
	a.mu.Lock()
	a.metaDir, a.vaultName = metaDir, name
	linking := a.state.State == "choose-vault" && a.token != ""
	a.mu.Unlock()
	if !linking {
		return nil
	}
	return a.ChooseVault(name, true)
}
