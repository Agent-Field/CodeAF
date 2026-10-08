package run

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// NewPostExecutor runs a post stage: one write through the item's source.
//
// A POST IS NEVER RETRIED. A write that errored may still have landed, and a
// second call could post twice; the error goes back to the runner and a
// person decides. Policy is read first, by code, and a refusal is a result
// with its reason on the proof sheet, not an error.
func NewPostExecutor(source func(repo string) factory.Source, policy func(repo string) []string) Executor {
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

// branchFor is the branch write named in the item's stream log
// (`branch: <name>`), else factory/<digits>-<slug>.
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
	digits := strings.TrimPrefix(it.Ref(), "#")
	var slug []rune
	dash := false
	for _, r := range strings.ToLower(it.Title) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			slug = append(slug, r)
			dash = false
		} else if !dash && len(slug) > 0 {
			slug = append(slug, '-')
			dash = true
		}
		if len(slug) >= 40 {
			break
		}
	}
	s := strings.Trim(string(slug), "-")
	if s == "" {
		return "factory/" + digits
	}
	return "factory/" + digits + "-" + s
}
