package protocol

import "time"

// Wire types of the device sign-in flows (docs/api.md). A device gets a
// device token, the bearer token of the sync protocol, in one of two
// ways:
//
//   - Desktop (browser + PKCE): the app makes a code verifier, opens the
//     web app's /app/connect page with the S256 challenge, the signed-in
//     browser asks the API for a one-time code bound to that challenge
//     (POST /v1/auth/connect) and hands it back through the nota://auth
//     deep link; the app trades code + verifier for a token
//     (POST /v1/auth/token, grant_type GrantAuthorizationCode).
//   - CLI (device code, RFC 8628 style): `nota login` asks for a device
//     code (POST /v1/auth/device), shows the user code and the link,
//     and polls POST /v1/auth/token with grant_type GrantDeviceCode
//     until the user approved it in the browser.

// Grant types of POST /v1/auth/token.
const (
	GrantAuthorizationCode = "authorization_code"
	GrantDeviceCode        = "urn:ietf:params:oauth:grant-type:device_code"
)

// Error codes of POST /v1/auth/token, as in OAuth.
const (
	CodeAuthorizationPending = "authorization_pending"
	CodeSlowDown             = "slow_down"
	CodeExpiredToken         = "expired_token"
	CodeAccessDenied         = "access_denied"
	CodeInvalidGrant         = "invalid_grant"
)

// DeviceAuthRequest is the body of POST /v1/auth/device.
type DeviceAuthRequest struct {
	// Device names the device the token is for ("laptop").
	Device string `json:"device"`
}

// DeviceAuthorization is the response of POST /v1/auth/device.
type DeviceAuthorization struct {
	// DeviceCode is the secret the device polls with.
	DeviceCode string `json:"device_code"`
	// UserCode is short and human: the user checks that the browser
	// shows the same one before approving.
	UserCode string `json:"user_code"`
	// VerificationURI is the page to approve on; the Complete form has
	// the user code filled in.
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	// Interval is the minimum number of seconds between polls.
	Interval int `json:"interval"`
}

// ConnectRequest is the body of POST /v1/auth/connect: the signed-in web
// app asks for a one-time code for the desktop app.
type ConnectRequest struct {
	// CodeChallenge is base64url(SHA-256(code_verifier)), no padding.
	CodeChallenge string `json:"code_challenge"`
	// CodeChallengeMethod must be "S256".
	CodeChallengeMethod string `json:"code_challenge_method"`
	Device              string `json:"device"`
	// RedirectURI is where the browser goes next: nota://auth, or a
	// loopback http://127.0.0.1:<port>/… URL.
	RedirectURI string `json:"redirect_uri"`
	// State is handed back unchanged in the redirect.
	State string `json:"state,omitempty"`
}

// ConnectResponse carries the one-time code and the full redirect URL
// (redirect_uri with code and state added).
type ConnectResponse struct {
	Code      string `json:"code"`
	Redirect  string `json:"redirect"`
	ExpiresIn int    `json:"expires_in"`
}

// TokenRequest is the body of POST /v1/auth/token.
type TokenRequest struct {
	GrantType string `json:"grant_type"`
	// Code and CodeVerifier for GrantAuthorizationCode.
	Code         string `json:"code,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	// DeviceCode for GrantDeviceCode.
	DeviceCode string `json:"device_code,omitempty"`
}

// TokenResponse is a newly issued device token.
type TokenResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	TokenID   int64  `json:"token_id"`
	Device    string `json:"device"`
	Email     string `json:"email"`
}

// Device is one device token of the signed-in user.
type Device struct {
	ID         int64      `json:"id"`
	Device     string     `json:"device"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	// Current marks the token the request was made with.
	Current bool `json:"current"`
}

// DeviceList is the body of GET /v1/devices.
type DeviceList struct {
	Devices []Device `json:"devices"`
}

// Me is the body of GET /v1/auth/me.
type Me struct {
	User struct {
		ID        int64     `json:"id"`
		Email     string    `json:"email"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"user"`
	Via    string `json:"via"`
	Device string `json:"device,omitempty"`
}

// AccountSettings is the body of GET and PATCH /v1/account/settings:
// the settings the account layer sets (internal/settings), and a
// revision that grows with every change.
type AccountSettings struct {
	Rev      int64             `json:"rev"`
	Settings map[string]string `json:"settings"`
}

// SettingsPatch is the body of PATCH /v1/account/settings. Keys change
// one by one, so two devices that change different keys never collide;
// for the same key the last write wins.
type SettingsPatch struct {
	Set   map[string]string `json:"set,omitempty"`
	Unset []string          `json:"unset,omitempty"`
}
