package sync

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// The account calls a device makes: signing in with a device code,
// trading a desktop one-time code for a token, and managing the user's
// device tokens. They need no vault; the sign-in calls need no token.

// StartDeviceAuth asks the server for a device code (nota login).
func (c *Client) StartDeviceAuth(ctx context.Context, device string) (protocol.DeviceAuthorization, error) {
	body, _ := json.Marshal(protocol.DeviceAuthRequest{Device: device})
	var out protocol.DeviceAuthorization
	err := c.json(ctx, "POST", "/v1/auth/device", nil, body, &out)
	return out, err
}

// RequestToken calls the token endpoint once.
func (c *Client) RequestToken(ctx context.Context, req protocol.TokenRequest) (protocol.TokenResponse, error) {
	body, _ := json.Marshal(req)
	var out protocol.TokenResponse
	err := c.json(ctx, "POST", "/v1/auth/token", nil, body, &out)
	return out, err
}

// ExchangeCode trades the desktop's one-time code and its PKCE verifier
// for a device token.
func (c *Client) ExchangeCode(ctx context.Context, code, verifier string) (protocol.TokenResponse, error) {
	return c.RequestToken(ctx, protocol.TokenRequest{GrantType: protocol.GrantAuthorizationCode, Code: code, CodeVerifier: verifier})
}

// ErrDenied is returned by PollDeviceToken when the user denied the
// sign-in; ErrExpiredAuth when it timed out.
var (
	ErrDenied      = errors.New("sync: the sign-in was denied")
	ErrExpiredAuth = errors.New("sync: the sign-in request expired; run nota login again")
)

// PollDeviceToken polls for the token of a device code until the user
// approves it, honouring the server's interval and slow_down. sleep
// waits between polls (time.Sleep-like, but returning early when ctx is
// done); nil uses a timer.
func (c *Client) PollDeviceToken(ctx context.Context, da protocol.DeviceAuthorization, sleep func(context.Context, time.Duration) error) (protocol.TokenResponse, error) {
	if sleep == nil {
		sleep = sleepCtx
	}
	interval := time.Duration(max(da.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(da.ExpiresIn) * time.Second)
	for {
		if err := sleep(ctx, interval); err != nil {
			return protocol.TokenResponse{}, err
		}
		res, err := c.RequestToken(ctx, protocol.TokenRequest{GrantType: protocol.GrantDeviceCode, DeviceCode: da.DeviceCode})
		if err == nil {
			return res, nil
		}
		ae := apiError(err)
		if ae == nil {
			// A network hiccup: keep polling until the request expires.
			if da.ExpiresIn > 0 && time.Now().After(deadline) {
				return protocol.TokenResponse{}, ErrExpiredAuth
			}
			continue
		}
		switch ae.Body.Code {
		case protocol.CodeAuthorizationPending:
		case protocol.CodeSlowDown:
			interval = time.Duration(max(ae.Body.Interval, int(interval.Seconds())+5)) * time.Second
		case protocol.CodeAccessDenied:
			return protocol.TokenResponse{}, ErrDenied
		case protocol.CodeExpiredToken:
			return protocol.TokenResponse{}, ErrExpiredAuth
		default:
			return protocol.TokenResponse{}, err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Me returns who the token belongs to.
func (c *Client) Me(ctx context.Context) (protocol.Me, error) {
	var out protocol.Me
	err := c.json(ctx, "GET", "/v1/auth/me", nil, nil, &out)
	return out, err
}

// Devices lists the user's active device tokens.
func (c *Client) Devices(ctx context.Context) ([]protocol.Device, error) {
	var out protocol.DeviceList
	err := c.json(ctx, "GET", "/v1/devices", nil, nil, &out)
	return out.Devices, err
}

// RevokeDevice revokes one of the user's device tokens.
func (c *Client) RevokeDevice(ctx context.Context, id int64) error {
	res, err := c.do(ctx, "DELETE", "/v1/devices/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// RevokeSelf revokes the client's own token (sign out).
func (c *Client) RevokeSelf(ctx context.Context) error {
	res, err := c.do(ctx, "DELETE", "/v1/devices/current", nil, nil)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// FindToken looks for a stored token of an account in the OS keyring
// and then in the credentials file, and says where it was found. It
// does not consult $NOTA_TOKEN.
func FindToken(account string) (token, store string, err error) {
	if t := lookupToken(account, StoreKeyring); t != "" {
		return t, StoreKeyring, nil
	}
	if t := lookupToken(account, StoreFile); t != "" {
		return t, StoreFile, nil
	}
	return "", "", ErrNoToken
}

func lookupToken(account, store string) string {
	if store == StoreKeyring && envKeyringFile() {
		return ""
	}
	t, err := loadStored(account, store)
	if err != nil {
		return ""
	}
	return t
}

// HistoryVersion fetches one version of a path from its history with its
// content (nil for a delete). Unlike GetVersion it follows renames.
func (c *Client) HistoryVersion(ctx context.Context, p string, version int64) (protocol.Entry, *string, error) {
	var out struct {
		protocol.Entry
		Content *string `json:"content"`
	}
	err := c.json(ctx, "GET", "/v1/api/vaults/"+url.PathEscape(c.Vault)+"/history/"+protocol.EscapePath(p)+
		"?version="+strconv.FormatInt(version, 10), nil, nil, &out)
	return out.Entry, out.Content, err
}

// Restore makes an old version of a path its newest version on the
// server (history is kept) and returns the new entry.
func (c *Client) Restore(ctx context.Context, p string, version int64) (protocol.Entry, error) {
	body, _ := json.Marshal(map[string]int64{"version": version})
	var out protocol.Entry
	err := c.json(ctx, "POST", "/v1/api/vaults/"+url.PathEscape(c.Vault)+"/history/"+protocol.EscapePath(p)+"/restore", nil, body, &out)
	return out, err
}
