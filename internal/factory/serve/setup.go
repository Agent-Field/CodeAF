package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/redact"
)

const setupWait = 40 * time.Second

// setupVerbContinue ends reading before every selected repository is read.
const setupVerbContinue = "continue"

// setupReadSkipped is the read state of a selected repository whose reading
// Continue ended: whatever it already brought stays held in Backlog, and the
// rest of its issues were never read.
const setupReadSkipped = "skipped"

// githubKBPerMB turns the repository size GitHub quotes in kilobytes into the
// megabytes setup shows.
const githubKBPerMB = 1024

// setupState keeps only public facts. Credentials remain behind the host's existing GitHub door.
type setupState struct {
	View    api.FactorySetup     `json:"view"`
	Waiting map[string]time.Time `json:"waiting"`
	Pages   map[string]int       `json:"pages"`
}

func (h *Handler) setupPath() string { return filepath.Join(h.config.Store.Root(), "setup.json") }
func (h *Handler) loadSetup(id string) (setupState, error) {
	out := setupState{View: api.FactorySetup{ID: id, State: "github", Mode: h.mode(), Repos: []api.SetupRepo{}}, Waiting: map[string]time.Time{}, Pages: map[string]int{}}
	if h.mode() == api.FactoryTeam {
		out.View.Server = h.factory.Address
	}
	data, err := os.ReadFile(h.setupPath())
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	out.View.ID = id
	if out.Pages == nil {
		out.Pages = map[string]int{}
	}
	if out.Waiting == nil {
		out.Waiting = map[string]time.Time{}
	}
	return out, nil
}
func (h *Handler) saveSetup(out setupState) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(h.config.Store.Root(), ".setup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Close()
	} else {
		_ = f.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), h.setupPath())
}
func (h *Handler) setupToken(ctx context.Context) (string, error) {
	if h.mode() == api.FactoryTeam {
		grant, err := factorygithub.LoadReadGrant(factorygithub.GrantPath(h.config.TokensPath))
		return grant.Token, err
	}
	token, _ := factorygithub.TokenAt(ctx, h.config.ProfileDir)
	return token, nil
}

// setupTokenRefused is whether GitHub refused token outright, as opposed to
// answering, or not being reached at all.
func (h *Handler) setupTokenRefused(ctx context.Context, token string) bool {
	var user struct{}
	resp, err := h.setupJSON(ctx, "/user", token, &user)
	return err != nil && resp != nil && resp.StatusCode == http.StatusUnauthorized
}

// setupJSON follows only fixed GitHub API paths; no client-supplied host receives a credential.
func (h *Handler) setupJSON(ctx context.Context, path, token string, out any) (*http.Response, error) {
	resp, _, err := h.setupJSONChanged(ctx, path, token, out)
	return resp, err
}

