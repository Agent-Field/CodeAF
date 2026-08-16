package codeaf

// The delta, not the absolute state.
//
// Full project verification asks the repository's own entrypoints whether they
// exit zero, and for most of this engine's life that answer was taken as the
// verdict on the change. It is not the same question. A repository can arrive
// already red — a test that asserts a permission error and cannot fail when the
// process runs as root, a flaky integration suite, a dependency that moved —
// and a gate reading the absolute state attributes every one of those to the
// patch in front of it. That is not a hypothetical: a correct one-line fix to
// spf13/cobra was thrown away on `make all exited 2`, where the 2 came from
// `TestFailGenFishCompletionFile`, which had been failing before the harness
// ever opened the directory.
//
// So the workspace is photographed while it is still pristine — every
// discovered entrypoint run once, its exit status and the NAMES of the tests it
// reported failing written down — and at verification time the failures are
// subtracted. Red before and red after is the repository's problem. Red only
// after is the change's, and it still fails, which is the half of this that
// must not bend: a patch that turns something new red is exactly as unshippable
// as it was yesterday.
//
// Nothing here knows what language the workspace is in. It does not run the
// tests — the engine already knows how to do that, and this reuses that path
// verbatim — it only reads the runners' own failure vocabulary, which is small,
// stable, and shared across every ecosystem this engine has met.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/fullverification"
)

// baselineEntry is one entrypoint as it behaved before the run touched
// anything: how it exited, and which tests it named as failing. The names are
// the whole of the identification — an acquittal is only ever granted for a
// failure that can be named on both sides.
type baselineEntry struct {
	Command  string   `json:"command"`
	Kind     string   `json:"kind"`
	Workdir  string   `json:"workdir,omitempty"`
	Exit     int      `json:"exit"`
	TimedOut bool     `json:"timed_out,omitempty"`
	Failing  []string `json:"failing,omitempty"`
}

func (entry baselineEntry) red() bool { return entry.Exit != 0 || entry.TimedOut }

// baselineRecord is the whole photograph, keyed exactly as the verification
// memo keys its entrypoints so the two sides cannot drift apart.
type baselineRecord struct {
	BaseSHA string                   `json:"base_sha"`
	Entries map[string]baselineEntry `json:"entries"`
}

// baselineFile is where the photograph is kept. It lives beside the resume
// checkpoint, under .codeaf, which the workspace already excludes from git: a
// resumed run must judge its delta against the tree as it was BEFORE the first
// attempt, and a baseline recaptured after the first attempt has written code
// would photograph the change and then acquit it of itself.
const baselineFile = "baseline-verification.json"

func baselinePath(workspace string) string {
	return filepath.Join(workspace, ".codeaf", baselineFile)
}

func loadBaseline(workspace string) (*baselineRecord, bool) {
	raw, err := os.ReadFile(baselinePath(workspace))
	if err != nil {
		return nil, false
	}
	var record baselineRecord
	if json.Unmarshal(raw, &record) != nil || record.Entries == nil {
		return nil, false
	}
	return &record, true
}

func saveBaseline(workspace string, record *baselineRecord) {
	path := baselinePath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}

// establishBaseline photographs the workspace, or restores the photograph a
// previous attempt took.
//
// It runs after the intake validity gate and before a single line of code is
// written, which is the one window where the tree is both real (the repository
// as the user handed it over) and untouched. A refused goal never pays for it;
// a resumed run never retakes it.
func (runner *pipeline) establishBaseline(ctx context.Context, resume bool) {
	if os.Getenv("CODEAF_BASELINE") == "0" {
		return
	}
	if restored, ok := loadBaseline(runner.workspace); ok {
		runner.baseline = restored
		runner.note("[codeaf] baseline: restored the pre-run verification photograph " +
			"(" + strconv.Itoa(len(restored.Entries)) + " entrypoints)\n")
		runner.events.stage("baseline", "restored", map[string]any{
			"entrypoints": len(restored.Entries), "red": restored.redCount(),
		})
		return
	}
	if resume {
		// The tree already carries the previous attempt's work. Photographing
		// it now would record the change as the baseline and then find no
		// delta against it, which is the one failure mode this file exists to
		// prevent.
		runner.note("[codeaf] baseline: resuming with no recorded pre-run photograph — " +
			"the absolute suite state stands in\n")
		return
	}
	runner.captureBaseline(ctx)
}

