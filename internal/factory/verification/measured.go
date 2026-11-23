package verification

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/gitpolicy"
)

// THE FACTORY'S OWN BASE COMPARISON. A step is asked to show that a failing
// test already fails on the job's base ([BaselineMatches]), and a step that
// does it another way, or not at all, leaves a red check nobody can clear:
// the job stops and asks its person about a failure the change did not cause.
// So when a round ends with failing Go tests, the factory runs each one
// itself, on the job checkout and on a clean worktree at the base, and
// compares which tests fail. Nothing here reads a step's words or its output.

// Measured outcomes.
const (
	// MeasuredOnBase: every test that fails on the checkout fails on the base.
	MeasuredOnBase = "on_base"
	// MeasuredPassed: the check passes on the checkout as it is now. The red
	// receipt was of another tree (a step's own run with its change stashed).
	MeasuredPassed = "passed"
	// MeasuredNew: at least one test fails on the checkout and not on the base.
	MeasuredNew = "new_failures"
	// MeasuredUnknown: one of the runs could not be read as test results.
	MeasuredUnknown = "unknown"
)

// Measured is one failing check as the factory measured it. It holds for the
// exact checkout (At) and base it was measured on, and for nothing else.
type Measured struct {
	Key     string    `json:"key"`
	At      Snapshot  `json:"at"`
	Base    string    `json:"base"`
	Outcome string    `json:"outcome"`
	Failed  []string  `json:"failed,omitempty"`
	New     []string  `json:"new,omitempty"`
	ID      string    `json:"id"`
	When    time.Time `json:"when"`
}

// Clears says whether this measurement accounts for the check's failure on
// the checkout at and base given.
func (m Measured) Clears(at Snapshot, base string) bool {
	return at.Head != "" && m.At == at && m.Base == base && (m.Outcome == MeasuredOnBase || m.Outcome == MeasuredPassed)
}

func measuredPath(transcript string) string {
	return filepath.Join(transcript+".checks", "base-measured.json")
}

// ReadMeasured is every measurement kept beside a step's transcript, by
// [Check.Key]. A missing or unreadable file is no measurements.
func ReadMeasured(transcript string) map[string]Measured {
	raw, err := os.ReadFile(measuredPath(transcript))
	if err != nil {
		return nil
	}
	var out map[string]Measured
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// GoTestRun runs one `go test` argv in dir with env added to the process's
// own, and answers its standard output and exit status.
type GoTestRun func(ctx context.Context, dir string, env, argv []string) (output string, exit int, err error)

// measureRunLimit bounds one test run the factory makes for a comparison.
const measureRunLimit = 20 * time.Minute

// MeasureBase compares every failing Go test check in latest with the job's
// base and keeps what it finds beside the transcript. It answers how many
// checks the comparison cleared. dir is the job checkout, at its snapshot as
// the caller read it; a checkout that moves while this runs clears nothing.
// run is the test runner, nil for the real one.
func MeasureBase(ctx context.Context, transcript, dir string, latest map[string]Check, at Snapshot, base string, run GoTestRun) (int, error) {
	if base == "" || at.Head == "" {
		return 0, nil
	}
	if run == nil {
		run = runGoTest
	}
	root, err := canonicalOwner(dir)
	if err != nil {
		return 0, err
	}
	kept := ReadMeasured(transcript)
	if kept == nil {
		kept = map[string]Measured{}
	}
	keys := make([]string, 0, len(latest))
	for key, check := range latest {
		if m, ok := kept[key]; ok && m.At == at && m.Base == base {
			continue
		}
		if measurable(check, root) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return cleared(kept, latest, at, base), nil
	}
	baseTree := ""
	defer func() {
		if baseTree != "" {
			removeBaseTree(root, baseTree)
		}
	}()
	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}
		check := latest[key]
		relative, _ := filepath.Rel(root, canonicalDir(check.Cwd))
		argv := measureArgs(check.Args)
		env := checkContextEnv(check.Context)
		m := Measured{Key: key, At: at, Base: base, Outcome: MeasuredUnknown, When: time.Now().UTC()}
		m.ID = fmt.Sprintf("factory-base-%x", sha256.Sum256([]byte(key+"\x00"+at.Head+"\x00"+at.Fingerprint+"\x00"+base)))[:44]

		mine, exit, ok := measureRun(ctx, run, filepath.Join(root, relative), root, env, argv)
		switch {
		case !ok:
		case exit == 0 && len(mine) == 0:
			m.Outcome = MeasuredPassed
		case exit != 0 && len(mine) > 0:
			if baseTree == "" {
				if baseTree, err = addBaseTree(ctx, root, base); err != nil {
					return cleared(kept, latest, at, base), err
				}
			}
			theirs, baseExit, ok := measureRun(ctx, run, filepath.Join(baseTree, relative), baseTree, env, argv)
			if !ok {
				break
			}
			m.Failed = mine
			for _, name := range mine {
				if baseExit == 0 || !slices.Contains(theirs, name) {
					m.New = append(m.New, name)
				}
			}
			m.Outcome = MeasuredOnBase
			if len(m.New) > 0 {
				m.Outcome = MeasuredNew
			}
		}
		if ctx.Err() != nil {
			// A cut run measured nothing; the next round may measure again.
			break
		}
		kept[key] = m
	}
	// The comparison ran the tests in the job checkout. A checkout it left
	// different is not the one it measured.
	if after, err := SnapshotAt(dir); err != nil || after != at {
		return 0, errors.New("the checkout changed while the factory compared it with the base")
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(transcript+".checks", 0700); err != nil {
		return 0, err
	}
	if err := atomicWrite(measuredPath(transcript), raw); err != nil {
		return 0, err
	}
	return cleared(kept, latest, at, base), nil
}

