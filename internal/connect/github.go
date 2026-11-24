package connect

// GitHub: a tool server whose way in is a token rather than a sign-in this
// package runs.
//
// ── WHY IT IS NOT ON THE TOOL-SERVER LIST ──
//
// GitHub's hosted tool server (https://api.githubcopilot.com/mcp/) is a tool
// server like Notion's, and its tools are fetched and called exactly as theirs
// are (mcp_tools.go). What differs is the way in. GitHub's sign-in does not let
// a program introduce itself (mcp_catalog.go says why that ruled it out), so
// codeaf signs in as an application registered by hand — the one the Factory
// already signs in with — through GitHub's device flow, and what comes back is
// one bearer token. The tool server takes that token as it is.
//
// ── ONE GITHUB PER MACHINE, AND IT IS NOT KEPT HERE ──
//
// The Factory reads GitHub with a token it already keeps (the profile's
// github_token row, gh's own login after the person said yes to it, or, in the
// desktop app, the app's keychain). A second copy in this package's store file
// would be two answers to "is GitHub connected?" that drift the first time a
// person disconnects in one place and not the other. So THIS PACKAGE HOLDS NO
// GITHUB TOKEN: every read and write goes through [GitHubDoors], which the
// program hangs over the Factory's own store, and connecting in either place
// is connected in both.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT: a [Manager] nobody gave doors to
// does not offer GitHub at all, rather than offering a row that cannot keep
// what it is handed.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"golang.org/x/oauth2"
)

// GitHubID is GitHub's id on the registry, the wire and in the Factory's
// credential handoff, which all spell it the same way.
const GitHubID = "github"

// GitHubTokenLink is where a person makes a token by hand, with the one scope
// the device flow asks for already ticked.
const GitHubTokenLink = "https://github.com/settings/tokens/new?scopes=repo&description=codeaf"

// githubToolAddress is GitHub's hosted tool server. Read off
// https://github.com/github/github-mcp-server (the README's remote server
// section), 2026-11-15, and proved the same day with a gh token: tools/list
// answers and get_me runs.
const githubToolAddress = "https://api.githubcopilot.com/mcp/"

// githubScopesWords is what the one scope the device flow asks for (`repo`)
// lets an agent do, in a person's words.
const githubScopesWords = "Read and change your repositories, issues and pull requests"

// The words a device sign-in that did not finish ends with. They leave this
// package whole ([SignInFailureReason]) because they were written here.
const (
	githubExpiredWords = "The code expired before it was approved. Start again for a new code."
	githubDeniedWords  = "GitHub sign-in was declined, so nothing was connected."
	githubNoDeviceWord = "This build cannot sign in to GitHub in the browser. Paste a token or use the gh login on this computer."
)

// ErrGitHubRefused is what [GitHubDoors.Login] wraps when GitHub answered and
// said no to a token, as opposed to not answering at all. The difference is
// the difference between "that token is wrong" and "try again".
var ErrGitHubRefused = errors.New("GitHub did not accept that token")

// GitHubDoors is how this package reaches the machine's one GitHub
// connection. Every field but Device is required.
type GitHubDoors struct {
	// Token is the token GitHub is read with right now, and how it was found
	// ("env" for GH_TOKEN or GITHUB_TOKEN, "gh", "token"), or "" for none. It
	// must not wait long: a menu asks it.
	Token func(ctx context.Context) (token, via string)
	// Keep keeps a token the person handed over (pasted, or from the device
	// flow), already accepted by GitHub, in the one place the Factory reads.
	Keep func(ctx context.Context, token string) error
	// KeepLocal is the person's yes to gh's own login: it keeps that yes and
	// answers the token gh holds, so it can be checked and named.
	KeepLocal func(ctx context.Context) (token string, err error)
	// Forget disconnects GitHub, for the Factory too.
	Forget func(ctx context.Context) error
	// LocalLogin is the login gh is signed in as on this computer, confirmed
	// by GitHub, or "". It never reads gh's token (that waits for the yes
	// above) and it must answer quickly: a menu asks it.
	LocalLogin func(ctx context.Context) string
	// Login asks GitHub whose token this is (GET /user). A refusal wraps
	// [ErrGitHubRefused].
	Login func(ctx context.Context, token string) (string, error)
	// Device is GitHub's device flow, nil when this build names no OAuth app.
	Device *GitHubDevice
}

// GitHubDevice is GitHub's device flow, as two calls.
type GitHubDevice struct {
	Start func(ctx context.Context) (GitHubCode, error)
	Poll  func(ctx context.Context, code string) (GitHubGrant, error)
}

