package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"
)

// RemoteFile is the vault's sync configuration in its .nota directory.
const RemoteFile = "sync.toml"

// Remote is the content of .nota/sync.toml: where the vault syncs to.
// The token is not in it; it lives in the OS keyring or the credentials
// file (TokenStore says which).
type Remote struct {
	// URL is the server, e.g. https://nota.example.com.
	URL string `toml:"url" json:"url"`
	// Vault is the vault name on the server.
	Vault string `toml:"vault" json:"vault"`
	// Device names this machine in conflict copies.
	Device string `toml:"device" json:"device"`
	// TokenStore is "keyring" or "file".
	TokenStore string `toml:"token_store" json:"token_store"`
}

// Account is the key the token is stored under: tokens belong to a
// device on a server, whatever vault they sync.
func (r Remote) Account() string { return r.URL + "#" + r.Device }

// NormalizeURL checks a server URL and strips a trailing slash.
func NormalizeURL(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("sync: %q is not an http(s) server URL", s)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("sync: server URL %q must not have a query or fragment", s)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// LoadRemote reads metaDir/sync.toml; ok is false when there is none.
func LoadRemote(metaDir string) (Remote, bool, error) {
	var r Remote
	_, err := toml.DecodeFile(filepath.Join(metaDir, RemoteFile), &r)
	if errors.Is(err, fs.ErrNotExist) {
		return Remote{}, false, nil
	}
	if err != nil {
		return Remote{}, false, fmt.Errorf("sync: %s: %w", filepath.Join(metaDir, RemoteFile), err)
	}
	if r.URL == "" || r.Vault == "" {
		return Remote{}, false, fmt.Errorf("sync: %s needs url and vault", filepath.Join(metaDir, RemoteFile))
	}
	return r, true, nil
}

// SaveRemote writes metaDir/sync.toml.
func SaveRemote(metaDir string, r Remote) error {
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# Where this vault syncs to; see `nota remote`. The token is not here.\n")
	if err := toml.NewEncoder(&buf).Encode(r); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(metaDir, RemoteFile), buf.Bytes(), 0o644)
}

// RemoveRemote deletes metaDir/sync.toml.
func RemoveRemote(metaDir string) error {
	err := os.Remove(filepath.Join(metaDir, RemoteFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func writeFileAtomic(dst string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Token stores.
const (
	StoreKeyring = "keyring"
	StoreFile    = "file"
)

// Environment variables for tokens.
const (
	// EnvKeyring set to "file" skips the OS keyring and uses the
	// credentials file (headless machines, CI, tests).
	EnvKeyring = "NOTA_KEYRING"
	// EnvToken overrides the stored token of every remote.
	EnvToken = "NOTA_TOKEN"
)

// KeyringService is the service name of Nota's entries in the OS keyring.
const KeyringService = "nota"

// keyringTimeout bounds keyring calls: on a headless Linux box the
// Secret Service can hang instead of failing.
const keyringTimeout = 5 * time.Second

func withTimeout(fn func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), keyringTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errors.New("the OS keyring did not answer")
	}
}

// CredentialsPath is the token file used when there is no OS keyring:
// $XDG_CONFIG_HOME/nota/credentials or ~/.config/nota/credentials.
func CredentialsPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "nota", "credentials"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "nota", "credentials"), nil
}

type credFile struct {
	Tokens map[string]string `toml:"tokens"`
}

func readCreds() (credFile, string, error) {
	p, err := CredentialsPath()
	if err != nil {
		return credFile{}, "", err
	}
	var c credFile
	if _, err := toml.DecodeFile(p, &c); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return credFile{}, p, fmt.Errorf("sync: %s: %w", p, err)
	}
	if c.Tokens == nil {
		c.Tokens = map[string]string{}
	}
	return c, p, nil
}

func writeCreds(c credFile, p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# Nota device tokens, used when no OS keyring is available. Keep this file private.\n")
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	return writeFileAtomic(p, buf.Bytes(), 0o600)
}

// SaveToken stores a token for an account: in the OS keyring when there
// is one, else in the credentials file (mode 0600). It returns the store
// used and, when it fell back to the file, why.
func SaveToken(account, token string) (store, fallback string, err error) {
	if os.Getenv(EnvKeyring) != StoreFile {
		kerr := withTimeout(func() error { return keyring.Set(KeyringService, account, token) })
		if kerr == nil {
			return StoreKeyring, "", nil
		}
		fallback = kerr.Error()
	} else {
		fallback = EnvKeyring + "=file"
	}
	c, p, err := readCreds()
	if err != nil {
		return "", "", err
	}
	c.Tokens[account] = token
	if err := writeCreds(c, p); err != nil {
		return "", "", err
	}
	return StoreFile, fallback, nil
}

// ErrNoToken is returned by LoadToken when no token is stored.
var ErrNoToken = errors.New("sync: no token stored for this remote; run `nota remote add` again")

// LoadToken returns the token of an account from store. $NOTA_TOKEN wins.
func LoadToken(account, store string) (string, error) {
	if t := os.Getenv(EnvToken); t != "" {
		return t, nil
	}
	if store == StoreKeyring {
		var tok string
		err := withTimeout(func() error {
			var err error
			tok, err = keyring.Get(KeyringService, account)
			return err
		})
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNoToken
		}
		if err != nil {
			return "", fmt.Errorf("sync: reading the token from the OS keyring: %w", err)
		}
		return tok, nil
	}
	c, _, err := readCreds()
	if err != nil {
		return "", err
	}
	if t := c.Tokens[account]; t != "" {
		return t, nil
	}
	return "", ErrNoToken
}

// DeleteToken removes a stored token; a missing one is not an error.
func DeleteToken(account, store string) error {
	if store == StoreKeyring {
		err := withTimeout(func() error { return keyring.Delete(KeyringService, account) })
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
		return nil
	}
	c, p, err := readCreds()
	if err != nil {
		return err
	}
	if _, ok := c.Tokens[account]; !ok {
		return nil
	}
	delete(c.Tokens, account)
	return writeCreds(c, p)
}
