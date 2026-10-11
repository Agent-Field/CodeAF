package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// maxFileBytes is how large a pipeline file may be. A recipe is a page, not a
// log; past this the read is refused rather than parsed.
const maxFileBytes = 256 << 10

// gitWait is how long one git read of the file may take.
const gitWait = 5 * time.Second

// PersonalPath is ~/.codeaf/factory.yaml for a home directory.
func PersonalPath(home string) string {
	return filepath.Join(home, ".codeaf", "factory.yaml")
}

// Load reads the pipeline of the checkout at dir.
//
// THE FILE IS READ FROM THE MAIN BRANCH, NEVER FROM A PULL REQUEST'S BRANCH,
// the same rule as factory.RecipeSource, so a pull request cannot change the
// rules for its own run. Yaml wins over the markdown recipe. When the branch
// has neither, the answer is [Defaults] even if the working tree has a file.
// Only a checkout with no branch to read uses the working tree.
func Load(dir string) (File, error) {
	text, from, kind, err := readCheckout(dir)
	if err != nil {
		f := Defaults()
		f.Source = SourceDefaults
		f.From = from
		f.Problems = []Problem{{Where: RepoFile, Why: err.Error()}}
		return f, err
	}
	switch kind {
	case "yaml":
		f, perr := Parse(text)
		f.From = from
		if perr != nil {
			f.Source = SourceDefaults
			return f, perr
		}
		f.Source = SourceFile
		return f, nil
	case "md":
		f := FromMarkdown(text)
		f.Source = SourceMarkdown
		f.From = from
		return f, nil
	default:
		f := Defaults()
		f.Source = SourceDefaults
		f.From = factory.FromDefault
		return f, nil
	}
}

// LoadMerged reads the checkout and tightens it with the personal file at
// path. A missing personal file is the checkout's file alone. A personal file
// that cannot be read leaves the checkout's file as it was.
func LoadMerged(dir, personal string) (File, error) {
	repo, err := Load(dir)
	if strings.TrimSpace(personal) == "" {
		return repo, err
	}
	pf, perr := readPersonal(personal)
	if perr != nil {
		if err == nil {
			err = perr
		}
		return repo, err
	}
	if pf == nil {
		return repo, err
	}
	merged, probs := Merge(repo, *pf)
	merged.Problems = append(append([]Problem{}, repo.Problems...), probs...)
	merged.Source = repo.Source
	merged.From = repo.From
	return merged, err
}

func readPersonal(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%s is too large", path)
	}
	if strings.ContainsRune(string(data), 0) {
		return nil, fmt.Errorf("%s is not text", path)
	}
	f, err := ParsePersonal(string(data))
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func readCheckout(dir string) (text, from, kind string, err error) {
	if strings.TrimSpace(dir) == "" {
		return "", "", "", errors.New("no checkout")
	}
	if ref := defaultBranch(dir); ref != "" {
		if t, ok, err := show(dir, ref, RepoFile); err != nil {
			return "", factory.FromMainBranch, "", err
		} else if ok {
			return t, factory.FromMainBranch, "yaml", nil
		}
		if t, ok, err := show(dir, ref, factory.RecipeFile); err != nil {
			return "", factory.FromMainBranch, "", err
		} else if ok {
			return t, factory.FromMainBranch, "md", nil
		}
		return "", factory.FromDefault, "", nil
	}
	if t, ok, err := readWorking(dir, RepoFile); err != nil {
		return "", factory.FromWorkingTree, "", err
	} else if ok {
		return t, factory.FromWorkingTree, "yaml", nil
	}
	if t, ok, err := readWorking(dir, factory.RecipeFile); err != nil {
		return "", factory.FromWorkingTree, "", err
	} else if ok {
		return t, factory.FromWorkingTree, "md", nil
	}
	return "", factory.FromDefault, "", nil
}

func show(dir, ref, rel string) (string, bool, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return "", false, err
	}
	spec := ref + ":" + rel
	if _, err := gitOut(dir, "cat-file", "-e", spec); err != nil {
		return "", false, nil
	}
	out, err := gitOut(dir, "show", spec)
	if err != nil {
		return "", false, err
	}
	if len(out) > maxFileBytes {
		return "", false, fmt.Errorf("%s is too large", rel)
	}
	if strings.ContainsRune(out, 0) {
		return "", false, fmt.Errorf("%s is not text", rel)
	}
	return out, true, nil
}

func readWorking(dir, rel string) (string, bool, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(data) > maxFileBytes {
		return "", false, fmt.Errorf("%s is too large", rel)
	}
	if strings.ContainsRune(string(data), 0) {
		return "", false, fmt.Errorf("%s is not text", rel)
	}
	return string(data), true, nil
}

func cleanRel(rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return "", errors.New("not a file inside the repository")
	}
	return rel, nil
}

