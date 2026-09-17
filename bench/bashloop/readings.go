package main

// readings.go — the numbers, taken from where the engine already keeps them.
//
// Every figure here is a reading, not a measurement of our own: cost from the
// home's usage ledger (the provider's own per-response accounting, the bench
// protocol's only honest source on a shared key), steps and the diagnostics
// from the family's own journals, the ending and the wall from the task
// notice the landing carries, and the family shape from the graph's own
// checkpoint document. Nothing asks a model, and nothing grades itself.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// readings is one invocation's harvest.
type readings struct {
	// Ending is the engine's own state word for how the work came home:
	// done, failed, unverified — or empty when the wall arrived first.
	Ending string
	// Report is the landing's own card: what it did, or what stopped it.
	Report string
	// StartedAt and EndedAt are the node's own record of its life. Wall is
	// EndedAt minus StartedAt when the node recorded both; WallDriver is the
	// driver's own clock over the whole invocation, the fallback a node that
	// never started cannot supply.
	StartedAt  time.Time
	EndedAt    time.Time
	WallDriver time.Duration

	// Steps counts the family's finished tool calls — the engine's own step
	// unit, journaled one `took` line per call.
	Steps int

	// CostUSD is the whole of what this invocation spent in its own home:
	// every priced row of that home's usage ledger, the task family's calls
	// and the conversation's auxiliary calls together. Unbilled counts the
	// rows whose receipt the provider never returned — a cost that reads low
	// by exactly that much, said rather than hidden.
	CostUSD  float64
	Unbilled int
	// ModelsUsed is every model slug the ledger says was actually billed,
	// sorted. What was asked for is on the CSV's own column; this is what was
	// paid for.
	ModelsUsed []string

	// ChangedFiles is the fixture's own count of files the work touched.
	ChangedFiles int

	// ChildrenDone and ChildrenTotal are the root task's own fan-out, read
	// from the graph checkpoint. NodesFailed is every node in the family —
	// root or child — the engine marked failed.
	ChildrenDone  int
	ChildrenTotal int
	NodesFailed   int

	// The branch-only diagnostics. Arm A has no bash belt, so its counts are
	// zero by construction, not by achievement.
	// InvalidActions counts the one-action envelope's rejections in the
	// family's journals: a tool result that carries the envelope's
	// diagnostic. Truncations counts the head+tail+path marker in tool
	// output. EditIdiomFlags counts in-place edit commands issued with no
	// counting search before them — the doctrine's own discipline — as a
	// mechanical input to the spot check, never a verdict on its own.
	InvalidActions int
	Truncations    int
	EditIdiomFlags int
}

// ── journal shapes, read as text ────────────────────────────────────────────

// journalEntry is the one line of a family journal this reader needs. The
// transcript carries far more than this; anything not named here is skipped
// untouched.
type journalEntry struct {
	Type      string            `json:"type"`
	Role      string            `json:"role,omitempty"`
	Content   string            `json:"content,omitempty"`
	ToolCalls []journalToolCall `json:"toolCalls,omitempty"`
	Took      *journalTookMark  `json:"took,omitempty"`
}

