//go:build !windows

package seniordev

// senior-dev's page, in senior-dev's words: every line of its action log — a
// stage, a step, its ending — as what a person reads under the step of its
// process it served (delegate.Delegate's Present). codeaf draws the page;
// what senior-dev's records MEAN is said here, once, beside the words its
// stages already have.
//
// THE PAGE SHOWS ONLY WHAT senior-dev REALLY DOES. It has no planner, no
// reviewer and no subagent (baked/agents/coder.md): one model context works
// through its spec, explores, pins a check, lists the requirements, implements
// and hands in, in the order that model chooses; senior-dev then checks the
// tree itself with the project's own build and tests and finishes. Around that
// it compacts its model's memory, moves to another model, and steers its model
// when it stops short — and each of those is a line here, said as senior-dev
// steering its own work. Everything else it reports is machinery, and is left
// out.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/app"
)

// stepWords is each step of senior-dev's process (app.Steps) in the one word
// its page prints at the head of the step and its task's row reads while it is
// in it. Reading the spec is `spec`, because that is where the brief is kept.
var stepWords = map[string]string{
	app.StepBrief:     "spec",
	app.StepExplore:   "explore",
	app.StepPin:       "pin",
	app.StepChecklist: "checklist",
	app.StepImplement: "implement",
	app.StepSubmit:    "submit",
	app.StepVerify:    "verify",
}

// The two words for what senior-dev does around its model context with no
// model at all: setting up the folder it works in, and finishing — putting
// back the tree it stands by and measuring the change.
const (
	setupWord  = "setup"
	finishWord = "finish"
)

// presentActions is senior-dev's reader of its own action log: one per log,
// told every line in the order it was written.
//
// IT REMEMBERS ONE THING, THE HIGHEST ATTEMPT SO FAR. senior-dev reports
// `implement · running` with its attempt each time it starts its model on a
// turn: attempt 0 at the start, one more after each nudge, and the SAME attempt
// again when it retries a dropped call or corrects a malformed one. Only a
// higher attempt than any before is a nudge.
func presentActions() delegate.ActionReader {
	attempt := 0
	return func(action delegate.Action) (delegate.Shown, bool) {
		if action.Kind == delegate.ActionStage && action.Stage == "implement" && action.Status == "running" {
			facts := stageFactsOf(action.Data)
			if next := facts.whole("attempt"); next > attempt {
				attempt = next
				return nudged(next), true
			}
			return delegate.Shown{}, false
		}
		return presentAction(action)
	}
}

// nudged is senior-dev telling its model, which stopped without handing in,
// what it found about the tree and to finish (solo.go's nudge).
func nudged(attempt int) delegate.Shown {
	return delegate.Shown{
		Step: stepWords[app.StepImplement], Steer: true,
		Text: fmt.Sprintf("told its model what it found, and to finish and hand in (nudge %d)", attempt),
	}
}

// presentAction is one line of the log in senior-dev's words, for every line
// that needs nothing before it to be read.
func presentAction(action delegate.Action) (delegate.Shown, bool) {
	switch action.Kind {
	case delegate.ActionStep:
		return presentStep(action)
	case delegate.ActionStage:
		return presentStage(action.Stage, action.Status, stageFactsOf(action.Data))
	case delegate.ActionEnd:
		// THE ENDING IS senior-dev's OWN SENTENCE — what its check of the
		// project found — under the step that finishes the run.
		return delegate.Shown{Step: finishWord, Text: strings.TrimSpace(action.Message)}, true
	}
	return delegate.Shown{}, false
}

// presentStep is one finished tool call: the verb a person would use for it,
// what it was aimed at, and for a command how it came out.
func presentStep(action delegate.Action) (delegate.Shown, bool) {
	tool := strings.TrimSpace(action.Tool)
	about := strings.TrimSpace(action.Command)
	if tool != "" {
		about = strings.TrimSpace(strings.TrimPrefix(about, tool+":"))
	}
	shown := delegate.Shown{Step: stepWords[action.Step]}
	own := ownRecord(action.Step)
	switch tool {
	case "submit":
		// The hand-in's own stage says whether it was taken and what it held.
		return delegate.Shown{}, false
	case "bash":
		if action.Step == app.StepVerify {
			// senior-dev's own check: the command is the whole of what it did.
			shown.Text = about
		} else {
			shown.Text = "ran " + about
		}
		shown.Outcome = delegate.ExitWord(action.Exit)
		if action.Exit == nil {
			shown.Outcome = "did not finish"
		}
	case "read":
		shown.Text = "read " + firstOf(own, about)
	case "write":
		switch action.Step {
		case app.StepPin:
			shown.Text = "pinned its check"
		case app.StepChecklist:
			shown.Text = "wrote its checklist"
		default:
			shown.Text = "wrote " + firstOf(own, about)
		}
	case "edit", "apply_patch":
		switch action.Step {
		case app.StepPin:
			shown.Text = "changed its pinned check"
		case app.StepChecklist:
			shown.Text = "updated its checklist"
		default:
			shown.Text = "edited " + firstOf(own, patchedFile(tool, about))
		}
	case "grep":
		shown.Text = "searched " + about
	case "glob":
		shown.Text = "listed " + about
	case "webfetch":
		shown.Text = "fetched " + about
	case "websearch":
		shown.Text = "searched the web for " + about
	case "question":
		shown.Text = "asked a question, with nobody there to answer it"
	default:
		shown.Text = strings.TrimSpace(action.Command)
	}
	shown.Detail = stepDetail(action)
	// A CHANGE TO THE WORK WEARS ITS LINES, `+N,-M`, the way git counts them.
	// senior-dev's own records — its spec, pinned check and checklist — are
	// its bookkeeping, not the work, and wear none.
	if (tool == "write" || tool == "edit" || tool == "apply_patch") && own == "" && action.Added != nil && action.Removed != nil {
		shown.Lines, shown.Added, shown.Removed = true, *action.Added, *action.Removed
	}
	return shown, strings.TrimSpace(shown.Text) != ""
}

