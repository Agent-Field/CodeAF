package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// ── THE FACTORY ON THIS MACHINE, AND ITS FIRST-RUN SETUP ────────────────────
//
// factory.local_* starts, and reports on, the factory THIS engine hosts for
// its own person: one solo factory on loopback, started only when a caller
// asks. factory.setup_* walks that factory's first run: connect GitHub, pick
// the visible repositories, read their open issues into Backlog, and clone
// each one or leave it for later.
//
// EVERY METHOD IS CLOSED AND TYPED. There is no method that forwards a path,
// a verb or a body to the factory; the engine speaks the factory's setup
// route with the verbs this file names and no others, and unknown params are
// bad_params, not ignored.
//
// SETUP STARTS NOTHING. It reads issues into Backlog and clones repositories.
// It starts no job, calls no model, and leaves no runner behind. A job runs
// only when a person starts it through [FactoryActor], later, and only on a
// machine whose runner a person turned on with factory.local_run: nothing
// else starts one, not launch, not factory.local_start, not setup_done.
//
// NOTHING SECRET AND NOTHING PRIVATE LEAVES. A pasted GitHub token goes in as
// a request and is never answered. A folder the person chose comes back only
// as the "~/…" it would be shown as, and never when it lies inside the app's
// own folders. There is no percentage for a clone: the factory reports a
// state (missing, cloning, found, skipped, failed) and the size in megabytes
// GitHub quoted, and this wire carries exactly that.
//
// "Caller" is whoever the app says is asking (a window). It is required, so an
// anonymous request is refused; the local factory itself is one per machine
// and shared by every window.
//
// ALL OF THIS IS OPTIONAL. An engine that is not a [FactoryLocal] answers
// these methods as unknown_method and does not announce [FeatureFactoryLocal].

// FeatureFactoryLocal is announced in hello when the engine serves
// factory.local_* and factory.setup_*.
const FeatureFactoryLocal = "factory.local"

// The states factory.local_status answers.
const (
	LocalStopped = "stopped"
	LocalRunning = "running"
)

// Who holds the factory authority on this machine.
const (
	// LocalAuthorityApp: this engine serves the factory.
	LocalAuthorityApp = "this_app"
	// LocalAuthorityOther: another codeaf on this machine already serves it
	// and this engine uses that one. Exactly one factory serves per machine.
	LocalAuthorityOther = "other_codeaf"
)

// The states of an existing `codeaf attach` the app can see.
const (
	AttachmentNone      = "none"
	AttachmentAvailable = "available"
	AttachmentAdopted   = "adopted"
	AttachmentUnsafe    = "unsafe"
)

// The verbs a [FactorySetupAct] carries. They are the factory's own.
const (
	SetupVerbDiscover   = "discover"
	SetupVerbConnect    = "connect"
	SetupVerbSelect     = "select_repos"
	SetupVerbRetryRead  = "retry_read"
	SetupVerbClone      = "clone"
	SetupVerbUseFolder  = "checkout"
	SetupVerbSkip       = "skip"
	SetupVerbBack       = "back"
	SetupVerbDone       = "done"
	SetupSourceGH       = "gh"
	SetupSourceToken    = "token"
	MaxFactorySetupRepo = 100
	MaxSetupFolder      = 1024
	MaxSetupToken       = 512
)

// FactoryLocalParams is the whole request of the local methods that take no
// more than who is asking.
type FactoryLocalParams struct {
	Caller string `json:"caller"`
}

// FactoryLocalAttachment is the `codeaf attach` the terminal already made, as
// far as the app may see it. The bearer in attach.json is never here and is
// never saved by the app.
type FactoryLocalAttachment struct {
	State   string `json:"state" enum:"none,available,adopted,unsafe"`
	Factory string `json:"factory,omitempty"`
	URL     string `json:"url,omitempty"`
	Name    string `json:"name,omitempty"`
}

// FactoryLocalStatus is the factory on this machine. Factory is the id
// factory.list and factory.queue use for it once it runs.
type FactoryLocalStatus struct {
	State     string `json:"state" enum:"stopped,running"`
	Authority string `json:"authority,omitempty" enum:"this_app,other_codeaf"`
	Factory   string `json:"factory,omitempty"`
	Name      string `json:"name,omitempty"`
	URL       string `json:"url,omitempty"`
	// Runner is true once a person turned this engine's runner on
	// (factory.local_run) and until the engine quits. A runner that waits
	// behind another process's on this machine still reads true: it takes
	// jobs the moment that one lets go.
	Runner     bool                   `json:"runner"`
	Attachment FactoryLocalAttachment `json:"attachment"`
}

