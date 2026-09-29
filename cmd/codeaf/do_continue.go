package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	homepkg "github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// CONTINUE IS THE SAME ASSIGNMENT PLUS ONE FINDING, run in the tree the last run
// left. A headless run edits the person's own directory in place, so the tree is
// already where it was; what a continuation needs from the record is the
// assignment, how the run ended and what its plan reached. It is composed here
// into the brief of an ordinary new run, so nothing below the door changes.
//
// The wrapper is written around the INNERMOST assignment ([assignmentOf]): a
// continuation of a continuation names the first request once, not a stack of
// wrappers.

const (
	// recordPrefix is what every private record folder is named with, and what
	// an id leaves off: the id is the part a person can type.
	recordPrefix = "codeaf-do-"
	// continueAssignmentHeader introduces the assignment inside the wrapper.
	continueAssignmentHeader = "The original assignment:\n"
	// continuePreamble is exactly what [continuationBrief] writes before the
	// assignment. It is matched EXACTLY when peeled, never as a substring.
	continuePreamble = "Carry on a task a previous run started in this directory. Its edits are already on disk, so parts of the assignment may be done. " +
		"Plan only what the assignment still needs; do not redo finished work or add verification the assignment does not ask for.\n\n" +
		continueAssignmentHeader
	continueEndingHeader  = "How the previous run ended: "
	continueReachedHeader = "What the previous run's plan reached:"
	continueSealedHeader  = "What the previous run sealed:"
	continueAskedHeader   = "What is asked of this round, in the person's words (the assignment above is unchanged):"
	// resultClip bounds one task's result in the carried account.
	resultClip = 400
)

// continueSections is every block the wrapper writes AFTER the assignment, so
// [assignmentOf] knows where an assignment stops.
var continueSections = []string{continueEndingHeader, continueReachedHeader, continueSealedHeader, continueAskedHeader}

// endingWords is the root's status in the product's own words.
var endingWords = map[plandb.Status]string{
	plandb.StatusDone:      "it finished",
	plandb.StatusFailed:    "it stopped before it finished",
	plandb.StatusCancelled: "it was stopped",
	plandb.StatusRunning:   "it was interrupted",
}

// priorRun is what a kept record says about a run: the assignment, the ending
// and one line for every task its plan made.
type priorRun struct {
	assignment string
	ending     string
	reached    []string
	// cellRoot is the cell the run worked in, and sealed what that cell's chain
	// holds; both are empty for a pre-cell record.
	cellRoot string
	sealed   sealedState
}

// assignmentOf recovers the innermost assignment from a brief that may itself
// be a continuation, and returns any other brief unchanged.
func assignmentOf(brief string) string {
	for {
		rest, wrapped := strings.CutPrefix(brief, continuePreamble)
		if !wrapped {
			return brief
		}
		end := len(rest)
		for _, section := range continueSections {
			if at := strings.Index(rest, "\n\n"+section); at >= 0 && at < end {
				end = at
			}
		}
		brief = strings.TrimSpace(rest[:end])
	}
}

// continuationBrief composes the brief of the next round: the wrapper, the
// assignment, the ending, what the plan reached, and the person's words when
// there are any. A block with nothing in it is not written.
func continuationBrief(prior priorRun, words string) string {
	var brief strings.Builder
	brief.WriteString(continuePreamble + prior.assignment)
	brief.WriteString("\n\n" + continueEndingHeader + prior.ending + ".")
	if len(prior.reached) > 0 {
		brief.WriteString("\n\n" + continueReachedHeader + "\n" + strings.Join(prior.reached, "\n"))
	}
	if !prior.sealed.empty() {
		brief.WriteString("\n\n" + prior.sealed.words())
	}
	if words = strings.TrimSpace(words); words != "" {
		brief.WriteString("\n\n" + continueAskedHeader + "\n" + words)
	}
	return brief.String()
}

