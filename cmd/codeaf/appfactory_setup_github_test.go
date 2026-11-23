package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

const deviceSecret = "gh-device-code-never-leaves-0123"

// deviceGitHub is github.com's two device-flow endpoints. Each exchange
// answers the next of answers; the last one repeats.
type deviceGitHub struct {
	mu        sync.Mutex
	answers   []string
	exchanges int
	starts    int
	scope     string
}

func (g *deviceGitHub) client(t *testing.T) *factorygithub.DeviceClient {
	return &factorygithub.DeviceClient{ClientID: "Iv1.test-client", HTTP: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "github.com" || r.Method != http.MethodPost {
			t.Errorf("device flow called %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		if form.Get("client_id") != "Iv1.test-client" {
			t.Errorf("client_id %q", form.Get("client_id"))
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		body := `{}`
		switch r.URL.Path {
		case "/login/device/code":
			g.starts++
			g.scope = form.Get("scope")
			body = `{"device_code":"` + deviceSecret + `","user_code":"WDJB-MJHT","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`
		case "/login/oauth/access_token":
			if form.Get("device_code") != deviceSecret {
				t.Errorf("exchanged %q", form.Get("device_code"))
			}
			i := g.exchanges
			if i >= len(g.answers) {
				i = len(g.answers) - 1
			}
			g.exchanges++
			body = g.answers[i]
		default:
			t.Errorf("device flow called %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
}

func (g *deviceGitHub) asked() int { g.mu.Lock(); defer g.mu.Unlock(); return g.exchanges }

// newDeviceSetup is a local setup whose build names an OAuth app, with
// github.com's device endpoints faked and the sign-in's clock in the test's hand.
func newDeviceSetup(t *testing.T, answers ...string) (*localSetup, *deviceGitHub, *factoryClock) {
	t.Helper()
	t.Setenv("CODEAF_GITHUB_CLIENT_ID", "Iv1.test-client")
	s := newLocalSetup(t)
	gh := &deviceGitHub{answers: answers}
	clock := &factoryClock{at: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	s.backend.setupGitHub.device = gh.client(t)
	s.backend.setupGitHub.now = clock.now
	return s, gh, clock
}

func (s *localSetup) poll(flow string) appwire.FactorySetupGitHubState {
	s.t.Helper()
	var st appwire.FactorySetupGitHubState
	s.must("factory.setup_github_poll", map[string]any{"caller": "w1", "flow": flow}, &st)
	return st
}

// THE BROWSER SIGN-IN CONNECTS THROUGH THE PASTED TOKEN'S OWN DOOR: the code
// is shown, GitHub is asked no faster than it says, and the token it grants
// lands where a pasted token lands, said back once for the app's Keychain.
func TestSetupGitHubBrowserSignInConnectsLikeAPastedToken(t *testing.T) {
	s, gh, clock := newDeviceSetup(t,
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down","interval":10}`,
		`{"access_token":"`+setupSecret+`","token_type":"bearer","scope":"repo"}`,
	)
	if !strings.Contains(s.hello, appwire.FeatureFactorySetupGitHub) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactorySetupGitHub)
	}
	s.start()
	var in appwire.FactorySignIn
	s.must("factory.setup_github_start", map[string]any{"caller": "w1"}, &in)
	if in.UserCode != "WDJB-MJHT" || in.VerificationURI != "https://github.com/login/device" || in.Interval != 5 || in.ExpiresIn != 900 || in.Flow == "" {
		t.Fatalf("start: %+v", in)
	}
	if gh.scope != factorygithub.DeviceScope {
		t.Fatalf("asked GitHub for scope %q", gh.scope)
	}

	// Too soon: answered without asking GitHub.
	if st := s.poll(in.Flow); st.State != appwire.SignInPending || st.Interval != 5 || gh.asked() != 0 {
		t.Fatalf("early poll: %+v, asked %d", st, gh.asked())
	}
	// Another window cannot poll this one's sign-in.
	s.mustFail("factory.setup_github_poll", map[string]any{"caller": "w2", "flow": in.Flow}, appwire.CodeNotFound)

	clock.advance(5 * time.Second)
	if st := s.poll(in.Flow); st.State != appwire.SignInPending || gh.asked() != 1 {
		t.Fatalf("pending: %+v", st)
	}
	clock.advance(5 * time.Second)
	if st := s.poll(in.Flow); st.State != appwire.SignInPending || st.Interval != 10 {
		t.Fatalf("slow_down: %+v", st)
	}
	clock.advance(5 * time.Second)
	if st := s.poll(in.Flow); st.State != appwire.SignInPending || gh.asked() != 2 {
		t.Fatalf("slow_down must hold the next ask back: %+v, asked %d", st, gh.asked())
	}
	clock.advance(5 * time.Second)
	st := s.poll(in.Flow)
	if st.State != appwire.SignInSignedIn || st.GitHubToken != setupSecret || st.View == nil {
		t.Fatalf("signed in: %+v", st)
	}
	if st.View.State != "repos" || !st.View.GitHub.Connected || len(st.View.Repos) != 3 {
		t.Fatalf("the view after the connect: %+v", st.View)
	}
	if key, _ := config.AppCredentialSource().Key("github"); key != setupSecret {
		t.Fatal("the granted token is not where a pasted token is kept")
	}
	// The exchange is single-use: the flow is gone.
	s.mustFail("factory.setup_github_poll", map[string]any{"caller": "w1", "flow": in.Flow}, appwire.CodeNotFound)

	// The device code never left the engine, and the token was said once.
	s.neverSaid(deviceSecret)
	said := 0
	s.mu.Lock()
	for _, line := range s.lines {
		said += strings.Count(line, setupSecret)
	}
	s.mu.Unlock()
	if said != 1 {
		t.Fatalf("the token was said %d times on the wire, want once", said)
	}
}

func TestSetupGitHubBrowserSignInEndsOnDenialExpiryAndCancel(t *testing.T) {
	s, _, clock := newDeviceSetup(t, `{"error":"access_denied"}`)
	s.start()
	start := func() appwire.FactorySignIn {
		var in appwire.FactorySignIn
		s.must("factory.setup_github_start", map[string]any{"caller": "w1"}, &in)
		return in
	}

	in := start()
	clock.advance(5 * time.Second)
	if st := s.poll(in.Flow); st.State != appwire.SignInDenied || st.GitHubToken != "" {
		t.Fatalf("denied: %+v", st)
	}
	s.mustFail("factory.setup_github_poll", map[string]any{"caller": "w1", "flow": in.Flow}, appwire.CodeNotFound)

	in = start()
	clock.advance(901 * time.Second)
	if st := s.poll(in.Flow); st.State != appwire.SignInExpired {
		t.Fatalf("expired: %+v", st)
	}

	in = start()
	// A second start from the same window replaces the first.
	again := start()
	s.mustFail("factory.setup_github_poll", map[string]any{"caller": "w1", "flow": in.Flow}, appwire.CodeNotFound)
	var empty json.RawMessage
	s.must("factory.setup_github_cancel", map[string]any{"caller": "w1", "flow": again.Flow}, &empty)
	s.mustFail("factory.setup_github_poll", map[string]any{"caller": "w1", "flow": again.Flow}, appwire.CodeNotFound)

	v := s.setup("factory.setup_read", map[string]any{})
	if v.GitHub.Connected {
		t.Fatal("a sign-in that never finished connected GitHub")
	}
}

// ABSENT, NOT BROKEN: a build with no OAuth app has no browser sign-in verb.
func TestSetupGitHubBrowserSignInIsAbsentWithoutAClientID(t *testing.T) {
	t.Setenv("CODEAF_GITHUB_CLIENT_ID", "")
	if factorygithub.DeviceClientID() != "" {
		t.Skip("this build was stamped with a client ID")
	}
	s := newLocalSetup(t)
	if strings.Contains(s.hello, appwire.FeatureFactorySetupGitHub) {
		t.Fatalf("hello announces %s with no client ID", appwire.FeatureFactorySetupGitHub)
	}
	s.start()
	s.mustFail("factory.setup_github_start", map[string]any{"caller": "w1"}, appwire.CodeUnknownMethod)
}