// GitHubCode is one started device sign-in. Code stays in the engine; UserCode
// and Link are what the person sees.
type GitHubCode struct {
	Code      string
	UserCode  string
	Link      string
	Interval  time.Duration
	ExpiresIn time.Duration
}

// The answers a poll gives before there is a token.
const (
	GitHubPending  = "pending"
	GitHubSlowDown = "slow_down"
	GitHubExpired  = "expired"
	GitHubDenied   = "denied"
)

// GitHubGrant is one poll: a token once the person approved, otherwise one of
// the GitHub* answers above, and GitHub's new pace when it gave one.
type GitHubGrant struct {
	Token    string
	Error    string
	Interval time.Duration
}

// UseGitHub hangs the machine's GitHub connection on this manager. Until it
// is called GitHub is not offered.
func (m *Manager) UseGitHub(doors GitHubDoors) {
	if doors.Token == nil || doors.Keep == nil || doors.KeepLocal == nil || doors.Forget == nil || doors.LocalLogin == nil || doors.Login == nil {
		panic("connect: GitHub doors are incomplete")
	}
	m.github = &doors
}

// GitHubDeviceFlow reports whether GitHub can be signed in to in the browser
// from this manager: the doors are hung and this build names an OAuth app.
func (m *Manager) GitHubDeviceFlow() bool {
	return m.github != nil && m.github.Device != nil
}

// SignInWords is a sign-in failure whose words were written in this package
// for a person, and so may be shown as they are.
type SignInWords struct{ Words string }

func (e *SignInWords) Error() string { return e.Words }

// init registers GitHub beside the tool servers. It is not registered as one
// of them (registerToolServer): those are offered in every build because they
// introduce themselves, and GitHub is offered only where doors were hung.
func init() {
	Register(&toolServer{
		service: Service{
			ID:       GitHubID,
			Name:     "GitHub",
			Category: categoryDeveloper,
			Auth:     AuthBrowser,
			Blurb:    "GitHub's own tools — your repositories, issues and pull requests — signed in in your browser.",
		},
		address: githubToolAddress,
		bearer:  true,
	})
	RegisterGenericCapabilities(GitHubID, nil)
}

// githubAddress is where GitHub's tool server answers: the hosted one, or a
// stand-in named by CODEAF_GITHUB_MCP_URL for a test. A STAND-IN THAT IS NOT
// HTTPS MUST BE ON THIS MACHINE, so no setting can send the token across a
// network in the clear.
func githubAddress() (string, error) {
	raw := strings.TrimSpace(env.Get("CODEAF_GITHUB_MCP_URL"))
	if raw == "" {
		return githubToolAddress, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("CODEAF_GITHUB_MCP_URL is not an address")
	}
	if u.Scheme == "https" {
		return raw, nil
	}
	if ip := net.ParseIP(u.Hostname()); u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
		return raw, nil
	}
	return "", errors.New("CODEAF_GITHUB_MCP_URL must be https, or http on this machine")
}

// githubTokenWait bounds a menu's read of the token: gh may be slow the very
// first time, and a menu is not a place to wait on it.
const githubTokenWait = 3 * time.Second

// githubToken is the token GitHub is read with, or "" when it is not
// connected or this manager was given no doors.
func (m *Manager) githubToken(ctx context.Context) (token, via string) {
	if m.github == nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(ctx, githubTokenWait)
	defer cancel()
	token, via = m.github.Token(ctx)
	return strings.TrimSpace(token), via
}

// githubStanding is [Manager.standing] for GitHub: an entry made from the
// shared token, never from the store file.
func (m *Manager) githubStanding() (stored, bool) {
	token, _ := m.githubToken(context.Background())
	if token == "" {
		return stored{}, false
	}
	return stored{Keys: &oauth2.Token{AccessToken: token}, Account: heldGitHubLogin(token)}, true
}

// ── whose token it is ───────────────────────────────────────────────────────

// githubLogins remembers whose each token is, by a hash of it, for the life of
// the process: a menu names the account on every draw and GitHub answers the
// same thing every time.
var githubLogins = struct {
	mu     sync.Mutex
	logins map[[32]byte]string
	asking map[[32]byte]chan struct{}
}{logins: map[[32]byte]string{}, asking: map[[32]byte]chan struct{}{}}