func (h *Handler) setupJSONChanged(ctx context.Context, path, token string, out any) (*http.Response, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com"+path, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := h.githubClient()
	resp, changed, err := forge.ReadHTTP(&client, req, "")
	if err != nil {
		return nil, false, errors.New("GitHub is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp, false, githubStatusError(resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
	return resp, changed, err
}

// setupGraphQL posts one GraphQL payload as the setup's token and answers the
// body of a 200; anything else is an error and its body is not read.
func (h *Handler) setupGraphQL(ctx context.Context, token string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := h.githubClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, githubStatusError(resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// commonOwner names the owner every discovered repository shares, or "" when
// there are none or they differ. THE LABEL IS A FACT ABOUT THE ROWS, never a claim of
// organization membership: owners compare case-insensitively and the first spelling is kept.
func commonOwner(repos []api.SetupRepo) string {
	label := ""
	for _, r := range repos {
		owner, _, _ := strings.Cut(r.ID, "/")
		switch {
		case label == "":
			label = owner
		case !strings.EqualFold(label, owner):
			return ""
		}
	}
	return label
}

func clearSetupConnection(out *setupState) {
	out.View.GitHub.Connected = false
	out.View.GitHub.Handle, out.View.GitHub.Name, out.View.GitHub.AvatarURL = "", "", ""
	out.View.GitHub.Organization = ""
	out.View.GitHub.VisibleRepos = 0
}

// A saved setup remembers progress, not proof that its credential is still held.
//
// A FINISHED SETUP ASKS GITHUB ONCE WHETHER ITS TOKEN STILL ANSWERS. A kept
// token outlives a launch, so it can be revoked or run out while setup says
// done, and reopening setup is how the person connects again. Only GitHub's
// refusal sends setup back; a network that is down proves nothing and changes
// nothing. The steps before done make GitHub calls of their own and say there
// when one is refused.
func (h *Handler) reconnectSetupIfNeeded(ctx context.Context, out *setupState) {
	token, err := h.setupToken(ctx)
	if err == nil && token != "" && (out.View.State != "done" || !h.setupTokenRefused(ctx, token)) {
		return
	}
	clearSetupConnection(out)
	switch out.View.State {
	case "repos", "reading", "checkouts", "done":
		out.View.State = "github"
	}
}

func (h *Handler) discoverSetup(ctx context.Context, out *setupState) error {
	previousIdentity := out.View.GitHub.Handle
	clearSetupConnection(out)
	out.View.GitHub.GH = nil
	if h.seam.GHLogin != nil && h.mode() == api.FactorySolo {
		login, err := h.seam.GHLogin(ctx)
		if err == nil && login != "" {
			path, _ := forge.LookPath("gh")
			out.View.GitHub.GH = &api.SetupGH{Login: login, Path: path}
		}
	}
	token, err := h.setupToken(ctx)
	if err != nil || token == "" {
		return err
	}
	var user struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if _, err = h.setupJSON(ctx, "/user", token, &user); err != nil {
		return err
	}
	out.View.GitHub.Connected = true
	out.View.GitHub.Handle = factory.NormalizeHandle(user.Login)
	out.View.GitHub.Name = user.Name
	out.View.GitHub.AvatarURL = user.AvatarURL
	// A source already learned for a repository survives rediscovery. It is
	// carried over, never re-fetched: discovery makes no per-repository call.
	known := map[string]*string{}
	counts := map[string]api.SetupRepo{}
	for _, r := range out.View.Repos {
		if strings.EqualFold(previousIdentity, user.Login) {
			counts[r.ID] = r
		}
		if r.Pipeline != nil {
			known[r.ID] = r.Pipeline
		}
	}
	out.View.Repos = []api.SetupRepo{}
	watched, err := h.config.Store.Repos()
	if err != nil {
		return err
	}
	dirs, err := h.config.Store.Checkouts()
	if err != nil {
		return err
	}
	allUnchanged := true
	var available []factory.RepoInfo
	for page := 1; page <= 100; page++ {
		var rows []struct {
			FullName string    `json:"full_name"`
			Private  bool      `json:"private"`
			Pushed   time.Time `json:"pushed_at"`
			Open     int       `json:"open_issues_count"`
			Archived bool      `json:"archived"`
			SizeKB   int       `json:"size"`
		}
		_, changed, readErr := h.setupJSONChanged(ctx, fmt.Sprintf("/user/repos?per_page=100&sort=pushed&page=%d", page), token, &rows)
		allUnchanged = allUnchanged && !changed
		if readErr != nil {
			return readErr
		}

		for _, row := range rows {
			if _, ok := counts[row.FullName]; !ok {
				allUnchanged = false
			}
			if !store.RepoName(row.FullName) {
				continue
			}
			userHome, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			repo := api.SetupRepo{ID: row.FullName, FullName: row.FullName, OpenIssues: row.Open, Archived: row.Archived, Selected: slices.Contains(watched, row.FullName), Read: "next", Clone: "missing", CloneMB: (row.SizeKB + githubKBPerMB - 1) / githubKBPerMB, SuggestedCheckout: filepath.Join(userHome, "code", strings.ReplaceAll(row.FullName, "/", "-"))}
			if previous, ok := counts[row.FullName]; ok && !changed {
				repo.OpenIssues, repo.OpenIssuesExact = previous.OpenIssues, previous.OpenIssuesExact
			}
			if dir := dirs[strings.ToLower(row.FullName)]; dir != "" {
				repo.Checkout = &dir
				repo.Clone = "found"
			}
			// THE PIPELINE SOURCE IS UNKNOWN until the repository is selected and
			// read: a large account has hundreds of visible repositories, and one
			// pipeline load each cannot finish inside the request. Unknown is nil,
			// never "defaults".
			repo.Pipeline = known[row.FullName]
			out.View.Repos = append(out.View.Repos, repo)
			owner, name, _ := strings.Cut(row.FullName, "/")
			available = append(available, factory.RepoInfo{Full: row.FullName, Name: name, Owner: owner, Private: row.Private, Pushed: row.Pushed, Open: row.Open})
		}
		if len(rows) < 100 {
			break
		}
		if page == 100 {
			return errors.New("GitHub repository list exceeds the setup limit")
		}
	}
	if !allUnchanged {
		names := make([]string, len(out.View.Repos))
		for i, repo := range out.View.Repos {
			names[i] = repo.ID
		}
		exact := forge.OpenIssueCounts(ctx, names, func(ctx context.Context, payload any) ([]byte, error) { return h.setupGraphQL(ctx, token, payload) })
		for i, n := range exact {
			out.View.Repos[i].OpenIssues, out.View.Repos[i].OpenIssuesExact = available[i].Open, false
			if n != nil {
				out.View.Repos[i].OpenIssues, out.View.Repos[i].OpenIssuesExact = *n, true
			}
		}
	}
	for i, repo := range out.View.Repos {
		if repo.OpenIssuesExact {
			n := repo.OpenIssues
			available[i].OpenIssues = &n
		}
	}
	factory.AttachCheckouts(available, func(repo string) string { return dirs[strings.ToLower(repo)] })
	if err := h.config.Store.SetAvailableRepos(store.AvailableRepos{Identity: strings.ToLower(user.Login), Credential: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), ReadAt: h.clock().UTC(), Repos: available}); err != nil {
		return err
	}
	out.View.GitHub.VisibleRepos = len(out.View.Repos)
	out.View.GitHub.Organization = commonOwner(out.View.Repos)
	out.View.GitHub.Error = nil
	if h.mode() == api.FactoryTeam && out.View.State == "github" {
		out.View.State = "repos"
	}
	return nil
}

func (h *Handler) factorySetup(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	var body api.FactorySetupRequest
	if r.Method == "POST" && !h.decode(w, r, &body, false) {
		return
	}
	id, err := setupID(r.PathValue("id"), body.ID)
	if err != nil {
		h.fail(w, 422, err.Error())
		return
	}
	continuing := body.Verb == setupVerbContinue && h.stopSetupReader(id)
	h.setupMu.Lock()
	defer h.setupMu.Unlock()
	if continuing {
		h.setupContinuing = false
	}
	out, err := h.loadSetup(id)
	if h.failed(w, err) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), setupWait)
	defer cancel()
	if r.Method == "GET" {
		h.reconnectSetupIfNeeded(ctx, &out)
		if out.View.State == "github" {
			if err = h.discoverSetup(ctx, &out); err != nil {
				words := redact.Secrets(err.Error())
				out.View.GitHub.Error = &words
			}
		}
	} else {
		if h.setupBusy {
			h.fail(w, 409, "setup is still working")
			return
		}
		if err = h.setupAction(ctx, &out, body); err != nil {
			h.fail(w, 409, err.Error())
			return
		}
	}
	for i := range out.View.Repos {
		repo := &out.View.Repos[i]
		if at := out.Waiting[repo.ID]; !at.IsZero() {
			repo.WaitingS = max(0, int(h.clock().Sub(at).Seconds()))
		}
		if repo.Read == "rate_limited" && repo.ResumesAt != nil && !h.clock().Before(*repo.ResumesAt) {
			repo.Read = "next"
			repo.ResumesAt = nil
		}
	}
	if h.failed(w, h.saveSetup(out)) {
		return
	}
	h.startSetup(out)
	h.write(w, 200, out.View)
}

// setupID is the setup the request names, by its route or its body, which
// must agree when both name one.
func setupID(route, body string) (string, error) {
	switch {
	case route != "" && body != "" && body != route:
		return "", errors.New("setup id does not match the route")
	case route != "":
		return route, nil
	case body != "":
		return body, nil
	}
	return "", errors.New("setup needs a factory id")
}

// stopSetupReader stops a reader that is still reading issues and waits until
// it has saved its last page and let go of the poller lock, so Continue
// decides from what was really read. A clone is never stopped by it. It
// answers whether it stopped one; the caller then holds setupContinuing and
// gives it up once it holds setupMu again.
func (h *Handler) stopSetupReader(id string) bool {
	cancel, done := h.setupReader(id)
	if cancel == nil {
		return false
	}
	cancel()
	<-done
	if continueGapHook != nil {
		continueGapHook()
	}
	return true
}

// continueGapHook runs in tests only, after a stopped reader has let go and
// before its Continue takes the setup lock, so a test can place a request in
// that gap without depending on the scheduler. It is nil in production.
var continueGapHook func()

// setupReader is the running reader's stop and finish, or nothing when no
// reader is reading issues, another Continue is already stopping it, or
// Continue would be refused anyway, so a refused Continue leaves reading
// running. Taking the stop also takes setupContinuing, which keeps any other
// request from starting a reader before this Continue saves its step.
func (h *Handler) setupReader(id string) (context.CancelFunc, chan struct{}) {
	h.setupMu.Lock()
	defer h.setupMu.Unlock()
	saved, err := h.loadSetup(id)
	if !h.setupBusy || h.setupContinuing || err != nil || saved.View.State != "reading" || !slices.ContainsFunc(saved.View.Repos, setupReadWhole) {
		return nil, nil
	}
	h.setupContinuing = true
	return h.setupCancel, h.setupDone
}

func (h *Handler) setupAction(ctx context.Context, out *setupState, body api.FactorySetupRequest) error {
	repo := (*api.SetupRepo)(nil)
	for i := range out.View.Repos {
		if out.View.Repos[i].ID == body.RepoID {
			repo = &out.View.Repos[i]
		}
	}
	switch body.Verb {
	case "discover":
		if out.View.State != "github" && out.View.State != "repos" {
			return errors.New("finish the current setup step first")
		}
		if err := h.discoverSetup(ctx, out); err != nil {
			return err
		}
		// Continuing an already verified connection uses the held credential;
		// returning from Repos must never require another pasted token.
		if out.View.GitHub.Connected {
			out.View.State = "repos"
		}
		return nil
	case "connect", "scope":
		if h.mode() == api.FactoryTeam {
			return errors.New("Give the factory read-only GitHub access.")
		}
		if out.View.State != "github" || h.seam.ConnectGitHub == nil {
			return errors.New("GitHub cannot be connected in this step")
		}
		token := ""
		switch body.Source {
		case "gh":
		case "token":
			token = strings.TrimSpace(body.Token)
			if token == "" {
				return errors.New("Paste a token")
			}
		default:
			return errors.New("choose gh or token")
		}
		if credentials := config.AppCredentialSource(); credentials != nil {
			if body.Source == "gh" {
				path, err := forge.LookPath("gh")
				if err != nil {
					return errors.New("gh is not available")
				}
				data, err := exec.CommandContext(ctx, path, "auth", "token", "--hostname", "github.com").Output()
				if err != nil {
					return errors.New("gh answered no token")
				}
				token = strings.TrimSpace(string(data))
			}
			if err := h.setupScope(ctx, token); err != nil {
				return err
			}
			// THE CONNECTION OUTLIVES THIS LAUNCH, each way kept by its owner:
			// the yes to gh in the factory's profile (gh keeps its own token),
			// a pasted token in the app's keychain, which hands it back with
			// keys.set at every launch. The yes is kept or taken back first, so
			// a write that fails leaves the connection as it was.
			keep := config.ForgetGitHubViaGH
			if body.Source == "gh" {
				keep = config.WriteGitHubViaGH
			}
			if profile := h.config.ProfileDir; profile != "" {
				if err := keep(profile); err != nil {
					return errors.New("codeaf could not keep this GitHub connection")
				}
			}
			credentials.Set("github", token)
		} else {
			if token != "" {
				if err := h.setupScope(ctx, token); err != nil {
					return err
				}
			}
			if err := h.seam.ConnectGitHub(ctx, token); err != nil {
				return errors.New("GitHub did not accept this connection")
			}
		}
		if err := h.discoverSetup(ctx, out); err != nil {
			return err
		}
		if !out.View.GitHub.Connected {
			return errors.New("GitHub is not connected")
		}
		out.View.State = "repos"
	case "select_repos", "ingest":
		if out.View.State == "github" && out.View.GitHub.Connected && h.mode() == api.FactoryTeam {
			out.View.State = "repos"
		}
		if out.View.State != "repos" || len(body.RepoIDs) == 0 {
			return errors.New("Choose a visible, unarchived repo")
		}
		for _, id := range body.RepoIDs {
			if !slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.ID == id && !r.Archived }) {
				return errors.New("Choose a visible, unarchived repo")
			}
		}
		for i := range out.View.Repos {
			r := &out.View.Repos[i]
			r.Selected = slices.Contains(body.RepoIDs, r.ID)
			r.Read = "next"
			r.ReadCount = 0
			out.Pages[r.ID] = 1
		}
		// New repositories become watched only after their imports are held, so a poll cannot start them first.
		out.View.State = "reading"
	case setupVerbContinue:
		return h.continueSetup(out)
	case "retry_read", "retry":
		if out.View.State != "reading" || repo == nil || repo.Read != "waiting" || h.clock().Sub(out.Waiting[repo.ID]) < setupWait {
			return errors.New("This repo is still waiting")
		}
		repo.Read = "next"
		repo.WaitingS = 0
		delete(out.Waiting, repo.ID)
	case "clone", "checkout":
		if out.View.State != "checkouts" || repo == nil || !repo.Selected || strings.TrimSpace(body.Path) == "" {
			return errors.New("Choose a checkout folder")
		}
		path, err := setupCheckoutPath(body.Path)
		if err != nil {
			return err
		}
		repo.Checkout = &path
		repo.CloneError = nil
		if body.Verb == "clone" {
			repo.Clone = "cloning"
		} else {
			if err = h.keepSetupCheckout(ctx, repo.ID, path); err != nil {
				return err
			}
			repo.Clone = "found"
		}
	case "skip":
		if out.View.State != "checkouts" || repo == nil || !repo.Selected {
			return errors.New("No checkout to skip")
		}
		repo.Clone = "skipped"
	case "back":
		switch out.View.State {
		case "repos":
			out.View.State = "github"
		case "checkouts":
			out.View.State = "reading"
			for i := range out.View.Repos {
				if out.View.Repos[i].Selected {
					out.View.Repos[i].Read = "next"
					out.View.Repos[i].ReadCount = 0
					out.Pages[out.View.Repos[i].ID] = 1
				}
			}
		default:
			return errors.New("This step has no Back")
		}
	case "done":
		if out.View.State != "checkouts" {
			return errors.New("finish reading before Done")
		}
		for i := range out.View.Repos {
			if out.View.Repos[i].Selected && out.View.Repos[i].Clone != "found" {
				out.View.Repos[i].Clone = "skipped"
			}
		}
		out.View.State = "done"
	default:
		return errors.New("Unknown setup action")
	}
	return nil
}

