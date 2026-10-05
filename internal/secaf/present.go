package secaf

// The audit's page, in its own words: what its records are as it runs (the
// watch, which turns sec-af's progress into the program's stage and step
// records) and what each record reads as on the task's page (presentActions).
//
// ONE LINE PER AGENT SESSION, NOT PER TOOL CALL. A standard audit runs well
// over a hundred agent sessions, eight at a time, each of a dozen reads; a page
// of every read would be thousands of lines nobody follows. What a person can
// follow is which agent ran, in which phase, and how it came out — and the raw
// calls stay one key away.
//
// NO MACHINERY WORDS. sec-af's notes say "verified" and "verdict"; this house
// says neither to a person, so a note is reworded before it is shown.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/secaf/backing"
)

// stepWords is each stage as the one word its page prints at the head of the
// stage's lines, and its task's row reads while the audit is in it.
var stepWords = map[string]string{
	stageStarting:    "setup",
	stageRecon:       "map",
	stageHunt:        "hunt",
	stageProve:       "test",
	stageRemediation: "fix",
	stageReport:      "report",
}

// The step records' tools: an agent session that ended, and one of sec-af's
// own progress notes.
const (
	toolSession = "session"
	toolNote    = "note"
)

// watch turns the audit's progress into the program's records. sec-af's
// phases run one after another, so the phase the last phase note named is the
// one every session since belongs to.
type watch struct {
	host  delegate.Host
	mu    sync.Mutex
	stage string
}

func newWatch(host delegate.Host) *backing.Watch {
	w := &watch{host: host, stage: stageStarting}
	return &backing.Watch{Session: w.session, Note: w.note}
}

func (w *watch) current() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stage
}

func (w *watch) session(label string, result backing.SessionResult, err error) {
	outcome := "answered"
	switch {
	case err != nil:
		outcome = "stopped: " + firstSentence(err.Error())
	case result.Failed != "":
		outcome = "no answer: " + firstSentence(result.Failed)
	}
	w.host.Step(delegate.StepRecord{
		Tool: toolSession, Step: w.current(), Command: label,
		Observation: fmt.Sprintf("%s · %d turn%s · %d read%s", outcome, result.Turns, plural(result.Turns), result.Tools, plural(result.Tools)),
	})
}

func (w *watch) note(message string, tags []string) {
	if len(tags) == 0 {
		return
	}
	switch tags[0] {
	case "phase":
		if len(tags) < 2 {
			return
		}
		stage := tags[1]
		if _, known := stageWords[stage]; !known {
			return
		}
		status := "running"
		for _, tag := range tags[2:] {
			if tag == "done" {
				status = "done"
			}
		}
		w.mu.Lock()
		w.stage = stage
		w.mu.Unlock()
		w.host.Stage(delegate.StageRecord{Stage: stage, Status: status, Data: stageData(map[string]any{"note": plainWords(message)})})
		return
	case "audit":
		// The audit's own start and end are the program's hello and terminal.
		return
	}
	for _, tag := range tags {
		// A progress block is data, and the incremental merge counts every
		// finding twice over what the hunt's own closing note says.
		if tag == "progress" || tag == "incremental" {
			return
		}
	}
	// AN AGENT'S START IS ITS SESSION'S LINE ALREADY, so its "starting" note
	// is left off; a hunter's is kept, because it names the kind of problem
	// the hunt has turned to.
	if strings.HasSuffix(message, " starting") && !strings.Contains(message, "hunter") {
		return
	}
	w.host.Step(delegate.StepRecord{Tool: toolNote, Step: w.current(), Command: plainWords(message)})
}

// machineryWords are sec-af's words for what its provers decide, and the
// person's words for them.
var machineryWords = []struct {
	pattern *regexp.Regexp
	plain   string
}{
	{regexp.MustCompile(`(?i)\bverdict agent\b`), "deciding agent"},
	{regexp.MustCompile(`(?i)\bverdicts?\b`), "decision"},
	{regexp.MustCompile(`(?i)\bverified\b`), "tested"},
	{regexp.MustCompile(`(?i)\bverifier\b`), "tester"},
	{regexp.MustCompile(`(?i)\bauditors?\b`), "checker"},
	{regexp.MustCompile(`(?i)\brefuted\b`), "ruled out"},
}

// plainWords is a note of sec-af's in a person's words.
func plainWords(text string) string {
	text = oneLine(text)
	for _, word := range machineryWords {
		text = word.pattern.ReplaceAllStringFunc(text, func(found string) string {
			// The plain word keeps the found word's capital, so a note that
			// opened a sentence still opens one.
			if found != "" && found[0] >= 'A' && found[0] <= 'Z' {
				return strings.ToUpper(word.plain[:1]) + word.plain[1:]
			}
			return word.plain
		})
	}
	return text
}

// stageData is a stage record's data, or nothing when it does not encode.
func stageData(fields map[string]any) json.RawMessage {
	data, err := json.Marshal(fields)
	if err != nil || len(data) > delegate.StageDataCap {
		return nil
	}
	return data
}

// presentActions is the audit's reader of its own action log.
func presentActions() delegate.ActionReader {
	return func(action delegate.Action) (delegate.Shown, bool) {
		switch action.Kind {
		case delegate.ActionStage:
			return presentStage(action)
		case delegate.ActionStep:
			return presentStep(action)
		case delegate.ActionEnd:
			return delegate.Shown{Step: stepWords[stageReport], Text: plainWords(action.Message)}, true
		}
		return delegate.Shown{}, false
	}
}

func presentStage(action delegate.Action) (delegate.Shown, bool) {
	var data map[string]any
	_ = json.Unmarshal(action.Data, &data)
	text := func(key string) string {
		value, _ := data[key].(string)
		return value
	}
	number := func(key string) int {
		value, _ := data[key].(float64)
		return int(value)
	}
	step := stepWords[action.Stage]
	switch {
	case action.Stage == stageStarting && action.Status == "running":
		return delegate.Shown{Step: step, Text: "audits " + text("scope")}, text("scope") != ""
	case action.Stage == stageStarting && action.Status == "changes":
		return delegate.Shown{Step: step, Text: fmt.Sprintf("%d changed file%s since %s, and %d near them",
			number("changed"), plural(number("changed")), text("base"), number("nearby"))}, true
	case action.Stage == stageReport:
		return delegate.Shown{Step: step, Text: "writes the report"}, true
	case action.Status == "done":
		if note := text("note"); note != "" {
			return delegate.Shown{Step: step, Text: note}, true
		}
		return delegate.Shown{}, false
	case action.Status == "running":
		return delegate.Shown{Step: step, Text: stageWords[action.Stage]}, stageWords[action.Stage] != ""
	}
	return delegate.Shown{}, false
}

func presentStep(action delegate.Action) (delegate.Shown, bool) {
	step := stepWords[action.Step]
	switch action.Tool {
	case toolSession:
		outcome, _, _ := strings.Cut(action.Observation, " · ")
		outcome, _, _ = strings.Cut(outcome, ":")
		return delegate.Shown{Step: step, Text: strings.ToLower(plainWords(action.Command)), Outcome: outcome,
			Detail: action.Observation}, true
	case toolNote:
		return delegate.Shown{Step: step, Text: plainWords(action.Command)}, strings.TrimSpace(action.Command) != ""
	}
	return delegate.Shown{}, false
}