// readPriorRun reads a kept record's plan. It opens the store the way a run
// does, so a folder that is not a record is refused rather than created.
func readPriorRun(folder string) (priorRun, error) {
	path := filepath.Join(folder, "plandb.db")
	if _, err := os.Stat(path); err != nil {
		return priorRun{}, fmt.Errorf("%s holds no record of a run", folder)
	}
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		return priorRun{}, fmt.Errorf("read the record at %s: %w", folder, err)
	}
	defer store.Close()
	root := store.Task(store.RootID())
	if root == nil {
		return priorRun{}, fmt.Errorf("the record at %s has no run in it", folder)
	}
	prior := priorRun{assignment: assignmentOf(strings.TrimSpace(root.Description)), ending: endingWords[root.Status]}
	if prior.ending == "" {
		prior.ending = "it did not finish"
	}
	for _, task := range store.Tasks() {
		if task.ID != root.ID {
			prior.reached = append(prior.reached, reachedLine(task))
		}
	}
	if prior.cellRoot = cellOf(folder); prior.cellRoot != "" {
		prior.sealed = sealedStateOf(prior.cellRoot)
	}
	return prior, nil
}

// reachedLine is one task of the earlier plan: how it stands and what it said.
func reachedLine(task *plandb.Task) string {
	said := strings.TrimSpace(task.Result + task.Error)
	line := fmt.Sprintf("- %s [%s]", task.Title, task.Status)
	if said == "" {
		return line
	}
	return line + ": " + clip(firstLine(said), resultClip)
}

// resolveRecord turns what a person typed into a record folder: the id `record
// kept at` printed, an unambiguous start of one, or the folder's own path.
func resolveRecord(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("--continue needs the id of a run · `record kept at` names it")
	}
	if info, err := os.Stat(id); err == nil && info.IsDir() {
		return id, nil
	}
	found, err := recordsStartingWith(recordPrefix + strings.TrimPrefix(id, recordPrefix))
	switch {
	case err != nil || len(found) == 0:
		return "", fmt.Errorf("no kept run is named %q · a run keeps its record when it did not finish, or with --keep", id)
	case len(found) > 1:
		return "", fmt.Errorf("%q names %d kept runs · say more of it", id, len(found))
	}
	return found[0], nil
}

// recordsStartingWith lists the record folders whose name starts with prefix.
func recordsStartingWith(prefix string) ([]string, error) {
	runs := homepkg.Join("runs")
	entries, err := os.ReadDir(runs)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			found = append(found, filepath.Join(runs, entry.Name()))
		}
	}
	sort.Strings(found)
	return found, nil
}

// recordID is the part of a record folder's name a person types after
// `--continue`.
func recordID(folder string) string {
	return strings.TrimPrefix(filepath.Base(folder), recordPrefix)
}

// continueHint is the stderr line that makes a kept record findable as a run
// to carry on.
func continueHint(folder string) string {
	return "continue it with: codeaf do --continue " + recordID(folder)
}

// continuation is what a continued run is handed: the brief of the next round,
// the assignment it carries on, and the cell it goes on working in ("" when the
// record has none).
type continuation struct {
	brief, assignment, cellRoot string
}

// continuedBrief is the whole door: an id and the person's words in, the next
// round out.
func continuedBrief(id, words string) (continuation, error) {
	folder, err := resolveRecord(id)
	if err != nil {
		return continuation{}, err
	}
	prior, err := readPriorRun(folder)
	if err != nil {
		return continuation{}, err
	}
	return continuation{continuationBrief(prior, words), prior.assignment, prior.cellRoot}, nil
}

// continuesOnRunRoad says the run road can carry this request on: the older engine
// keeps its record in another store, which --continue does not read.
func continuesOnRunRoad() error {
	if session.BashBeltAsked() {
		return nil
	}
	return errors.New("--continue reads a run's record, which only the run engine keeps · unset CODEAF_TASK_BELT to use it")
}
