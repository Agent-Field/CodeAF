package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// ── SIGNING IN TO GITHUB FROM THIS MACHINE, IN THE BROWSER ───────────────────
//
// The app's first run connects GitHub with GitHub's OAuth device flow: the
// person is shown a one-time code, approves it on github.com/login/device,
// and the token GitHub then hands back is a token like any pasted one. It
// goes through the same connect as a pasted token and is kept where a pasted
// token is kept. The device flow suits a desktop app: it needs no client
// secret and no local redirect server.
//
// THE CLIENT ID IS NOT A SECRET. It names the OAuth app the person approves
// (registered by the owner under the AgentField organization, with device
// flow enabled). It is stamped in at build time and may be set at run time
// with CODEAF_GITHUB_CLIENT_ID. A BUILD WITH NO CLIENT ID HAS NO BROWSER
// SIGN-IN: the verb is absent, and the first run offers the gh login and a
// pasted token as before.

// clientID is the OAuth app stamped in by the build:
//
//	-X github.com/Agent-Field/codeaf/internal/factory/github.clientID=<id>
var clientID string

// DeviceScope is what the browser sign-in asks GitHub for. `repo` is the one
// classic scope that lets the factory read issues, comment, push a branch and
// open a pull request on a private repository; classic OAuth has no narrower
// scope for private repositories, and GitHub answers 404 for a private
// repository a token cannot see. Nothing else is asked for: reading who signed
// in (GET /user) needs no scope.
const DeviceScope = "repo"

// deviceTimeout bounds each call to GitHub's sign-in endpoints.
const deviceTimeout = 15 * time.Second

// DeviceClientID is the OAuth app the browser sign-in uses, or "" when this
// build has none.
func DeviceClientID() string {
	if id := strings.TrimSpace(env.Get("CODEAF_GITHUB_CLIENT_ID")); id != "" {
		return id
	}
	return strings.TrimSpace(clientID)
}

// deviceBase is where the sign-in endpoints live: github.com, or a stand-in
// GitHub named by CODEAF_GITHUB_LOGIN_URL for a test. A STAND-IN THAT IS NOT
// HTTPS MUST BE ON THIS MACHINE, so no setting can send the code across a
// network in the clear.
func deviceBase() (string, error) {
	raw := strings.TrimRight(strings.TrimSpace(env.Get("CODEAF_GITHUB_LOGIN_URL")), "/")
	if raw == "" {
		return "https://github.com", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("CODEAF_GITHUB_LOGIN_URL is not an address")
	}
	if u.Scheme == "https" {
		return raw, nil
	}
	if ip := net.ParseIP(u.Hostname()); u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
		return raw, nil
	}
	return "", errors.New("CODEAF_GITHUB_LOGIN_URL must be https, or http on this machine")
}

// DeviceCode is one started sign-in. Code is GitHub's device code, which
// stays in the engine; UserCode and VerificationURI are what the person sees.
type DeviceCode struct {
	Code            string
	UserCode        string
	VerificationURI string
	Interval        int
	ExpiresIn       int
}

// The answers GitHub gives a poll that has no token yet.
const (
	DevicePending  = "authorization_pending"
	DeviceSlowDown = "slow_down"
	DeviceExpired  = "expired_token"
	DeviceDenied   = "access_denied"
)

// DeviceGrant is one poll. Token is set once the person approved; otherwise
// Error is one of the Device* answers and Interval may be GitHub's new pace.
type DeviceGrant struct {
	Token    string
	Scope    string
	Error    string
	Interval int
}

// DeviceClient speaks GitHub's device-flow endpoints. The zero value uses
// http.DefaultClient and [DeviceClientID].
type DeviceClient struct {
	ClientID string
	HTTP     *http.Client
}

func (c DeviceClient) id() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return DeviceClientID()
}

// StartDevice asks GitHub for a code the person approves in the browser.
func (c DeviceClient) StartDevice(ctx context.Context) (DeviceCode, error) {
	id := c.id()
	if id == "" {
		return DeviceCode{}, errors.New("this build has no GitHub sign-in")
	}
	var out struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
		Error           string `json:"error"`
	}
	if err := c.post(ctx, "/login/device/code", url.Values{"client_id": {id}, "scope": {DeviceScope}}, &out); err != nil {
		return DeviceCode{}, err
	}
	if out.Error == "device_flow_disabled" {
		return DeviceCode{}, errors.New("GitHub sign-in is turned off for this app")
	}
	if out.DeviceCode == "" || out.UserCode == "" || out.ExpiresIn <= 0 {
		return DeviceCode{}, errors.New("GitHub sent an invalid sign-in code")
	}
	if u, err := url.Parse(out.VerificationURI); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return DeviceCode{}, errors.New("GitHub sent an invalid sign-in address")
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	return DeviceCode{Code: out.DeviceCode, UserCode: out.UserCode, VerificationURI: out.VerificationURI, Interval: out.Interval, ExpiresIn: out.ExpiresIn}, nil
}

// PollDevice asks GitHub once whether the person approved code.
func (c DeviceClient) PollDevice(ctx context.Context, code string) (DeviceGrant, error) {
	var out struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		Interval    int    `json:"interval"`
	}
	err := c.post(ctx, "/login/oauth/access_token", url.Values{
		"client_id":   {c.id()},
		"device_code": {code},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &out)
	if err != nil {
		return DeviceGrant{}, err
	}
	if out.AccessToken != "" {
		return DeviceGrant{Token: out.AccessToken, Scope: out.Scope}, nil
	}
	switch out.Error {
	case DevicePending, DeviceSlowDown, DeviceExpired, DeviceDenied:
		return DeviceGrant{Error: out.Error, Interval: out.Interval}, nil
	case "":
		return DeviceGrant{}, errors.New("GitHub did not complete sign-in")
	}
	// Any other answer (a wrong client, a used code) ends the sign-in. The
	// words are GitHub's error code, never anything the request carried.
	return DeviceGrant{Error: DeviceExpired}, nil
}

func (c DeviceClient) post(ctx context.Context, path string, form url.Values, out any) error {
	base, err := deviceBase()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, deviceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// A transport error can quote the address; it never quotes the form.
		return errors.New("GitHub could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return errors.New("GitHub sign-in is not answering right now")
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out) != nil {
		return errors.New("GitHub sign-in returned an invalid response")
	}
	return nil
}
