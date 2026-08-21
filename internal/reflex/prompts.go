package reflex

import (
	"fmt"
	"strings"
)

// The three prompts, and what they have in common.
//
// Each is written for a model that is small, fast and not clever: short
// imperative sentences, the answer's shape spelled out as a literal object, and
// two or three worked examples — because a near-free model copies a shape far
// more reliably than it follows a description of one. Each ends by demanding
// ONE JSON object and forbidding everything else by name; the fence and the
// preamble are the two things a small model adds unasked, so they are refused
// in the prompt as well as tolerated in the parser.
//
// THE ENUMS ARE INTERPOLATED FROM THE VALIDATOR'S OWN LISTS (reflex.go). A
// prompt that named its words in prose would be a second spelling of them, and
// the day the two disagreed every call would fail its enum check and look like
// a bad model.

// repairInstruction is the whole of the one retry. It is four words because the
// failure it fixes is a model that wrote the right answer with something around
// it, and a longer instruction on a small model is a second chance to be
// creative.
const repairInstruction = "Reply with only the JSON object."

const routePrompt = `You are the memory router for a chat. You see the person's new message and an index of what the chat already remembers about them. Answer which remembered lines belong in this turn.

Inject an id ONLY when the assistant would answer differently for having read it. Most turns need nothing, and an empty list is the normal answer — injecting a line that merely sounds related makes the assistant repeat things nobody asked about.

Set cmd only when the person plainly commanded memory itself: "remember that ...", "forget the ...". Never infer a command from ordinary talk about the past.

Answer with ONE JSON object and nothing else. No prose, no code fence.

{"inject": ["id", ...], "cmd": {"name": "remember" or "forget", "arg": "what to remember or forget"} or null}

MESSAGE:
when is standup again

INDEX:
- m3: standup is at 9:15 [fact/project]
- m7: prefers dark themes [preference/user]

{"inject":["m3"],"cmd":null}

MESSAGE:
write me a haiku about rain

INDEX:
- m3: standup is at 9:15 [fact/project]
- m7: prefers dark themes [preference/user]

{"inject":[],"cmd":null}

MESSAGE:
remember that I deploy on Fridays

INDEX:
- m3: standup is at 9:15 [fact/project]

{"inject":[],"cmd":{"name":"remember","arg":"deploys on Fridays"}}`

// extractPrompt is built rather than written because its two enums are the
// validator's lists.
var extractPrompt = fmt.Sprintf(extractTemplate,
	strings.Join(memoryTypes, " | "), strings.Join(memoryScopes, " | "))

const extractTemplate = `You read one exchange between a person and an assistant and answer whether anything in it is worth remembering after this session ends.

Set mem to 1 ONLY for something durable: a preference, a fact about the person or their machine, a decision they made, a correction they gave you, or where a piece of work now stands. Set mem to 0 for everything else — questions, answers, code, chatter, anything true only for the next ten minutes. Most exchanges are 0.

type is one of: %s
scope is one of: %s
title is at most six words. text is one line, in the person's own terms.

Add state ONLY when the exchange MOVED THE WORK — the goal changed, something finished, something started, the next step changed, a question opened, or a file or link became the thing being worked on. An exchange that only answered a question moved nothing and has no state.

When a REMEMBERED section is given, also answer which of those memory ids actually bore on the answer — used. A line bore on the answer if the assistant would have answered differently without it. Copy the ids exactly. Most lines bore on nothing, and an empty list is the normal answer. Omit used when there is no REMEMBERED section.

Answer with ONE JSON object and nothing else. No prose, no code fence.

{"mem": 0 or 1, "type": "...", "scope": "...", "title": "...", "text": "...", "tags": ["..."], "used": ["id", ...], "state": {"goal": "...", "done": ["..."], "inflight": ["..."], "next": ["..."], "open": ["..."], "refs": ["..."]} or omitted}

USER:
what does this regex do

ASSISTANT:
it matches an ISO date at a word boundary.

{"mem":0}

USER:
stop explaining things back to me, just make the change

ASSISTANT:
understood — I will make the change and say what I changed.

{"mem":1,"type":"preference","scope":"user","title":"wants changes made not explained","text":"Prefers the change made directly, with a short note of what changed, rather than an explanation first.","tags":["style"]}

USER:
what time is standup

ASSISTANT:
standup is at 9:15.

REMEMBERED:
- m3: standup is at 9:15
- m7: prefers dark themes

{"mem":0,"used":["m3"]}

USER:
the import script is done and green, next is the migration

ASSISTANT:
good — I will start on the migration.

{"mem":1,"type":"project_state","scope":"project","title":"import done migration next","text":"The import script is finished and its tests pass; the migration is the next piece of work.","tags":["import","migration"],"state":{"goal":"move the data across","done":["import script, tests green"],"inflight":[],"next":["the migration"],"open":[],"refs":[]}}`

// decidePrompt is built for the same reason: its four operations are the
// validator's list.
var decidePrompt = fmt.Sprintf(decideTemplate, strings.Join(decideOps, " | "))