func cleared(kept map[string]Measured, latest map[string]Check, at Snapshot, base string) int {
	n := 0
	for key := range latest {
		if m, ok := kept[key]; ok && m.Clears(at, base) {
			n++
		}
	}
	return n
}

// measurable is a failed `go test` the factory can run again by itself: its
// own argv, inside the job checkout, with nothing in it that selects other
// code or another program.
func measurable(check Check, root string) bool {
	if check.Diagnostic || check.Problem != "" || check.Exit == nil || *check.Exit == 0 || !measurableGoArgs(check.Args) {
		return false
	}
	if len(check.Context) != len(checkContextNames) {
		return false
	}
	for i, name := range checkContextNames {
		if value := check.Context[i]; name == "GOFLAGS" && !plainGoFlags(value) || name == "GOWORK" && value != "" && value != "off" {
			return false
		}
	}
	if !filepath.IsAbs(check.Cwd) {
		return false
	}
	relative, err := filepath.Rel(root, canonicalDir(check.Cwd))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// plainGoFlags admits a GOFLAGS that only sets how the same code is built.
// Both runs get it. A flag that can bring in other code (an overlay, another
// module file, another directory) would make the base run not the base.
func plainGoFlags(value string) bool {
	for _, flag := range strings.Fields(value) {
		name, _, _ := strings.Cut(flag, "=")
		switch name {
		case "-mod", "-trimpath", "-buildvcs", "-tags", "-race", "-p", "-count", "-v", "-short":
		default:
			return false
		}
	}
	return true
}

func canonicalDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return filepath.Clean(dir)
}

// measurableGoArgs admits `go test` with packages under the directory and
// the flags that only choose or pace tests. Anything else stays unmeasured.
func measurableGoArgs(args []string) bool {
	if len(args) < 3 || args[0] != "go" || args[1] != "test" {
		return false
	}
	packages := 0
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			if arg != "." && (!strings.HasPrefix(arg, "./") || slices.Contains(strings.Split(arg, "/"), "..") || strings.HasSuffix(arg, ".go")) {
				return false
			}
			packages++
			continue
		}
		flag, value, hasValue := strings.Cut(arg, "=")
		switch flag {
		case "-json", "-race", "-short", "-v", "-failfast":
			if hasValue && value != "true" && value != "false" {
				return false
			}
		case "-count", "-run", "-skip", "-timeout", "-p", "-parallel", "-tags":
			if !hasValue {
				i++
				if i == len(args) {
					return false
				}
				value = args[i]
			}
			if value == "" {
				return false
			}
		default:
			return false
		}
	}
	return packages > 0
}

// measureArgs is the check's argv as the factory runs it: the same packages
// and selection, as a JSON stream, run once and never from the test cache.
func measureArgs(args []string) []string {
	out := []string{"go", "test", "-json", "-count=1"}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		flag, _, hasValue := strings.Cut(arg, "=")
		switch flag {
		case "-json", "-v", "-failfast":
			continue
		case "-count":
			if !hasValue {
				i++
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}

func checkContextEnv(context []string) []string {
	var env []string
	for i, name := range checkContextNames {
		if i < len(context) && context[i] != "" {
			env = append(env, name+"="+context[i])
		}
	}
	return env
}

// measureRun is one run read as test results: the sorted names of the tests
// that failed, the exit, and whether the stream could be read at all.
func measureRun(ctx context.Context, run GoTestRun, dir, root string, env, argv []string) ([]string, int, bool) {
	runCtx, cancel := context.WithTimeout(ctx, measureRunLimit)
	defer cancel()
	output, exit, err := run(runCtx, dir, env, argv)
	if err != nil || runCtx.Err() != nil || exit < 0 || exit > 1 {
		return nil, exit, false
	}
	failed := goJSONFailures(output, root)
	if failed == nil {
		return nil, exit, false
	}
	names := make([]string, 0, len(failed))
	for id := range failed {
		names = append(names, id.pkg+"."+id.name)
	}
	sort.Strings(names)
	if (exit == 0) != (len(names) == 0) {
		return nil, exit, false
	}
	return names, exit, true
}

func runGoTest(ctx context.Context, dir string, env, argv []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	boundSnapshot(cmd)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0, nil
	case errors.As(err, &exit):
		return string(out), exit.ExitCode(), nil
	}
	return "", -1, err
}

// addBaseTree makes a clean detached worktree of the job's repository at
// base, outside the job checkout, and answers its path.
func addBaseTree(ctx context.Context, root, base string) (string, error) {
	parent, err := os.MkdirTemp("", "codeaf-base-")
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolved
	}
	tree := filepath.Join(parent, "tree")
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", gitpolicy.WithoutAutoMaintenance("worktree", "add", "--detach", "--quiet", tree, base)...)
	cmd.Dir = root
	boundSnapshot(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(parent)
		return "", fmt.Errorf("could not check out the base to compare with: %s", strings.TrimSpace(string(out)))
	}
	return tree, nil
}

func removeBaseTree(root, tree string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", gitpolicy.WithoutAutoMaintenance("worktree", "remove", "--force", tree)...)
	cmd.Dir = root
	boundSnapshot(cmd)
	_ = cmd.Run()
	_ = os.RemoveAll(filepath.Dir(tree))
	prune := exec.CommandContext(ctx, "git", gitpolicy.WithoutAutoMaintenance("worktree", "prune")...)
	prune.Dir = root
	boundSnapshot(prune)
	_ = prune.Run()
}
