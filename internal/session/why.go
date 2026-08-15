package session

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Why answers "what did you just change, and why" from the journal alone.
//
// No model is asked. The answer is a derivation over the last turn's messages:
// the instruction the person gave, then every tool call that turn made, each
// read back as the thing it did — "edited loop.go (+32 −2 via 2 replacements),
// ran go build ./… (ok), wrote out.txt". Deterministic, offline, and free, so a
// surface can bind it to a key and get the same sentence every time.
//
// That is a deliberate refusal of the obvious implementation. Asking the model
// to explain itself costs a request, takes seconds, and returns a plausible
// account of what it MEANT to do — which is exactly the thing a person reaches
// for this to check. The journal knows what actually ran; the model knows what
// it was going for. The "why" here is the person's own instruction, quoted, and
// the "what" is the record. Nothing in between is invented.
//
// The whyLimit caps how much of the instruction is quoted: this is a reminder of
// what was asked, not a re-print of the message.
const whyLimit = 400

// whyCommandLimit bounds one shell command in the answer. A command longer than
// this is a script, and the line it is on is a summary.
const whyCommandLimit = 72

func (a *Agent) Why() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	start, ok := a.lastTurnStartLocked()
	if !ok {
		return "Nothing to explain yet: this session has not been asked for anything."
	}

	asked := clip(strings.TrimSpace(messageContentText(a.messages[start])), whyLimit)
	turn := a.messages[start+1:]

	// Results are keyed by call id rather than read positionally: a batch's
	// results are journaled in call order (loop.go), but reading them by
	// position would put the wrong verdict beside a call the moment that changes.
	results := make(map[string]string, len(turn))
	for _, message := range turn {
		if message.Role == "tool" && message.ToolCallID != "" {
			results[message.ToolCallID] = messageContentText(message)
		}
	}

	var actions []string
	for _, message := range turn {
		for _, call := range message.ToolCalls {
			if phrase := whyPhrase(call, results[call.ID]); phrase != "" {
				actions = append(actions, phrase)
			}
		}
	}

	var out strings.Builder
	for _, line := range strings.Split(asked, "\n") {
		out.WriteString("> " + line + "\n")
	}
	out.WriteString("\n")
	if len(actions) == 0 {
		out.WriteString("No tools ran: that turn was an answer, not a change.")
		return out.String()
	}
	out.WriteString(strings.Join(actions, ", "))
	return out.String()
}

// whyPhrase reads one call back as the thing it did, with its result's verdict
// where the result carries one.
//
// The verdicts are read from the tool's own wire text (bare/tools.go) rather
// than from an error flag, because the transcript keeps the text and not the
// flag. Only the three tools whose success sentence is fixed by pi's source get
// one — bash, edit, write — and every other call is reported without a claim
// about how it went. An unearned "(ok)" beside a read is worse than nothing:
// it is the fluent reconstruction this whole call exists to avoid.
func whyPhrase(call ai.ToolCall, result string) string {
	arguments := whyArgumentsOf(call)
	switch call.Function.Name {
	case "edit":
		path := arguments.String("path")
		if path == "" {
			return "edited a file"
		}
		if !strings.HasPrefix(result, "Successfully replaced") && result != "" {
			return "tried to edit " + path + " (failed)"
		}
		added, removed, replacements := editDiffstat(call)
		if replacements == 0 {
			return "edited " + path
		}
		return fmt.Sprintf("edited %s (+%d −%d via %d replacement%s)",
			path, added, removed, replacements, plural(replacements))
	case "write":
		path := arguments.String("path")
		if path == "" {
			return "wrote a file"
		}
		if !strings.HasPrefix(result, "Successfully wrote") && result != "" {
			return "tried to write " + path + " (failed)"
		}
		return "wrote " + path
	case "bash":
		command := firstLine(arguments.String("command"))
		if command == "" {
			return "ran a command"
		}
		phrase := "ran " + clip(command, whyCommandLimit)
		if result == "" {
			return phrase
		}
		if bashFailed(result) {
			return phrase + " (failed)"
		}
		return phrase + " (ok)"
	case "read":
		return withTarget("read", arguments.String("path"))
	case "ls":
		return withTarget("listed", arguments.String("path"))
	case "grep":
		return withTarget("searched for", arguments.String("pattern"))
	case "find":
		return withTarget("looked for files matching", arguments.String("pattern"))
	case "jobs":
		return withTarget("jobs", arguments.String("action"))
	case "note":
		return withTarget("remembered", arguments.String("fact"))
	case "forget":
		return withTarget("forgot", arguments.String("pattern"))
	default:
		// A tool this build does not know — the workforce verbs, when they
		// attach — is still a thing that happened, and the name is the honest
		// whole of what can be said about it here.
		return call.Function.Name
	}
}

func withTarget(verb, target string) string {
	target = firstLine(target)
	if target == "" {
		return verb
	}
	return clip(verb+" "+target, hintLimit)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// editDiffstat counts the lines one edit call replaced, from the call's own
// arguments.
//
// It counts the ARGUMENTS, not the file: the edit already happened, the file on
// disk has moved on, and re-reading it would answer a different question. Every
// line of an oldText is a line that went and every line of a newText is a line
// that came, which is the same arithmetic a diffstat does for a replacement
// block and is exact for the edits pi's tool actually applies.
func editDiffstat(call ai.ToolCall) (added, removed, replacements int) {
	var parsed struct {
		Edits []struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		} `json:"edits"`
		// The legacy single-replacement shape pi still accepts (bare/tools.go).
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &parsed); err != nil {
		return 0, 0, 0
	}
	for _, edit := range parsed.Edits {
		removed += lineCount(edit.OldText)
		added += lineCount(edit.NewText)
		replacements++
	}
	if parsed.OldText != "" || parsed.NewText != "" {
		removed += lineCount(parsed.OldText)
		added += lineCount(parsed.NewText)
		replacements++
	}
	return added, removed, replacements
}

// lineCount is how many lines a replacement block is. Empty is zero lines — a
// deletion adds nothing — and text with no trailing newline still counts its
// last line.
func lineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}

// bashFailed reads pi's own status footer. bash appends exactly one of these
// lines when a command did not succeed (bare/tools.go:appendStatus), after any
// truncation footer, so the last non-empty line is where the verdict is.
func bashFailed(result string) bool {
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "Command exited with code") ||
			strings.HasPrefix(line, "Command timed out after") ||
			line == "Command aborted"
	}
	return false
}

// whyArguments is one call's arguments decoded once, for the fields the phrases
// read. Arguments that do not parse decode to nothing rather than to an error:
// a malformed call still happened, and the phrase degrades to the bare verb.
type whyArguments map[string]json.RawMessage

func (w whyArguments) String(field string) string {
	raw, present := w[field]
	if !present {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(value)
}

func whyArgumentsOf(call ai.ToolCall) whyArguments {
	var decoded whyArguments
	if err := json.Unmarshal([]byte(call.Function.Arguments), &decoded); err != nil {
		return nil
	}
	return decoded
}
