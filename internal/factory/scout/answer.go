package scout

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/reporead"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
)

// reply is the answer the scout's system prompt asks for.
type reply struct {
	Files []struct {
		Path string `json:"path"`
		Why  string `json:"why"`
	} `json:"files"`
	Plan     []string        `json:"plan"`
	Unknowns []string        `json:"unknowns"`
	Rubric   json.RawMessage `json:"rubric"`
	Tests    []string        `json:"tests"`
}

// finish checks the answer against the checkout and keeps what holds: each
// path that exists, each citation whose file exists, the rubric only whole.
func (r *run) finish(ctx context.Context, raw json.RawMessage) (factory.Scout, error) {
	var a reply
	if err := json.Unmarshal(raw, &a); err != nil {
		return r.out, ErrNoAnswer
	}
	out := &r.out
	for _, f := range a.Files {
		if path, ok := r.exists(ctx, f.Path); ok && len(out.Files) < FilesMost {
			out.Files = append(out.Files, factory.ScoutFile{Path: path, Why: words(f.Why, ScoutWhyWords)})
		}
	}
	for _, t := range a.Tests {
		if path, ok := r.exists(ctx, t); ok && len(out.Tests) < TestsMost {
			out.Tests = append(out.Tests, path)
		}
	}
	out.Plan = lines(a.Plan, PlanSteps)
	out.Unknowns = lines(a.Unknowns, UnknownsMost)
	if rub := triage.ParseRubric(a.Rubric, factory.RubricFromScout); rub != nil {
		r.cite(ctx, rub, a.Rubric)
		out.Rubric = rub
	}
	if out.Rubric == nil && len(out.Files) == 0 && len(out.Plan) == 0 {
		return r.out, ErrNoAnswer
	}
	return r.out, nil
}

// cite keeps each factor's `path:line` when its file exists.
func (r *run) cite(ctx context.Context, rub *factory.Rubric, raw json.RawMessage) {
	var cites map[string]struct {
		Cite string `json:"cite"`
	}
	_ = json.Unmarshal(raw, &cites)
	for i, f := range rub.Factors() {
		c := strings.TrimSpace(cites[factory.FactorNames[i]].Cite)
		if c == "" {
			continue
		}
		path, line, _ := strings.Cut(c, ":")
		if kept, ok := r.exists(ctx, path); ok {
			f.Cite = kept
			if line = strings.TrimSpace(line); line != "" {
				f.Cite += ":" + line
			}
		}
	}
}

// exists says a path the answer named is in the checkout and allowed,
// counting it as cited, and as dropped when it is not.
func (r *run) exists(ctx context.Context, path string) (string, bool) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return "", false
	}
	r.out.Cited++
	got, err := r.look(ctx, reporead.Request{Op: reporead.OpRead, Path: path, From: 1, To: 1})
	if errors.Is(err, reporead.ErrBinary) {
		err = nil
	}
	if err != nil && !errors.Is(err, reporead.ErrDenied) {
		// A folder is not a file to read: it exists when it lists.
		got, err = r.look(ctx, reporead.Request{Op: reporead.OpTree, Path: path})
		if err == nil && len(got.Entries) == 0 {
			err = reporead.NotFound(path)
		}
	}
	if err != nil {
		r.out.Dropped++
		return "", false
	}
	if r.out.Head == "" {
		r.out.Head = got.Commit
	}
	return path, true
}

func lines(list []string, most int) []string {
	var out []string
	for _, s := range list {
		if s = strings.Join(strings.Fields(s), " "); s != "" && len(out) < most {
			out = append(out, s)
		}
	}
	return out
}

func words(s string, most int) string {
	f := strings.Fields(s)
	return strings.Join(f[:min(len(f), most)], " ")
}
