package server

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// signedIn returns a browser with a fresh account (open sign-up).
func signedIn(t *testing.T, e *env, email string) *browser {
	t.Helper()
	b := e.browser()
	if st, m, _ := b.register(email, ""); st != 200 {
		t.Fatalf("register %s: %d %v", email, st, m)
	}
	return b
}

// device calls the API without a session, like the CLI and the desktop.
func (e *env) device(tok, method, path string, body any) (int, map[string]any) {
	e.t.Helper()
	data := ""
	if body != nil {
		b, _ := json.Marshal(body)
		data = string(b)
	}
	res, raw := e.do(tok, method, path, nil, data)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m
}

func TestDeviceCodeFlow(t *testing.T) {
	e, c := authEnv(t, Config{Signup: SignupOpen, WebURL: "https://nota.example.com"})
	b := signedIn(t, e, "anton@example.com")

	st, m := e.device("", "POST", "/v1/auth/device", protocol.DeviceAuthRequest{Device: "cli"})
	if st != 200 {
		t.Fatalf("device: %d %v", st, m)
	}
	var da protocol.DeviceAuthorization
	raw, _ := json.Marshal(m)
	_ = json.Unmarshal(raw, &da)
	if da.Interval != 5 || da.ExpiresIn != 600 || len(da.UserCode) != 9 || da.VerificationURI != "https://nota.example.com/app/connect" ||
		da.VerificationURIComplete != "https://nota.example.com/app/connect?code="+da.UserCode {
		t.Fatalf("authorization: %+v", da)
	}
	poll := func(dc string) (int, map[string]any) {
		return e.device("", "POST", "/v1/auth/token", protocol.TokenRequest{GrantType: protocol.GrantDeviceCode, DeviceCode: dc})
	}
	if st, m := poll(da.DeviceCode); st != 400 || code(m) != protocol.CodeAuthorizationPending {
		t.Fatalf("first poll: %d %v", st, m)
	}
	if st, m := poll(da.DeviceCode); st != 400 || code(m) != protocol.CodeSlowDown || m["interval"].(float64) != 10 {
		t.Fatalf("fast poll: %d %v", st, m)
	}
	// The browser shows which device asks, then approves. User codes are
	// case- and dash-insensitive.
	typed := strings.ToLower(strings.ReplaceAll(da.UserCode, "-", ""))
	var look DeviceAuth
	if st, m := b.call("POST", "/v1/auth/device/lookup", UserCodeRequest{UserCode: typed}, &look); st != 200 || look.Device != "cli" {
		t.Fatalf("lookup: %d %v", st, m)
	}
	// A device token cannot approve sign-ins; that takes a browser session.
	owner := e.user("eva@example.com")
	if st, m := e.device(owner, "POST", "/v1/auth/device/approve", UserCodeRequest{UserCode: da.UserCode}); st != 403 {
		t.Fatalf("approve with a token: %d %v", st, m)
	}
	if st, m := b.call("POST", "/v1/auth/device/approve", UserCodeRequest{UserCode: typed}, nil); st != 204 {
		t.Fatalf("approve: %d %v", st, m)
	}
	c.Add(11 * time.Second)
	st, m = poll(da.DeviceCode)
	if st != 200 || m["email"] != "anton@example.com" || m["device"] != "cli" || !strings.HasPrefix(m["token"].(string), TokenPrefix) {
		t.Fatalf("token: %d %v", st, m)
	}
	tok := m["token"].(string)
	if st, m := poll(da.DeviceCode); st != 400 || code(m) != protocol.CodeExpiredToken {
		t.Fatalf("poll after success: %d %v", st, m)
	}
	// The token works for sync, as that user.
	var me MeResponse
	res, body := e.do(tok, "GET", "/v1/auth/me", nil, "")
	_ = json.Unmarshal(body, &me)
	if res.StatusCode != 200 || me.User.Email != "anton@example.com" || me.Via != "token" || me.Device != "cli" {
		t.Fatalf("me with the token: %d %s", res.StatusCode, body)
	}
	if res, body := e.do(tok, "POST", protocol.VaultsPath(), nil, `{"name":"main"}`); res.StatusCode != 201 {
		t.Fatalf("create vault with the new token: %d %s", res.StatusCode, body)
	}

	// Denied and expired requests.
	_, m = e.device("", "POST", "/v1/auth/device", protocol.DeviceAuthRequest{Device: "other"})
	denied, uc := m["device_code"].(string), m["user_code"].(string)
	no := false
	if st, m := b.call("POST", "/v1/auth/device/approve", UserCodeRequest{UserCode: uc, Approve: &no}, nil); st != 204 {
		t.Fatalf("deny: %d %v", st, m)
	}
	if st, m := poll(denied); st != 400 || code(m) != protocol.CodeAccessDenied {
		t.Fatalf("denied poll: %d %v", st, m)
	}
	_, m = e.device("", "POST", "/v1/auth/device", protocol.DeviceAuthRequest{Device: "late"})
	late, uc := m["device_code"].(string), m["user_code"].(string)
	c.Add(DeviceCodeTTL + time.Second)
	if st, m := poll(late); st != 400 || code(m) != protocol.CodeExpiredToken {
		t.Fatalf("expired poll: %d %v", st, m)
	}
	if st, m := b.call("POST", "/v1/auth/device/approve", UserCodeRequest{UserCode: uc}, nil); st != 404 {
		t.Fatalf("approve expired: %d %v", st, m)
	}
	if st, m := poll("ndev_unknown"); st != 400 || code(m) != protocol.CodeInvalidGrant {
		t.Fatalf("unknown device code: %d %v", st, m)
	}
	if st, m := e.device("", "POST", "/v1/auth/device", protocol.DeviceAuthRequest{Device: " "}); st != 400 {
		t.Fatalf("empty device: %d %v", st, m)
	}
}