// stepDetail is the whole of one step as the log kept it: the command or
// argument the tool was called with, and what came back, for the page to open
// under the step's one line.
func stepDetail(action delegate.Action) string {
	var parts []string
	if command := strings.TrimSpace(action.Command); command != "" {
		parts = append(parts, command)
	}
	if observation := strings.TrimRight(action.Observation, " \n\t"); strings.TrimSpace(observation) != "" {
		parts = append(parts, observation)
	}
	return strings.Join(parts, "\n\n")
}

// ownRecord is how the page names one of senior-dev's own records when an
// action served its step, and "" for every other step.
func ownRecord(step string) string {
	switch step {
	case app.StepBrief:
		return "its spec"
	case app.StepPin:
		return "its pinned check"
	case app.StepChecklist:
		return "its checklist"
	}
	return ""
}

// patchedFile is the first file a patch names, from its own header, when the
// action was a patch; anything else is what it was about already.
func patchedFile(tool, about string) string {
	if tool != "apply_patch" {
		return about
	}
	for _, header := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
		if at := strings.Index(about, header); at >= 0 {
			name := strings.TrimSpace(about[at+len(header):])
			if end := strings.Index(name, " "); end > 0 {
				name = name[:end]
			}
			if name != "" {
				return name
			}
		}
	}
	return "its files"
}

