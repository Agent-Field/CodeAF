package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// PostOptions builds a post executor.
type PostOptions struct {
	// Source answers the item's source, nil when none.
	Source func(repo string) factory.Source
	// Policy answers the repo's recipe policy lines.
	Policy func(repo string) []string
	// Push publishes the item's branch from its folder before a `pr` is
	// opened from it; the wire hands [GitPush]. Nil pushes nothing, and the
	// pull request is opened from whatever the remote already has.
	Push func(dir, branch string) error
}

// NewPostExecutor runs a post stage: one write through the item's source.
func NewPostExecutor(source func(repo string) factory.Source, policy func(repo string) []string) Executor {
	return NewPostExecutorWith(PostOptions{Source: source, Policy: policy})
}

// NewPostExecutorWith runs a post stage: one write through the item's source.
//
// A POST IS NEVER RETRIED. A write that errored may still have landed, and a
// second call could post twice; the error goes back to the runner and a
// person decides. Policy is read first, by code, and a refusal is a result
// with its reason on the proof sheet, not an error.
//
// A PR PUSHES ITS BRANCH FIRST, and never with force: the branch is the one
// the item's worktree was made on (its `branch:` log line), pushed with
// `git push -u origin <branch>` from the round's folder, and only then is the
// pull request opened from it. A push that fails is an error like a write
// that fails, with git's own last line.
func NewPostExecutorWith(o PostOptions) Executor {
	source, policy := o.Source, o.Policy
	return ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if err := ctx.Err(); err != nil {
			return factory.StageResult{}, err
		}
		it := job.Item
		verb, rest := splitAsk(job.Stage.Ask)
		action := factory.Action{Verb: verb, Item: it}
		switch verb {
		case "comment":
			action.Body = rest
			if action.Body == "" {
				action.Body = defaultBody(it, job.Prior)
			}
		case "label":
			action.Labels = splitList(rest)
		case "pr":
			action.Draft = strings.EqualFold(strings.TrimSpace(rest), "draft")
			action.Branch = branchFor(it)
			action.Body = proofList(it, job.Prior) + "\n\nby codeaf's factory floor"
		case "close":
		default:
			return factory.StageResult{}, fmt.Errorf("a post stage says comment, label, pr or close")
		}

		var src factory.Source
		if source != nil {
			src = source(it.Repo)
		}
		if src == nil {
			return factory.StageResult{}, fmt.Errorf("%s is not on a source codeaf can post to", it.Ref())
		}
		if policy != nil {
			if ok, why := PolicyAllows(policy(it.Repo), it, action, job.Prior); !ok {
				return factory.StageResult{
					Output: "post refused: " + why,
					Claims: []factory.Claim{{Text: verb + " refused by policy", OK: false, Evidence: why, Medium: "policy"}},
				}, nil
			}
		}
		if verb == "pr" && o.Push != nil {
			if err := push(o.Push, job.Dir, it, action.Branch); err != nil {
				return factory.StageResult{}, err
			}
		}
		rcpt, err := src.Write(ctx, action)
		if err != nil {
			return factory.StageResult{}, err
		}
		if job.Log != nil {
			job.Log("posted " + verb + " · " + rcpt.URL)
		}
		return factory.StageResult{
			Done:   true,
			Output: "posted " + verb + " · " + rcpt.URL,
			Claims: []factory.Claim{{Text: "posted " + verb, OK: true, Evidence: rcpt.URL, Medium: "transcript"}},
		}, nil
	})
}

// splitAsk reads the ask's verb and the words after it; `comment: text` and
// `comment text` both work, and `pull request` is read as pr.
func splitAsk(ask string) (verb, rest string) {
	ask = strings.TrimSpace(ask)
	i := strings.IndexFunc(ask, func(r rune) bool { return unicode.IsSpace(r) || r == ':' })
	if i < 0 {
		return strings.ToLower(ask), ""
	}
	verb, rest = strings.ToLower(ask[:i]), strings.TrimSpace(strings.TrimLeft(ask[i:], ": \t"))
	if verb == "open-pr" {
		verb = "pr"
	}
	return verb, rest
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// defaultBody is the last prior stage's output (2k chars), else the proof.
func defaultBody(it factory.Item, prior []factory.StageResult) string {
	if n := len(prior); n > 0 {
		if out := strings.TrimSpace(prior[n-1].Output); out != "" {
			r := []rune(out)
			if len(r) > 2000 {
				r = r[:2000]
			}
			return string(r)
		}
	}
	return proofList(it, prior)
}

// proofList renders the item's proof claims, and any claims the prior stages
// made, as `✓ … / ✕ …` lines.
func proofList(it factory.Item, prior []factory.StageResult) string {
	claims := append([]factory.Claim(nil), it.Proof...)
	for _, r := range prior {
		claims = append(claims, r.Claims...)
	}
	var lines []string
	for _, c := range claims {
		mark := "✕"
		if c.OK {
			mark = "✓"
		}
		lines = append(lines, mark+" "+c.Text)
	}
	return strings.Join(lines, "\n")
}

// push runs the push and says what went wrong in a sentence.
func push(run func(dir, branch string) error, dir string, it factory.Item, branch string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("codeaf does not know where %s is checked out", strings.TrimSpace(it.Repo))
	}
	err := run(dir, branch)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoRemote):
		return fmt.Errorf("%s has no remote to push to", it.Ref())
	}
	return fmt.Errorf("could not push %s: %s", branch, lastLine(err.Error()))
}

// branchFor is the branch the item's worktree was made on, as its stream log
// names it (`branch: <name>`, written once when the worktree is made), else
// the name the worktree would have: factory/<digits>-<slug>.
func branchFor(it factory.Item) string {
	if it.Stream != nil {
		for i := len(it.Stream.Log) - 1; i >= 0; i-- {
			t := strings.TrimSpace(it.Stream.Log[i].Text)
			if rest, ok := strings.CutPrefix(t, "branch:"); ok {
				if b := strings.TrimSpace(rest); b != "" {
					return b
				}
			}
		}
	}
	return itemBranch(it)
}
