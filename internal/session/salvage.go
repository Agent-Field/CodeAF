package session

// A tool call the output limit cut in half is the most expensive failure this
// loop has. The model streamed thousands of tokens of arguments — all billed,
// all already in the transcript — and the old answer was to hand the tool a
// half-sent JSON string, let its parser say "unexpected end of JSON input",
// and watch the model pay the whole bill again rewriting from the top. For a
// large write that doubles the dearest call of the turn and teaches nothing:
// the error names the encoding, not the cause, so the model cannot even choose
// a smaller shape the second time.
//
// This file is the salvage yard. When the LAST call of a step arrives with
// arguments that are not valid JSON — only the last can be cut; a provider
// truncates the tail, never the middle — the loop asks two questions here.
// First: is this a clean truncation (a strict prefix of well-formed arguments)
// rather than genuine garbage? Second: is it a write whose severed content can
// land on disk exactly as the model meant it, up to the last complete line?
//
// A salvageable write gets its arguments REPAIRED — the parsed path, the
// recovered content trimmed to its final newline, the append flag if one made
// it through — and then runs through the ordinary pipeline: the approval gate
// sees the real bytes, the surface renders the real call, the transcript
// records arguments that actually parse. What changes is only the result the
// model reads back: instead of "Successfully wrote", it is told the cut
// happened, what landed, where the file now ends, and that ONE append call
// finishes the job. The retry then costs the tail, not the file.
//
// Every other tool keeps its refusal — a severed bash command or edit must
// never run on guessed arguments, and their parse-first Execute already
// guarantees nothing does — but the refusal's wording becomes the cause and
// the way out, because "Invalid arguments" was the one sentence that could
// not help.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// writeSalvageContinuations bounds how many severed writes one turn may
// salvage. It is larger than truncationContinuations because a salvage is not
// a retry: every one lands new content on disk, so a file several times the
// output ceiling legitimately arrives as a chain of cut-and-append steps. What
// the bound exists for is the model that ignores the append instruction and
// restarts the file from the top each time — the same prefix landing forever —
// and four salvages is room for the biggest honest file while capping that
// loop at a known spend.
const writeSalvageContinuations = 4

// severance is one severed call's disposition, decided before the batch runs
// and applied to its result after. index is the call's position in the batch
// (always the last — the provider cuts tails); the strings are the three ways
// the result can be reshaped, and exactly one of them is used per outcome.
type severance struct {
	index   int
	rewrote bool
	// note replaces the whole result of a salvaged write that succeeded: the
	// model must read what landed and how to continue, not "Successfully
	// wrote" for a file it believes is whole.
	note string
	// aside is appended to a result that already says something the model
	// must not lose — a refusal, a disk error, or the success of a write
	// whose content had in fact fully arrived.
	aside string
	// fallback replaces the result when nothing could be salvaged: the old
	// parser error, reworded into cause and remedy.
	fallback string
}

// amend reshapes the severed call's result. It runs after the batch so the
// approval gate and the tool itself have had their ordinary say.
func (s *severance) amend(result toolResult) toolResult {
	if !s.rewrote {
		return toolResult{text: s.fallback, isError: true}
	}
	if result.isError {
		// The write was refused or failed on disk. That answer stands — the
		// aside only adds that the call had been cut, so a model retrying
		// after an approval knows it is retrying a partial.
		result.text += "\n\n" + s.aside
		return result
	}
	if s.note != "" {
		return toolResult{text: s.note}
	}
	result.text += "\n\n" + s.aside
	return result
}