func (h *Handler) keepSetupCheckout(ctx context.Context, repo, path string) error {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "remote", "get-url", "origin").Output()
	if err != nil {
		return errors.New("Choose an existing GitHub checkout")
	}
	remote := strings.TrimSpace(string(out))
	if !strings.EqualFold(remote, "https://github.com/"+repo+".git") && !strings.EqualFold(remote, "https://github.com/"+repo) && !strings.EqualFold(remote, "git@github.com:"+repo+".git") {
		return errors.New("checkout origin does not match the selected repository")
	}
	return h.config.Store.SetCheckout(repo, path)
}

// startSetup keeps ingestion and cloning alive when a window leaves. Each completed step is saved before the next.
func (h *Handler) startSetup(out setupState) {
	if h.setupBusy || h.setupClosed || h.setupContinuing {
		return
	}
	todo := out.View.State == "reading" && slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.Selected && (r.Read == "next" || r.Read == "reading") })
	if out.View.State == "reading" && !slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.Selected && r.Read != "done" && r.Read != "empty" }) {
		todo = true
	}
	todo = todo || slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.Clone == "cloning" })
	if !todo {
		h.scheduleSetupDeadline(out)
		return
	}
	if h.setupTimer != nil {
		h.setupTimer.Stop()
		h.setupTimer = nil
	}
	// The worker owns a deep copy because the response is marshaled while it starts.
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	var worker setupState
	if json.Unmarshal(data, &worker) != nil {
		return
	}
	h.setupBusy = true
	h.setupDone = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	h.setupCancel = cancel
	guard.Go("factory/setup", func() { defer cancel(); h.runSetup(ctx, worker) })
}
func (h *Handler) setupProgress(out setupState) error {
	h.setupMu.Lock()
	defer h.setupMu.Unlock()
	return h.saveSetup(out)
}
func (h *Handler) runSetup(ctx context.Context, out setupState) {
	defer func() {
		h.setupMu.Lock()
		h.setupBusy = false
		h.setupCancel = nil
		close(h.setupDone)
		h.scheduleSetupDeadline(out)
		h.setupMu.Unlock()
	}()
	for i := range out.View.Repos {
		repo := &out.View.Repos[i]
		if !repo.Selected {
			continue
		}
		if out.View.State == "reading" && (repo.Read == "next" || repo.Read == "reading") {
			repo.Read = "reading"
			out.Waiting[repo.ID] = h.clock()
			if h.setupProgress(out) != nil {
				return
			}
			stepCtx, cancel := context.WithTimeout(ctx, setupWait)
			if source := h.setupPipelineSource(stepCtx, repo); source != nil {
				repo.Pipeline = source
			}
			resume, err := h.ingestSetupRepo(stepCtx, repo, out.Pages[repo.ID], func(page int) error { out.Pages[repo.ID] = page + 1; return h.setupProgress(out) })
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				repo.Read = "waiting"
				if resume != nil {
					repo.Read = "rate_limited"
					repo.ResumesAt = resume
				}
			} else {
				repo.Read = "done"
				if repo.ReadCount == 0 {
					repo.Read = "empty"
				}
			}
		}
		if repo.Clone == "cloning" && repo.Checkout != nil {
			stepCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			err := h.cloneSetupRepo(stepCtx, repo.ID, *repo.Checkout)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				words := redact.Secrets(err.Error())
				repo.CloneError = &words
				repo.Clone = "failed"
			} else {
				repo.Clone = "found"
				repo.CloneError = nil
			}
		}
		if h.setupProgress(out) != nil {
			return
		}
	}
	if out.View.State == "reading" && !slices.ContainsFunc(out.View.Repos, setupReadPending) {
		if err := h.finishSetupReading(&out); err != nil {
			words := redact.Secrets(err.Error())
			out.View.GitHub.Error = &words
		}
	}
	_ = h.setupProgress(out)
}