// defaultBranch is the checkout's main branch as a ref git can read, or ""
// when there is none: origin/HEAD's target, else main, else master.
func defaultBranch(dir string) string {
	var tries []string
	if out, err := gitOut(dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			tries = append(tries, ref)
		}
	}
	tries = append(tries, "main", "master")
	for _, ref := range tries {
		if _, err := gitOut(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return ref
		}
	}
	return ""
}

func gitOut(dir string, args ...string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("no checkout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") || strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	out, err := cmd.Output()
	return string(out), err
}

// Merge tightens repo with personal. A personal key that would let more work
// start, or more of a step run, is ignored and named in the problems. The
// repo file is not changed.
func Merge(repo, personal File) (File, []Problem) {
	out := cloneFile(repo)
	var probs []Problem
	if personal.set.labels {
		out.Start.Labels, probs = tightenLabels(repo.Start.Labels, personal.Start.Labels, probs)
	}
	if personal.set.maxSize {
		out.Start.MaxSize, probs = tightenSize(repo.Start.MaxSize, personal.Start.MaxSize, probs)
	}
	if personal.set.maxCost {
		out.Start.MaxCost, probs = tightenCost(repo.Start.MaxCost, personal.Start.MaxCost, probs)
	}
	if personal.set.never {
		out.Start.Never = unionExact(out.Start.Never, personal.Start.Never)
	}
	if personal.set.askFirst {
		out.Start.AskFirst = unionFold(out.Start.AskFirst, personal.Start.AskFirst)
	}
	if personal.set.prLines {
		out.Size.PRLines, probs = tightenLines(repo.Size.PRLines, personal.Size.PRLines, probs)
	}
	if personal.set.issue && !strings.EqualFold(strings.TrimSpace(personal.Size.Issue), strings.TrimSpace(repo.Size.Issue)) {
		probs = append(probs, Problem{Where: "size.issue", Why: "ignored; a personal file cannot change how issue size is read"})
	}
	fixed := unionExact(repo.Fixed, nil)
	if personal.set.fixed {
		if missingAny(repo.Fixed, personal.Fixed) {
			probs = append(probs, Problem{Where: "fixed", Why: "kept the repo's fixed steps; a personal file cannot unfix one"})
		}
		for _, name := range personal.Fixed {
			if !knownStep(repo, name) {
				probs = append(probs, Problem{Where: "fixed", Why: name + " is ignored; a personal file cannot add a step"})
				continue
			}
			if !listed(fixed, name) {
				fixed = append(fixed, name)
			}
		}
		out.Fixed = append([]string(nil), fixed...)
	}
	for kind := range personal.set.pipeline {
		next, why := tightenPipeline(repo.Pipeline[kind], personal.Pipeline[kind], fixed)
		if why != "" {
			probs = append(probs, Problem{Where: "pipeline." + string(kind), Why: why})
			continue
		}
		if out.Pipeline == nil {
			out.Pipeline = map[factory.Kind][]string{}
		}
		out.Pipeline[kind] = next
	}
	if personal.set.rules {
		if missingFold(repo.Rules, personal.Rules) {
			probs = append(probs, Problem{Where: "rules", Why: "kept the repo's rules; a personal file cannot drop one"})
		}
		out.Rules = unionFold(out.Rules, personal.Rules)
	}
	for name, step := range personal.Steps {
		probs = tightenStep(&out, repo, name, step, probs)
	}
	// Parse problems from the personal file belong with the refusals, or a
	// key it could not read disappears while the rest of the file tightens.
	probs = append(append([]Problem{}, personal.Problems...), probs...)
	out.Problems = append(out.Problems, probs...)
	return out, probs
}

func tightenLabels(repo, personal []string, probs []Problem) ([]string, []Problem) {
	var keep []string
	for _, label := range repo {
		if hasFold(personal, label) {
			keep = append(keep, label)
		}
	}
	if keep == nil {
		keep = []string{}
	}
	for _, label := range personal {
		if !hasFold(repo, label) {
			probs = append(probs, Problem{Where: "start.labels", Why: label + " is ignored; a personal file cannot add a label"})
		}
	}
	return keep, probs
}

func tightenSize(repo, personal string, probs []Problem) (string, []Problem) {
	ps, okP := normSize(personal)
	rs, okR := normSize(repo)
	if !okR {
		rs = DefaultMaxSize
	}
	if !okP {
		return rs, append(probs, Problem{Where: "start.max_size", Why: "ignored; size is S, M or L"})
	}
	if sizeRank(ps) > sizeRank(rs) {
		return rs, append(probs, Problem{Where: "start.max_size", Why: ps + " is looser than " + rs + "; a personal file may only be stricter"})
	}
	return ps, probs
}

func tightenCost(repo, personal float64, probs []Problem) (float64, []Problem) {
	if math.IsNaN(personal) || math.IsInf(personal, 0) || personal < 0 {
		return repo, append(probs, Problem{Where: "start.max_cost", Why: "ignored; cost is a number of dollars, zero or more"})
	}
	if personal > repo {
		return repo, append(probs, Problem{Where: "start.max_cost", Why: money(personal) + " is looser than " + money(repo) + "; a personal file may only be stricter"})
	}
	return personal, probs
}