// considerSeverance inspects the batch's last call and, when its arguments
// were cut, decides what the loop should do about it. It may rewrite that
// call's arguments in place — which is why it runs BEFORE the assistant
// message is recorded: the transcript and the tool must see the same repaired
// bytes. A nil return means the batch is ordinary.
func (a *Agent) considerSeverance(calls []ai.ToolCall, end store.EndKind, salvaged *int) *severance {
	if len(calls) == 0 {
		return nil
	}
	index := len(calls) - 1
	last := &calls[index]
	raw := last.Function.Arguments
	if json.Valid([]byte(raw)) {
		return nil
	}
	cut := end == store.EndLength
	cause := "mid-stream"
	if cut {
		cause = "at the output limit"
	}
	name := last.Function.Name
	if name != "write" {
		// Anything but a write must not run on repaired guesses — a command
		// or an edit missing its tail is a different action, not a shorter
		// one. Execution is already safe (every tool parses before it acts,
		// so severed arguments refuse themselves); only the wording is owed.
		// And without the provider saying "length", invalid JSON on some
		// other stop is not a cut we can name, so the tool's own error keeps
		// the floor.
		if !cut {
			return nil
		}
		remedy := "Retry it with smaller arguments."
		if name == "edit" {
			remedy = "Split the change into several smaller edit calls, or rebuild the file in parts with write and append:true."
		}
		return &severance{
			index: index,
			fallback: fmt.Sprintf(
				"This %s call was cut off %s before its arguments finished; nothing was run. %s",
				name, cause, remedy),
		}
	}
	unsalvageable := &severance{
		index: index,
		fallback: fmt.Sprintf(
			"This write was cut off %s before enough of it arrived to save; nothing was written. "+
				"Send the file in parts: one write with the first portion, then write calls with append:true for the rest.",
			cause),
	}
	recovered, ok := analyzeSeveredWrite(raw)
	if !ok {
		if !cut {
			return nil
		}
		return unsalvageable
	}
	if *salvaged >= writeSalvageContinuations {
		return unsalvageable
	}
	repaired, ok := recovered.arguments()
	if !ok {
		if !cut {
			return nil
		}
		return unsalvageable
	}
	*salvaged++
	last.Function.Arguments = repaired
	return recovered.severance(index, cause)
}

// severance builds the result reshaping for a salvaged write, with the line
// counts and the file's new tail spelled out so the continuation needs no
// read call to find its footing.
func (w severedWrite) severance(index int, cause string) *severance {
	if w.complete {
		// Only the closing braces were cut: the write the model meant is the
		// write that ran, whole. Its success line stands; the aside stops
		// the model from re-sending a file that is already finished.
		return &severance{
			index:   index,
			rewrote: true,
			aside: fmt.Sprintf(
				"Your reply was cut off %s just after this write's content finished — the whole content was saved and nothing is missing. Do not resend it.",
				cause),
		}
	}
	lines := strings.Count(w.content, "\n")
	landing := fmt.Sprintf("%s now holds the %d complete line%s that arrived", w.path, lines, plural(lines))
	if w.appendMode {
		landing = fmt.Sprintf("the %d complete line%s that arrived were appended to %s, which", lines, plural(lines), w.path)
	}
	return &severance{
		index:   index,
		rewrote: true,
		note: fmt.Sprintf(
			"Saved what arrived: this write was cut off %s partway through its content. %s ends with:\n\n%s\n\n"+
				"The partial line at the cut was dropped. Continue the file with one write shaped "+
				"{\"path\": %q, \"append\": true, \"content\": ...} carrying ONLY the rest — "+
				"do not resend what is already saved, and do not start the file again.",
			cause, landing, savedTail(w.content), w.path),
		aside: fmt.Sprintf(
			"This write had been cut off %s; only the %d complete line%s that arrived were attempted.",
			cause, lines, plural(lines)),
	}
}