func TestPKCEConnectFlow(t *testing.T) {
	e, c := authEnv(t, Config{Signup: SignupOpen})
	b := signedIn(t, e, "anton@example.com")
	verifier := strings.Repeat("v3rifier-", 6) // 54 characters
	challenge := s256(verifier)

	connect := func(req protocol.ConnectRequest) (int, map[string]any, protocol.ConnectResponse) {
		var out protocol.ConnectResponse
		st, m := b.call("POST", "/v1/auth/connect", req, &out)
		return st, m, out
	}
	good := protocol.ConnectRequest{CodeChallenge: challenge, CodeChallengeMethod: "S256", Device: "MacBook", RedirectURI: "nota://auth", State: "s1"}
	st, m, cr := connect(good)
	if st != 200 || cr.ExpiresIn != 120 {
		t.Fatalf("connect: %d %v", st, m)
	}
	u, err := url.Parse(cr.Redirect)
	if err != nil || u.Scheme != "nota" || u.Host != "auth" || u.Query().Get("code") != cr.Code || u.Query().Get("state") != "s1" {
		t.Fatalf("redirect %q", cr.Redirect)
	}
	exchange := func(code, verifier string) (int, map[string]any) {
		return e.device("", "POST", "/v1/auth/token", protocol.TokenRequest{GrantType: protocol.GrantAuthorizationCode, Code: code, CodeVerifier: verifier})
	}
	// A wrong verifier burns the code.
	if st, m := exchange(cr.Code, strings.Repeat("x", 43)); st != 400 || code(m) != protocol.CodeInvalidGrant {
		t.Fatalf("wrong verifier: %d %v", st, m)
	}
	if st, m := exchange(cr.Code, verifier); st != 400 || code(m) != protocol.CodeInvalidGrant {
		t.Fatalf("code reused after a wrong verifier: %d %v", st, m)
	}
	_, _, cr = connect(good)
	st, m = exchange(cr.Code, verifier)
	if st != 200 || m["device"] != "MacBook" || m["email"] != "anton@example.com" || m["token_type"] != "bearer" {
		t.Fatalf("exchange: %d %v", st, m)
	}
	if st, m := exchange(cr.Code, verifier); st != 400 || code(m) != protocol.CodeInvalidGrant {
		t.Fatalf("code used twice: %d %v", st, m)
	}
	// Codes are short-lived.
	_, _, cr = connect(good)
	c.Add(ConnectCodeTTL + time.Second)
	if st, m := exchange(cr.Code, verifier); st != 400 || code(m) != protocol.CodeInvalidGrant || !strings.Contains(m["message"].(string), "expired") {
		t.Fatalf("expired code: %d %v", st, m)
	}
	// Loopback redirects work; anything else does not.
	loop := good
	loop.RedirectURI = "http://127.0.0.1:53682/callback"
	if st, m, cr := connect(loop); st != 200 || !strings.HasPrefix(cr.Redirect, "http://127.0.0.1:53682/callback?code=") {
		t.Fatalf("loopback: %d %v %+v", st, m, cr)
	}
	for _, bad := range []protocol.ConnectRequest{
		{CodeChallenge: challenge, CodeChallengeMethod: "S256", Device: "x", RedirectURI: "https://evil.example.org/steal"},
		{CodeChallenge: challenge, CodeChallengeMethod: "S256", Device: "x", RedirectURI: "nota://evil"},
		{CodeChallenge: challenge, CodeChallengeMethod: "plain", Device: "x"},
		{CodeChallenge: "short", CodeChallengeMethod: "S256", Device: "x"},
		{CodeChallenge: challenge, CodeChallengeMethod: "S256", Device: ""},
	} {
		if st, m, _ := connect(bad); st != 400 {
			t.Errorf("connect %+v: %d %v", bad, st, m)
		}
	}
	// No session, no code.
	if st, m := e.device("", "POST", "/v1/auth/connect", good); st != 401 {
		t.Fatalf("connect without a session: %d %v", st, m)
	}
	if st, m := exchange("x", "short"); st != 400 || code(m) != protocol.CodeInvalidGrant {
		t.Fatalf("short verifier: %d %v", st, m)
	}
	if st, m := e.device("", "POST", "/v1/auth/token", protocol.TokenRequest{GrantType: "password"}); st != 400 || code(m) != protocol.CodeBadRequest {
		t.Fatalf("unknown grant: %d %v", st, m)
	}
}

