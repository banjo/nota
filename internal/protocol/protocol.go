// Package protocol defines the Nota sync protocol: the routes, headers,
// wire types and error codes shared by the sync server (internal/server)
// and the client engine (internal/sync). docs/sync-protocol.md is the
// prose version of this file.
//
// The protocol is versioned. Every request carries the Nota-Protocol
// header with the client's major version; a server that does not speak
// that major refuses the request with 426 Upgrade Required. Fields may be
// added within a major version, so decoders must ignore unknown fields.
//
// All routes are scoped to a vault: /v1/vaults/{vault}/…. Paths inside a
// vault are slash-separated, relative and in Unicode NFC. Ordering comes
// only from server-assigned versions, never from device clocks.
package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Version is the protocol major version this build speaks.
const Version = 1

// Headers.
const (
	// HeaderProtocol carries the protocol major version on every request
	// and response ("1"). A "1.x" value is accepted as major 1.
	HeaderProtocol = "Nota-Protocol"
	// HeaderBaseVersion is the version a PUT or DELETE was based on; 0
	// creates a file that does not exist (or is deleted).
	HeaderBaseVersion = "Base-Version"
	// HeaderVersion is the file version of a GET response.
	HeaderVersion = "Nota-Version"
	// HeaderHash is the content hash (see Hash). On a PUT the client may
	// send it; the server then refuses a body that does not match.
	HeaderHash = "Nota-Hash"
	// HeaderKind is the file kind of a GET response ("md" or "blob").
	HeaderKind = "Nota-Kind"
	// HeaderDevice is the device that wrote the version (GET response).
	HeaderDevice = "Nota-Device"
	// HeaderModifiedAt is the client's modification time (RFC 3339),
	// for display only. Sent on PUT, returned on GET.
	HeaderModifiedAt = "Nota-Modified-At"
	// HeaderMovedFrom names the path a version was moved from (GET).
	HeaderMovedFrom = "Nota-Moved-From"
)

// QueryProtocol may carry the protocol version instead of the header, for
// clients that cannot set headers (a browser's EventSource).
const QueryProtocol = "protocol"

// Kind is the kind of a file: markdown notes are merged line by line,
// everything else is an opaque blob.
type Kind string

// File kinds.
const (
	KindMD   Kind = "md"
	KindBlob Kind = "blob"
)

// KindOf returns the kind of a vault path from its extension.
func KindOf(p string) Kind {
	if strings.EqualFold(path.Ext(p), ".md") {
		return KindMD
	}
	return KindBlob
}

// Entry is the state of one path at one version: the current state in a
// change list, or one step of its history.
type Entry struct {
	// Path is the vault-relative NFC path.
	Path string `json:"path"`
	// Version is assigned by the server from the vault's sequence. Every
	// change to any path in the vault gets a higher one, so versions are
	// also the cursor of the change feed.
	Version int64 `json:"version"`
	// Hash is the content hash (see Hash). A tombstone keeps the hash of
	// the content it deleted.
	Hash string `json:"hash"`
	// Size is the content length in bytes.
	Size int64 `json:"size"`
	// Deleted marks a tombstone.
	Deleted bool `json:"deleted"`
	Kind    Kind `json:"kind"`
	// DeviceID names the device (token) that wrote this version.
	DeviceID string `json:"device_id"`
	// ModifiedAt is the writing client's clock. Display only: never use
	// it for ordering.
	ModifiedAt time.Time `json:"modified_at"`
	// MovedFrom is set on the version a move created: the path the
	// content came from. History follows it.
	MovedFrom string `json:"moved_from,omitempty"`
}

// VaultInfo describes one vault of the authenticated user.
type VaultInfo struct {
	Name string `json:"name"`
	// Cursor is the vault's latest version (0 for an empty vault).
	Cursor    int64     `json:"cursor"`
	CreatedAt time.Time `json:"created_at"`
}

// VaultList is the body of GET /v1/vaults.
type VaultList struct {
	Vaults []VaultInfo `json:"vaults"`
}

// CreateVault is the body of POST /v1/vaults.
type CreateVault struct {
	Name string `json:"name"`
}

// Changes is the body of GET /v1/vaults/{vault}/changes: the current
// state of every path whose version is above the requested cursor, in
// version order.
type Changes struct {
	Entries []Entry `json:"entries"`
	// Cursor is where the next request continues: the last entry's
	// version when More is set, else the vault's latest version.
	Cursor int64 `json:"cursor"`
	// More reports that the page was full; fetch again from Cursor.
	More bool `json:"more"`
}

// DefaultChangesLimit and MaxChangesLimit bound a page of changes.
const (
	DefaultChangesLimit = 500
	MaxChangesLimit     = 5000
)

// Move is the body of POST /v1/vaults/{vault}/moves: rename From to To.
// It is a delete of From and a create of To with the same content, done
// atomically, and To's history continues From's.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
	// BaseVersion is the version of From the client has.
	BaseVersion int64 `json:"base_version"`
}

// MoveResult is the response to a move: the tombstone of From and the
// new entry of To.
type MoveResult struct {
	From Entry `json:"from"`
	To   Entry `json:"to"`
}

