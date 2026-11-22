package sync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// Client speaks the sync protocol to one vault on a server.
type Client struct {
	// BaseURL is the server, e.g. https://nota.example.com (no trailing path).
	BaseURL string
	// Vault is the vault name on the server.
	Vault string
	// Token is the device token sent as a bearer token.
	Token string
	// HTTP is the client to use; a client with sane timeouts when nil.
	HTTP *http.Client
	// UserAgent is sent with every request.
	UserAgent string
}

// APIError is an error response from the server.
type APIError struct {
	Status int
	Body   protocol.Error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("server: %s (%d %s)", e.Body.Message, e.Status, e.Body.Code)
}

// Code is the error code from the body.
func (e *APIError) Code() string { return e.Body.Code }

// Current is the path's current state from the body, if any.
func (e *APIError) Current() *protocol.Entry { return e.Body.Current }

// IsCode reports whether err is an APIError with the given code.
func IsCode(err error, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Body.Code == code
}

func apiError(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

var defaultHTTP = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 2 * time.Minute,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	MaxIdleConnsPerHost:   4,
}}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

// do sends a request and returns the response of a 2xx status; any other
// status is turned into an *APIError.
func (c *Client) do(ctx context.Context, method, path string, hdr http.Header, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	req.Header.Set(protocol.HeaderProtocol, strconv.Itoa(protocol.Version))
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 == 2 {
		return res, nil
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	ae := &APIError{Status: res.StatusCode}
	if json.Unmarshal(data, &ae.Body) != nil || ae.Body.Code == "" {
		ae.Body.Code = "http_" + strconv.Itoa(res.StatusCode)
		ae.Body.Message = strings.TrimSpace(string(data))
		if ae.Body.Message == "" {
			ae.Body.Message = res.Status
		}
	}
	return nil, ae
}

func (c *Client) json(ctx context.Context, method, path string, hdr http.Header, body []byte, out any) error {
	if hdr == nil {
		hdr = http.Header{}
	}
	if body != nil && hdr.Get("Content-Type") == "" {
		hdr.Set("Content-Type", "application/json")
	}
	res, err := c.do(ctx, method, path, hdr, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("sync: decode %s %s: %w", method, path, err)
	}
	return nil
}

// Vaults lists the user's vaults.
func (c *Client) Vaults(ctx context.Context) ([]protocol.VaultInfo, error) {
	var out protocol.VaultList
	err := c.json(ctx, "GET", protocol.VaultsPath(), nil, nil, &out)
	return out.Vaults, err
}

// CreateVault creates a vault named name.
func (c *Client) CreateVault(ctx context.Context, name string) (protocol.VaultInfo, error) {
	body, _ := json.Marshal(protocol.CreateVault{Name: name})
	var out protocol.VaultInfo
	err := c.json(ctx, "POST", protocol.VaultsPath(), nil, body, &out)
	return out, err
}

// Changes fetches one page of the change feed after since.
func (c *Client) Changes(ctx context.Context, since int64, limit int) (protocol.Changes, error) {
	q := url.Values{"since": {strconv.FormatInt(since, 10)}}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out protocol.Changes
	err := c.json(ctx, "GET", protocol.ChangesPath(c.Vault)+"?"+q.Encode(), nil, nil, &out)
	return out, err
}

// Get downloads the current content of a file and its entry. The content
// is checked against the hash the server sent.
func (c *Client) Get(ctx context.Context, p string) (protocol.Entry, []byte, error) {
	return c.get(ctx, protocol.FilePath(c.Vault, p), p)
}

// GetVersion downloads one version of a file from its history.
func (c *Client) GetVersion(ctx context.Context, p string, version int64) (protocol.Entry, []byte, error) {
	return c.get(ctx, protocol.FilePath(c.Vault, p)+"?version="+strconv.FormatInt(version, 10), p)
}

func (c *Client) get(ctx context.Context, route, p string) (protocol.Entry, []byte, error) {
	res, err := c.do(ctx, "GET", route, nil, nil)
	if err != nil {
		return protocol.Entry{}, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return protocol.Entry{}, nil, err
	}
	h := res.Header
	e := protocol.Entry{Path: p, Hash: h.Get(protocol.HeaderHash), Size: int64(len(data)),
		Kind: protocol.Kind(h.Get(protocol.HeaderKind)), DeviceID: h.Get(protocol.HeaderDevice)}
	e.Version, _ = strconv.ParseInt(h.Get(protocol.HeaderVersion), 10, 64)
	e.ModifiedAt, _ = time.Parse(time.RFC3339Nano, h.Get(protocol.HeaderModifiedAt))
	if mf := h.Get(protocol.HeaderMovedFrom); mf != "" {
		e.MovedFrom, _ = url.PathUnescape(mf)
	}
	if got := protocol.Hash(data); got != e.Hash || e.Version <= 0 {
		return protocol.Entry{}, nil, fmt.Errorf("sync: GET %s: content hash %s does not match %s (version %d)", p, got, e.Hash, e.Version)
	}
	return e, data, nil
}

// Put uploads content for a path based on version base (0 creates).
func (c *Client) Put(ctx context.Context, p string, base int64, data []byte, modified time.Time) (protocol.Entry, error) {
	hdr := http.Header{}
	hdr.Set(protocol.HeaderBaseVersion, strconv.FormatInt(base, 10))
	hdr.Set(protocol.HeaderHash, protocol.Hash(data))
	hdr.Set("Content-Type", "application/octet-stream")
	if !modified.IsZero() {
		hdr.Set(protocol.HeaderModifiedAt, modified.UTC().Format(time.RFC3339Nano))
	}
	if data == nil {
		data = []byte{}
	}
	var out protocol.Entry
	err := c.json(ctx, "PUT", protocol.FilePath(c.Vault, p), hdr, data, &out)
	return out, err
}

// Delete writes a tombstone for a path based on version base.
func (c *Client) Delete(ctx context.Context, p string, base int64) (protocol.Entry, error) {
	hdr := http.Header{}
	hdr.Set(protocol.HeaderBaseVersion, strconv.FormatInt(base, 10))
	var out protocol.Entry
	err := c.json(ctx, "DELETE", protocol.FilePath(c.Vault, p), hdr, nil, &out)
	return out, err
}

// Move renames a file on the server.
func (c *Client) Move(ctx context.Context, from, to string, base int64) (protocol.MoveResult, error) {
	body, _ := json.Marshal(protocol.Move{From: from, To: to, BaseVersion: base})
	var out protocol.MoveResult
	err := c.json(ctx, "POST", protocol.MovesPath(c.Vault), nil, body, &out)
	return out, err
}

// History lists the versions of a path, newest first.
func (c *Client) History(ctx context.Context, p string) (protocol.History, error) {
	var out protocol.History
	err := c.json(ctx, "GET", protocol.HistoryPath(c.Vault, p), nil, nil, &out)
	return out, err
}

// Events subscribes to the vault's change nudges and calls fn with the
// cursor of each one, until ctx is done or the stream breaks. It returns
// nil only when ctx was cancelled.
func (c *Client) Events(ctx context.Context, fn func(cursor int64)) error {
	hdr := http.Header{}
	hdr.Set("Accept", "text/event-stream")
	// The stream has no response deadline beyond the headers; use a
	// client without the overall timeout callers may set on HTTP.
	res, err := c.do(ctx, "GET", protocol.EventsPath(c.Vault), hdr, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	var event string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:") && event == protocol.EventChange:
			var ce protocol.ChangeEvent
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ce) == nil {
				fn(ce.Cursor)
			}
		case line == "":
			event = ""
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("sync: event stream closed by the server")
}