func heldGitHubLogin(token string) string {
	githubLogins.mu.Lock()
	defer githubLogins.mu.Unlock()
	return githubLogins.logins[sha256.Sum256([]byte(token))]
}

func holdGitHubLogin(token, login string) {
	if login == "" {
		return
	}
	githubLogins.mu.Lock()
	defer githubLogins.mu.Unlock()
	githubLogins.logins[sha256.Sum256([]byte(token))] = login
}

// githubAccount is whose token this is, asking GitHub at most once at a time
// and waiting for the answer no longer than wait. A slow GitHub leaves the
// account empty on this draw and named on the next (THE EMPTINESS LAW).
func (m *Manager) githubAccount(token string, wait time.Duration) string {
	if token == "" || m.github == nil {
		return ""
	}
	key := sha256.Sum256([]byte(token))
	githubLogins.mu.Lock()
	if login, ok := githubLogins.logins[key]; ok {
		githubLogins.mu.Unlock()
		return login
	}
	ask, out := githubLogins.asking[key]
	if !out {
		ask = make(chan struct{})
		githubLogins.asking[key] = ask
		login := m.github.Login
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
			defer cancel()
			name, err := login(ctx, token)
			githubLogins.mu.Lock()
			if err == nil && name != "" {
				githubLogins.logins[key] = name
			}
			delete(githubLogins.asking, key)
			githubLogins.mu.Unlock()
			close(ask)
		}()
	}
	githubLogins.mu.Unlock()
	select {
	case <-ask:
	case <-time.After(wait):
	}
	return heldGitHubLogin(token)
}

// ── connecting ──────────────────────────────────────────────────────────────

// checkGitHub asks GitHub whose token this is, as a key connection's check:
// a refusal is "refused", anything else that went wrong is "no_answer".
func (m *Manager) checkGitHub(ctx context.Context, token string) (string, error) {
	login, err := m.github.Login(ctx, token)
	if err != nil {
		if errors.Is(err, ErrGitHubRefused) {
			return "", &KeyFailure{Kind: "refused", Words: "GitHub did not accept that token · nothing was saved"}
		}
		return "", &KeyFailure{Kind: "no_answer", Words: "GitHub did not answer · nothing was saved"}
	}
	if login == "" {
		return "", &KeyFailure{Kind: "refused", Words: "GitHub did not say whose token that is · nothing was saved"}
	}
	return login, nil
}

// keepGitHub keeps a token GitHub has accepted and answers the connection.
func (m *Manager) keepGitHub(ctx context.Context, token, login string, keep func(context.Context, string) error) (Status, error) {
	if err := keep(ctx, token); err != nil {
		return Status{}, fmt.Errorf("connect GitHub: %w", err)
	}
	holdGitHubLogin(token, login)
	forgetTools(GitHubID)
	plug, _ := m.plug(GitHubID)
	return Status{Service: plug.Service(), Connected: true, Checked: true, Account: login}, nil
}

// connectGitHubKey is [Manager.ConnectKey] for GitHub: a token, pasted.
func (m *Manager) connectGitHubKey(ctx context.Context, key string) (Status, error) {
	token := strings.TrimSpace(key)
	switch {
	case token == "":
		return Status{}, &KeyFailure{Kind: "shape", Words: "Paste a GitHub token"}
	case strings.ContainsAny(token, " \t\r\n"):
		return Status{}, &KeyFailure{Kind: "shape", Words: "A GitHub token is one word, with no spaces"}
	case strings.HasPrefix(token, "$"):
		return Status{}, &KeyFailure{Kind: "shape", Words: "Paste the token itself; GitHub is not read from a variable here"}
	}
	login, err := m.checkGitHub(ctx, token)
	if err != nil {
		return Status{}, err
	}
	return m.keepGitHub(ctx, token, login, m.github.Keep)
}

// ConnectLocal adopts the login already on this computer for one service:
// for GitHub, gh's own. It is the person's yes to that login, so it is the
// first time gh's token is read.
func (m *Manager) ConnectLocal(ctx context.Context, id string) (Status, error) {
	if strings.TrimSpace(id) != GitHubID || m.github == nil {
		return Status{}, &KeyFailure{Kind: "shape", Words: "There is no login on this computer to use for that"}
	}
	token, err := m.github.KeepLocal(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return Status{}, &KeyFailure{Kind: "refused", Words: "gh is not signed in on this computer · run gh auth login, then try again"}
	}
	login, err := m.checkGitHub(ctx, strings.TrimSpace(token))
	if err != nil {
		// gh's yes was kept before GitHub was asked; a token GitHub refuses
		// must not leave that yes standing.
		_ = m.github.Forget(ctx)
		return Status{}, err
	}
	holdGitHubLogin(strings.TrimSpace(token), login)
	forgetTools(GitHubID)
	plug, _ := m.plug(GitHubID)
	return Status{Service: plug.Service(), Connected: true, Checked: true, Account: login}, nil
}