func TestDevicesListAndRevoke(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b := signedIn(t, e, "anton@example.com")
	u, _ := e.srv.Store().UserByEmail(t.Context(), "anton@example.com")
	laptop, lt, _ := e.srv.Store().CreateToken(t.Context(), u.ID, "laptop")
	phone, pt, _ := e.srv.Store().CreateToken(t.Context(), u.ID, "phone")
	eva := e.user("eva@example.com")

	var list protocol.DeviceList
	res, body := e.do(laptop, "GET", "/v1/devices", nil, "")
	_ = json.Unmarshal(body, &list)
	if res.StatusCode != 200 || len(list.Devices) != 2 || !list.Devices[0].Current || list.Devices[1].Current || list.Devices[1].Device != "phone" {
		t.Fatalf("devices: %d %s", res.StatusCode, body)
	}
	// Another user's device is not found.
	if st, m := e.device(eva, "DELETE", "/v1/devices/"+itoa(lt.ID), nil); st != 404 {
		t.Fatalf("revoke someone else's device: %d %v", st, m)
	}
	// The browser signs the phone out.
	if st, m := b.call("DELETE", "/v1/devices/"+itoa(pt.ID), nil, nil); st != 204 {
		t.Fatalf("revoke phone: %d %v", st, m)
	}
	if res, _ := e.do(phone, "GET", protocol.VaultsPath(), nil, ""); res.StatusCode != 401 {
		t.Fatalf("revoked token still works: %d", res.StatusCode)
	}
	if st, _ := b.call("GET", "/v1/devices", nil, &list); st != 200 || len(list.Devices) != 1 || list.Devices[0].Current {
		t.Fatalf("devices from the browser: %+v", list)
	}
	// nota logout: the token revokes itself.
	if st, m := e.device(laptop, "DELETE", "/v1/devices/current", nil); st != 204 {
		t.Fatalf("revoke current: %d %v", st, m)
	}
	if st, _ := e.device(laptop, "GET", "/v1/devices", nil); st != 401 {
		t.Fatalf("after logout: %d", st)
	}
	if st, _ := b.call("DELETE", "/v1/devices/current", nil, nil); st != 400 {
		t.Fatalf("current from a browser: %d", st)
	}
}
