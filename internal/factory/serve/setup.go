package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	"github.com/Agent-Field/codeaf/internal/redact"
)

const setupWait = 40 * time.Second

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

// setupJSON follows only fixed GitHub API paths; no client-supplied host receives a credential.
func (h *Handler) setupJSON(ctx context.Context, path, token string, out any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := h.githubClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp, githubStatusError(resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
	return resp, err
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

func (h *Handler) discoverSetup(ctx context.Context, out *setupState) error {
	// An unsuccessful rediscovery cannot retain a previous footer owner.
	out.View.GitHub.Organization = ""
	if h.seam.GHLogin != nil && h.mode() == api.FactorySolo {
		login, err := h.seam.GHLogin(ctx)
		if err == nil && login != "" {
			path, _ := exec.LookPath("gh")
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
	for _, r := range out.View.Repos {
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
	for page := 1; page <= 100; page++ {
		var rows []struct {
			FullName string `json:"full_name"`
			Open     int    `json:"open_issues_count"`
			Archived bool   `json:"archived"`
		}
		if _, err = h.setupJSON(ctx, fmt.Sprintf("/user/repos?per_page=100&sort=pushed&page=%d", page), token, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			if !store.RepoName(row.FullName) {
				continue
			}
			userHome, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			repo := api.SetupRepo{ID: row.FullName, FullName: row.FullName, OpenIssues: row.Open, Archived: row.Archived, Selected: slices.Contains(watched, row.FullName), Read: "next", Clone: "missing", SuggestedCheckout: filepath.Join(userHome, "code", strings.ReplaceAll(row.FullName, "/", "-"))}
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
		}
		if len(rows) < 100 {
			break
		}
		if page == 100 {
			return errors.New("GitHub repository list exceeds the setup limit")
		}
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
	h.setupMu.Lock()
	defer h.setupMu.Unlock()
	id := r.PathValue("id")
	var body api.FactorySetupRequest
	if r.Method == "POST" {
		if !h.decode(w, r, &body, false) {
			return
		}
		if id == "" {
			id = body.ID
		} else if body.ID != "" && body.ID != id {
			h.fail(w, 422, "setup id does not match the route")
			return
		}
	}
	if id == "" {
		h.fail(w, 422, "setup needs a factory id")
		return
	}
	out, err := h.loadSetup(id)
	if h.failed(w, err) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), setupWait)
	defer cancel()
	if r.Method == "GET" {
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
				path, err := exec.LookPath("gh")
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
	if h.setupBusy || h.setupClosed {
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
	if out.View.State == "reading" && !slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.Selected && r.Read != "done" && r.Read != "empty" }) {
		selected := []string{}
		for _, repo := range out.View.Repos {
			if repo.Selected {
				selected = append(selected, repo.ID)
			}
		}
		if err := h.config.Store.SetRepos(selected); err != nil {
			words := redact.Secrets(err.Error())
			out.View.GitHub.Error = &words
			_ = h.setupProgress(out)
			return
		}
		out.View.State = "done"
		if slices.ContainsFunc(out.View.Repos, func(r api.SetupRepo) bool { return r.Selected && r.Clone != "found" }) {
			out.View.State = "checkouts"
		}
	}
	_ = h.setupProgress(out)
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
			Assignee *struct {
				Login string `json:"login"`
			} `json:"assignee"`
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
			item := factory.Item{Repo: repo.ID, Num: row.Number, Title: row.Title, Body: row.Body, URL: row.URL, Created: row.Created, Kind: factory.KindIssue, Origin: factory.OriginForge, Author: row.User.Login, Labels: labels, SetupHeld: true}
			if row.Assignee != nil {
				item.Assignee = factory.NormalizeHandle(row.Assignee.Login)
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
			repo.OpenIssues = count
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