// FactorySetupGitHub is what the first step knows of GitHub. Login is the
// `gh` login when `gh` is signed in on this machine, which is what the
// "use gh" choice would connect; it is never a token.
type FactorySetupGitHub struct {
	Connected    bool   `json:"connected"`
	GHLogin      string `json:"gh_login,omitempty"`
	Organization string `json:"organization,omitempty"`
	VisibleRepos int    `json:"visible_repos"`
	Error        string `json:"error,omitempty"`
}

// FactorySetupRepo is one repository the person can see. Folder and
// SuggestedFolder are shown paths ("~/code/acme-api"), Folder empty until a
// checkout is chosen or found. Read counts issues read so far; Clone has no
// percentage because the factory reports none.
type FactorySetupRepo struct {
	ID              string     `json:"id"`
	OpenIssues      int        `json:"open_issues"`
	Pipeline        string     `json:"pipeline,omitempty" enum:"file,defaults"`
	Archived        bool       `json:"archived"`
	Selected        bool       `json:"selected"`
	Read            string     `json:"read" enum:"next,reading,done,empty,waiting,rate_limited"`
	ReadCount       int        `json:"read_count"`
	WaitingS        int        `json:"waiting_s,omitempty"`
	ResumesAt       *time.Time `json:"resumes_at,omitempty"`
	Clone           string     `json:"clone" enum:"missing,cloning,found,skipped,failed"`
	CloneError      string     `json:"clone_error,omitempty"`
	CloneMB         int        `json:"clone_mb,omitempty"`
	Folder          string     `json:"folder,omitempty"`
	SuggestedFolder string     `json:"suggested_folder,omitempty"`
}

// FactorySetupView is the first run as the factory reports it, with nothing
// private. State is the step: github, repos, reading, checkouts, done.
type FactorySetupView struct {
	State  string             `json:"state" enum:"github,repos,reading,checkouts,done"`
	GitHub FactorySetupGitHub `json:"github"`
	Repos  []FactorySetupRepo `json:"repos"`
}

// FactorySetupConnectParams connects GitHub: from `gh` (Token empty) or from a
// pasted token. The token is held only by the engine and never answered.
type FactorySetupConnectParams struct {
	Caller string `json:"caller"`
	Source string `json:"source" enum:"gh,token"`
	Token  string `json:"token,omitempty"`
}

// FactorySetupSelectParams picks the repositories to read, as owner/name.
type FactorySetupSelectParams struct {
	Caller string   `json:"caller"`
	Repos  []string `json:"repos"`
}

// FactorySetupRepoParams names one repository, as owner/name.
type FactorySetupRepoParams struct {
	Caller string `json:"caller"`
	Repo   string `json:"repo"`
}

// FactorySetupFolderParams names a repository and the folder the person
// chose: an absolute path or one starting "~/". Clone makes it there; use
// keeps a checkout that is already there.
type FactorySetupFolderParams struct {
	Caller string `json:"caller"`
	Repo   string `json:"repo"`
	Folder string `json:"folder"`
}

// FactorySetupAct is one setup step as the engine is handed it, checked.
type FactorySetupAct struct {
	Verb   string
	Source string
	Token  string
	Repos  []string
	Repo   string
	Folder string
}

// FactoryLocal is the engine's half of factory.local_* and factory.setup_*.
// Implementations answer wire errors ([Error]); the context carries the
// call's deadline.
type FactoryLocal interface {
	// FactoryLocalStart serves, or joins, the one factory of this machine.
	// It is idempotent and starts no job.
	FactoryLocalStart(ctx context.Context, caller string) (FactoryLocalStatus, error)
	FactoryLocalStatus(ctx context.Context, caller string) (FactoryLocalStatus, error)
	// FactoryLocalRun is the one door that turns this engine's runner on: it
	// takes jobs of the factory this engine serves, with the engine's own
	// keys and model settings. Idempotent. It starts no job by itself (a
	// person starts one with factory.job_start), and it is refused when the
	// factory is not started or another codeaf serves it.
	FactoryLocalRun(ctx context.Context, caller string) (FactoryLocalStatus, error)
	// FactoryLocalAdopt shows the app the factory `codeaf attach` named, when
	// it is safe to; the bearer stays in memory.
	FactoryLocalAdopt(ctx context.Context, caller string) (FactoryLocalStatus, error)
	FactorySetupRead(ctx context.Context, caller string) (FactorySetupView, error)
	FactorySetupAct(ctx context.Context, caller string, act FactorySetupAct) (FactorySetupView, error)
}

func withFactoryLocalFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryLocal); ok {
		hello = withFeature(hello, FeatureFactoryLocal)
	}
	return hello
}

func factoryLocalOf(e Engine, name string) (FactoryLocal, error) {
	l, ok := e.(FactoryLocal)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return l, nil
}

// setupFolder is a folder the person chose: absolute or "~/…", one line.
func setupFolder(folder string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" || len(folder) > MaxSetupFolder || strings.IndexFunc(folder, unicode.IsControl) >= 0 {
		return "", Errorf(CodeBadParams, "folder is required, one line, at most %d characters", MaxSetupFolder)
	}
	if !strings.HasPrefix(folder, "/") && !strings.HasPrefix(folder, "~/") && !(len(folder) > 2 && folder[1] == ':') {
		return "", Errorf(CodeBadParams, "folder must be a full path or start with ~/")
	}
	return folder, nil
}

func setupToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > MaxSetupToken || strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", Errorf(CodeBadParams, "token is one word of at most %d characters", MaxSetupToken)
	}
	return token, nil
}

// factoryLocalCall builds one factory.local_* method over params P that only
// carry a caller.
func factoryLocalCall(name string, call func(FactoryLocal, context.Context, string) (FactoryLocalStatus, error)) method {
	return shaped[FactoryLocalParams, FactoryLocalStatus](func(e Engine, raw json.RawMessage) (*FactoryLocalStatus, error) {
		l, err := factoryLocalOf(e, name)
		if err != nil {
			return nil, err
		}
		var p FactoryLocalParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := call(l, ctx, caller)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// factorySetupCall builds one factory.setup_* method: params P are decoded
// strictly, prep turns them into the caller and the one act they mean.
func factorySetupCall[P any](name string, prep func(P) (string, FactorySetupAct, error)) method {
	return shaped[P, FactorySetupView](func(e Engine, raw json.RawMessage) (*FactorySetupView, error) {
		l, err := factoryLocalOf(e, name)
		if err != nil {
			return nil, err
		}
		var p P
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		caller, act, err := prep(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := l.FactorySetupAct(ctx, caller, act)
		if err != nil {
			return nil, err
		}
		if out.Repos == nil {
			out.Repos = []FactorySetupRepo{}
		}
		return &out, nil
	})
}

func plainSetup(verb string) func(FactoryLocalParams) (string, FactorySetupAct, error) {
	return func(p FactoryLocalParams) (string, FactorySetupAct, error) {
		caller, err := factoryCaller(p.Caller)
		return caller, FactorySetupAct{Verb: verb}, err
	}
}

func setupRepo(caller, repo, verb string) (string, FactorySetupAct, error) {
	caller, err := factoryCaller(caller)
	if err != nil {
		return "", FactorySetupAct{}, err
	}
	name, err := FactoryRepoName(repo)
	if err != nil {
		return "", FactorySetupAct{}, err
	}
	return caller, FactorySetupAct{Verb: verb, Repo: name}, nil
}

func setupRepoFolder(p FactorySetupFolderParams, verb string) (string, FactorySetupAct, error) {
	caller, act, err := setupRepo(p.Caller, p.Repo, verb)
	if err != nil {
		return "", act, err
	}
	if act.Folder, err = setupFolder(p.Folder); err != nil {
		return "", act, err
	}
	return caller, act, nil
}

func init() {
	for name, m := range map[string]method{
		"factory.local_start":  factoryLocalCall("factory.local_start", FactoryLocal.FactoryLocalStart),
		"factory.local_status": factoryLocalCall("factory.local_status", FactoryLocal.FactoryLocalStatus),
		"factory.local_adopt":  factoryLocalCall("factory.local_adopt", FactoryLocal.FactoryLocalAdopt),
		"factory.local_run":    factoryLocalCall("factory.local_run", FactoryLocal.FactoryLocalRun),
		"factory.setup_read": shaped[FactoryLocalParams, FactorySetupView](func(e Engine, raw json.RawMessage) (*FactorySetupView, error) {
			l, err := factoryLocalOf(e, "factory.setup_read")
			if err != nil {
				return nil, err
			}
			var p FactoryLocalParams
			if err := decodeStrict("factory.setup_read", raw, &p); err != nil {
				return nil, err
			}
			caller, err := factoryCaller(p.Caller)
			if err != nil {
				return nil, err
			}
			ctx, cancel := factoryCall()
			defer cancel()
			out, err := l.FactorySetupRead(ctx, caller)
			if err != nil {
				return nil, err
			}
			if out.Repos == nil {
				out.Repos = []FactorySetupRepo{}
			}
			return &out, nil
		}),
		"factory.setup_discover": factorySetupCall("factory.setup_discover", plainSetup(SetupVerbDiscover)),
		"factory.setup_back":     factorySetupCall("factory.setup_back", plainSetup(SetupVerbBack)),
		"factory.setup_done":     factorySetupCall("factory.setup_done", plainSetup(SetupVerbDone)),
		"factory.setup_connect": factorySetupCall("factory.setup_connect", func(p FactorySetupConnectParams) (string, FactorySetupAct, error) {
			caller, err := factoryCaller(p.Caller)
			if err != nil {
				return "", FactorySetupAct{}, err
			}
			switch p.Source {
			case SetupSourceGH:
				if strings.TrimSpace(p.Token) != "" {
					return "", FactorySetupAct{}, Errorf(CodeBadParams, "a token goes with source token, not gh")
				}
				return caller, FactorySetupAct{Verb: SetupVerbConnect, Source: SetupSourceGH}, nil
			case SetupSourceToken:
				token, err := setupToken(p.Token)
				if err != nil {
					return "", FactorySetupAct{}, err
				}
				return caller, FactorySetupAct{Verb: SetupVerbConnect, Source: SetupSourceToken, Token: token}, nil
			}
			return "", FactorySetupAct{}, Errorf(CodeBadParams, "source must be gh or token")
		}),
		"factory.setup_select": factorySetupCall("factory.setup_select", func(p FactorySetupSelectParams) (string, FactorySetupAct, error) {
			caller, err := factoryCaller(p.Caller)
			if err != nil {
				return "", FactorySetupAct{}, err
			}
			if len(p.Repos) == 0 || len(p.Repos) > MaxFactorySetupRepo {
				return "", FactorySetupAct{}, Errorf(CodeBadParams, "repos is from 1 to %d repositories", MaxFactorySetupRepo)
			}
			seen := map[string]bool{}
			repos := make([]string, 0, len(p.Repos))
			for _, repo := range p.Repos {
				name, err := FactoryRepoName(repo)
				if err != nil {
					return "", FactorySetupAct{}, err
				}
				if !seen[name] {
					seen[name] = true
					repos = append(repos, name)
				}
			}
			return caller, FactorySetupAct{Verb: SetupVerbSelect, Repos: repos}, nil
		}),
		"factory.setup_retry_read": factorySetupCall("factory.setup_retry_read", func(p FactorySetupRepoParams) (string, FactorySetupAct, error) {
			return setupRepo(p.Caller, p.Repo, SetupVerbRetryRead)
		}),
		"factory.setup_skip": factorySetupCall("factory.setup_skip", func(p FactorySetupRepoParams) (string, FactorySetupAct, error) {
			return setupRepo(p.Caller, p.Repo, SetupVerbSkip)
		}),
		"factory.setup_clone": factorySetupCall("factory.setup_clone", func(p FactorySetupFolderParams) (string, FactorySetupAct, error) {
			return setupRepoFolder(p, SetupVerbClone)
		}),
		"factory.setup_use_folder": factorySetupCall("factory.setup_use_folder", func(p FactorySetupFolderParams) (string, FactorySetupAct, error) {
			return setupRepoFolder(p, SetupVerbUseFolder)
		}),
	} {
		methods[name] = m
		factoryLocalMethods[name] = m
	}
}

// factoryLocalMethods is the table of factory.local_* and factory.setup_*,
// for the tests that walk it.
var factoryLocalMethods = map[string]method{}
