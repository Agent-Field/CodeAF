package appwire

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// ── TURNING ONE REPOSITORY ON OR OFF ────────────────────────────────────────
//
// factory.repo_set turns ONE repository on or off in the factory's watched
// list, for the operator who holds the connection now. factory.repos (the read
// of the list and of what could be added) is unchanged and is how a caller
// learns the state after any outcome that was not a plain yes.
//
// THE PARAMS ARE EXACTLY {factory, repo, on}. There is no method that names a
// route, a verb or a body, no path to a folder, and no way to ask for anything
// but the one switch; an unknown field is bad_params and never believed to have
// been honoured. "on" has no default: a request that leaves it out is refused
// rather than read as "off".
//
// TURNING A REPOSITORY OFF STOPS WATCHING IT AND NOTHING ELSE. Its jobs, its
// checkouts and the folders on any machine are left as they are; no method here
// deletes, moves or cleans up anything, and nothing is written to GitHub.
//
// A WRITE IS SENT ONCE. It is never retried, replayed or tried by a second
// route when the first said no. An outcome the engine cannot confirm (a dropped
// connection, a timeout, a 5xx, a refusal that does not prove nothing was
// stored) is [CodeUncertain]: the repository may or may not have changed, and
// the answer claims neither. The receipt is answered only after the factory
// said it completed, and it repeats what was asked, never more.
//
// ALL OF THIS IS OPTIONAL AND DISTINCT FROM factory.admin AND factory.links. An
// engine that is not a [FactoryRepoAdmin] answers the method as unknown_method
// and does not announce [FeatureFactoryReposManage].

// FeatureFactoryReposManage is announced in hello when the engine serves
// factory.repo_set.
const FeatureFactoryReposManage = "factory.repos_manage"

// The longest an owner and a name may be (GitHub's own limits), so a request
// that cannot name a repository is never sent.
const (
	RepoOwnerMax = 39
	RepoNameMax  = 100
)

var (
	repoOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,39}$`)
	repoNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
)

// FactoryRepoSetParams is the whole request. On is a pointer so that an
// omitted value is refused instead of being taken as false.
type FactoryRepoSetParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	On      *bool  `json:"on"`
}

// FactoryRepoSetDone is what the factory confirmed: the same connection,
// repository and switch that were asked for. Read factory.repos for the list.
type FactoryRepoSetDone struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	On      bool   `json:"on"`
}

// FactoryRepoAdmin is the engine's half of factory.repo_set. It makes at most
// one write to the factory and never sends it twice; an outcome it cannot
// confirm is [CodeUncertain].
type FactoryRepoAdmin interface {
	FactoryRepoSet(ctx context.Context, p FactoryRepoSetParams) (FactoryRepoSetDone, error)
}

func withFactoryReposManageFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRepoAdmin); ok {
		hello = withFeature(hello, FeatureFactoryReposManage)
	}
	return hello
}

// CheckRepoSwitch bounds the request without asking anyone: a valid
// owner/name (path-safe characters, GitHub's lengths, neither part "." or
// "..") and an explicit on. It answers the trimmed repo.
func CheckRepoSwitch(p FactoryRepoSetParams) (FactoryRepoSetParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	owner, name, ok := strings.Cut(strings.TrimSpace(p.Repo), "/")
	if !ok || !repoOwnerPattern.MatchString(owner) || !repoNamePattern.MatchString(name) ||
		owner == "." || owner == ".." || name == "." || name == ".." {
		return p, Errorf(CodeBadParams, "repo must be owner/name")
	}
	p.Repo = owner + "/" + name
	if p.On == nil {
		return p, Errorf(CodeBadParams, "on must be true or false")
	}
	return p, nil
}

func init() {
	const name = "factory.repo_set"
	methods[name] = repoSetMethod(name)
	factoryRepoMethods[name] = methods[name]
}

// repoSetMethod reads params STRICTLY, checks them, then asks the engine once.
func repoSetMethod(name string) method {
	return shaped[FactoryRepoSetParams, FactoryRepoSetDone](func(e Engine, raw json.RawMessage) (*FactoryRepoSetDone, error) {
		a, ok := e.(FactoryRepoAdmin)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactoryRepoSetParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckRepoSwitch(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryRepoSet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// factoryRepoMethods is the table of factory.repo_set, for the tests that walk it.
var factoryRepoMethods = map[string]method{}
