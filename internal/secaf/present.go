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
// sec-af's OWN WORDS, BUT NOT ITS MACHINERY. The phases and agents keep sec-af's
// names — RECON, HUNT, PROVE, the Verifier — because those are what its notes
// say; but this house says none of `verdict`, `verified`, `auditor` or
// `refuted` to a person, so those are reworded, and a note that only exposes
// the algorithm's plumbing is reworded or left off ([noteRewrites]).

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
	stageRecon:       "recon",
	stageHunt:        "hunt",
	stageProve:       "prove",
	stageRemediation: "remediate",
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
	return &backing.Watch{Session: w.session, Note: w.note, Call: w.call}
}

// pagedCalls are the single structured calls that are an agent of sec-af's
// own, by the schema their answer takes, and the agent's name. The Verdict
// agent is the fourth of the proof chain; it reads the other three's evidence
// in one call rather than a session, and without this line the page showed a
// chain of three. The duplicate checks, the compliance mapping and the like
// are many small calls of plumbing, and stay off the page.
var pagedCalls = map[string]string{"VerdictDecision": "verdict agent"}

func (w *watch) call(schema string, err error) {
	agent := pagedCalls[schema]
	if agent == "" {
		return
	}
	outcome := "answered"
	if err != nil {
		outcome = "stopped: " + firstSentence(err.Error())
	}
	w.host.Step(delegate.StepRecord{Tool: toolSession, Step: w.current(), Command: plainWords(agent), Observation: outcome + " · one call"})
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
	default:
		// WHAT IT FOUND, NOT ONLY THAT IT ANSWERED: a scan says how many
		// places it will look at, and an enrichment the finding it made of
		// one, so the hunt reads as what the hunters turned up.
		found, title := sessionFound(result.JSON)
		if found != "" {
			outcome = found
		}
		if title != "" {
			label += " · " + title
		}
	}
	// THE RECORD SAYS IT AS EVERY SURFACE DOES: a shell run prints the
	// record's words as they are, so the agent's name is put in a person's
	// words here, once, rather than only by the page's reader.
	w.host.Step(delegate.StepRecord{
		Tool: toolSession, Step: w.current(), Command: plainWords(label),
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
	// is left off — a hunter's too, now that its scan's line names it.
	if strings.HasSuffix(message, " starting") {
		return
	}
	if message = rewriteNote(message); message == "" {
		return
	}
	w.host.Step(delegate.StepRecord{Tool: toolNote, Step: w.current(), Command: plainWords(message)})
}

// sessionFound is what a session's answer says it found, as an outcome word
// and a title: a scan's locations (`2 locations`, `nothing`), or an
// enrichment's finding (`high`, and its title). Any other answer says neither.
func sessionFound(answer json.RawMessage) (outcome, title string) {
	var shape struct {
		Locations *[]json.RawMessage `json:"locations"`
		Title     string             `json:"title"`
		Severity  string             `json:"severity"`
	}
	if len(answer) == 0 || json.Unmarshal(answer, &shape) != nil {
		return "", ""
	}
	switch {
	case shape.Locations != nil:
		if n := len(*shape.Locations); n > 0 {
			return fmt.Sprintf("%d location%s", n, plural(n)), ""
		}
		return "nothing", ""
	case shape.Title != "" && shape.Severity != "":
		return strings.ToLower(shape.Severity), oneLine(shape.Title)
	}
	return "", ""
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
	{regexp.MustCompile(`(?i)\bauditors?\b`), "checker"},
	{regexp.MustCompile(`(?i)\brefuted\b`), "ruled out"},
}

// noteRewrites are sec-af notes that say how its algorithm works rather than
// what it found, in words a person reads; a rewrite to "" leaves the note off.
var noteRewrites = []struct {
	pattern *regexp.Regexp
	plain   string
}{
	// The expansion is computed and never handed to the hunters, so saying
	// it widened the hunt would be untrue.
	{regexp.MustCompile(`^CWE expansion suggested .*$`), ""},
	{regexp.MustCompile(`fingerprint-unique findings, running semantic dedup`), "distinct findings, merging duplicates"},
}

// rewriteNote is a note as the page says it, or "" for one it leaves off.
func rewriteNote(message string) string {
	for _, rewrite := range noteRewrites {
		message = rewrite.pattern.ReplaceAllString(message, rewrite.plain)
	}
	return strings.TrimSpace(message)
}

// plainWords is a note of sec-af's without its machinery words.
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
		if note := text("note"); note != "" {
			return delegate.Shown{Step: step, Text: note}, true
		}
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
		return delegate.Shown{Step: step, Text: plainWords(action.Command), Outcome: outcome,
			Detail: action.Observation}, true
	case toolNote:
		return delegate.Shown{Step: step, Text: plainWords(action.Command)}, strings.TrimSpace(action.Command) != ""
	}
	return delegate.Shown{}, false
}