// captureBaseline runs each discovered entrypoint once against the pristine
// tree. It costs one suite run, spent where it buys the most: without it every
// later verdict in this run is about the repository rather than about the work.
func (runner *pipeline) captureBaseline(ctx context.Context) {
	plan := fullverification.Discover(runner.workspace)
	record := &baselineRecord{
		BaseSHA: gitOutput(ctx, runner.workspace, "rev-parse", "HEAD"),
		Entries: map[string]baselineEntry{},
	}
	commands := []any{}
	for _, entrypoint := range plan.Entrypoints {
		exit, timedOut, output := runner.executeEntrypoint(ctx, entrypoint)
		entry := baselineEntry{
			Command: entrypoint.Command, Kind: string(entrypoint.Kind),
			Workdir: entrypoint.Workdir, Exit: exit, TimedOut: timedOut,
		}
		if entry.red() && !timedOut {
			entry.Failing = failingTestNames(output)
		}
		record.Entries[verificationMemoKey(entrypoint)] = entry
		commands = append(commands, map[string]any{
			"cmd": entrypoint.Command, "exit": float64(exit),
			"kind": string(entrypoint.Kind), "failing": entry.Failing,
		})
		state := "green"
		if timedOut {
			state = "hung"
		} else if exit != 0 {
			state = "ALREADY RED before this run: " + describeFailing(entry.Failing)
		}
		runner.note("[codeaf] baseline " + string(entrypoint.Kind) + ": " +
			entrypoint.Command + " — " + state + "\n")
	}
	runner.baseline = record
	saveBaseline(runner.workspace, record)
	status := "green"
	if record.redCount() > 0 {
		status = "red"
	}
	runner.events.stage("baseline", status, map[string]any{
		"commands": commands, "entrypoints": len(record.Entries), "red": record.redCount(),
	})
}

func (record *baselineRecord) redCount() int {
	if record == nil {
		return 0
	}
	count := 0
	for _, entry := range record.Entries {
		if entry.red() {
			count++
		}
	}
	return count
}

// baselineDelta is the verdict on one failing entrypoint: whose failure is it.
type baselineDelta struct {
	// PreExisting says every failure this entrypoint reports was already
	// reported before the run began. It is the acquittal, and it is the only
	// field that suppresses a blocker.
	PreExisting bool
	// Known and New split the failing tests. New is never empty when
	// PreExisting is false and names were readable, and it is what the blocker
	// is worded from — a repair hint pointing at the tests the change broke is
	// worth more than one pointing at the exit code.
	Known []string
	New   []string
	// Note is the sentence written into the auditor's prompt and into the
	// leaf's evidence. It is prose because both readers are models.
	Note string
}

// judge subtracts the baseline from one failing entrypoint's output.
func (record *baselineRecord) judge(
	entrypoint fullverification.Entrypoint, exit int, output string,
) baselineDelta {
	if record == nil {
		return baselineDelta{}
	}
	entry, ok := record.Entries[verificationMemoKey(entrypoint)]
	if !ok || !entry.red() || entry.TimedOut {
		// Green before, red now — or never photographed at all. Either way
		// there is nothing to subtract, and the failure stands as the change's.
		return baselineDelta{}
	}
	before := map[string]bool{}
	for _, name := range entry.Failing {
		before[name] = true
	}
	after := failingTestNames(output)
	var known, fresh []string
	for _, name := range after {
		if before[name] {
			known = append(known, name)
			continue
		}
		fresh = append(fresh, name)
	}
	switch {
	case len(after) > 0 && len(fresh) == 0:
		return baselineDelta{PreExisting: true, Known: known, Note: "`" + entrypoint.Command +
			"` exited " + strconv.Itoa(exit) + ", and every failing test it reports (" +
			describeFailing(known) + ") was ALREADY failing at this commit before the run " +
			"touched the workspace. This is the repository's pre-existing state, not this " +
			"change's doing, and it is not a blocker."}
	case len(fresh) > 0:
		note := "`" + entrypoint.Command + "` exited " + strconv.Itoa(exit) + ". " +
			describeFailing(fresh) + " were NOT failing before this run — this change turned them red."
		if len(known) > 0 {
			note += " (" + describeFailing(known) + " were already failing beforehand.)"
		}
		return baselineDelta{Known: known, New: fresh, Note: note}
	}
	// Nothing named a test on this side, so nothing can be attributed. An
	// acquittal here would be the broadest possible one — "the whole suite was
	// red before, so its being red now proves nothing" — and it is exactly
	// wrong on the case that matters most: a run whose whole job was to turn
	// that red suite green. A red entrypoint with no readable failure list is
	// judged as it always was.
	//
	// The same reasoning is why a build that does not build is never acquitted
	// even when it did not build yesterday either: a workspace that will not
	// compile produced no evidence about the change at all, and "it was already
	// like this" argues for fixing it, not for shipping on top of it. A named
	// test failing inside a build-kind entrypoint — `make all`, which compiles
	// and then tests — still acquits above, because there the suite did run and
	// did say which test it was.
	return baselineDelta{}
}