// presentStage is one stage record. A stage that is only machinery — the
// run's contract, a model turn being configured, a withdrawn call, the usage
// rollup — is left out; so is one another line already says (the hand-in's
// `implement · submitted`, the check's `landing · checked`, a tree left as it
// was). `implement · running` is the reader's own ([presentActions]).
func presentStage(stage, status string, facts stageFacts) (delegate.Shown, bool) {
	switch stage + "/" + status {
	case "bootstrap/ready":
		// How it keeps its record of the tree: in git, or — in a folder with no
		// git history, `--in-place` — in checkpoints of its own outside it.
		outcome := facts.text("recorder")
		if outcome == "snapshot" {
			outcome = "no git history"
		}
		return delegate.Shown{Step: setupWord, Text: "set up its workspace", Outcome: outcome}, true
	case "intake/captured":
		return delegate.Shown{Step: stepWords[app.StepBrief], Text: "wrote your brief down as its spec"}, true

	case "implement/transport-retry":
		text := "the call to its model dropped; started a fresh turn"
		if retry, most := facts.whole("retry"), facts.whole("max_retries"); retry > 0 && most > 0 {
			text += fmt.Sprintf(" (retry %d of %d)", retry, most)
		}
		return delegate.Shown{Text: text, Steer: true}, true
	case "implement/tool-call-leak":
		return delegate.Shown{Text: "its model wrote a tool call as text; told it to call the tool", Steer: true}, true
	case "implement/turn-error":
		text := "its model's turn failed"
		if why := facts.text("error"); why != "" {
			text += ": " + why
		}
		return delegate.Shown{Text: text}, true
	case "implement/unsubmitted":
		return delegate.Shown{Text: "stopped without handing in its work"}, true

	case "compaction-capacity/pinned":
		text := "learned how much its model can hold"
		if limit := facts.whole("limit_tokens"); limit > 0 {
			text = fmt.Sprintf("learned its model holds %s tokens", thousands(limit))
		}
		return delegate.Shown{Text: text}, true
	case "compaction/summarized", "compaction/fallback":
		shown := delegate.Shown{Text: "compacted its memory", Memory: true}
		if status == "fallback" {
			shown.Outcome = "kept its own record"
		}
		return shown, true
	case "model-switch/switched":
		to := facts.text("to")
		if to == "" {
			return delegate.Shown{}, false
		}
		return delegate.Shown{Text: "switched to " + modelWord(to), Model: to, Reason: switchReason(facts.text("reason"))}, true

	case "submit/frozen":
		var outcome []string
		if files := facts.whole("patch_files"); files > 0 {
			outcome = append(outcome, plural(files, "file", "files"))
		}
		if items := facts.whole("checklist_items"); items > 0 {
			outcome = append(outcome, fmt.Sprintf("%d of %d ticked", facts.whole("checklist_ticked"), items))
		}
		return delegate.Shown{Step: stepWords[app.StepSubmit], Text: "handed in its work", Outcome: strings.Join(outcome, " · ")}, true
	case "submit/refused":
		return delegate.Shown{Step: stepWords[app.StepSubmit], Text: "its hand-in was refused", Outcome: refusalWord(facts.text("reason_class"))}, true

	case "verification/running":
		return delegate.Shown{Step: stepWords[app.StepVerify], Text: "checked its work itself, with the project's own build and tests"}, true
	case "verification/pass", "verification/fail":
		shown := delegate.Shown{Step: stepWords[app.StepVerify]}
		switch {
		case facts.yes("vacuous"):
			shown.Text = "found no build or tests to run"
		case status == "pass":
			shown.Text = "the project's own build and tests pass"
		default:
			shown.Text = "the project's own build or tests fail"
		}
		if commands := facts.whole("commands"); commands > 0 {
			shown.Outcome = plural(commands, "command", "commands")
		}
		return shown, true
	case "landing/repair-turn":
		return delegate.Shown{Step: stepWords[app.StepImplement], Steer: true, Text: "time is short: gave its model one last turn to finish"}, true
	case "landing/restored":
		return delegate.Shown{Step: finishWord, Text: "put the tree back to " + app.RestoredFrom(facts.text("source"))}, true
	case "landing/restore-failed":
		return delegate.Shown{Step: finishWord, Text: "could not put the tree back"}, true
	case "ship/restored":
		return delegate.Shown{Step: finishWord, Text: "put back the work it handed in, which had changed since"}, true
	case "ship/restore-failed":
		return delegate.Shown{Step: finishWord, Text: "could not put back the work it handed in"}, true
	case "patch-summary/completed":
		files := facts.whole("files")
		if files < 1 {
			return delegate.Shown{Step: finishWord, Text: "its change is empty"}, true
		}
		outcome := plural(files, "file", "files")
		if facts.has("additions") || facts.has("deletions") {
			outcome += fmt.Sprintf(" · +%d -%d", facts.whole("additions"), facts.whole("deletions"))
		}
		return delegate.Shown{Step: finishWord, Text: "measured its change", Outcome: outcome}, true
	}
	return delegate.Shown{}, false
}

// refusalWord is a refused hand-in's reason, in a person's words.
func refusalWord(class string) string {
	switch class {
	case "already-submitted":
		return "it had already handed in"
	case "empty-tree":
		return "nothing had changed"
	case "no-checklist":
		return "it had no checklist"
	case "capture-error", "record-error":
		return "the tree could not be recorded"
	}
	return ""
}

// switchReason is why the router moved the coder, in a person's words, or ""
// when the move was the router's ordinary choice.
func switchReason(reason string) string {
	reason = strings.TrimPrefix(reason, "constraint-relaxed:")
	switch reason {
	case "previous-cooling":
		return "the last one kept failing"
	case "previous-rate-limited":
		return "the last one was rate-limited"
	case "previous-busy":
		return "the last one was busy"
	case "better-score":
		return "it was doing better"
	case "all-cooling":
		return "every model was failing"
	}
	return ""
}

// modelWord is a model id as a line names it: the part after the last vendor.
func modelWord(id string) string {
	id = strings.TrimSpace(id)
	if at := strings.LastIndexByte(id, '/'); at >= 0 && at+1 < len(id) {
		return id[at+1:]
	}
	return id
}

func firstOf(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// thousands writes a count with its thousands grouped, the way a person reads a
// window size.
func thousands(n int) string {
	digits := fmt.Sprint(n)
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}

// stageFacts is a stage record's data, read forgivingly: a key that is absent
// or of another shape reads as nothing.
type stageFacts map[string]any

func stageFactsOf(raw json.RawMessage) stageFacts {
	var facts stageFacts
	if len(raw) == 0 || json.Unmarshal(raw, &facts) != nil {
		return nil
	}
	return facts
}

func (f stageFacts) has(key string) bool { _, ok := f[key]; return ok }

func (f stageFacts) text(key string) string {
	text, _ := f[key].(string)
	return strings.TrimSpace(text)
}

func (f stageFacts) whole(key string) int {
	n, _ := f[key].(float64)
	return int(n)
}

func (f stageFacts) yes(key string) bool {
	yes, _ := f[key].(bool)
	return yes
}