type journalToolCall struct {
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type journalTookMark struct {
	CallID string `json:"callId"`
}

// usageLine is one row of the home's usage ledger.
type usageLine struct {
	Model    string  `json:"model,omitempty"`
	USD      float64 `json:"usd,omitempty"`
	Unbilled bool    `json:"unbilled,omitempty"`
}

// checkpointDocument is the graph's own checkpoint: the family as the engine
// recorded it.
type checkpointDocument struct {
	Nodes []checkpointNode `json:"nodes"`
}

type checkpointNode struct {
	ID     uint64 `json:"id"`
	Parent uint64 `json:"parent,omitempty"`
	State  string `json:"state"`
}

// ── the markers the diagnostics count ───────────────────────────────────────

// truncationMarker is the head+tail+path cut's marker, in the spelling the
// design gives it (DESIGN.md, Decision 3).
const truncationMarker = "[output truncated; full output: "

// invalidActionMarkers are the one-action envelope's rejection diagnostics,
// counted in tool results, in the spelling the belt actually writes
// (internal/session's bashbelt_envelope.go): every rejection leads with
// "no action executed", and the design's own words ("invalid action") never
// reach a transcript. The README says the list out loud so a reader knows
// what was counted.
var invalidActionMarkers = []string{
	"no action executed",
}

// ── collection ──────────────────────────────────────────────────────────────

// collectReadings harvests one invocation's numbers off its disk: the home's
// ledger, the family's journals, the graph's checkpoint, and the work's
// footprint against the embedded pristine. A reading that cannot be taken
// (no ledger, no journals) is a zero, and the row says the run never
// produced one — the same honesty bench/e2e's own autopsy keeps.
func collectReadings(home, placeDir, journalDir, workDir string, pristine fixtureFiles) readings {
	var r readings
	r.CostUSD, r.Unbilled, r.ModelsUsed = readLedger(filepath.Join(home, "v3", "usage.jsonl"))
	r.Steps, r.InvalidActions, r.Truncations, r.EditIdiomFlags = readJournals(journalDir)
	r.ChildrenDone, r.ChildrenTotal, r.NodesFailed = readCheckpoint(filepath.Join(placeDir, "tasks.json"))
	r.ChangedFiles = countChangedFiles(workDir, pristine)
	return r
}

// readLedger sums the home's ledger: what the invocation spent, by whose
// accounting. A missing ledger is a run that never made a call — a zero, and
// an honest one.
func readLedger(path string) (usd float64, unbilled int, models []string) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, nil
	}
	defer f.Close()
	seen := map[string]bool{}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var row usageLine
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		usd += row.USD
		if row.Unbilled {
			unbilled++
		}
		if row.Model != "" && !seen[row.Model] {
			seen[row.Model] = true
			models = append(models, row.Model)
		}
	}
	sort.Strings(models)
	return usd, unbilled, models
}

// readJournals walks the family's journals: steps from the `took` lines, the
// diagnostics from the text the workers left behind. The walk is recursive:
// the task door keeps its journals flat under the session folder's tasks/
// directory, and the do door nests them by session and run under the home's
// v3/runs — one reader for both doors, so the counts cannot drift apart.
func readJournals(journalDir string) (steps, invalid, truncations, idiomFlags int) {
	var names []string
	err := filepath.WalkDir(journalDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // a run that kept no journals is a zero, not a failure
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			names = append(names, path)
		}
		return nil
	})
	if err != nil {
		return 0, 0, 0, 0
	}
	// Oldest first: the edit-idiom discipline is a question of what came
	// before, and the journal names carry a timestamp.
	sort.Strings(names)
	for _, name := range names {
		s, i, t, f := readOneJournal(name)
		steps += s
		invalid += i
		truncations += t
		idiomFlags += f
	}
	return steps, invalid, truncations, idiomFlags
}

// readOneJournal reads one node's transcript. Steps are the `took` lines;
// the diagnostics are counted over tool results and bash calls in file
// order, which is the order they happened in.
func readOneJournal(path string) (steps, invalid, truncations, idiomFlags int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, 0
	}
	defer f.Close()
	countedSearch := false // a grep -c or grep -n has run in this journal
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var e journalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		switch {
		case e.Type == "took":
			steps++
		case e.Type == "message" && e.Role == "tool":
			if strings.Contains(e.Content, truncationMarker) {
				truncations++
			}
			if isInvalidAction(e.Content) {
				invalid++
			}
		case e.Type == "message" && e.Role == "assistant":
			for _, call := range e.ToolCalls {
				if call.Function.Name != "bash" {
					continue
				}
				command := bashCommand(call.Function.Arguments)
				if isCountingSearch(command) {
					countedSearch = true
					continue
				}
				if isInPlaceEdit(command) && !countedSearch {
					idiomFlags++
				}
			}
		}
	}
	return steps, invalid, truncations, idiomFlags
}

// bashCommand pulls the command text out of a bash tool call's arguments —
// the wire shape is {"command": "..."}.
func bashCommand(arguments string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	return args.Command
}