func describeFailing(names []string) string {
	if len(names) == 0 {
		return "no individually named tests"
	}
	if len(names) > 8 {
		return strings.Join(names[:8], ", ") + " and " + strconv.Itoa(len(names)-8) + " more"
	}
	return strings.Join(names, ", ")
}

// ── reading a test runner's failures ────────────────────────────────────────

// failingTestPatterns is the failure vocabulary of the runners this engine
// meets, each pattern capturing one identity that is stable between two runs of
// the same suite: no durations, no line numbers that move, no counts.
//
// A pattern that over-matches is safe in one direction only, and that is the
// direction it is written for: a phantom name read out of BOTH runs cancels,
// and a phantom read out of the AFTER run alone is scored as a new failure,
// which fails the run. Nothing here can turn a real regression green.
var failingTestPatterns = []*regexp.Regexp{
	// go test
	regexp.MustCompile(`(?m)^\s*--- FAIL:\s+([^\s(]+)`),
	regexp.MustCompile(`(?m)^FAIL\s+(\S+)\s`),
	// pytest
	regexp.MustCompile(`(?m)^(?:FAILED|ERROR)\s+(\S+::\S+)`),
	regexp.MustCompile(`(?m)^(?:FAILED|ERROR)\s+(\S+\.py)\s*$`),
	// python unittest
	regexp.MustCompile(`(?m)^(?:FAIL|ERROR):\s+([\w.]+\s*\([\w.]+\))`),
	// jest / vitest / mocha
	regexp.MustCompile(`(?m)^\s*[✕✗×]\s+(.+?)\s*$`),
	regexp.MustCompile(`(?m)^\s*●\s+(.+?)\s*$`),
	// cargo test
	regexp.MustCompile(`(?m)^test\s+(\S+)\s+\.\.\.\s+FAILED`),
	// maven surefire / gradle
	regexp.MustCompile(`(?m)^\[ERROR\]\s+(\S+)\s+Time elapsed`),
	regexp.MustCompile(`(?m)^\s*(\S+)\s+>\s+\S+\s+FAILED\s*$`),
	// dotnet test / xunit
	regexp.MustCompile(`(?m)^\s*(?:Failed|X)\s+(\S+)\s`),
	// rspec
	regexp.MustCompile(`(?m)^rspec\s+(\./\S+:\d+)`),
	// ctest
	regexp.MustCompile(`(?m)^\s*\d+\s+-\s+(\S+)\s+\(Failed\)`),
	// TAP
	regexp.MustCompile(`(?m)^not ok\s+\d+\s+-?\s*(.+?)\s*$`),
}

var (
	ansiEscape     = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	trailingTiming = regexp.MustCompile(`\s*[\(\[]\s*[\d.,]+\s*(?:ms|s|sec|secs|seconds)?\s*[\)\]]\s*$`)
	digitRun       = regexp.MustCompile(`\d+`)
	spaceRun       = regexp.MustCompile(`\s+`)
)

// failingTestNames reads every test identity a runner named as failing, sorted
// and deduplicated so two runs of one suite compare as sets rather than as
// transcripts.
func failingTestNames(output string) []string {
	clean := ansiEscape.ReplaceAllString(output, "")
	seen := map[string]bool{}
	var names []string
	for _, pattern := range failingTestPatterns {
		for _, match := range pattern.FindAllStringSubmatch(clean, -1) {
			name := normalizeTestName(match[1])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func normalizeTestName(raw string) string {
	name := strings.TrimSpace(trailingTiming.ReplaceAllString(strings.TrimSpace(raw), ""))
	name = spaceRun.ReplaceAllString(name, " ")
	name = strings.Trim(name, ":.,")
	// A "name" that is a count, a bare verb or a punctuation run is a false
	// read of a summary line, and carrying it would make two identical runs
	// disagree with each other.
	if len(name) < 2 || len(name) > 200 {
		return ""
	}
	if digitRun.ReplaceAllString(name, "") == "" {
		return ""
	}
	switch strings.ToLower(name) {
	case "console", "failures", "failed", "error", "errors", "test", "tests":
		return ""
	}
	return name
}
