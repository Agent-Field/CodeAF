package connect

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// NO TEST IN THIS FILE TOUCHES THE NETWORK. GitHub's tool server is the same
// stand-in every tool server's test uses (mcp_fake_test.go), reached through
// CODEAF_GITHUB_MCP_URL, and the machine's GitHub connection is a fake set of
// doors holding one token in memory — which is exactly the shape of the real
// arrangement: this package keeps no GitHub token of its own.

// fakeGitHub is the machine's one GitHub connection, in memory.
type fakeGitHub struct {
	mu       sync.Mutex
	token    string
	via      string
	gh       string // the token gh holds, "" when gh is not signed in
	ghLogin  string
	accepted map[string]string // token → login GitHub answers for it
	forgets  int
	polls    []string // what each poll answers, in order; then the token
	grant    string
}

func (f *fakeGitHub) doors(device bool) GitHubDoors {
	d := GitHubDoors{
		Token: func(context.Context) (string, string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.token, f.via
		},
		Keep: func(_ context.Context, token string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.token, f.via = token, "token"
			return nil
		},
		KeepLocal: func(context.Context) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.gh == "" {
				return "", errors.New("gh answered no token")
			}
			f.token, f.via = f.gh, "gh"
			return f.gh, nil
		},
		Forget: func(context.Context) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.token, f.via = "", ""
			f.forgets++
			return nil
		},
		LocalLogin: func(context.Context) string {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.ghLogin
		},
		Login: func(_ context.Context, token string) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if login, ok := f.accepted[token]; ok {
				return login, nil
			}
			return "", ErrGitHubRefused
		},
	}
	if device {
		d.Device = &GitHubDevice{
			Start: func(context.Context) (GitHubCode, error) {
				return GitHubCode{Code: "device-secret", UserCode: "ABCD-1234", Link: "https://github.com/login/device", Interval: time.Millisecond, ExpiresIn: time.Minute}, nil
			},
			Poll: func(_ context.Context, code string) (GitHubGrant, error) {
				f.mu.Lock()
				defer f.mu.Unlock()
				if code != "device-secret" {
					return GitHubGrant{Error: GitHubExpired}, nil
				}
				if len(f.polls) > 0 {
					answer := f.polls[0]
					f.polls = f.polls[1:]
					return GitHubGrant{Error: answer}, nil
				}
				return GitHubGrant{Token: f.grant}, nil
			},
		}
	}
	return d
}

// gitHubManager is a manager whose registry is GitHub alone, hung over fake.
func gitHubManager(t *testing.T, fake *fakeGitHub, device bool) *Manager {
	t.Helper()
	plug := findPlug(t, GitHubID)
	withPlugs(t, plug)
	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	manager.UseGitHub(fake.doors(device))
	t.Cleanup(func() { forgetTools(GitHubID) })
	return manager
}

func findPlug(t *testing.T, id string) Plug {
	t.Helper()
	for _, plug := range Registered() {
		if plug.Service().ID == id {
			return plug
		}
	}
	t.Fatalf("no plug %q registered", id)
	return nil
}

func TestGitHubIsAbsentFromAManagerWithNoDoors(t *testing.T) {
	withPlugs(t, findPlug(t, GitHubID))
	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(manager.Services()) != 0 || len(manager.Plugins()) != 0 {
		t.Fatalf("GitHub was offered with nowhere to keep it: %+v", manager.Services())
	}
	if _, err := manager.BeginAuth(context.Background(), GitHubID, ""); err == nil {
		t.Fatal("a sign-in began with nowhere to keep its token")
	}
}

func TestGitHubRowSaysHowItConnects(t *testing.T) {
	fake := &fakeGitHub{ghLogin: "octocat"}
	browser := gitHubManager(t, fake, true).Plugins()
	if len(browser) != 1 {
		t.Fatalf("plugins: %+v", browser)
	}
	row := browser[0]
	if row.Kind != AuthBrowser || !row.AlsoKey || !row.KeyCheckable || row.Category != categoryDeveloper || row.Name != "GitHub" {
		t.Fatalf("browser row: %+v", row)
	}
	if row.KeyHint == nil || row.KeyHint.URL != GitHubTokenLink {
		t.Fatalf("key hint: %+v", row.KeyHint)
	}
	if row.LocalLogin == nil || row.LocalLogin.Account != "octocat" || row.LocalLogin.Source != "gh" {
		t.Fatalf("local login: %+v", row.LocalLogin)
	}
	if row.Blurb != "Your repositories, issues and pull requests" {
		t.Fatalf("blurb: %q", row.Blurb)
	}

	// A BUILD WITH NO OAUTH APP HAS NO BROWSER VERB: a token, and gh.
	keyed := gitHubManager(t, &fakeGitHub{}, false).Plugins()[0]
	if keyed.Kind != AuthKey || keyed.AlsoKey || keyed.LocalLogin != nil {
		t.Fatalf("keyed row: %+v", keyed)
	}
}

