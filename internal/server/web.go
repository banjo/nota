package server

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// This file has the browser-facing plumbing shared by the auth routes
// and the web API: the client IP behind trusted proxies, rate limiting,
// CORS for the web app's origin and the CSRF rules for cookie requests.

// HeaderClient must be sent by the web app on every state-changing
// request that is authenticated by the session cookie (and on the
// sign-in calls). A custom header cannot be set cross-site without a
// CORS preflight, which only the configured web origins pass.
const HeaderClient = "Nota-Client"

// ParseProxies reads a comma-separated list of trusted proxy addresses
// and CIDR prefixes. "private" stands for loopback plus the private
// ranges (10/8, 172.16/12, 192.168/16, fc00::/7), which covers a reverse
// proxy on the same Docker network such as Coolify's Traefik.
func ParseProxies(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		switch {
		case f == "":
		case f == "private":
			for _, p := range []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"} {
				out = append(out, netip.MustParsePrefix(p))
			}
		case strings.Contains(f, "/"):
			p, err := netip.ParsePrefix(f)
			if err != nil {
				return nil, fmt.Errorf("server: trusted proxy %q: %w", f, err)
			}
			out = append(out, p.Masked())
		default:
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("server: trusted proxy %q: %w", f, err)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out, nil
}

func trusted(proxies []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	return slices.ContainsFunc(proxies, func(p netip.Prefix) bool { return p.Contains(a) })
}

// clientIP returns the address of the client: the TCP peer, or, when the
// peer is a trusted proxy, the right-most address in X-Forwarded-For that
// is not a trusted proxy itself. Headers from untrusted peers are
// ignored, so clients cannot pick their own rate-limit bucket.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !trusted(s.cfg.TrustedProxies, peer) {
		return peer.String()
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break // a malformed hop ends the trustworthy part of the chain
		}
		client = a.Unmap()
		if !trusted(s.cfg.TrustedProxies, client) {
			break
		}
	}
	return client.String()
}

// limiter is a token bucket per key (an IP), with buckets dropped once
// they have been full for a while.
type limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
	sweep   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(perMinute, burst int, now func() time.Time) *limiter {
	return &limiter{rate: float64(perMinute) / 60, burst: float64(burst), buckets: map[string]*bucket{}, now: now}
}

// allow takes a token for key; when there is none it returns false and
// how long until the next one.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.sweep) > time.Minute {
		full := time.Duration(l.burst / l.rate * float64(time.Second))
		for k, b := range l.buckets {
			if now.Sub(b.last) > full {
				delete(l.buckets, k)
			}
		}
		l.sweep = now
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// rateLimit answers 429 when the client IP is out of tokens.
func (s *Server) rateLimit(r *http.Request) error {
	if s.limit == nil {
		return nil
	}
	ip := s.clientIP(r)
	ok, wait := s.limit.allow(ip)
	if ok {
		return nil
	}
	info(r).attrs = append(info(r).attrs, "rate_limited", true)
	return &apiErr{http.StatusTooManyRequests, protocol.Error{Code: CodeRateLimited,
		Message: fmt.Sprintf("too many attempts; try again in %d s", int(wait.Seconds())+1)}}
}

// allowedOrigin reports whether origin is one of the web app's origins.
func (s *Server) allowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	return slices.ContainsFunc(s.cfg.WebOrigins, func(o string) bool { return strings.EqualFold(o, origin) })
}

// exposed are the response headers the web app may read.
var exposed = strings.Join([]string{protocol.HeaderProtocol, protocol.HeaderVersion, protocol.HeaderHash,
	protocol.HeaderKind, protocol.HeaderDevice, protocol.HeaderModifiedAt, protocol.HeaderMovedFrom, "ETag", "Retry-After", "Content-Disposition"}, ", ")

var allowedHeaders = strings.Join([]string{"Content-Type", "Authorization", HeaderClient, protocol.HeaderProtocol,
	protocol.HeaderBaseVersion, protocol.HeaderHash, protocol.HeaderModifiedAt, "Last-Event-ID"}, ", ")

// cors adds CORS headers for the configured web origins (credentials
// allowed) and answers preflight requests.
func (s *Server) cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		ok := s.allowedOrigin(origin)
		w.Header().Add("Vary", "Origin")
		if ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", exposed)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if ok {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// checkCSRF enforces the rules for requests a browser could be tricked
// into sending: an unsafe method must carry the Nota-Client header, and
// an Origin header, when present, must be a web origin of this server or
// the server itself. Bearer-token requests are not cookie-based and skip
// this.
func (s *Server) checkCSRF(r *http.Request) error {
	if safeMethod(r.Method) {
		return nil
	}
	forbidden := func(msg string) error {
		return &apiErr{http.StatusForbidden, protocol.Error{Code: CodeCSRF, Message: msg}}
	}
	if r.Header.Get(HeaderClient) == "" {
		return forbidden("missing " + HeaderClient + " header")
	}
	if o := r.Header.Get("Origin"); o != "" && !s.allowedOrigin(o) && !s.sameOrigin(r, o) {
		return forbidden("origin " + o + " is not allowed")
	}
	return nil
}

// sameOrigin reports whether origin is the server's own (public) origin.
func (s *Server) sameOrigin(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if s.cfg.PublicURL != "" {
		pu, err := url.Parse(s.cfg.PublicURL)
		return err == nil && strings.EqualFold(pu.Scheme, u.Scheme) && strings.EqualFold(pu.Host, u.Host)
	}
	return strings.EqualFold(u.Host, r.Host)
}

// Error codes of the account and web routes, next to the protocol's.
const (
	CodeRateLimited   = "rate_limited"
	CodeCSRF          = "csrf"
	CodeForbidden     = "forbidden"
	CodeSignupClosed  = "signup_closed"
	CodeInviteNeeded  = "invite_required"
	CodeBadInvite     = "invalid_invite"
	CodeWebAuthn      = "webauthn_failed"
	CodeNeedsPasskey  = "passkey_required"
	CodeExpired       = "expired"
	CodeLastPasskey   = "last_passkey"
	CodeBadCredential = "invalid_credentials"
)
