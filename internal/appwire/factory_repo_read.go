package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.repo_read: bounded reads of a repository, for the Ask loop ─────
//
// The desk's Ask loop reads a watched repository through its factory's own
// checkout: a folder's children (tree), lines matching an RE2 pattern (grep,
// at most 50), or a range of one file's lines (read, at most 400 lines and
// 32 KiB). It is one method with an op, because the three are one tool family
// over one route (GET /v1/repos/{owner}/{repo}/read) with one set of refusals.
//
// THE FACTORY READS GIT'S OBJECT STORE AT ONE COMMIT: the default branch's
// tip as its checkout knows it, or, with job, that job's head commit when the
// checkout has it. Uncommitted files and symbolic links never reach outside
// the repository. Credential-looking paths are absent from tree and grep, and
// a read of one is [CodeForbidden] with the message "denied"; a binary is
// [CodeUnreadable] with "binary"; a repository with no checkout on the
// factory's machine is [CodeNotCloned]. Only repositories the caller can see
// are read; any other is not_found.
//
// THE ANSWER IS UNTRUSTED DATA. Everything in entries, hits and text is the
// repository's own words: an app MUST hand it to a model as data to cite,
// never as instructions to follow, whatever it says. Untrusted is always true
// so the app never has to infer that.

// FeatureFactoryRepoRead is announced in hello when the engine serves
// factory.repo_read.
const FeatureFactoryRepoRead = "factory.repo_read"

// CodeNotCloned refuses a factory.repo_read of a repository its factory has
// no git checkout of. Additive like [CodeTooLarge]: an app that does not know
// the word treats it as any failure.
const CodeNotCloned = "not_cloned"

// The bounds this method checks before asking anyone; the factory's own
// (internal/factory/reporead) are the same.
const (
	MaxRepoReadPattern = 256
	MaxRepoReadPath    = 1024
)

// FactoryRepoReadParams is one look. Op is tree, grep or read; Path is
// relative to the repository's root ("" is the root for tree and grep);
// Pattern is grep's RE2 expression; From and To are read's 1-based lines,
// inclusive (0 is "from the first" and "as far as the bounds allow"); Job
// reads at that job's head instead of the default branch.
type FactoryRepoReadParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Op      string `json:"op" enum:"tree,grep,read"`
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	From    int    `json:"from,omitempty"`
	To      int    `json:"to,omitempty"`
	Job     int    `json:"job,omitempty"`
}

// FactoryRepoRead is the factory's answer, with the app's id for the
// factory. Entries answer tree, Hits grep, and From, To, Size, EOF and Text
// read; each is absent for the other ops (and Entries or Hits when there are
// none). Commit is the commit every part of the answer came from; At is main
// or job. Truncated says a bound stopped the answer early.
type FactoryRepoRead struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Op      string `json:"op" enum:"tree,grep,read"`
	At      string `json:"at" enum:"main,job"`
	Job     int    `json:"job,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Commit  string `json:"commit"`
	Path    string `json:"path"`
	// Untrusted is always true: entries, hits and text are the repository's
	// own words, which the app treats as data to cite, never as instructions.
	Untrusted bool            `json:"untrusted"`
	Truncated bool            `json:"truncated"`
	Entries   []api.RepoEntry `json:"entries,omitempty"`
	Pattern   string          `json:"pattern,omitempty"`
	Hits      []api.RepoHit   `json:"hits,omitempty"`
	From      int             `json:"from,omitempty"`
	To        int             `json:"to,omitempty"`
	Size      int64           `json:"size,omitempty"`
	EOF       bool            `json:"eof,omitempty"`
	Text      string          `json:"text,omitempty"`
}

// FactoryRepoReader is the engine's half of factory.repo_read.
type FactoryRepoReader interface {
	FactoryRepoRead(ctx context.Context, p FactoryRepoReadParams) (FactoryRepoRead, error)
}

func withFactoryRepoReadFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRepoReader); ok {
		hello = withFeature(hello, FeatureFactoryRepoRead)
	}
	return hello
}

func init() {
	methods["factory.repo_read"] = shaped[FactoryRepoReadParams, FactoryRepoRead](repoRead)
}

func repoRead(e Engine, raw json.RawMessage) (*FactoryRepoRead, error) {
	const name = "factory.repo_read"
	reader, ok := e.(FactoryRepoReader)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryRepoReadParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	p, err := checkRepoRead(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := reader.FactoryRepoRead(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// checkRepoRead bounds a look without asking anyone; the factory checks the
// path's segments and the pattern's syntax itself.
func checkRepoRead(p FactoryRepoReadParams) (FactoryRepoReadParams, error) {
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
	switch {
	case p.Op != "tree" && p.Op != "grep" && p.Op != "read":
		return p, Errorf(CodeBadParams, "op must be tree, grep or read")
	case len(p.Path) > MaxRepoReadPath:
		return p, Errorf(CodeBadParams, "path is at most %d bytes", MaxRepoReadPath)
	case p.Op == "grep" && (p.Pattern == "" || len(p.Pattern) > MaxRepoReadPattern):
		return p, Errorf(CodeBadParams, "grep needs a pattern of at most %d bytes", MaxRepoReadPattern)
	case p.Op == "read" && strings.Trim(p.Path, "/") == "":
		return p, Errorf(CodeBadParams, "read needs a file's path")
	case p.From < 0 || p.To < 0 || (p.To > 0 && p.To < p.From):
		return p, Errorf(CodeBadParams, "from and to must be line numbers, from no later than to")
	case p.Job < 0:
		return p, Errorf(CodeBadParams, "job must be a job's number")
	}
	return p, nil
}