func TestAPastedGitHubTokenIsCheckedThenKeptInTheSharedPlace(t *testing.T) {
	fake := &fakeGitHub{accepted: map[string]string{"good-token": "octocat"}}
	manager := gitHubManager(t, fake, true)
	ctx := context.Background()

	for key, kind := range map[string]string{"": "shape", "two words": "shape", "$GH_TOKEN": "shape", "bad-token": "refused"} {
		_, err := manager.ConnectKey(ctx, GitHubID, key)
		var failure *KeyFailure
		if !errors.As(err, &failure) || failure.Kind != kind {
			t.Fatalf("key %q: %v, want %s", key, err, kind)
		}
		if strings.Contains(failure.Words, key) && key != "" {
			t.Fatalf("the refusal quoted the key: %q", failure.Words)
		}
	}
	if fake.token != "" {
		t.Fatal("a refused token was kept")
	}

	status, err := manager.ConnectKey(ctx, GitHubID, " good-token ")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Connected || !status.Checked || status.Account != "octocat" {
		t.Fatalf("status: %+v", status)
	}
	if fake.token != "good-token" {
		t.Fatalf("kept %q", fake.token)
	}
	// AND THE SHARED PLACE IS THE ONLY PLACE: nothing reached the store file.
	if entries, _ := manager.store.load(); len(entries) != 0 {
		t.Fatalf("the store file holds GitHub: %+v", entries)
	}
	if !manager.Connected(GitHubID) || manager.Plugins()[0].Account != "octocat" {
		t.Fatalf("not connected after keeping: %+v", manager.Plugins())
	}
}

func TestAFactoryConnectionShowsConnectedHereAndDisconnectClearsIt(t *testing.T) {
	// The factory kept a token by itself; this page never saw it.
	fake := &fakeGitHub{token: "factory-token", via: "token", accepted: map[string]string{"factory-token": "hubot"}}
	manager := gitHubManager(t, fake, true)
	row := manager.Plugins()[0]
	if !row.Connected || row.Account != "hubot" {
		t.Fatalf("row: %+v", row)
	}
	if err := manager.Disconnect(GitHubID); err != nil {
		t.Fatal(err)
	}
	if fake.forgets != 1 || fake.token != "" || manager.Connected(GitHubID) {
		t.Fatalf("disconnect left GitHub: forgets %d token %q", fake.forgets, fake.token)
	}
}

func TestTheLocalGHLoginIsAdoptedOnlyWhenGitHubAcceptsIt(t *testing.T) {
	ctx := context.Background()
	none := gitHubManager(t, &fakeGitHub{}, true)
	if _, err := none.ConnectLocal(ctx, GitHubID); err == nil {
		t.Fatal("adopted a gh that holds nothing")
	}

	refused := &fakeGitHub{gh: "stale", accepted: map[string]string{}}
	if _, err := gitHubManager(t, refused, true).ConnectLocal(ctx, GitHubID); err == nil {
		t.Fatal("adopted a gh token GitHub refused")
	}
	if refused.token != "" || refused.forgets != 1 {
		t.Fatalf("a refused gh login was left standing: %q", refused.token)
	}

	fake := &fakeGitHub{gh: "gh-token", accepted: map[string]string{"gh-token": "octocat"}}
	status, err := gitHubManager(t, fake, true).ConnectLocal(ctx, GitHubID)
	if err != nil || status.Account != "octocat" || fake.via != "gh" {
		t.Fatalf("status %+v err %v via %q", status, err, fake.via)
	}
}

