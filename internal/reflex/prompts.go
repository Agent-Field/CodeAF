package reflex

import (
	"fmt"
	"strconv"
	"strings"
)

// The prompts, and what they have in common.
//
// Each is written for a model that is small, fast and not clever: short
// imperative sentences, the answer's shape spelled out as a literal object, and
// worked examples — because a near-free model copies a shape far more reliably
// than it follows a description of one. Each ends by demanding ONE JSON object
// and forbidding everything else by name.
//
// THE SCOPE WORDS ARE INTERPOLATED FROM THE VALIDATOR'S OWN LIST (reflex.go). A
// prompt that named its words in prose would be a second spelling of them.

// repairInstruction is the whole of the one retry.
const repairInstruction = "Reply with only the JSON object."

// stateRules is the half of the instruction both prompts share: what the state
// card is told about an exchange.
const stateRules = `Add state ONLY when the exchange MOVED THE WORK — the goal changed, something finished, something started, the next step changed, a question opened, or a file or link became the thing being worked on. An exchange that only answered a question moved nothing and has no state. state is {"goal": "...", "done": [...], "inflight": [...], "next": [...], "open": [...], "refs": [...]} with short lines.`

// statePrompt is the call when the conversation may not keep anything on its
// own — the person turned automatic saving off — and only the state card is
// asked about.
const statePrompt = `You read one exchange between a person and an assistant and answer what it did to the work.

` + stateRules + `

If KEPT lines are provided, used is an array of their numbers actually applied in the assistant answer. Never count a line merely present in context. contradicted lists KEPT numbers explicitly corrected by the PERSON, never by the assistant alone.

Answer with ONE JSON object and nothing else. No prose, no code fence.

{"used": [], "state": {...}} or {}

USER:
what does this regex do

ASSISTANT:
it matches an ISO date at a word boundary.

{}

USER:
the import script is done and green, next is the migration

ASSISTANT:
good — I will start on the migration.

{"state":{"goal":"move the data across","done":["import script, tests green"],"inflight":[],"next":["the migration"],"open":[],"refs":[]}}`

// noticePrompt is the call that may also keep one line.
var noticePrompt = fmt.Sprintf(noticeTemplate, strings.Join(memoryScopes, " | "))

const noticeTemplate = `You read one exchange between a person and an assistant. Answer two things.

FIRST, keep: did the PERSON say something worth remembering in every future conversation? Only these count: a preference ("I like short answers"), a standing rule ("always run the tests first", "never touch the public API"), a correction of the assistant, a decision they made, or a lasting fact about them, their machine or this project. It must be TIMELESS: anything tied to a time or an event ("remind me at six", "every Monday", "when CI goes red") is not a memory — leave it out. Never keep what only the assistant said, the work in progress, code, or a question. Most exchanges keep nothing: keep is then "".

durability is one of standing_rule, decision, durable_fact, project_state, ephemeral. Ephemeral requests, progress chatter, and one-off instructions belong only in the transcript: keep must be empty. Lasting project state may be kept as project_state; prefer state for ordinary progress. Progress paragraphs about landed/passed/verified work, versions or issue IDs are project_state even if phrased with prefer, always or a decision verb; these words do not make status a standing rule. used is an array of KEPT line numbers actually applied in the ASSISTANT answer, never merely present or echoed. Use [] when none applied. contradicted lists KEPT numbers the PERSON explicitly corrected, even if the replacement was already saved. Never infer contradiction from the assistant alone.

Write keep as one short line in the person's own terms. Normalize wording without adding a detail or interpretation from the assistant. Keep every condition and the original mandatory, prohibited or preferred force; never soften must/only/never into may/prefer. An explicit acceptance such as "yes, use that" can adopt the agreed proposal; silence does not accept extra assistant suggestions.

scope is one of: %s. Use project unless the person said it holds everywhere or for all their work (user), or only for this machine (env).

KEPT lists the lines already remembered here, numbered. If one of them already says it, keep is "". If the new line corrects or updates one of them, set replaces to that line's number; otherwise replaces is 0. A narrower exception coexists with the broader rule: do not replace it unless the person actually revokes that rule.

SECOND, state: ` + stateRules + `

Answer with ONE JSON object and nothing else. No prose, no code fence.

{"keep": "..." or "", "scope": "...", "durability": "durable_fact", "used": [], "contradicted": [], "replaces": 0, "state": {...} or omitted}

USER:
what does this regex do

ASSISTANT:
it matches an ISO date at a word boundary.

KEPT:
(nothing yet)

{"keep":"","scope":"project","replaces":0}

USER:
stop explaining things back to me, just make the change — that goes for every project

ASSISTANT:
understood — I will make the change and say what I changed.

KEPT:
1. uses tabs, not spaces

{"keep":"wants the change made directly, not an explanation first","scope":"user","replaces":0}

USER:
For Quartz reports, the legend must appear only when multiple series are plotted; never smooth raw measurements.

ASSISTANT:
I will also rotate labels by 45 degrees to make them easier to read.

KEPT:
(nothing yet)

{"keep":"Quartz reports require a legend only when multiple series are plotted; never smooth raw measurements","scope":"project","durability":"standing_rule","replaces":0}

USER:
actually we deploy on Tuesdays now, not Fridays

ASSISTANT:
noted — Tuesdays from now on.

KEPT:
1. deploys on Fridays
2. the staging database is read-only

{"keep":"deploys on Tuesdays","scope":"project","replaces":1}

USER:
remind me at 6 to check the build

ASSISTANT:
I can set that up as a reminder.

KEPT:
(nothing yet)

{"keep":"","scope":"project","replaces":0}`

// noticeInput is the exchange, each side clipped on its own so a long answer
// cannot push the person's own words out of the prompt, followed by the kept
// lines when the call may keep one.
func noticeInput(userMsg, assistantMsg string, kept []string, askMemory bool) string {
	input := "USER:\n" + clip(userMsg, messageLimit) +
		"\n\nASSISTANT:\n" + clip(assistantMsg, messageLimit)
	if !askMemory && len(kept) == 0 {
		return input
	}
	var lines strings.Builder
	lines.WriteString("\n\nKEPT:\n")
	if len(kept) == 0 {
		lines.WriteString("(nothing yet)")
	}
	for index, line := range kept {
		if index > 0 {
			lines.WriteString("\n")
		}
		lines.WriteString(strconv.Itoa(index+1) + ". " + clip(line, messageLimit))
	}
	return input + lines.String()
}

// clip bounds one piece of text, in runes, marking the cut.
func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
