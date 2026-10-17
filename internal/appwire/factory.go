package appwire

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// ── THE FACTORY, AS AN ATTACHED REMOTE ──────────────────────────────────────
//
// The factory runs a repository's issues through its pipeline. This engine can
// ATTACH to a factory that another machine serves, read it, and sign a person
// in to it. It cannot host a factory or run a job, and the capabilities answer
// says so, so a screen never offers what is not here.
//
// EVERY METHOD IS TYPED. There is no method that forwards an arbitrary path or
// an untyped body to the factory: a read is one method per resource, and its
// answer is the factory's own [api] struct, so the schema is the contract.
//
// CREDENTIALS NEVER LEAVE THE ENGINE. A session or an access token is held in
// this process's memory; a device code is held beside the flow it belongs to.
// What crosses the wire is progress (a user code to type on GitHub, a state)
// and the signed-in person's profile, never a bearer, a device code, or the
// access link a sign-in was started from.
//
// "Caller" on the sign-in methods is whoever the app says is asking (a window).
// A flow belongs to the caller that started it: another caller cannot poll it,
// cancel it, or learn that it exists.

// FeatureFactory is announced in hello when the engine serves factory.*.
const FeatureFactory = "factory.remote"

// The states a connection is seen in.
const (
	FactoryOK            = "ok"
	FactoryUnreachable   = "unreachable"
	FactorySignedOut     = "signed_out"
	FactoryGitHubExpired = "github_expired"
)

// The states a device sign-in poll answers.
const (
	SignInPending  = "pending"
	SignInSignedIn = "signed_in"
	SignInExpired  = "expired"
	SignInDenied   = "denied"
)

// The bounds the wire promises, so an app can size to them.
const (
	// MaxFactoryConnections is the most factories one engine stays attached to.
	MaxFactoryConnections = 32
	// MaxFactoryFlows is the most sign-in and access-link flows pending at
	// once, across callers. A flow expires; an abandoned one is dropped.
	MaxFactoryFlows = 256
	// DefaultFactoryActivity and MaxFactoryActivity bound factory.activity.
	DefaultFactoryActivity = 200
	MaxFactoryActivity     = 1000
)

// factoryCallBudget is the most one factory.* call may take, the engine's own
// bounds included.
const factoryCallBudget = 25 * time.Second

// FactoryCapabilities is what this engine can do about a factory, stated
// plainly so an unsupported verb is absent from the screen and not broken.
type FactoryCapabilities struct {
	// Contract is the factory API contract this engine speaks.
	Contract int `json:"contract"`
	// Remote: attach to, read and sign in to a factory served elsewhere.
	Remote bool `json:"remote"`
	// Registry: more than one factory may be attached at once.
	Registry bool `json:"registry"`
	// AccessLinks: codeaf://factory/join links can be checked and accepted.
	AccessLinks bool `json:"access_links"`
	// SignIn: a person can sign in to GitHub on an attached factory.
	SignIn bool `json:"sign_in"`
	// Local: this engine can serve a factory of its own, on request, and walk
	// its first-run setup (factory_setup.go). True only for an engine that
	// implements [FactoryLocal].
	Local bool `json:"local"`
	// Runner: this engine can run the jobs of the factory it serves, but only
	// when a person asks (factory.local_run); it never starts one at launch or
	// from setup. True only for an engine that implements [FactoryLocal].
	Runner bool `json:"runner"`
	// Writes: jobs can be started, stopped, approved or edited through the
	// wire. False here; every factory.* method only reads or signs in.
	Writes bool `json:"writes"`
	// Admin: factory.sign_out and factory.revoke_sessions are served (feature
	// factory.admin). True only for an engine that implements [FactoryAdmin].
	Admin bool `json:"admin"`
	// Links: factory.links, link_create, link_copy and link_revoke are served
	// (feature factory.links). True only for an engine that implements
	// [FactoryLinkAdmin].
	Links bool `json:"links"`
	// CopyLink: this engine can put a freshly made invitation on THIS
	// machine's clipboard right now. False unless a trusted local launch
	// supplied a working clipboard; no invitation is made without it.
	CopyLink       bool `json:"copy_link"`
	MaxConnections int  `json:"max_connections"`
	MaxFlows       int  `json:"max_flows"`
}

