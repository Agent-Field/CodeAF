package session

// write, wrapped: pi's tool with one optional argument, the same shape bash
// got its background flag by (tools_jobs.go). Without append the call is
// handed to bare verbatim — same queueing, same wording, same wire text. With
// append the new content lands after what the file already holds.
//
// The flag exists for one flow above all: a large write the output limit cut
// in half is salvaged up to its last complete line (salvage.go), and the
// continuation the model is asked for must be able to say "add this to the
// end" without re-sending — and re-billing — everything that already landed.
// It is a belt wrapper rather than a change to bare because bare's schemas
// are pinned verbatim to pi's source, and a subharness leaf should keep
// getting exactly pi's write.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// appendSentence is what the wrapper adds to pi's write description. The
// ordering instruction is load-bearing: salvage can only recover a cut call
// whose append flag had already streamed, so the flag must come before the
// content it qualifies.
const appendSentence = " Set append:true to add the content to the END of the file instead of replacing it (state append before content in the arguments). Write very large files in parts: a first write, then append:true for the rest. If a large write is ever cut off mid-content, the complete lines that arrived are saved and the result tells you exactly how to continue with append — never resend what was already saved."

// appendProperty is the one property the wrapper adds to pi's write schema.
const appendProperty = `"append":{"type":"boolean","description":"Add the content to the end of the file instead of replacing it (default: false)"},`

// schemaWithAppend inserts the append property into pi's write schema by
// textual surgery rather than a map round-trip, because the ORDER of the
// properties is part of the design: models overwhelmingly emit arguments in
// schema order, and append must stream before content for a cut call to
// remain salvageable (salvage.go). Go's map marshalling would alphabetise
// path to the back. A schema without the expected anchor falls back to the
// original — append is then unsupported on the wire, a smaller failure than a
// malformed schema failing the whole belt.
func schemaWithAppend(schema json.RawMessage) json.RawMessage {
	const anchor = `"content":{`
	text := string(schema)
	at := strings.Index(text, anchor)
	if at < 0 {
		return schema
	}
	return json.RawMessage(text[:at] + appendProperty + text[at:])
}

// appendableWrite wraps bare's write: the same tool, with one optional
// argument.
func (a *Agent) appendableWrite(inner bare.Tool) bare.Tool {
	return bare.Tool{
		Name:        inner.Name,
		Description: inner.Description + appendSentence,
		Schema:      schemaWithAppend(inner.Schema),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				Append  bool   `json:"append"`
			}
			// A call whose arguments do not parse belongs to bare: it owns
			// the wording of every other write error, and a second parser
			// reporting the same fault in different words helps nobody.
			if err := decodeToolArguments(args, &parsed); err != nil || !parsed.Append {
				return inner.Execute(ctx, args)
			}
			if strings.TrimSpace(parsed.Path) == "" {
				return "Invalid arguments: path is required", true, nil
			}
			// The current bytes are read here and the combined result is
			// handed to the inner tool, so the write itself keeps pi's whole
			// contract — the mutation queue, the parent directories, the
			// wording of every disk error. A file that does not exist yet
			// appends onto nothing, which makes append safe to reach for
			// without checking first.
			existing, err := os.ReadFile(bare.ResolvePath(parsed.Path, a.config.Workspace))
			if err != nil && !os.IsNotExist(err) {
				return "Error reading the file to append to: " + err.Error(), true, nil
			}
			combined, err := json.Marshal(struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}{parsed.Path, string(existing) + parsed.Content})
			if err != nil {
				return "Error preparing the append: " + err.Error(), true, nil
			}
			text, isError, execErr := inner.Execute(ctx, combined)
			if execErr != nil || isError {
				return text, isError, execErr
			}
			// The success line speaks in lines rather than bytes because
			// lines are the unit a continuation reasons in — the salvage
			// note counts them the same way (salvage.go).
			added := lineTally(parsed.Content)
			total := lineTally(string(existing) + parsed.Content)
			return fmt.Sprintf("Appended %d line%s to %s; the file now has %d line%s.",
				added, plural(added), parsed.Path, total, plural(total)), false, nil
		},
	}
}

// lineTally counts the lines a text holds: every newline closes one, and a
// trailing unterminated fragment still counts as a line someone will read.
func lineTally(text string) int {
	if text == "" {
		return 0
	}
	lines := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		lines++
	}
	return lines
}