// GitHubLocalLogin is the gh login this computer could adopt, or "".
func (m *Manager) GitHubLocalLogin(ctx context.Context) string {
	if m.github == nil {
		return ""
	}
	return strings.TrimSpace(m.github.LocalLogin(ctx))
}

// disconnectGitHub forgets the one GitHub connection, for the Factory too.
func (m *Manager) disconnectGitHub(ctx context.Context) error {
	if err := m.github.Forget(ctx); err != nil {
		return fmt.Errorf("disconnect GitHub: %w", err)
	}
	forgetTools(GitHubID)
	return nil
}

// ── the device flow ─────────────────────────────────────────────────────────

// beginGitHub starts GitHub's device flow: the flow's address is GitHub's
// device page and [Flow.Code] the code the person types there. The wait polls
// GitHub at its own pace until the person approves, declines, or the code
// expires.
func (m *Manager) beginGitHub(ctx context.Context, plug Plug) (*Flow, error) {
	if !m.GitHubDeviceFlow() {
		return nil, &SignInWords{Words: githubNoDeviceWord}
	}
	device := m.github.Device
	code, err := device.Start(ctx)
	if err != nil {
		return nil, err
	}
	if code.Interval <= 0 {
		code.Interval = 5 * time.Second
	}
	if code.ExpiresIn <= 0 {
		// GitHub's own lifetime for a device code is fifteen minutes.
		code.ExpiresIn = 15 * time.Minute
	}
	flowCtx, cancel := context.WithTimeout(context.Background(), code.ExpiresIn)
	flow := &Flow{
		manager: m,
		plug:    plug,
		url:     code.Link,
		code:    code.UserCode,
		cancel:  cancel,
		ctx:     flowCtx,
		done:    make(chan flowResult, 1),
	}
	flow.keep = func(ctx context.Context, keys *oauth2.Token) (Status, error) {
		token := strings.TrimSpace(keys.AccessToken)
		login, err := m.checkGitHub(ctx, token)
		if err != nil {
			return Status{}, err
		}
		var status Status
		err = flow.commit(func() error {
			var keepErr error
			status, keepErr = m.keepGitHub(ctx, token, login, m.github.Keep)
			return keepErr
		})
		return status, err
	}
	go func() { flow.done <- pollGitHub(flowCtx, device, code) }()
	return flow, nil
}

// pollGitHub asks GitHub at its pace until there is an answer that ends the
// sign-in. GITHUB ASKS FOR SLOWER POLLS WITH slow_down AND MEANS IT: each one
// adds five seconds, and a client that ignores it is refused.
func pollGitHub(ctx context.Context, device *GitHubDevice, code GitHubCode) flowResult {
	interval := code.Interval
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return flowResult{err: &SignInWords{Words: githubExpiredWords}}
			}
			return flowResult{err: ctx.Err()}
		case <-time.After(interval):
		}
		grant, err := device.Poll(ctx, code.Code)
		if err != nil {
			// GitHub did not answer this once; the code still stands.
			continue
		}
		if grant.Token != "" {
			return flowResult{keys: &oauth2.Token{AccessToken: grant.Token, TokenType: "bearer"}}
		}
		switch grant.Error {
		case GitHubPending:
		case GitHubSlowDown:
			interval += 5 * time.Second
		case GitHubDenied:
			return flowResult{err: &SignInWords{Words: githubDeniedWords}}
		default:
			return flowResult{err: &SignInWords{Words: githubExpiredWords}}
		}
		if grant.Interval > interval {
			interval = grant.Interval
		}
	}
}

// ── the tool server ─────────────────────────────────────────────────────────

// githubClient is an HTTP client that carries the shared token to GitHub's
// tool server, and the address to send it to.
func (m *Manager) githubClient(ctx context.Context) (*http.Client, string, error) {
	token, _ := m.githubToken(ctx)
	if token == "" {
		return nil, "", errors.New("GitHub is not connected")
	}
	address, err := githubAddress()
	if err != nil {
		return nil, "", err
	}
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"}))
	client.Timeout = mcpCallTimeout
	return client, address, nil
}
