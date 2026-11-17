//go:build e2e

package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE LIVE BROWSER SIGN-IN, UP TO GITHUB'S OWN DEVICE PAGE. The app's door end
// to end: factory.setup_github_start and _poll over the real wire, the real
// device client speaking HTTP to a stand-in for github.com/login (named by
// CODEAF_GITHUB_LOGIN_URL, on loopback), and then the REAL GitHub: the token
// the stand-in grants is a real one, so the connect that follows reads the
// real account and its real repositories. Only the two device endpoints are
// stood in, because registering the OAuth app they need is the owner's step.
//
// Opt-in: CODEAF_LIVE_GITHUB_SIGNIN=1 and CODEAF_LIVE_GITHUB_TOKEN_FILE=<file>,
// a file readable by its owner alone (mode 600) holding a GitHub token that
// can read the account's repositories. The token is never printed; the run
// asserts it crossed the wire exactly once and was written to no file.

func TestLiveBrowserSignInConnectsTheRealAccount(t *testing.T) {
	if os.Getenv("CODEAF_LIVE_GITHUB_SIGNIN") != "1" {
		t.Skip("opt-in live lane: set CODEAF_LIVE_GITHUB_SIGNIN=1 and CODEAF_LIVE_GITHUB_TOKEN_FILE=<mode-600 file>")
	}
	file := os.Getenv("CODEAF_LIVE_GITHUB_TOKEN_FILE")
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("BLOCKED: CODEAF_LIVE_GITHUB_TOKEN_FILE must be a mode-600 file (%v)", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))

	// The real account, read with the same token, to compare with what setup says.
	var me struct {
		Login string `json:"login"`
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("BLOCKED: GitHub did not accept the token (%v)", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()

	// github.com/login's two device endpoints: one "not yet", then the grant.
	var mu sync.Mutex
	var seen []string
	exchanges := 0
	stand := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Method+" "+r.URL.Path+" scope="+r.Form.Get("scope")+" client="+r.Form.Get("client_id"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/device/code":
			_, _ = w.Write([]byte(`{"device_code":"live-device-code","user_code":"LIVE-0001","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`))
		case "/login/oauth/access_token":
			exchanges++
			if exchanges == 1 {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": token, "token_type": "bearer", "scope": "repo"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(stand.Close)

	appHome, userHome, decoy := useLocalHome(t)
	t.Setenv("CODEAF_GITHUB_CLIENT_ID", "Ov23-live-placeholder")
	t.Setenv("CODEAF_GITHUB_LOGIN_URL", stand.URL)
	w := newFactoryWire(t, testFactoryHub(t))
	gh := &readOnlyGitHub{}
	w.backend.local.githubHTTP = gh.client()
	s := &localSetup{factoryWire: w, home: appHome, userHm: userHome}
	if !strings.Contains(s.hello, appwire.FeatureFactorySetupGitHub) {
		t.Fatal("hello does not offer the browser sign-in")
	}
	s.start()

	var in appwire.FactorySignIn
	s.must("factory.setup_github_start", map[string]any{"caller": "w1"}, &in)
	if in.UserCode != "LIVE-0001" || in.VerificationURI != "https://github.com/login/device" {
		t.Fatalf("start: %+v", in)
	}
	var st appwire.FactorySetupGitHubState
	deadline := time.Now().Add(60 * time.Second)
	pendings := 0
	for {
		st = s.poll(in.Flow)
		if st.State != appwire.SignInPending {
			break
		}
		pendings++
		if time.Now().After(deadline) {
			t.Fatal("the sign-in never finished")
		}
		time.Sleep(300 * time.Millisecond)
	}
	if st.State != appwire.SignInSignedIn || st.View == nil {
		t.Fatalf("poll: state %q", st.State)
	}
	v := *st.View
	if v.State != "repos" || !v.GitHub.Connected || v.GitHub.Handle != factory.NormalizeHandle(me.Login) || len(v.Repos) == 0 {
		t.Fatalf("connected view: state %q connected %v handle equal %v repos %d error %q", v.State, v.GitHub.Connected, v.GitHub.Handle == factory.NormalizeHandle(me.Login), len(v.Repos), v.GitHub.Error)
	}
	if st.GitHubToken != token {
		t.Fatal("the signed-in answer does not hand the app the connected token")
	}
	if key, _ := config.AppCredentialSource().Key("github"); key != token {
		t.Fatal("the token is not where a pasted token is kept")
	}
	if len(gh.refused) > 0 {
		t.Fatalf("setup wrote to GitHub: %v", gh.refused)
	}

	// Said once on the wire; written to no file of the app, the person or the profile.
	said := 0
	s.mu.Lock()
	for _, line := range s.lines {
		said += strings.Count(line, token)
	}
	s.mu.Unlock()
	if said != 1 {
		t.Fatalf("the token crossed the wire %d times, want once", said)
	}
	for _, root := range []string{appHome, userHome, decoy} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if b, e := os.ReadFile(p); e == nil && strings.Contains(string(b), token) {
				t.Errorf("the token was written to %s", strings.TrimPrefix(p, root))
			}
			return nil
		})
	}
	mu.Lock()
	t.Logf("device endpoints asked: %v; pending answers %d; account @%s; %d repositories visible; %d GitHub reads, 0 writes", seen, pendings, v.GitHub.Handle, len(v.Repos), gh.reads)
	mu.Unlock()
}