// setupReadWhole is a selected repository whose open issues were all read.
func setupReadWhole(r api.SetupRepo) bool {
	return r.Selected && (r.Read == "done" || r.Read == "empty")
}

// setupReadPending is a selected repository not yet read whole.
func setupReadPending(r api.SetupRepo) bool { return r.Selected && !setupReadWhole(r) }

// continueSetup ends reading now. Repositories already read keep their state;
// every other selected one stops being read and says skipped. At least one
// repository must be read whole, because setup watches only those.
func (h *Handler) continueSetup(out *setupState) error {
	if out.View.State != "reading" {
		return errors.New("Continue ends reading; setup is not reading")
	}
	if !slices.ContainsFunc(out.View.Repos, setupReadWhole) {
		return errors.New("Wait until one repo is read")
	}
	for i := range out.View.Repos {
		repo := &out.View.Repos[i]
		if setupReadPending(*repo) {
			repo.Read, repo.ResumesAt, repo.WaitingS = setupReadSkipped, nil, 0
			delete(out.Waiting, repo.ID)
		}
	}
	return h.finishSetupReading(out)
}

// finishSetupReading watches the repositories read whole and moves setup on
// to checkouts, or to done when every selected one already has its checkout.
// New repositories become watched only after their imports are held, so a
// poll cannot start them first: a repository whose reading was skipped stays
// unwatched, because a poll would bring its unread issues in unheld.
func (h *Handler) finishSetupReading(out *setupState) error {
	watched := []string{}
	for _, repo := range out.View.Repos {
		if setupReadWhole(repo) {
			watched = append(watched, repo.ID)
		}
	}
	if err := h.setSetupRepos(watched); err != nil {
		return err
	}
	out.View.State = "done"
	if slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.Selected && r.Clone != "found" }) {
		out.View.State = "checkouts"
	}
	return nil
}