func tightenLines(repo, personal [2]int, probs []Problem) ([2]int, []Problem) {
	if personal[0] < 0 || personal[1] < personal[0] || personal[0] > repo[0] || personal[1] > repo[1] {
		return repo, append(probs, Problem{Where: "size.pr_lines", Why: "ignored; a personal file may only lower the bounds"})
	}
	return personal, probs
}

func tightenPipeline(base, want, fixed []string) ([]string, string) {
	if base == nil {
		base = []string{}
	}
	seen := map[string]bool{}
	var seq []string
	for _, name := range want {
		if seen[name] {
			return nil, name + " is named twice"
		}
		seen[name] = true
		if name != factory.ApproveName && !listed(base, name) {
			return nil, "a personal file cannot add " + name
		}
		if name != factory.ApproveName {
			seq = append(seq, name)
		}
	}
	if !subsequence(base, seq) {
		return nil, "a personal file keeps the repo's step order"
	}
	for _, name := range fixed {
		if listed(base, name) && !seen[name] {
			return nil, name + " is fixed and a personal file cannot drop it"
		}
	}
	next := append([]string(nil), want...)
	if next == nil {
		next = []string{}
	}
	return next, ""
}

func tightenStep(out *File, repo File, name string, step Step, probs []Problem) []Problem {
	cur, ok := repo.Steps[name]
	if !ok {
		return append(probs, Problem{Where: "steps." + name, Why: "a personal file cannot add a step"})
	}
	if step.wrotePrompt && strings.TrimSpace(step.Prompt) != strings.TrimSpace(cur.Prompt) {
		probs = append(probs, Problem{Where: "steps." + name, Why: "a personal file cannot change the prompt"})
	}
	if step.wroteUntil {
		next, ok := untilTighten(cur.Until, step.Until)
		if !ok {
			probs = append(probs, Problem{Where: "steps." + name + ".until", Why: "a personal file cannot change this until"})
		} else {
			cur.Until = next
		}
	}
	if step.wroteMax {
		if rounds(step.Max) > rounds(cur.Max) {
			probs = append(probs, Problem{Where: "steps." + name + ".max", Why: "a personal file cannot raise the rounds"})
		} else if rounds(step.Max) < rounds(cur.Max) {
			cur.Max = step.Max
			cur.wroteMax = true
		}
	}
	if out.Steps == nil {
		out.Steps = map[string]Step{}
	}
	out.Steps[name] = cur
	return probs
}

func untilTighten(repo, personal string) (string, bool) {
	repo = strings.TrimSpace(repo)
	personal = strings.TrimSpace(personal)
	if personal == "" || strings.EqualFold(personal, repo) {
		return repo, true
	}
	switch strings.ToLower(repo) {
	case "", "done", "the step says it is done":
		return personal, true
	default:
		return repo, false
	}
}

func rounds(max int) int {
	if max < 1 {
		return 1
	}
	return max
}

func knownStep(f File, name string) bool {
	if _, ok := f.Steps[name]; ok {
		return true
	}
	if factory.ApproveWord(name) {
		return true
	}
	for _, names := range f.Pipeline {
		if listed(names, name) {
			return true
		}
	}
	return false
}

func subsequence(base, seq []string) bool {
	i := 0
	for _, name := range base {
		if i < len(seq) && name == seq[i] {
			i++
		}
	}
	return i == len(seq)
}

func missingAny(repo, personal []string) bool {
	for _, name := range repo {
		if !listed(personal, name) {
			return true
		}
	}
	return false
}

func missingFold(repo, personal []string) bool {
	for _, rule := range repo {
		if !hasFold(personal, rule) {
			return true
		}
	}
	return false
}

func unionExact(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, s := range b {
		if !listed(out, s) {
			out = append(out, s)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func unionFold(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, s := range b {
		if !hasFold(out, s) {
			out = append(out, s)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func hasFold(list []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, s := range list {
		if strings.ToLower(strings.TrimSpace(s)) == want {
			return true
		}
	}
	return false
}

func cloneFile(f File) File {
	out := f
	out.markdownAdapt = copyAdapt(f.markdownAdapt)
	out.Start.Labels = append([]string(nil), f.Start.Labels...)
	out.Start.Never = append([]string(nil), f.Start.Never...)
	out.Start.AskFirst = append([]string(nil), f.Start.AskFirst...)
	out.Fixed = append([]string(nil), f.Fixed...)
	out.Rules = append([]string(nil), f.Rules...)
	out.Problems = append([]Problem(nil), f.Problems...)
	out.Pipeline = map[factory.Kind][]string{}
	for kind, names := range f.Pipeline {
		out.Pipeline[kind] = append([]string(nil), names...)
	}
	out.Steps = map[string]Step{}
	for name, step := range f.Steps {
		out.Steps[name] = step
	}
	out.set.pipeline = map[factory.Kind]bool{}
	for kind, on := range f.set.pipeline {
		out.set.pipeline[kind] = on
	}
	return out
}