// FactoryConnection is one attached factory. URL is an origin with no path,
// query or credentials, and nothing here is a secret.
type FactoryConnection struct {
	ID       string     `json:"id"`
	URL      string     `json:"url"`
	Name     string     `json:"name"`
	Mode     string     `json:"mode,omitempty" enum:"solo,team"`
	Operator bool       `json:"operator"`
	State    string     `json:"state" enum:"ok,unreachable,signed_out,github_expired"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}

// FactoryList is factory.list's answer.
type FactoryList struct {
	Factories []FactoryConnection `json:"factories"`
}

// FactoryRef names one attached factory, by the id factory.list gave.
type FactoryRef struct {
	Factory string `json:"factory"`
}

// FactoryAttachParams attaches a factory. Token is an access token the person
// was given for it (codeaf factory token); it is held in memory only and is
// never written to disk or answered.
type FactoryAttachParams struct {
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// FactoryJobParams names one job of one factory.
type FactoryJobParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
}

// FactoryRepoParams names one repository, as owner/name.
type FactoryRepoParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
}

// FactoryActivityParams reads the factory's record. Since is an RFC 3339
// instant; Limit is the newest events to answer.
type FactoryActivityParams struct {
	Factory string     `json:"factory"`
	Since   *time.Time `json:"since,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

// FactoryRepo is one repository the factory can see. The factory's own
// checkout path is deliberately not here: it names a folder on another
// machine.
type FactoryRepo struct {
	Full    string    `json:"full"`
	Name    string    `json:"name"`
	Owner   string    `json:"owner"`
	Private bool      `json:"private"`
	Pushed  time.Time `json:"pushed"`
	Open    int       `json:"open"`
}

// FactoryRepos is factory.repos' answer.
type FactoryRepos struct {
	Factory   api.Factory   `json:"factory"`
	Watched   []string      `json:"watched"`
	Available []FactoryRepo `json:"available"`
}

// FactoryPipeline is factory.pipeline's answer: the repository's pipeline as
// its file reads, and why a reading failed.
type FactoryPipeline struct {
	Factory  api.Factory  `json:"factory"`
	Pipeline api.Pipeline `json:"pipeline"`
	Problems []string     `json:"problems"`
}

// FactoryActivity is factory.activity's answer, oldest first. Truncated says
// older events than Limit exist.
type FactoryActivity struct {
	Factory   api.Factory    `json:"factory"`
	Events    []record.Event `json:"events"`
	Truncated bool           `json:"truncated"`
	// People is the signed-in GitHub name and photo of the handles named in
	// Events; a handle with no verified profile is absent.
	People map[string]factory.PersonSummary `json:"people,omitempty"`
}

// FactoryAccess is what checking an access link says. It is api.FactoryAccess
// without the two fields that are secrets: the session grant a completed
// sign-in earns (the engine keeps it) and the link itself (the caller has it).
// Factory.ID is the id factory.list uses once the factory is joined.
type FactoryAccess struct {
	State     string            `json:"state" enum:"ready,waiting,joined,expired,revoked,used_up,no_access,unreachable,already_joined"`
	Factory   api.AccessFactory `json:"factory"`
	Detail    string            `json:"detail"`
	Message   string            `json:"message,omitempty"`
	Help      string            `json:"help,omitempty"`
	SignInURL string            `json:"sign_in_url,omitempty"`
	UserCode  string            `json:"user_code,omitempty"`
}

// FactoryAccessParams carries an access link and who is opening it.
type FactoryAccessParams struct {
	Link   string `json:"link"`
	Caller string `json:"caller"`
}

// FactorySignInStartParams starts GitHub sign-in on an attached factory.
type FactorySignInStartParams struct {
	Factory string `json:"factory"`
	Caller  string `json:"caller"`
}

// FactorySignInFlowParams names a flow factory.sign_in_start gave.
type FactorySignInFlowParams struct {
	Flow   string `json:"flow"`
	Caller string `json:"caller"`
}

// FactorySignIn is a started GitHub device sign-in: show UserCode, send the
// person to VerificationURI, and poll Flow no sooner than Interval seconds.
// Flow is this engine's own handle; the device code itself stays here.
type FactorySignIn struct {
	Flow            string `json:"flow"`
	Factory         string `json:"factory"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

// FactorySignInState is one poll of a sign-in. Me is present only once signed
// in, and Message only when the factory gave a reason for a denial.
type FactorySignInState struct {
	State    string  `json:"state" enum:"pending,signed_in,expired,denied"`
	Interval int     `json:"interval,omitempty"`
	Message  string  `json:"message,omitempty"`
	Me       *api.Me `json:"me,omitempty"`
}

// FactoryMe is the public view of factory.me: who this caller is, and nothing
// the factory keeps about the machine behind it. It is NOT api.Me, which also
// carries runners, machines, checkouts and the session's id.
//
// GitHubSignedIn is true ONLY when the factory attested a verified profile for the
// caller. A script or host access key can hold a handle and even operator
// rights without a person having signed in through GitHub, so the handle and
// the operator flag never stand in for it.
type FactoryMe struct {
	Factory       api.Factory `json:"factory"`
	CodeafVersion string      `json:"codeaf_version"`
	Contract      int         `json:"contract"`
	api.Profile
	Mode           string `json:"mode"`
	Operator       bool   `json:"operator"`
	Access         string `json:"access"`
	GitHubSignedIn bool   `json:"github_signed_in"`
	CanSignOut     bool   `json:"can_sign_out"`
	// GitHubSignInAvailable and InviteAvailable are what the serving factory
	// says it is configured for (GitHub device sign-in; making and redeeming an
	// invitation). They gate an affordance; they are not a permission, and an
	// older factory that says nothing reads as false.
	GitHubSignInAvailable bool `json:"github_sign_in_available"`
	InviteAvailable       bool `json:"invite_available"`
}

// NewFactoryMe narrows the factory's HTTP identity to the public view. Sign-in
// is the factory's explicit github_signed_in attestation; a session id, handle,
// name or operator flag never implies it, and an older server's omission is false.
func NewFactoryMe(me api.Me) FactoryMe {
	out := FactoryMe{
		Factory: me.Factory, CodeafVersion: me.CodeafVersion, Contract: me.Contract,
		Profile: me.Profile, Mode: me.Mode, Operator: me.Operator, Access: me.Access,
		GitHubSignedIn: me.GitHubSignedIn,
		CanSignOut:     me.GitHubSignedIn && FactorySessionCanEnd(me.SessionID),

		GitHubSignInAvailable: me.GitHubSignInAvailable,
		InviteAvailable:       me.GitHubSignInAvailable && me.InviteAvailable,
	}
	if out.Scopes == nil {
		out.Scopes = []string{}
	}
	return out
}

// Factory is the engine's half of the factory wire. An [Engine] that is not
// one refuses factory.* as unknown_method, and does not announce
// [FeatureFactory]. Implementations answer wire errors ([Error]) for codes the
// app acts on; the context carries the call's deadline.
type Factory interface {
	FactoryCapabilities(ctx context.Context) (FactoryCapabilities, error)
	FactoryList(ctx context.Context) (FactoryList, error)
	FactoryAttach(ctx context.Context, url, token string) (FactoryConnection, error)
	FactoryDetach(ctx context.Context, id string) error
	FactoryMe(ctx context.Context, id string) (FactoryMe, error)
	FactoryQueue(ctx context.Context, id string) (api.Queue, error)
	FactoryJob(ctx context.Context, id string, job int) (api.Job, error)
	FactoryRepos(ctx context.Context, id string) (FactoryRepos, error)
	FactoryPipeline(ctx context.Context, id, repo string) (FactoryPipeline, error)
	FactoryRunners(ctx context.Context, id string) (api.Runners, error)
	FactoryPeople(ctx context.Context, id string) (api.People, error)
	FactoryActivity(ctx context.Context, id string, since time.Time, limit int) (FactoryActivity, error)
	FactorySetup(ctx context.Context, id string) (api.FactorySetup, error)
	FactoryAccessCheck(ctx context.Context, caller, link string) (FactoryAccess, error)
	FactoryAccessSignIn(ctx context.Context, caller, link string) (FactoryAccess, error)
	FactoryAccessCancel(ctx context.Context, caller, link string) (FactoryAccess, error)
	FactorySignInStart(ctx context.Context, caller, id string) (FactorySignIn, error)
	FactorySignInPoll(ctx context.Context, caller, flow string) (FactorySignInState, error)
	FactorySignInCancel(ctx context.Context, caller, flow string) error
}

func withFactoryFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactory)
	}
	return hello
}

func factoryOf(e Engine, name string) (Factory, error) {
	f, ok := e.(Factory)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return f, nil
}

func factoryCall() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), factoryCallBudget)
}

// factoryID, factoryCaller and factoryLink are the params every method checks
// before the engine is asked anything.
func factoryID(id string) (string, error) { return required("factory", id) }

func factoryCaller(caller string) (string, error) {
	caller = strings.TrimSpace(caller)
	if caller == "" || len(caller) > 128 {
		return "", Errorf(CodeBadParams, "caller is required and at most 128 characters")
	}
	return caller, nil
}

func factoryLink(link string) (string, error) {
	link = strings.TrimSpace(link)
	if link == "" || len(link) > 4096 {
		return "", Errorf(CodeBadParams, "link is required and at most 4096 characters")
	}
	return link, nil
}

// FactoryRepoName is repo as owner/name, or why it is not.
func FactoryRepoName(repo string) (string, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(repo), "/")
	if !ok || owner == "" || name == "" || strings.ContainsAny(owner+name, "/\\?#% ") || owner == "." || owner == ".." || name == "." || name == ".." {
		return "", Errorf(CodeBadParams, "repo must be owner/name")
	}
	return owner + "/" + name, nil
}

func init() {
	for name, m := range factoryMethods {
		methods[name] = m
	}
}

// factoryRead is a method that takes one factory and answers one of its
// resources.
func factoryRead[R any](name string, read func(Factory, context.Context, string) (R, error)) method {
	return typed(func(e Engine, p FactoryRef) (*R, error) {
		f, err := factoryOf(e, name)
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := read(f, ctx, id)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func factoryAccess(name string, call func(Factory, context.Context, string, string) (FactoryAccess, error)) method {
	return typed(func(e Engine, p FactoryAccessParams) (*FactoryAccess, error) {
		f, err := factoryOf(e, name)
		if err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		link, err := factoryLink(p.Link)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := call(f, ctx, caller, link)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

var factoryMethods = map[string]method{
	"factory.capabilities": typed(func(e Engine, _ Empty) (*FactoryCapabilities, error) {
		f, err := factoryOf(e, "factory.capabilities")
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactoryCapabilities(ctx)
		return &out, err
	}),
	"factory.list": typed(func(e Engine, _ Empty) (*FactoryList, error) {
		f, err := factoryOf(e, "factory.list")
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactoryList(ctx)
		if out.Factories == nil {
			out.Factories = []FactoryConnection{}
		}
		return &out, err
	}),
	"factory.attach": typed(func(e Engine, p FactoryAttachParams) (*FactoryConnection, error) {
		f, err := factoryOf(e, "factory.attach")
		if err != nil {
			return nil, err
		}
		if _, err := required("url", p.URL); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactoryAttach(ctx, strings.TrimSpace(p.URL), strings.TrimSpace(p.Token))
		if err != nil {
			return nil, err
		}
		return &out, nil
	}),
	"factory.detach": typed(func(e Engine, p FactoryRef) (*Empty, error) {
		f, err := factoryOf(e, "factory.detach")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		return done(f.FactoryDetach(ctx, id))
	}),
	"factory.me": factoryRead("factory.me", func(f Factory, ctx context.Context, id string) (FactoryMe, error) {
		return f.FactoryMe(ctx, id)
	}),
	"factory.queue": factoryRead("factory.queue", func(f Factory, ctx context.Context, id string) (api.Queue, error) {
		return f.FactoryQueue(ctx, id)
	}),
	"factory.job": typed(func(e Engine, p FactoryJobParams) (*api.Job, error) {
		f, err := factoryOf(e, "factory.job")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		if p.Job < 1 {
			return nil, Errorf(CodeBadParams, "job must be a whole number from 1")
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactoryJob(ctx, id, p.Job)
		if err != nil {
			return nil, err
		}
		return &out, nil
	}),
	"factory.repos": factoryRead("factory.repos", func(f Factory, ctx context.Context, id string) (FactoryRepos, error) {
		return f.FactoryRepos(ctx, id)
	}),
	"factory.pipeline": typed(func(e Engine, p FactoryRepoParams) (*FactoryPipeline, error) {
		f, err := factoryOf(e, "factory.pipeline")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		repo, err := FactoryRepoName(p.Repo)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactoryPipeline(ctx, id, repo)
		if err != nil {
			return nil, err
		}
		if out.Problems == nil {
			out.Problems = []string{}
		}
		return &out, nil
	}),
	"factory.runners": factoryRead("factory.runners", func(f Factory, ctx context.Context, id string) (api.Runners, error) {
		return f.FactoryRunners(ctx, id)
	}),
	"factory.people": factoryRead("factory.people", func(f Factory, ctx context.Context, id string) (api.People, error) {
		return f.FactoryPeople(ctx, id)
	}),
	"factory.activity": typed(func(e Engine, p FactoryActivityParams) (*FactoryActivity, error) {
		f, err := factoryOf(e, "factory.activity")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		limit := p.Limit
		switch {
		case limit == 0:
			limit = DefaultFactoryActivity
		case limit < 0 || limit > MaxFactoryActivity:
			return nil, Errorf(CodeBadParams, "limit must be from 1 to %d", MaxFactoryActivity)
		}
		var since time.Time
		if p.Since != nil {
			since = *p.Since
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactoryActivity(ctx, id, since, limit)
		if err != nil {
			return nil, err
		}
		if out.Events == nil {
			out.Events = []record.Event{}
		}
		return &out, nil
	}),
	"factory.setup": factoryRead("factory.setup", func(f Factory, ctx context.Context, id string) (api.FactorySetup, error) {
		return f.FactorySetup(ctx, id)
	}),
	"factory.access_check": factoryAccess("factory.access_check", func(f Factory, ctx context.Context, caller, link string) (FactoryAccess, error) {
		return f.FactoryAccessCheck(ctx, caller, link)
	}),
	"factory.access_sign_in": factoryAccess("factory.access_sign_in", func(f Factory, ctx context.Context, caller, link string) (FactoryAccess, error) {
		return f.FactoryAccessSignIn(ctx, caller, link)
	}),
	"factory.access_cancel": factoryAccess("factory.access_cancel", func(f Factory, ctx context.Context, caller, link string) (FactoryAccess, error) {
		return f.FactoryAccessCancel(ctx, caller, link)
	}),
	"factory.sign_in_start": typed(func(e Engine, p FactorySignInStartParams) (*FactorySignIn, error) {
		f, err := factoryOf(e, "factory.sign_in_start")
		if err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactorySignInStart(ctx, caller, id)
		if err != nil {
			return nil, err
		}
		return &out, nil
	}),
	"factory.sign_in_poll": typed(func(e Engine, p FactorySignInFlowParams) (*FactorySignInState, error) {
		f, err := factoryOf(e, "factory.sign_in_poll")
		if err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		flow, err := required("flow", p.Flow)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := f.FactorySignInPoll(ctx, caller, flow)
		if err != nil {
			return nil, err
		}
		return &out, nil
	}),
	"factory.sign_in_cancel": typed(func(e Engine, p FactorySignInFlowParams) (*Empty, error) {
		f, err := factoryOf(e, "factory.sign_in_cancel")
		if err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		flow, err := required("flow", p.Flow)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		return done(f.FactorySignInCancel(ctx, caller, flow))
	}),
}