const decideTemplate = `You are given one candidate memory and the lines already stored nearest to it. Answer what to do with the candidate.

op is one of: %s

skip — a stored line already says this. Nothing is written.
update — a stored line says this less precisely, or with less of it. Give its id and the merged line.
supersede — a stored line CONTRADICTS this: it was true and is not any more. Give its id and the new line.
add — nothing near it says this. It is new.

Prefer skip over add when you are unsure: a second copy of a fact is worse than a fact remembered once.

Answer with ONE JSON object and nothing else. No prose, no code fence.

{"op": "...", "target_id": "id for update or supersede, else empty", "title": "...", "text": "...", "tags": ["..."]}

CANDIDATE: prefers dark themes — Uses a dark theme everywhere.
NEIGHBOURS:
- m7: prefers dark themes — Uses a dark theme in the editor and the terminal.

{"op":"skip","target_id":"","title":"","text":"","tags":[]}

CANDIDATE: deploys on Tuesdays — Deploys are on Tuesday mornings now.
NEIGHBOURS:
- m2: deploys on Fridays — Deploys go out on Friday afternoons.

{"op":"supersede","target_id":"m2","title":"deploys on Tuesdays","text":"Deploys go out on Tuesday mornings.","tags":["release"]}

CANDIDATE: runs postgres 16 locally — The local database is postgres 16.
NEIGHBOURS:
- m5: prefers dark themes — Uses a dark theme in the editor and the terminal.

{"op":"add","target_id":"","title":"runs postgres 16 locally","text":"The local database is postgres 16.","tags":["env"]}`

// ── what the model is shown ─────────────────────────────────────────────────

// routeInput is the router's user message: what was typed, and the index it may
// choose from.
func routeInput(userMsg string, index []Stub) string {
	return "MESSAGE:\n" + clip(userMsg, messageLimit) + "\n\nINDEX:\n" + renderIndex(index)
}

// extractInput is the exchange, each side clipped on its own so a long answer
// cannot push the person's own words out of the prompt — and, on the turns that
// were shown something, the remembered lines whose usefulness is being asked
// about.
//
// THE SECTION IS ABSENT AND NOT EMPTY when nothing was injected. An empty
// heading reads to a small model like a list it failed to receive, and this one
// is asked about by name, so a model that saw the heading and no ids has been
// invited to invent some.
func extractInput(userMsg, assistantMsg string, injected []Stub) string {
	input := "USER:\n" + clip(userMsg, messageLimit) +
		"\n\nASSISTANT:\n" + clip(assistantMsg, messageLimit)
	lines := make([]string, 0, len(injected))
	for _, stub := range injected {
		id := strings.TrimSpace(stub.ID)
		if id == "" {
			// A stub with no id cannot be named back, so showing it can only
			// produce an answer that has to be dropped.
			continue
		}
		lines = append(lines, "- "+id+": "+strings.TrimSpace(stub.Title))
	}
	if len(lines) == 0 {
		return input
	}
	return input + "\n\nREMEMBERED:\n" + strings.Join(lines, "\n")
}

// decideInput is the candidate and the nearest lines already stored.
func decideInput(candidate ExtractResult, neighbors []Neighbor) string {
	var builder strings.Builder
	builder.WriteString("CANDIDATE: ")
	builder.WriteString(clip(strings.TrimSpace(candidate.Title), messageLimit))
	if text := strings.TrimSpace(candidate.Text); text != "" {
		builder.WriteString(" — ")
		builder.WriteString(clip(text, messageLimit))
	}
	if len(candidate.Tags) > 0 {
		builder.WriteString("\nTAGS: ")
		builder.WriteString(strings.Join(candidate.Tags, ", "))
	}
	builder.WriteString("\nNEIGHBOURS:\n")
	if len(neighbors) > neighborLimit {
		neighbors = neighbors[:neighborLimit]
	}
	written := 0
	for _, neighbor := range neighbors {
		id := strings.TrimSpace(neighbor.ID)
		if id == "" {
			continue
		}
		builder.WriteString("- " + id + ": " + strings.TrimSpace(neighbor.Title))
		if text := strings.TrimSpace(neighbor.Text); text != "" {
			builder.WriteString(" — " + clip(text, messageLimit))
		}
		builder.WriteString("\n")
		written++
	}
	if written == 0 {
		// Said in words rather than left blank: an empty heading reads to a
		// small model like a list it failed to receive, and the answer to "there
		// is nothing near it" is add, which is exactly what it should conclude.
		builder.WriteString("(nothing stored near it)\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

// renderIndex is the memory index as one line per stub — the id first, because
// the id is the only part the model has to copy back exactly.
func renderIndex(index []Stub) string {
	if len(index) > indexLimit {
		index = index[:indexLimit]
	}
	lines := make([]string, 0, len(index))
	for _, stub := range index {
		id := strings.TrimSpace(stub.ID)
		if id == "" {
			// A stub with no id cannot be injected, so showing it can only
			// produce an answer that has to be dropped.
			continue
		}
		line := "- " + id + ": " + strings.TrimSpace(stub.Title)
		kind, scope := strings.TrimSpace(stub.Type), strings.TrimSpace(stub.Scope)
		if kind != "" || scope != "" {
			line += " [" + kind + "/" + scope + "]"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "(nothing remembered yet)"
	}
	return strings.Join(lines, "\n")
}

// clip cuts text to a rune ceiling. Runes rather than bytes because a cut
// through a multi-byte character sends the provider a replacement glyph, and
// the ellipsis is there so the model can tell a clipped message from a short
// one and does not read a truncated sentence as a finished thought.
func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