// savedTail renders the last few saved lines for the continuation note — enough
// for the model to recognise exactly where the file stops, small enough that
// the note never rivals the content it is about.
func savedTail(content string) string {
	trimmed := strings.TrimRight(content, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	for i, line := range lines {
		lines[i] = clip(line, 120)
	}
	return strings.Join(lines, "\n")
}

// severedWrite is what a cut write call still carried, recovered from the
// prefix that arrived.
type severedWrite struct {
	path       string
	content    string
	appendMode bool
	// complete marks the case where content's value had fully closed and the
	// cut only took the trailing braces: nothing of the file is missing.
	complete bool
}

// arguments re-marshals the recovered call into arguments that parse, in the
// same shape the schema teaches (path, then append, then content).
func (w severedWrite) arguments() (string, bool) {
	repaired, err := json.Marshal(struct {
		Path    string `json:"path"`
		Append  bool   `json:"append,omitempty"`
		Content string `json:"content"`
	}{w.path, w.appendMode, w.content})
	if err != nil {
		return "", false
	}
	return string(repaired), true
}

// analyzeSeveredWrite walks the severed arguments token by token and decides
// whether they are a clean prefix of a write call worth landing. The rules
// are conservative on purpose, each guarding a way a salvage could write the
// wrong thing:
//
//   - The path must have arrived WHOLE. A truncated path is a different file;
//     nothing is ever written to a guessed name.
//   - The cut must have fallen inside (or after) content's own value. A cut
//     anywhere earlier means there is nothing to save.
//   - A severed content keeps only its complete lines: the fragment is
//     unescaped and trimmed back to its final newline, so the file on disk
//     never ends mid-line and the continuation starts on a clean boundary.
//   - Anything that is not truncation-shaped — arrays, nested objects, syntax
//     garbage — is refused, and the ordinary error path keeps the call.
func analyzeSeveredWrite(raw string) (severedWrite, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var (
		out         severedWrite
		depth       int
		expectValue bool
		pendingKey  string
		texts       = map[string]string{}
		flags       = map[string]bool{}
		lastOffset  int64
		terminal    error
	)
	for {
		// The offset is taken BEFORE each Token call: when the call fails,
		// everything from here — separators included — is the unparsed
		// remainder the fragment recovery below reads.
		lastOffset = decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			terminal = err
			break
		}
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{':
				depth++
				if depth > 1 {
					return out, false
				}
				expectValue = false
			case '}':
				depth--
			default:
				return out, false
			}
		case string:
			if depth != 1 {
				return out, false
			}
			if expectValue {
				texts[pendingKey] = value
				expectValue = false
			} else {
				pendingKey = value
				expectValue = true
			}
		case bool:
			if depth != 1 || !expectValue {
				return out, false
			}
			flags[pendingKey] = value
			expectValue = false
		default:
			// A number or null value is not this schema, but tolerating it
			// costs nothing: it is parsed past and ignored.
			if depth != 1 || !expectValue {
				return out, false
			}
			expectValue = false
		}
	}
	if !truncationShaped(terminal) {
		return out, false
	}
	path, hasPath := texts["path"]
	if !hasPath || strings.TrimSpace(path) == "" {
		return out, false
	}
	out.path = path
	out.appendMode = flags["append"]
	if whole, arrived := texts["content"]; arrived {
		out.content = whole
		out.complete = true
		return out, true
	}
	// The content value itself was mid-flight. The remainder must read as
	// `: "` (separators intact, because the offset predates them) followed by
	// the fragment the cut ended.
	if !expectValue || pendingKey != "content" || int(lastOffset) > len(raw) {
		return out, false
	}
	rest := strings.TrimLeft(raw[lastOffset:], " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return out, false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return out, false
	}
	fragment, ok := unescapeFragment(rest[1:])
	if !ok {
		return out, false
	}
	newline := strings.LastIndexByte(fragment, '\n')
	if newline < 0 {
		// A single unfinished line saves nothing worth an extra round trip.
		return out, false
	}
	out.content = fragment[:newline+1]
	if strings.TrimSpace(out.content) == "" {
		return out, false
	}
	return out, true
}

// truncationShaped separates "the input simply stopped" from "the input was
// wrong": only the former is a cut this file may act on. Decoder.Token says
// the first as io.ErrUnexpectedEOF (or a clean io.EOF when the cut landed
// exactly on a token boundary); a *json.SyntaxError anywhere else is a model
// producing malformed arguments, which keeps the ordinary error path.
func truncationShaped(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return strings.Contains(syntax.Error(), "unexpected end of JSON input")
	}
	return false
}

// unescapeFragment turns the raw inside of a cut-off JSON string back into
// text. The tail may end mid-escape (`\`, `\u00`), so it retries with one
// more byte trimmed until the fragment closes cleanly; six bytes bounds the
// longest escape form (\uXXXX).
func unescapeFragment(fragment string) (string, bool) {
	for trim := 0; trim <= 6 && trim <= len(fragment); trim++ {
		candidate := fragment[:len(fragment)-trim]
		var text string
		if err := json.Unmarshal([]byte(`"`+candidate+`"`), &text); err == nil {
			return text, true
		}
	}
	return "", false
}