func TestTheDeviceFlowShowsACodeAndKeepsTheTokenOnApproval(t *testing.T) {
	fake := &fakeGitHub{polls: []string{GitHubPending, GitHubSlowDown}, grant: "device-token", accepted: map[string]string{"device-token": "octocat"}}
	manager := gitHubManager(t, fake, true)
	flow, err := manager.BeginAuth(context.Background(), GitHubID, "")
	if err != nil {
		t.Fatal(err)
	}
	if flow.URL() != "https://github.com/login/device" || flow.Code() != "ABCD-1234" {
		t.Fatalf("flow: %q %q", flow.URL(), flow.Code())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := flow.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.Account != "octocat" || fake.token != "device-token" {
		t.Fatalf("status %+v kept %q", status, fake.token)
	}
}

func TestADeviceFlowThatEndsSaysWhyInPlainWords(t *testing.T) {
	for answer, words := range map[string]string{GitHubDenied: githubDeniedWords, GitHubExpired: githubExpiredWords} {
		fake := &fakeGitHub{polls: []string{answer}}
		flow, err := gitHubManager(t, fake, true).BeginAuth(context.Background(), GitHubID, "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = flow.Wait(context.Background())
		if err == nil || SignInFailureReason(err) != words {
			t.Fatalf("%s: %v → %q", answer, err, SignInFailureReason(err))
		}
		if fake.token != "" {
			t.Fatal("an unfinished sign-in kept a token")
		}
	}
}

func TestACancelledDeviceFlowKeepsNothing(t *testing.T) {
	fake := &fakeGitHub{polls: []string{GitHubPending, GitHubPending, GitHubPending, GitHubPending}, grant: "late-token", accepted: map[string]string{"late-token": "octocat"}}
	flow, err := gitHubManager(t, fake, true).BeginAuth(context.Background(), GitHubID, "")
	if err != nil {
		t.Fatal(err)
	}
	flow.Cancel()
	if _, err := flow.Wait(context.Background()); err == nil {
		t.Fatal("a cancelled sign-in answered connected")
	}
	time.Sleep(20 * time.Millisecond)
	if fake.token != "" {
		t.Fatalf("a cancelled sign-in kept %q", fake.token)
	}
}

func TestNoDeviceFlowIsSaidRatherThanBroken(t *testing.T) {
	_, err := gitHubManager(t, &fakeGitHub{}, false).BeginAuth(context.Background(), GitHubID, "")
	if err == nil || SignInFailureReason(err) != githubNoDeviceWord {
		t.Fatalf("%v", err)
	}
}

func TestGitHubToolsComeFromItsServerWithTheSharedBearer(t *testing.T) {
	server := startFakeToolServer(t, fakeShape{})
	server.mu.Lock()
	server.live["shared-token"] = true
	server.mu.Unlock()
	t.Setenv("CODEAF_GITHUB_MCP_URL", server.address())

	fake := &fakeGitHub{token: "shared-token", via: "token", accepted: map[string]string{"shared-token": "octocat"}}
	manager := gitHubManager(t, fake, true)
	if !manager.MCPService(GitHubID) {
		t.Fatal("GitHub does not bring its own tools")
	}
	ctx := context.Background()
	tools, err := manager.MCPTools(ctx, GitHubID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 4 {
		t.Fatalf("tools: %+v", tools)
	}
	said, err := manager.MCPCall(ctx, GitHubID, "find_pages", []byte(`{"query":"x"}`))
	if err != nil || !strings.Contains(said, "Found") {
		t.Fatalf("call: %q %v", said, err)
	}
	// Nothing about the OAuth discovery dance: the bearer went straight in.
	if introductions, _, _ := server.counted(); introductions != 0 {
		t.Fatalf("GitHub was introduced to: %d", introductions)
	}

	d, err := manager.DescribePlugin(ctx, GitHubID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Accounts) != 1 || d.Accounts[0] != "octocat" || len(d.ScopesWords) != 1 {
		t.Fatalf("describe: %+v", d)
	}
	states := map[string]CapabilityState{}
	for _, c := range d.Capabilities {
		states[c.ID] = c.State
	}
	if states[CapabilityRead] != StateYes || states[CapabilityAct] != StateAsk {
		t.Fatalf("capabilities: %+v", d.Capabilities)
	}
}

func TestGitHubIsFeaturedFirst(t *testing.T) {
	ids, words := Featured()
	if ids[0] != GitHubID || words[GitHubID].What == "" || words[GitHubID].Does == "" {
		t.Fatalf("featured: %v %+v", ids, words[GitHubID])
	}
}