// isInPlaceEdit recognizes the two in-place edit commands the doctrine's
// discipline is about.
func isInPlaceEdit(command string) bool {
	for _, marker := range []string{"sed -i", "perl -pi", "perl -pe", "ed -s"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

// isCountingSearch recognizes the discipline's own preliminary: a grep that
// counts or numbers its matches before an edit is made against them.
func isCountingSearch(command string) bool {
	if !strings.Contains(command, "grep") {
		return false
	}
	for _, flag := range []string{"-c", "-n"} {
		for _, form := range []string{" " + flag + " ", " " + flag, flag + " "} {
			if strings.Contains(command, form) {
				return true
			}
		}
	}
	return false
}

// isInvalidAction says whether one tool result carries the envelope's
// rejection.
func isInvalidAction(content string) bool {
	if content == "" {
		return false
	}
	lower := strings.ToLower(content)
	for _, marker := range invalidActionMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// readCheckpoint reads the graph's own record of the family.
func readCheckpoint(path string) (childrenDone, childrenTotal, nodesFailed int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0
	}
	var doc checkpointDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, 0, 0
	}
	for _, n := range doc.Nodes {
		if n.Parent == 0 {
			continue
		}
		childrenTotal++
		if n.State == "done" {
			childrenDone++
		}
	}
	for _, n := range doc.Nodes {
		if n.State == "failed" {
			nodesFailed++
		}
	}
	return childrenDone, childrenTotal, nodesFailed
}

// countChangedFiles is the work's footprint: the files whose bytes differ
// from the embedded pristine fixture, plus the files the work added that the
// fixture never had. The engine's tree copy carries no .git, so the seed
// commit is unreachable there — the embedded pristine is the same honesty
// in a shape the tree can answer. A run that changed nothing did nothing,
// whatever its log narrates; the harness's own furniture (.codeaf, .furrow)
// is nobody's work and is not counted.
func countChangedFiles(workDir string, pristine fixtureFiles) int {
	if strings.TrimSpace(workDir) == "" || len(pristine) == 0 {
		return 0
	}
	changed := 0
	for name, wantBytes := range pristine {
		workBytes, err := os.ReadFile(filepath.Join(workDir, name))
		if err != nil || !bytes.Equal(workBytes, wantBytes) {
			changed++
		}
	}
	_ = filepath.WalkDir(workDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".codeaf" || entry.Name() == ".furrow" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(workDir, path)
		if err != nil {
			return nil
		}
		if _, tracked := pristine[filepath.ToSlash(rel)]; !tracked {
			changed++
		}
		return nil
	})
	return changed
}

// applyNotice folds the landing's own facts onto a reading: the ending, the
// report, the node's own wall. Called once, when the landing arrives.
func (r *readings) applyNotice(state, report string, started, ended time.Time) {
	r.Ending = state
	r.Report = report
	r.StartedAt = started
	r.EndedAt = ended
}

// wallSeconds answers the cell's wall: the node's own record when it kept
// one, the driver's clock when the node never got to record.
func (r readings) wallSeconds() (float64, string) {
	if !r.StartedAt.IsZero() && !r.EndedAt.IsZero() && !r.EndedAt.Before(r.StartedAt) {
		return r.EndedAt.Sub(r.StartedAt).Seconds(), "node"
	}
	return r.WallDriver.Seconds(), "driver"
}

// modelsUsedLine is the honesty column: what was billed, not what was asked.
func (r readings) modelsUsedLine() string {
	return strings.Join(r.ModelsUsed, "+")
}

// describe is the one line the driver prints as an invocation lands.
func (r readings) describe(label string) string {
	wall, _ := r.wallSeconds()
	return fmt.Sprintf("%s  %s  steps=%d  $%.4f  wall=%.0fs  changed=%d  invalid=%d  trunc=%d  idiom=%d",
		label, r.Ending, r.Steps, r.CostUSD, wall, r.ChangedFiles,
		r.InvalidActions, r.Truncations, r.EditIdiomFlags)
}