// History is the body of GET /v1/vaults/{vault}/history/{path}: every
// version of the path, newest first, continuing through moves.
type History struct {
	Path     string  `json:"path"`
	Versions []Entry `json:"versions"`
}

// ChangeEvent is the data of an SSE "change" event: the vault's latest
// version. It is only a nudge to pull; the change feed is the truth.
type ChangeEvent struct {
	Cursor int64 `json:"cursor"`
}

// SettingsEvent is the data of an SSE "settings" event on
// /v1/account/events: the account's settings revision. Like a change
// event it is a nudge: fetch GET /v1/account/settings.
type SettingsEvent struct {
	Rev int64 `json:"rev"`
}

// SSE event names.
const (
	EventChange   = "change"
	EventSettings = "settings"
)

// Error is the JSON body of every error response.
type Error struct {
	// Code is a stable machine-readable reason (see the Code constants).
	Code    string `json:"code"`
	Message string `json:"message"`
	// Current is the path's current state, for version_mismatch, deleted
	// and exists.
	Current *Entry `json:"current,omitempty"`
	// Existing is the path already in the vault for case_collision.
	Existing string `json:"existing,omitempty"`
	// Supported lists the protocol majors the server speaks, for
	// unsupported_protocol.
	Supported []int `json:"supported,omitempty"`
	// Interval is the poll interval in seconds, for slow_down and
	// authorization_pending (device sign-in).
	Interval int `json:"interval,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Error codes.
const (
	CodeBadRequest          = "bad_request"
	CodeUnauthorized        = "unauthorized"
	CodeNotFound            = "not_found"
	CodeDeleted             = "deleted"
	CodeVersionMismatch     = "version_mismatch"
	CodeCaseCollision       = "case_collision"
	CodeExists              = "exists"
	CodeTooLarge            = "too_large"
	CodeInvalidPath         = "invalid_path"
	CodeHashMismatch        = "hash_mismatch"
	CodeUnsupportedProtocol = "unsupported_protocol"
	CodeInternal            = "internal"
)

// ParseVersion reads a Nota-Protocol value ("1" or "1.2") and returns the
// major version.
func ParseVersion(s string) (int, error) {
	s = strings.TrimSpace(s)
	major, _, _ := strings.Cut(s, ".")
	n, err := strconv.Atoi(major)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("protocol: bad version %q", s)
	}
	return n, nil
}

// Hash returns the content hash used everywhere in the protocol: the
// lower-case hex SHA-256 of the bytes.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ValidHash reports whether s looks like a Hash.
func ValidHash(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// ErrInvalidPath is returned by CleanPath.
var ErrInvalidPath = errors.New("invalid path")

// CleanPath checks a vault path from the wire and returns it in NFC. A
// valid path is relative, slash-separated, has no empty, "." or ".."
// segments, no backslashes and no control characters. Paths are not
// otherwise rewritten: "a//b" is refused rather than cleaned, so client
// and server always agree on identity.
func CleanPath(p string) (string, error) {
	p = norm.NFC.String(p)
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w %q: %s", ErrInvalidPath, p, why)
	}
	if p == "" {
		return bad("empty")
	}
	if len(p) > 1024 {
		return bad("longer than 1024 bytes")
	}
	if strings.ContainsRune(p, '\\') {
		return bad("contains a backslash")
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return bad("contains a control character")
		}
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "":
			return bad("empty segment or leading/trailing slash")
		case ".", "..":
			return bad("contains . or ..")
		}
	}
	return p, nil
}

// FoldKey is the key two paths collide on in a case-insensitive file
// system: NFC and lower case. Two different paths with the same FoldKey
// cannot both exist in a vault.
func FoldKey(p string) string { return strings.ToLower(norm.NFC.String(p)) }

var vaultName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidVaultName reports whether s can name a vault: 1–64 ASCII letters,
// digits, ".", "_" or "-", starting with a letter or digit.
func ValidVaultName(s string) bool { return vaultName.MatchString(s) }

// EscapePath escapes a vault path for use in a URL, segment by segment,
// so slashes stay separators and everything else is percent-encoded.
func EscapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// Route builders. They return paths (no host) with the vault name and
// file path escaped.

// VaultsPath is GET (list) and POST (create).
func VaultsPath() string { return "/v1/vaults" }

// ChangesPath is GET ?since=&limit=.
func ChangesPath(vault string) string { return "/v1/vaults/" + url.PathEscape(vault) + "/changes" }

// FilePath is GET, PUT and DELETE of one file.
func FilePath(vault, p string) string {
	return "/v1/vaults/" + url.PathEscape(vault) + "/files/" + EscapePath(p)
}

// MovesPath is POST of a Move.
func MovesPath(vault string) string { return "/v1/vaults/" + url.PathEscape(vault) + "/moves" }

// HistoryPath is GET of a path's History.
func HistoryPath(vault, p string) string {
	return "/v1/vaults/" + url.PathEscape(vault) + "/history/" + EscapePath(p)
}

// EventsPath is the SSE stream of change nudges.
func EventsPath(vault string) string { return "/v1/vaults/" + url.PathEscape(vault) + "/events" }