func (h *Handler) setSetupRepos(repos []string) error {
	h.reposMu.Lock()
	defer h.reposMu.Unlock()
	return h.config.Store.SetRepos(repos)
}

// setupPipelineSource reads the one selected repository's pipeline through the
// host's own loader and names where it came from. A repository with no recipe
// file is truly "defaults"; a failed read stays unknown (nil) rather than
// guessing. Nothing here starts a job.
func (h *Handler) setupPipelineSource(ctx context.Context, repo *api.SetupRepo) *string {
	if h.config.Pipeline == nil {
		return nil
	}
	dir := ""
	if repo.Checkout != nil {
		dir = *repo.Checkout
	} else if h.config.Store != nil {
		dir = h.config.Store.CheckoutDir(repo.ID)
	}
	f, err := h.config.Pipeline(ctx, repo.ID, dir)
	source := "defaults"
	switch {
	case err == nil && f.Source == "file":
		source = "file"
	case (err == nil && f.Source == "defaults") || errors.Is(err, os.ErrNotExist):
	default:
		return nil
	}
	return &source
}

func (h *Handler) cloneSetupRepo(ctx context.Context, repo, path string) error {
	if _, err := os.Stat(path); err == nil {
		return h.keepSetupCheckout(ctx, repo, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Git obtains credentials from the person's credential helper; no token enters argv or a remote URL.
	cmd := exec.CommandContext(ctx, "git", "clone", "--", "https://github.com/"+repo+".git", path)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	token, err := h.setupToken(ctx)
	if err != nil {
		return err
	}
	if token != "" {
		// Git reads this temporary header from the child environment and never stores it in origin or argv.
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+auth)
	}
	if err := cmd.Run(); err != nil {
		return errors.New("Clone failed; check GitHub access and the destination, then try again")
	}
	return h.keepSetupCheckout(ctx, repo, path)
}
func (h *Handler) ingestSetupRepo(ctx context.Context, repo *api.SetupRepo, cursor int, progress func(int) error) (*time.Time, error) {
	release, ok := h.config.Store.TryPoller()
	if !ok {
		return nil, errors.New("GitHub is already being read")
	}
	defer release()
	token, err := h.setupToken(ctx)
	if err != nil || token == "" {
		return nil, errors.New("GitHub is not connected")
	}
	count := repo.ReadCount
	tiers := setupTiers{h: h, token: token, repo: repo.ID, seen: map[string]factory.Tier{}}
	for page := max(1, cursor); page <= 1000; page++ {
		var rows []struct {
			Number  int             `json:"number"`
			Title   string          `json:"title"`
			Body    string          `json:"body"`
			URL     string          `json:"html_url"`
			Created time.Time       `json:"created_at"`
			Pull    json.RawMessage `json:"pull_request"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			// The assignees array, never the deprecated singular assignee,
			// which is only its first entry. A pointer keeps an absent array
			// (unknown) apart from an empty one (nobody).
			Assignees *[]struct {
				Login string `json:"login"`
			} `json:"assignees"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		}
		resp, err := h.setupJSON(ctx, "/repos/"+repo.ID+"/issues?state=open&per_page=100&page="+strconv.Itoa(page), token, &rows)
		if err != nil {
			if resp != nil && (resp.StatusCode == 429 || (resp.StatusCode == 403 && (resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0"))) {
				seconds, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
				resume := h.clock().Add(time.Minute)
				if seconds > 0 {
					resume = time.Unix(seconds, 0)
				}
				if retry, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && retry > 0 {
					resume = h.clock().Add(time.Duration(retry) * time.Second)
				}
				return &resume, err
			}
			return nil, err
		}
		items := []factory.Item{}
		for _, row := range rows {
			if len(row.Pull) > 0 && string(row.Pull) != "null" {
				continue
			}
			labels := []string{}
			for _, label := range row.Labels {
				labels = append(labels, label.Name)
			}
			item := factory.Item{Repo: repo.ID, Num: row.Number, Title: row.Title, Body: row.Body, URL: row.URL, Created: row.Created, Kind: factory.KindIssue, Origin: factory.OriginForge, Author: row.User.Login, Tier: tiers.of(ctx, row.User.Login), Labels: labels, SetupHeld: true}
			if row.Assignees != nil {
				item.SourceAssignees = []string{}
				for _, a := range *row.Assignees {
					item.SourceAssignees = append(item.SourceAssignees, a.Login)
				}
				item.Assignee = factory.SoleSourceAssignee(item.SourceAssignees)
			}
			items = append(items, item)
			count++
		}
		if err = factorygithub.Merge(h.config.Store, factorygithub.Name, items); err != nil {
			return nil, err
		}
		repo.ReadCount = count
		if err = progress(page); err != nil {
			return nil, err
		}
		if len(rows) < 100 {
			repo.OpenIssues, repo.OpenIssuesExact = count, true
			return nil, nil
		}
	}
	return nil, errors.New("GitHub issue list exceeds the setup limit")
}

// setupScope checks advertised classic-token scopes without probing a write against someone's repository.
// Fine-grained tokens carry no OAuth scope header; GitHub enforces their selected permissions on each action.
func (h *Handler) setupScope(ctx context.Context, token string) error {
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("a GitHub token is one word")
	}
	var user struct {
		Login string `json:"login"`
	}
	response, err := h.setupJSON(ctx, "/user", token, &user)
	if err != nil || user.Login == "" {
		return errors.New("GitHub did not accept this connection")
	}
	if scopes := response.Header.Get("X-OAuth-Scopes"); scopes != "" {
		for _, scope := range strings.Split(scopes, ",") {
			if strings.TrimSpace(scope) == "repo" {
				return nil
			}
		}
		return errors.New("GitHub token needs repo access to read issues and publish pull requests")
	}
	return nil
}

func setupCheckoutPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "~" || strings.HasPrefix(path, "~/") {
		base, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = base
		} else {
			path = filepath.Join(base, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("Choose an absolute checkout folder")
	}
	return filepath.Clean(path), nil
}

// SetupCheckout records a team member's checkout on their app host, rather than cloning on the remote server.
// The app registry checks selection and operator authority through the server before using this local door.
func SetupCheckout(ctx context.Context, st *store.Store, profileDir, repo, path string, clone bool) (string, error) {
	if st == nil || !store.RepoName(repo) {
		return "", errors.New("checkout needs a selected repository")
	}
	path, err := setupCheckoutPath(path)
	if err != nil {
		return "", err
	}
	h := &Handler{config: Config{Mode: api.FactorySolo, Store: st, ProfileDir: profileDir}}
	if clone {
		err = h.cloneSetupRepo(ctx, repo, path)
	} else {
		err = h.keepSetupCheckout(ctx, repo, path)
	}
	return path, err
}

// Rate-limit waits belong to the engine clock even when no window is polling setup.
func (h *Handler) scheduleSetupDeadline(out setupState) {
	if h.setupTimer != nil {
		h.setupTimer.Stop()
		h.setupTimer = nil
	}
	if h.setupClosed || out.View.State != "reading" {
		return
	}
	var next time.Time
	for _, repo := range out.View.Repos {
		if repo.Selected && repo.Read == "rate_limited" && repo.ResumesAt != nil && (next.IsZero() || repo.ResumesAt.Before(next)) {
			next = *repo.ResumesAt
		}
	}
	if next.IsZero() {
		return
	}
	h.setupTimer = time.AfterFunc(max(time.Duration(0), next.Sub(h.clock())), func() {
		h.setupMu.Lock()
		defer h.setupMu.Unlock()
		if h.setupClosed || h.setupBusy {
			return
		}
		saved, err := h.loadSetup("local")
		if err != nil {
			return
		}
		for i := range saved.View.Repos {
			repo := &saved.View.Repos[i]
			if repo.Read == "rate_limited" && repo.ResumesAt != nil && !h.clock().Before(*repo.ResumesAt) {
				repo.Read = "next"
				repo.ResumesAt = nil
			}
		}
		if h.saveSetup(saved) == nil {
			h.startSetup(saved)
		}
	})
}

// setupTiers records, for each issue read in at setup, whether its author
// may write to the repository, as the GitHub source does on every poll:
// the signed-in person is the owner, write, maintain or admin access is a
// collaborator, and anything else, or no answer, is a stranger.
type setupTiers struct {
	h     *Handler
	token string
	repo  string
	me    *string
	seen  map[string]factory.Tier
}

func (s *setupTiers) of(ctx context.Context, login string) factory.Tier {
	if login == "" {
		return factory.TierStranger
	}
	key := strings.ToLower(login)
	if t, ok := s.seen[key]; ok {
		return t
	}
	if s.me == nil {
		var user struct {
			Login string `json:"login"`
		}
		_, _ = s.h.setupJSON(ctx, "/user", s.token, &user)
		s.me = &user.Login
	}
	t := factory.TierStranger
	if *s.me != "" && strings.EqualFold(*s.me, login) {
		t = factory.TierOwner
	} else {
		var p struct {
			Permission string `json:"permission"`
			RoleName   string `json:"role_name"`
		}
		if _, err := s.h.setupJSON(ctx, "/repos/"+s.repo+"/collaborators/"+url.PathEscape(login)+"/permission", s.token, &p); err == nil {
			switch {
			case p.RoleName == "maintain", p.Permission == "admin", p.Permission == "maintain", p.Permission == "write":
				t = factory.TierCollab
			}
		}
	}
	s.seen[key] = t
	return t
}
