package shaped

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE OTHER SHAPE ──────────────────────────────────────────────────────────
//
// Every ask in this package until now wanted a JSON object and the failure it
// repaired was prose. There is one ask in the system that wants the opposite,
// and it is the most consequential ask there is: the deliverable — what a worker
// hands over at the end, in the words a person will read.
//
// A worker that answers THAT in an object has broken the same contract, in the
// same way, for the same reasons: it read a shape somewhere in its instructions
// and matched it instead of the one it was asked for. bench/deepswe
// textual-richlog-follow-state, nemotron-3.5-lightning n1 is the measured case —
// the leaf finished its work, changed six files, and handed over
// `{"contract": "Write the _log.py and _rich_log.py files …"}`, which is not an
// answer and not even a claim about one. The delivery gate then judged that
// object three times, the store recorded three refusals, and NO structured
// repair fired at all, because the one seam that knows how to say "answer in
// the shape asked" had never been pointed at this direction.
//
// So it is pointed at it here, and it repairs exactly as everything else does:
// one re-ask, carrying the model's own words back, journaled as a repair so a
// run that had to ask twice does not look like one that asked once.

// RepairReshaped: the answer came back as a data object where the ask wanted
// the work itself, and it was asked once more for the shape it was asked for.
const RepairReshaped RepairKind = "reshaped"

// ObjectShaped reports whether the WHOLE of an answer is one data object.
//
// It is a structural reading and never a reading of what the object says: the
// text, stripped of a code fence, opens on a brace, closes on its match, and
// parses. That is a fact about the answer's shape, which is what this repairs;
// what the object CONTAINS is the worker's business and none of this package's.
//
// It is deliberately stricter than provider.DecodeJSONObject, which finds an
// object anywhere inside a reply. An answer that explains something and happens
// to quote a JSON snippet is prose and must be left alone; only an answer that
// IS an object is the failure being repaired.
func ObjectShaped(text string) bool {
	trimmed := unfenced(strings.TrimSpace(text))
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal([]byte(trimmed), &object) == nil && len(object) > 0
}

// unfenced strips a whole-answer code fence, which is the wrapper a model puts
// around an object it thinks it was asked for.
func unfenced(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if newline := strings.IndexByte(text, '\n'); newline >= 0 {
		text = text[newline+1:]
	}
	if fence := strings.LastIndex(text, "```"); fence >= 0 {
		text = text[:fence]
	}
	return strings.TrimSpace(text)
}

// Prose is Answer's mirror: one repair for an answer that came back as a data
// object where the ask wanted the answer itself.
//
// It returns the answer to use and whether anything was repaired. An answer that
// was never object-shaped comes back untouched and unrepaired, which is every
// delivery in the system but the ones this exists for. A repair that could not
// be had — the model unreachable, an empty reply, a second object — comes back
// as the ORIGINAL, because a delivery whose shape could not be fixed is still
// the delivery and dropping it would lose the only account the run has.
//
// Both endings are journaled. A reshape that failed is exactly as interesting to
// an autopsy as one that worked, and rather more so: it is a model that would
// not answer in the shape asked twice running.
func Prose(ctx context.Context, client Completer, ask Ask, answered string) (string, bool) {
	if client == nil || !ObjectShaped(answered) {
		return answered, false
	}
	model := provider.CallFrom(ctx).Model()
	note(ctx, Repair{Lane: ask.Lane, Model: model, Kind: RepairReshaped, Round: 1})
	response, err := client.CompleteWithMessages(ctx, reshape(ask, answered))
	if err != nil {
		note(ctx, Repair{Lane: ask.Lane, Model: model, Kind: RepairFailed, Round: 1})
		return answered, false
	}
	again := strings.TrimSpace(text(response))
	if again == "" || ObjectShaped(again) {
		note(ctx, Repair{Lane: ask.Lane, Model: model, Kind: RepairFailed, Round: 1})
		return answered, false
	}
	return again, true
}

// reshape is that one re-ask. It quotes the object back for the reason reask
// does — a model told only "that was the wrong shape" has to guess which part
// of what it said was the problem — and it asks for the SUBSTANCE rather than
// for a reformatting, because an object whose one field is a description of
// work still has to become the work's account before anybody can read it.
//
func reshape(ask Ask, offending string) []ai.Message {
	contract := "Your answer came back as a data object. This ask wanted the answer itself, " +
		"in your own plain words, written for the person who asked for the work."
	contract += "\n\nThis is what you sent:\n" +
		clip(strings.TrimSpace(offending), offendingQuoteBytes) +
		"\n\nAnswer again, in the shape that was asked for: plain prose, no JSON, no code fence, no field names. " +
		"Say what was done, what it was checked against and what came back. " +
		"If the object above holds the substance, write that substance out as sentences; " +
		"if it holds only a description of what was to be done, say what you actually did instead."
	messages := make([]ai.Message, 0, len(ask.Messages)+1)
	messages = append(messages, ask.Messages...)
	return append(messages, ai.Message{Role: "user",
		Content: []ai.ContentPart{{Type: "text", Text: contract}}})
}
