package head

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Every deterministic recognizer in this package was written after a live
// failure, and each one answered its failure by learning more words. That road
// has no end: a person can always phrase "kill everything except the finance
// one" in a way no cue list anticipated. So this file stops constraining the
// utterance space and constrains the action space instead. The model is handed
// typed tools over the graph and nothing else — it can read the board, one
// job's whole result, a file that job actually wrote, and aforge's own manual;
// it can ask for one of five verbs against ids it read there; and it can write
// one durable line into the notebook. Every rule that
// makes a change safe lives inside the tools: the store's own legality table,
// the class path's unit rule, the surgery gates, the ordinary journalled
// commands, the same structured confirm question. A model that misreads the
// sentence therefore costs at most one confirmation question, never a silent
// wrong action, and the deterministic recognizers keep the fast path they have.

const (
	beltToolBoard    = "board"
	beltToolControl  = "control"
	beltToolSteer    = "steer"
	beltToolRevise   = "revise"
	beltToolExpedite = "expedite"
	// beltToolManual is the belt's only read that is not about the graph. It
	// rides here rather than in a loop of its own because the two questions
	// arrive in the same sentence often enough — "why did you cancel that?" is
	// about the board and about aforge at once — and a second loop would have
	// to guess which one to open.
	beltToolManual = "manual"
	// beltToolResult is the belt's answer to the same failure the deep slices
	// answer from the other side. Precomputed depth guesses which jobs a message
	// is about; this lets the model decide, after it has read the board and
	// knows which row the user meant. Both exist because the guess is free and
	// the decision is right.
	beltToolResult = "result"
	// beltToolPlan is the read that answers "what's the plan?" — the question
	// this belt could not answer at all. board says how many parts are running
	// and result says what a part came back with; neither says what the work was
	// broken into, which part waits on which, or where the thing is up to. That
	// structure has been journaled on every job's root since plans became
	// durable, and nothing in this package had ever opened it, so the head's
	// entire structural vocabulary was "N running, M queued".
	beltToolPlan = "plan"
	// beltToolRead finishes what result starts. result returns what a job said
	// about itself and the paths it wrote; when what was asked for is inside one
	// of those documents, result hands back a pointer and the loop used to stop
	// there — offering to fetch a file it had no way to open. artifact.go holds
	// the boundary this reads through.
	//
	// This read is deliberately never compressed, while a leaf's sh is: a leaf
	// reads to decide what to do next, and the head reads to quote. internal/rtk
	// shortens the first because the bytes are a means; it stays away from the
	// second because here the bytes are the answer.
	beltToolRead = "read"
	// beltToolCompetence, beltToolStanding and beltToolSpending are the three
	// reads that are about the employee rather than the work. Each was already
	// measured, journalled and rendered somewhere else — the competence map for
	// the router, the watch status for doctor, self-spend for the rail — and
	// each reached a conversation through a hardcoded list of substrings or not
	// at all. They are tools for the reason everything else here is a tool: the
	// question "am I asking too much of you lately?" matches no list anybody
	// will ever finish writing, and a model holding the read can recognise it.
	beltToolCompetence = "competence"
	beltToolStanding   = "standing"
	beltToolSpending   = "spending"
	// beltToolHistory is the read that makes time a dimension of this belt
	// rather than a word in it. Every other read is topical — board is BM25 over
	// the snapshot, result and read need an id — and "what did you do yesterday"
	// carries no topic and no id, only a window. It is also the only read that
	// reaches work a territory has packed away, because it queries the settled
	// rows directly rather than the compacted snapshot every other read sees.
	beltToolHistory = "history"
	// beltToolSearch is the read for a question about something that was SAID.
	//
	// Every other read here starts from work: a board row, an id, a settled
	// window. But the messages table had no index at all, so anything that never
	// became a job — a decision reached in conversation, a number quoted in
	// passing, a name that changed — was unreachable by every read in the
	// product, forever. The head saw the last ten to twenty lines and the prompt
	// forbade it from saying so, which made confident reconstruction of a
	// conversation that no longer existed the only sanctioned output.
	//
	// It searches the three places memory actually lives — the conversation, the
	// notebook, and folded jobs — because a person asking "what did we decide
	// about pricing?" has no idea which of the three holds the answer and should
	// not have to.
	beltToolSearch = "search"
	// beltToolSpawn is the tool that ends the split brain.
	//
	// Spawning used to be the TERMINAL decision of a router turn (targets.go's
	// ancestor, fanout.go) and the belt was explicitly forbidden from queuing —
	// so the head could read the board, or commission work, and never both in one
	// breath. Part 6 decision 2 moves the two things that made that terminal
	// position safe INTO the tool: the fan-out cap of six, and the consequence
	// gate that refuses to let anything irreversible ride a reflex.
	beltToolSpawn = "spawn"
	// beltToolWrite is the artifact law's door (12.5.1). Without it, "answer
	// inline" was the only route a deliverable had: session bd3c78ed's SVG was
	// authored into a reply, cut in half by an output cap, journaled unmarked,
	// and then unrecoverable because the only copy was the truncated one. A
	// document, a diagram, code or data is born on disk and referenced by path.
	beltToolWrite = "write"
	// beltToolAct is the head's own hands, and the only thing on this belt that
	// touches the world without going through the graph.
	//
	// Everything durable flows through spawn, which is right for work and was,
	// until this tool, the only route ANYTHING had. So a request measured in
	// seconds — open this file, list that directory, show me the top of that log
	// — either bought a whole job (compile, plan, workspace, three model calls,
	// the wrong directory) or came back as a refusal the voice law forbids and
	// that was said out loud anyway, because it was true. act.go holds the
	// boundary, which is time and consequence and never topic, and the floor
	// underneath it.
	beltToolAct = "act"
	// beltToolCorrect is the settled-work half of revision. revise edits the
	// remaining plan of a job still in flight; this hands a job that already
	// DELIVERED its own previous attempt plus what the user says is wrong with
	// it. correction.go holds the whole doctrine, including the line that says
	// the user outranks the predecessor's account of itself.
	beltToolCorrect = "correct"
	// beltToolRule changes a standing rule without touching the work: retire it,
	// hold it, retime it, reword it, put it back on probation. The verbs were a
	// cue vocabulary; the transitions they mapped onto are the store's own.
	beltToolRule = "rule"
	// beltToolService acts on the persistent things the person is running. They
	// are their own effects rather than graph work, which is why stopping them
	// is unconditional and why stopping EVERYTHING asks once about the jobs.
	beltToolService = "service"
	// beltToolCraft acts on a LEARNED WAY OF WORKING — a workflow the resident
	// distilled from jobs it has done and can run again. It is the rule tool's
	// shape for the other half of what the resident carries: rules are what it
	// watches for, crafts are how it does things.
	//
	// It exists because the three verbs a person has for one of these are all
	// sentences they say rather than pages they visit — "run the release notes
	// one", "that last version was worse, put it back", "stop doing it that way"
	// — and until it did, the only door was a page in a window, which made the
	// notebook's own verbs unreachable from the conversation that is supposed to
	// be the one mouth.
	beltToolCraft = "craft"
	// beltToolAnswerQuestion settles a worker's open question. Part 6 decision 1
	// is still open and this tool does not settle it: 12.1.4 found that every
	// existing AskQuestion producer is consent-bearing and none is labeled
	// informational, so the conservative default from 9.4 — unlabeled means
	// consent means escalate — is the ONLY behaviour this tool has today. It
	// exists so the head can put a worker's question to the person in its own
	// words instead of being unable to see it at all, and it refuses, in a
	// sentence the loop must speak to, every question it is not licensed to end.
	beltToolAnswerQuestion = "answer_question"
	// beltToolSay is the turn learning to talk while it is still working.
	//
	// Everything the head said used to be the LAST thing it did: one message, at
	// the end, after every read and every act. So "have a quick look and tell me
	// what you find" was answered by a silence the length of the looking, and a
	// turn that found something worth saying halfway through had two choices —
	// stop and say it, or carry on and say it in the past tense. This posts one
	// line into the room now and leaves the turn running. The final reply is
	// unchanged and still lands; this is the sentence in front of it.
	beltToolSay = "say"
	// beltToolForget is note's opposite, and it exists because the wave would
	// otherwise have been a net loss. The router carried a `retract` field: a
	// numbered notebook line the person had just said was untrue, quarantined
	// rather than superseded, because they were throwing a belief away rather
	// than giving you the new version of it. That field died with the router and
	// nothing replaced it, so the head could accumulate beliefs and never let one
	// go — which is the accumulation failure the consolidator then has to clean
	// up by guessing.
	beltToolForget = "forget"
	// beltToolAsk is the numbered question, kept as a mechanism rather than left
	// to prose. Every deterministic arm that resolved a referent could end in one
	// — "Which job do you mean?" with the candidates as durable options — and the
	// options are what the surfaces render as rows a person clicks (5.22: no
	// typed-only actions). A loop that could only ask in prose would have taken
	// that affordance away from every ambiguity in the product at once.
	//
	// It carries no action encoding on purpose. The answer is not applied by the
	// question machinery; it comes back to the loop as an ordinary turn with the
	// question and the choice both in the thread, which is the only party that
	// knows what the choice was FOR.
	beltToolAsk = "ask"
	// beltToolInterrupt is turn-cancel as something other than a keypress. It is
	// reachable from the loop today and from the journal the moment the one-arm
	// seam in 12.3.3 opens; interrupt.go holds both halves and the exact edit.
	beltToolInterrupt = "interrupt"
	// beltToolFork is 8.2.12: spawn, carrying the conversation. It is a variant
	// rather than a mechanism — fork.go composes a brief and calls spawn, so
	// every guard that makes commissioning safe applies without being restated —
	// and it exists because a person who has spent ten minutes settling what
	// they want and then says "go do it" has put the requirements in the chat.
	beltToolFork = "fork"
	// beltToolThread is 8.2.10's on-demand transcript read, and it is the one
	// amendment 5.7's knowledge contract takes: the head still never sees another
	// room's conversation AMBIENTLY — that would put every room in every prompt
	// and answer this room's question with that room's context — but it can now
	// go and READ one when the person refers to it. transcript.go holds the
	// bounds, the scope and the sanitizing, and the reason each one is there.
	beltToolThread = "thread"
	// beltToolNote is the belt's only write that never touches the graph. The
	// loop could change work and answer questions and had nowhere at all to put
	// a durable instruction about its own behaviour, so "always answer from the
	// result" was replied to warmly and recorded nowhere, and the next session
	// failed identically. It writes through the same fact machinery the router's
	// remember already uses: one fact_learned event, the same notebook.
	beltToolNote = "note"

	// beltConfirmAction and beltKeepAction ride the existing surgery option
	// codec, so a belt confirmation replays through exactly the durable
	// question path the class confirmations already use.
	beltConfirmAction = "belt"
	beltKeepAction    = "beltkeep"
)

const (
	// BoardRowCap bounds every board read. A board longer than this is a log,
	// not a board: the model reads it to choose a target, and a dozen live jobs
	// is already more than a person holds in their head at once.
	BoardRowCap = 12
	// beltControlIDCap bounds one control call. The unit rule collapses whole
	// subtrees into single ids, so a legitimate set is small; a longer list is
	// a model enumerating leaves it should have named by their root.
	beltControlIDCap = 32
	// beltManualSections is how much of the manual one read returns. The belt
	// allows four calls in total, so a read that hands back a whole chapter
	// spends the message's budget on prose the answer will not use.
	beltManualSections = 4
	// beltResultBytes bounds one result read. It is larger than a deep slice
	// because this read was chosen rather than guessed — the model spent a call
	// on this exact job — and it stays in the manual read's league because both
	// are one message's whole grounding.
	beltResultBytes = 4 << 10
	// beltNoteBytes bounds one notebook line. A durable preference that will not
	// fit in a sentence is not one preference, and the notebook is read into
	// every later prompt under a budget of its own.
	beltNoteBytes = 400
	// beltGroundingBytes bounds each of the three self-reads. They are rendered
	// elsewhere for surfaces with a whole pane to spend; here one read is one
	// answer's grounding and shares a message with the board, so it stays in the
	// manual read's league rather than the pane's.
	beltGroundingBytes = 2 << 10
	// beltSelfReceiptCap is how many recent self-work receipts one spending read
	// names. Past a handful this is a ledger, and the totals above it already
	// say what the ledger would.
	beltSelfReceiptCap = 5
	// beltSpendJobCap is how many jobs a windowed spending read names. The
	// question behind it — "what has been expensive lately?" — is answered by
	// the heaviest few and by the total above them; a full ledger is a different
	// question, and history is the read that answers it.
	beltSpendJobCap = 5
)

// beltTool and beltProp mirror the leaf toolbox's definition idiom. They are
// three lines each and unexported there, so they are restated rather than
// exported across a package boundary that has no other reason to open.
func beltTool(name, description string, properties map[string]any, required ...string) ai.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: description, Parameters: parameters,
	}}
}

func beltProp(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

// beltDefinitions are what the model sees. Each description is one sentence of
// interface plus its arguments, because policy is stated once in the prompt's
// judgment section and a description that repeats it is the same doctrine paid
// for twice on every turn. What survives here is only what changes the call
// itself: the person's words travel verbatim, ids come from a read, act's
// boundary, and ask ending the turn.
// beltReadOnly separates looking from acting. The turn's own budget no longer
// divides along it — one runaway bound covers the whole belt now — but the
// DELIVERY turn is armed from it (absorb.go): a turn woken by finished work has
// everything to find out and nothing left to do.
//
// The reads are named rather than the acts, and deliberately: the set of tools
// that change nothing is closed and every one of them is described as "always
// safe" in its own definition, while acts are added whenever the head grows a
// new hand. A tool nobody classified is therefore treated as an act, which is
// the harmless direction — an unclassified act is simply out of the delivery
// turn's reach, and a read misfiled as an act costs that turn one way of looking
// rather than costing anybody a guarantee.
func beltReadOnly(name string) bool {
	switch strings.TrimSpace(name) {
	case beltToolBoard, beltToolResult, beltToolPlan, beltToolRead, beltToolManual,
		beltToolCompetence, beltToolStanding, beltToolSpending, beltToolHistory,
		beltToolSearch, beltToolThread,
		// The lens (lens.go). All three change nothing: one searches, one opens,
		// one reports. They join the closed set for the same reason the others
		// are in it — a read miscounted as an act only costs the turn a call it
		// did not need to spend.
		beltToolRecall, beltToolOpen, beltToolStatus:
		return true
	}
	return false
}

// beltReadDefinitions is the looking half of the belt, as definitions. It is
// filtered from the one list rather than written as a second one, so a read
// added to the belt reaches the delivery turn the moment it exists and there is
// no way for the two accounts of "what can be read" to disagree.
func beltReadDefinitions() []ai.ToolDefinition {
	whole := beltDefinitions()
	reads := make([]ai.ToolDefinition, 0, len(whole))
	for _, definition := range whole {
		if beltReadOnly(definition.Function.Name) {
			reads = append(reads, definition)
		}
	}
	return reads
}

func beltDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		beltTool(beltToolBoard, "Read the work: every live job, one line each, and the only place ids come from. Aim it with q or id to reach finished jobs too.", map[string]any{
			"status": beltProp("string", `"running", "queued", "failed", or "all"`),
			"q":      beltProp("string", "free text naming the work, matched against titles and briefs"),
			"id":     beltProp("string", "one id from an earlier board read"),
		}),
		beltTool(beltToolSpawn, "Commission new work — the only way anything the workforce does gets started. Pass the user's own words verbatim; do not improve or summarize them. Use orders only for pieces that are genuinely independent. When it could be read either way it is one. after names work this follows on from.", map[string]any{
			"instruction": beltProp("string", "the user's words for the work, verbatim"),
			"orders": map[string]any{"type": "array", "description": "several independent pieces of work, each with that piece's own verbatim words",
				"items": map[string]any{"type": "string"}},
			"reflex":   beltProp("boolean", "one obvious reversible seconds-scale action; refused for anything that spends, sends, publishes or deletes"),
			"after":    beltProp("string", "id of work this continues, from a board read"),
			"separate": beltProp("boolean", `true ONLY when they say in so many words that this is a job BESIDE work already running — "as well as", "a separate one"`),
			"fresh":    beltProp("boolean", `true only when they ask for this to be worked out from first principles rather than the way it has been done before — "don't use the template this time". It is about method, never content`),
		}),
		beltTool(beltToolControl, "Cancel, pause, resume, restart, or reprioritize the ids you name; ids come from a board read. Pass describes instead when you have no id, and it hands back the candidates.", map[string]any{
			"verb":      beltProp("string", "cancel, pause, resume, restart, or reprioritize"),
			"ids":       map[string]any{"type": "array", "description": "ids from a board read", "items": map[string]any{"type": "string"}},
			"describes": beltProp("string", "the user's own words for the work, when you have no id for it"),
		}, "verb"),
		beltTool(beltToolSteer, "Tell the parts of a job already under way something now, without changing its plan.", map[string]any{
			"job":     beltProp("string", "job id from a board read"),
			"message": beltProp("string", "what the work already under way should hear, in the user's own terms"),
		}, "job", "message"),
		beltTool(beltToolRevise, "Hand the user's words, verbatim, to a job so its remaining plan is edited to match them.", map[string]any{
			"job":   beltProp("string", "job id from a board read"),
			"words": beltProp("string", "the user's message, verbatim"),
		}, "job", "words"),
		beltTool(beltToolExpedite, "Make a job arrive sooner: it goes next, and its unstarted tail is trimmed to the shortest path to the deliverable. It never adds work.", map[string]any{
			"job": beltProp("string", "job id from a board read"),
		}, "job"),
		beltTool(beltToolManual, "Read aforge's own manual — what it can do, how a mechanism works, why it behaved that way — the only source for answers about aforge itself.", map[string]any{
			"q":    beltProp("string", "the question, in the user's own words"),
			"page": beltProp("string", "one page name to read whole, from a page list you have seen"),
		}),
		beltTool(beltToolResult, "Read what one job produced: findings in full, the files it wrote, what it spent, and how its parts ended.", map[string]any{
			"id": beltProp("string", "one id from a board read"),
		}, "id"),
		beltTool(beltToolPlan, "Read how one job was broken up: every step in order, what each waits on, how each is going, what each cost.", map[string]any{
			"job": beltProp("string", "one id from a board read"),
		}, "job"),
		beltTool(beltToolRead, "Open a file a job recorded and read what is inside it. Never offer to fetch something you can fetch with this call right now.", map[string]any{
			"job":  beltProp("string", "the job that wrote it, id from a board or result read"),
			"file": beltProp("string", "the path or filename as that job recorded it; omit it when the job wrote one file"),
		}, "job"),
		beltTool(beltToolCompetence, "Read the measured view of your own strengths, weak spots and learning frontier; a self-assessment given without it is invention.", map[string]any{}),
		beltTool(beltToolStanding, "Read what you keep watch over: the last wake, the next check, and every standing rule with what it watches for and how often.", map[string]any{}),
		beltTool(beltToolSpending, "Read what has been spent: today against the daily limit, your own upkeep separately, or any window you bound with since and until.", map[string]any{
			"since": beltProp("string", `where the window starts, "2026-08-06" or "2026-08-06T09:00"`),
			"until": beltProp("string", "where it ends, same spelling"),
		}),
		beltTool(beltToolHistory, "Read what has been done, newest first, with what each job concluded and what it cost — the read that answers WHEN.", map[string]any{
			"since": beltProp("string", `where the window starts, "2026-08-06" or "2026-08-06T09:00"`),
			"until": beltProp("string", "where it ends, same spelling"),
		}),
		beltTool(beltToolSearch, "Search everything you remember — this conversation, the notebook, jobs long finished — the only read that reaches what was merely said.", map[string]any{
			"q": beltProp("string", "the words to look for, in the user's own terms"),
		}, "q"),
		beltTool(beltToolRecall, "Search everything settled or said at once — the conversation, the notebook, work live and finished, standing rules and services — and get back real content with an id for each hit that open takes. Always safe. Narrow with kind, since/until, or session; leave them off to search everything. If it comes back empty, say so plainly.", map[string]any{
			"q":       beltProp("string", "the words to look for, in the user's own terms"),
			"kind":    beltProp("string", "message, job, result, belief, rule, or service; omit to search all six"),
			"since":   beltProp("string", `local date or time the window starts, "2026-08-06" or "2026-08-06T09:00"`),
			"until":   beltProp("string", "local date or time the window ends, same spelling"),
			"session": beltProp("string", "one conversation id, to look only in that room"),
		}, "q"),
		beltTool(beltToolOpen, "Open one thing whole: running work as its plan with every step's state, its workers' own progress, files and spend so far; finished work as its whole result, files, spend and how parts ended; a file as its actual bytes; a rule, service or #notebook line as its full record. Always safe. Longer than one page is PAGED, never cut — read on with part when it says so.", map[string]any{
			"id":   beltProp("string", "what to open: a job id from a board or recall read, a file's name, a standing rule's id, a service's name, or a #number from the notebook"),
			"job":  beltProp("string", "the job that wrote the file, when two jobs wrote a file of the same name"),
			"part": beltProp("integer", "which page to read, from a part-of line you have been shown; omit for the first"),
			"raw":  beltProp("boolean", "true for the journal's own rows — every event and message anchored to it, unredacted"),
		}, "id"),
		beltTool(beltToolStatus, "Read the whole system on one page: running, queued and failed counts, today's cost against the daily limit, standing watches and next checks, services, measured competence, and what waits on an answer. Always safe.", map[string]any{}),
		beltTool(beltToolFork, "Commission new work that inherits this conversation, for when the requirements are in what you just discussed rather than in one sentence.", map[string]any{
			"instruction": beltProp("string", "what to go and do, in their words; omit it when their message is the instruction"),
			"after":       beltProp("string", "id of work this continues, from a board read"),
			"fresh":       beltProp("boolean", "true only when they ask for this to be worked out from first principles"),
		}),
		beltTool(beltToolThread, "Read another of the person's conversations; call it with no arguments to list the rooms. This conversation is already in front of you.", map[string]any{
			"room": beltProp("string", "one conversation id, from this tool's own list"),
		}),
		beltTool(beltToolNote, "Write one durable thing into the notebook — a preference, a correction, a lasting fact about them or their setup. The test is whether it still matters after this conversation is forgotten.", map[string]any{
			"body":     beltProp("string", "one sharp sentence, in the user's own terms"),
			"scope":    beltProp("string", `"user" for a personal preference, otherwise tool:<name>, repo:<path>, file:<path>, or domain:<topic>`),
			"kind":     beltProp("string", `"preference" for how they want things done, "fact" for something simply true`),
			"replaces": beltProp("integer", "the number of the notebook line this makes untrue; never a number you were not shown"),
		}, "body"),
		beltTool(beltToolWrite, "Put a document on disk and hand back its path: how anything they will use outside this conversation is produced.", map[string]any{
			"name": beltProp("string", "the file's own name with its extension, like architecture.svg — no directories"),
			"body": beltProp("string", "the whole document, exactly as it should be on disk"),
			"what": beltProp("string", "one short line saying what it is, for the receipt"),
		}),
		beltTool(beltToolAct, "Run one instant shell command and read what actually happened: the boundary is one instant, reversible command a person at the keyboard would run in two seconds without thinking, so anything that produces a deliverable, takes real time or cannot be undone is work — use spawn. When unsure, spawn.", map[string]any{
			"command": beltProp("string", "the one command, exactly as it would be typed at a shell"),
		}, "command"),
		beltTool(beltToolCorrect, "Redo a deliverable that was wrong, keeping whatever they did not object to — for whenever they reject, dispute or ask you to change something already delivered. Pass their words verbatim.", map[string]any{
			"job":   beltProp("string", "id of the finished job whose deliverable was wrong, from a board or search read"),
			"words": beltProp("string", "what the user said is wrong with it, verbatim"),
		}, "job"),
		beltTool(beltToolRule, "Change a standing rule: retire it, hold it, re-time it, reword it, or put it back to asking before each run. Read standing for ids, or pass describes.", map[string]any{
			"verb":      beltProp("string", "retire, pause, cadence, wording, or probation"),
			"id":        beltProp("string", "the rule's id, from a standing read or this tool's candidate list"),
			"describes": beltProp("string", "the user's own words for the rule, when you have no id for it"),
			"words":     beltProp("string", "the new rhythm for cadence, or the new message for wording"),
		}, "verb"),
		beltTool(beltToolService, "Act on something the person is running: stop it, restart it, or turn its auto-restart on or off; stop_everything is the total one.", map[string]any{
			"verb":      beltProp("string", "stop, restart, auto_restart_on, auto_restart_off, or stop_everything"),
			"describes": beltProp("string", "the user's own words for the service; omit for stop_everything"),
		}, "verb"),
		beltTool(beltToolCraft, "Act on a learned way of working: run does it that way again, revert goes back one version, retire stops it being reached for. Nothing is deleted.", map[string]any{
			"verb":  beltProp("string", "run, revert, or retire"),
			"name":  beltProp("string", "the workflow's own name, as the user named it"),
			"words": beltProp("string", "for run, what to work on; for revert, what the newer version got wrong; for retire, why"),
		}, "verb", "name"),
		beltTool(beltToolAnswerQuestion, "Settle a worker's open question; call it with no arguments to see what is open. A question not marked informational is a consent question and is refused here.", map[string]any{
			"question": beltProp("integer", "the question's number, from this tool's own list"),
			"answer":   beltProp("string", "the answer, in the words the worker asked for"),
		}),
		beltTool(beltToolSay, "Say one short line to the person right now, without ending the turn — the first real finding of a longer look, or what you are about to do when it will take a few reads. Never narrate tool calls or repeat yourself.", map[string]any{
			"text": beltProp("string", "one short line, in their terms"),
		}, "text"),
		beltTool(beltToolForget, "Let go of one numbered notebook line they have told you is untrue. NOT for a correction aimed at work — deleting a belief in answer to a rejected deliverable loses the correction entirely, so use correct, or note with replaces for a new version.", map[string]any{
			"belief": beltProp("integer", "the #number shown beside the notebook line"),
		}, "belief"),
		beltTool(beltToolAsk, "Put one short numbered question to the person, with the candidates as options named the way THEY would recognise them. This ends the turn: their next message is the answer.", map[string]any{
			"question": beltProp("string", "one short question, in their terms"),
			"options": map[string]any{"type": "array", "description": "two to four choices, each named the way the user would recognise it",
				"items": map[string]any{"type": "string"}},
			"category": beltProp("string", `omit unless this is the quick-look-or-proper-job boundary, where it is "scope" — that one is learned, so it may come back already assumed instead of asked`),
		}, "question", "options"),
		beltTool(beltToolInterrupt, "Stop the head turn in flight, only when they asked you to stop what you are doing in this conversation; cancelling work on the board is control.", map[string]any{
			"reason": beltProp("string", "one short line for the record, in the user's terms"),
		}),
	}
}

// beltRun is one control loop's hands and its memory of what they did. The
// receipt is written from this and nothing else, so the reply can only claim
// what a tool actually reported.
type beltRun struct {
	head       *Head
	user       store.Message
	acted      bool
	commandSeq int64
	confirm    *beltConfirm
	did        []string
	// spoke records that a tool already posted this turn's whole reply — a consent
	// question, or the interrupted line. The loop must not post a second voice over
	// the top of it.
	spoke bool
	// said records that a tool posted an INTERIM line — the say tool. It is the
	// opposite of spoke in the one way that matters: the turn carries on, and its
	// final words are still owed. All it suppresses is the empty-reply floor, so a
	// turn that said everything it had to say mid-flight does not follow it with
	// an apology for having nothing to add (loop.go).
	said bool
	// commissioned is the words this turn has already turned into work. spawn
	// dedupes the orders inside ONE call; nothing stopped a loop from calling it
	// twice with the same sentence, and the second call was a second job — a
	// second compile, a second plan, a second "Here's my reading" under the
	// first, and two workforces doing the same thing to the same files. One ask
	// is one job however many times the loop asks for it in one breath.
	commissioned map[string]bool
}

// alreadyCommissioned reports that these exact words have already become work
// in this turn, and records them when they have not. Comparison is on the
// trimmed sentence, which is what spawn journals and what the person said.
func (run *beltRun) alreadyCommissioned(instruction string) bool {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return false
	}
	if run.commissioned[instruction] {
		return true
	}
	if run.commissioned == nil {
		run.commissioned = make(map[string]bool, 2)
	}
	run.commissioned[instruction] = true
	return false
}

// beltConfirm is a change the gates stopped. Nothing has been journalled; the
// head asks the question once the loop stops talking.
type beltConfirm struct {
	kind   store.CommandKind
	ids    []string
	set    classSet
	impact store.SurgeryImpact
}

func (run *beltRun) execute(name, arguments string) (string, bool) {
	// One guard for every tool rather than one per tool. Three of the reads
	// already carried their own because a surface can legitimately register no
	// competence map; this is the other kind of absence — no graph at all — and
	// a tool belt that panicked on it would take the conversation down with it.
	if run == nil || run.head == nil || run.head.store == nil {
		return "this surface has no graph behind it, so nothing can be read or changed here", true
	}
	args := map[string]any{}
	if trimmed := strings.TrimSpace(arguments); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return "those arguments were not valid JSON: " + err.Error(), true
		}
	}
	switch name {
	case beltToolBoard:
		return run.board(args)
	case beltToolControl:
		return run.control(args)
	case beltToolSteer:
		return run.steer(args)
	case beltToolRevise:
		return run.revise(args)
	case beltToolExpedite:
		return run.expedite(args)
	case beltToolManual:
		return run.manual(args)
	case beltToolResult:
		return run.result(args)
	case beltToolPlan:
		return run.plan(args)
	case beltToolRead:
		return run.read(args)
	case beltToolCompetence:
		return run.competence()
	case beltToolStanding:
		return run.standing()
	case beltToolSpending:
		return run.spending(args)
	case beltToolHistory:
		return run.history(args)
	case beltToolSearch:
		return run.search(args)
	case beltToolRecall:
		return run.recall(args)
	case beltToolOpen:
		return run.open(args)
	case beltToolStatus:
		return run.status()
	case beltToolThread:
		return run.thread(args)
	case beltToolNote:
		return run.note(args)
	case beltToolSpawn:
		return run.spawn(args)
	case beltToolFork:
		return run.fork(args)
	case beltToolWrite:
		return run.write(args)
	case beltToolAct:
		return run.act(args)
	case beltToolCorrect:
		return run.correct(args)
	case beltToolRule:
		return run.rule(args)
	case beltToolService:
		return run.service(args)
	case beltToolCraft:
		return run.craft(args)
	case beltToolAnswerQuestion:
		return run.answerQuestion(args)
	case beltToolSay:
		return run.say(args)
	case beltToolForget:
		return run.forget(args)
	case beltToolAsk:
		return run.ask(args)
	case beltToolInterrupt:
		return run.interrupt(args)
	}
	return fmt.Sprintf("there is no tool named %q", name), true
}

func (run *beltRun) board(args map[string]any) (string, bool) {
	rows, err := run.head.boardRows(run.user.SessionID,
		beltString(args, "q"), beltString(args, "status"), beltString(args, "id"))
	if err != nil {
		return err.Error(), true
	}
	if len(rows) == 0 {
		return "no live work matches that.", false
	}
	return renderBoard(rows), false
}

func (run *beltRun) control(args map[string]any) (string, bool) {
	kind, known := beltVerbKind(beltString(args, "verb"))
	if !known {
		return "verb must be one of cancel, pause, resume, restart, reprioritize", true
	}
	ids := beltStrings(args, "ids")
	if len(ids) == 0 {
		// The verb knows what it wants to do and not what to do it to. That is an
		// ordinary thing to say, and the union of the live jobs and the standing
		// rules the words reach is the honest answer to it — handed back as
		// candidates rather than acted on, because choosing for the user is how a
		// request to withdraw fourteen queued tasks became "Cancelling line-scan."
		return run.controlCandidates(kind, beltString(args, "describes"))
	}
	switch {
	case len(ids) > beltControlIDCap:
		return fmt.Sprintf("that is %d ids; name the jobs rather than their steps", len(ids)), true
	}
	if run.confirm != nil {
		return "the user has already been asked to confirm a change; nothing else may act until they answer", true
	}
	set, err := run.head.beltSet(ids, kind, true)
	if err != nil {
		return err.Error(), true
	}
	if len(set.Units) == 0 {
		return fmt.Sprintf("there is nothing there that %s can touch right now", surgeryVerb(kind)), true
	}
	impact, err := run.head.classImpact(set)
	if err != nil {
		return "the cost of that could not be read: " + err.Error(), true
	}
	if surgeryNeedsConfirmation(kind, impact) {
		run.confirm = &beltConfirm{kind: kind, ids: ids, set: set, impact: impact}
		return fmt.Sprintf("needs_confirmation: %s %s crosses the consent gate. NOTHING has changed. The user is being asked and their answer settles it.",
			surgeryVerb(kind), classSetPhrase(set, classIntent{Class: classAll})), false
	}
	labels, seq := run.head.journalUnits(run.user, kind, run.user.Body, set.Units)
	if len(labels) == 0 {
		return "none of that could be queued", true
	}
	run.record(seq, classReceipt(kind, classIntent{Class: classAll}, set, labels))
	return fmt.Sprintf("%s %d: %s", surgeryProgressive(kind), len(labels), strings.Join(labels, ", ")), false
}

func (run *beltRun) steer(args map[string]any) (string, bool) {
	job, err := run.head.beltJob(beltString(args, "job"))
	if err != nil {
		return err.Error(), true
	}
	message := strings.TrimSpace(beltString(args, "message"))
	if message == "" {
		return "message must say what the workers should hear", true
	}
	// The node-anchored broadcast is the reliable half of a redirection with
	// none of its plan editing: the workers mid-turn hear it, the plan is left
	// exactly as it stands. Nothing here is journalled, so nothing needs a gate.
	informed, err := resident.BroadcastRedirection(run.head.store, job.ID, run.user.SessionID, message)
	if err != nil {
		return "that could not be passed on: " + err.Error(), true
	}
	label := surgeryTargetLabel(job)
	if informed == 0 {
		run.record(0, "Nothing on "+label+" is running at this moment, so there was no one to pass that to.")
		return fmt.Sprintf("nothing on %s is running at this moment, so no one was told", label), false
	}
	run.record(0, fmt.Sprintf("Passed that on to the %d %s of %s already under way.",
		informed, pluralWord(informed, "step", "steps"), label))
	return fmt.Sprintf("told %d %s of %s already under way", informed, pluralWord(informed, "step", "steps"), label), false
}

func (run *beltRun) revise(args map[string]any) (string, bool) {
	job, err := run.head.beltJob(beltString(args, "job"))
	if err != nil {
		return err.Error(), true
	}
	words := strings.TrimSpace(beltString(args, "words"))
	if words == "" {
		words = strings.TrimSpace(run.user.Body)
	}
	seq, err := run.head.journalRevision(run.user, store.CommandRedirect, job.ID, words)
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	// The fallback receipt promises only the handoff. What actually changed in
	// the plan and who was told is written a moment later by the reconciler
	// that did it, which is the only place that honestly knows.
	run.record(seq, "Taking that to "+surgeryTargetLabel(job)+" — I'll say what changed once it lands.")
	return "the remaining plan of " + surgeryTargetLabel(job) + " will be edited to match those words", false
}

func (run *beltRun) expedite(args map[string]any) (string, bool) {
	job, err := run.head.beltJob(beltString(args, "job"))
	if err != nil {
		return err.Error(), true
	}
	seq, err := run.head.journalRevision(run.user, store.CommandExpedite, job.ID, strings.TrimSpace(run.user.Body))
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(seq, "Pushing "+surgeryTargetLabel(job)+" to the front and trimming what it has not started.")
	return surgeryTargetLabel(job) + " moves up the claim order and its unstarted tail is trimmed", false
}

// manual is a read like board is a read: it never records, so a message that
// only asked what aforge is journals no command and the reply carries no
// command seq. The answer is grounded or it is not given.
func (run *beltRun) manual(args map[string]any) (string, bool) {
	if name := beltString(args, "page"); name != "" {
		text, found := manual.Page(name)
		if !found {
			return "there is no manual page named " + name + " — the pages are: " +
				strings.Join(manual.Pages(), ", "), true
		}
		return text, false
	}
	query := beltString(args, "q")
	if query == "" {
		query = strings.TrimSpace(run.user.Body)
	}
	sections := manual.Search(query, beltManualSections)
	if len(sections) == 0 {
		return "the manual has nothing on that. Its pages are: " +
			strings.Join(manual.Pages(), ", "), false
	}
	return manual.Render(sections), false
}

// result is a read like board and manual are reads: it records nothing, so a
// message that only asked what a job found journals no command.
func (run *beltRun) result(args map[string]any) (string, bool) {
	node, err := run.head.beltRecordedJob(beltString(args, "id"), "id")
	if err != nil {
		return err.Error(), true
	}
	return run.head.renderResult(node), false
}

// plan is the read that answers a question about shape. It records nothing and
// journals nothing, exactly as board, result and read do: asking what the work
// was broken into changed none of it.
func (run *beltRun) plan(args map[string]any) (string, bool) {
	node, err := run.head.beltRecordedJob(beltString(args, "job"), "job")
	if err != nil {
		return err.Error(), true
	}
	return run.head.renderPlan(node)
}

// read is the third read, and the only one whose subject is outside the graph.
// It records nothing and journals nothing for the same reason board, manual and
// result do not: asking what a document says changed nothing. What it may open
// is decided entirely in artifact.go, which is where the security boundary and
// its reasons are written down.
func (run *beltRun) read(args map[string]any) (string, bool) {
	// A file this head wrote is recorded exactly as firmly as a file a worker
	// wrote, so it belongs to the same openable set rather than to a second door
	// beside it. This is the repair doctrine's second half: a head that wrote a
	// document an hour ago can open it to fix it, instead of offering to and then
	// discovering it cannot.
	if job := strings.TrimSpace(beltString(args, "job")); job == "" {
		rendered, err := run.head.readWrittenArtifact(beltString(args, "file"))
		if err != nil {
			return err.Error(), true
		}
		return rendered, false
	}
	node, err := run.head.beltRecordedJob(beltString(args, "job"), "job")
	if err != nil {
		return err.Error(), true
	}
	rendered, err := run.head.readArtifact(node, beltString(args, "file"))
	if err != nil {
		return err.Error(), true
	}
	return rendered, false
}

// competence, standing and spending are the employee's account of itself, and
// they are reads in the same sense board and manual are: nothing is journalled,
// nothing acts, and a message that only asked how the work has been going
// carries no command seq. Each returns the honest empty answer rather than an
// error when the surface never registered it — a chat that has no competence
// measurement is a fact about the chat, and one the model can say out loud.
func (run *beltRun) competence() (string, bool) {
	if run.head == nil || run.head.competence == nil {
		return "no competence measurement is available on this surface.", false
	}
	measured := strings.TrimSpace(run.head.competence())
	if measured == "" {
		return "nothing has been measured yet — not enough work has settled to say where you are strong or weak.", false
	}
	return truncateBytes(measured, beltGroundingBytes), false
}

func (run *beltRun) standing() (string, bool) {
	if run.head == nil || run.head.standingWatch == nil {
		return "standing-watch status is not available on this surface.", false
	}
	status := strings.TrimSpace(run.head.standingWatch())
	if status == "" {
		return "nothing is on watch and no standing check is arranged.", false
	}
	return truncateBytes(status, beltGroundingBytes), false
}

// spending answers the money question the head could not answer at all. The
// daily rail was its only cost signal above one job, so "what have you been
// spending on yourself?" had no reachable ground truth while the prompt forbade
// saying so — the two halves of an invented number. Self-work has its own
// receipts, one per settled OriginSelf splice, each carrying what it cost and
// whether it learned anything; those are the answer, and the day's total is the
// context for it.
//
// Today was also the only window it could speak in, and "how much have you cost
// me this month?" is not a today-shaped question. The windowed reads it needs
// were already written and tested in the store and reached from nowhere, so a
// month became a model adding up whichever finished jobs history had handed it.
// The bounds arrive here in exactly the spelling history's do, from the same
// clock, because one time vocabulary across the belt is the whole reason the
// model can compose a window at all.
func (run *beltRun) spending(args map[string]any) (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "spending is not available on this surface.", false
	}
	since, ok := parseHistoryBound(beltString(args, "since"), false)
	if !ok {
		return `since must be a local date or time, "2026-08-06" or "2026-08-06T09:00"`, true
	}
	until, ok := parseHistoryBound(beltString(args, "until"), true)
	if !ok {
		return `until must be a local date or time, "2026-08-06" or "2026-08-06T09:00"`, true
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return "until is before since — the window is empty as written", true
	}
	var rendered strings.Builder
	if !since.IsZero() || !until.IsZero() {
		lines, err := run.head.spendWindowLines(since, until)
		if err != nil {
			return "that could not be read: " + err.Error(), true
		}
		rendered.WriteString(strings.Join(lines, "\n") + "\n")
	}
	// The rate and the projections, computed rather than left to be worked out
	// in a sentence (5.23's ordering law; 13.3's head edge). A window the caller
	// left open still gets a rate, because "what does this cost me" is a
	// question about a span even when it was asked without one.
	if lines := run.head.spendRateBlock(since, until); len(lines) > 0 {
		rendered.WriteString(strings.Join(lines, "\n") + "\n")
	}
	if run.head.dailyRailSet {
		if rail, err := run.head.store.DailyRailToday(run.head.dailyBudgetUSD); err == nil {
			if rail.Unlimited {
				fmt.Fprintf(&rendered, "today: $%.2f spent; daily rail unlimited\n", rail.Spend)
			} else {
				fmt.Fprintf(&rendered, "today: $%.2f spent of a $%.2f daily rail\n", rail.Spend, rail.Ceiling)
			}
		}
	}
	self, err := run.head.store.SelfSpendToday()
	if err != nil {
		return "that could not be read: " + err.Error(), true
	}
	fmt.Fprintf(&rendered, "your own upkeep today: $%.2f\n", self)
	if lines := run.head.selfWorkLines(); len(lines) > 0 {
		rendered.WriteString("what that upkeep bought, most recent last:\n" +
			strings.Join(lines, "\n") + "\n")
	}
	return truncateBytes(strings.TrimSpace(rendered.String()), beltGroundingBytes), false
}

// spendWindowLines is what one stretch of time cost and what the money went on.
// The total comes from every priced run in the window; the rows underneath it
// are the heaviest job roots, each named by the title the user's own words gave
// it, so the answer can be composed as a sentence — "$18.40 this month, mostly
// the Lisbon research" — rather than as a figure with nothing behind it.
//
// The rows deliberately do not add up to the total, and the last line says so
// rather than letting a model quietly imply that they do: work under no job root
// at all — planning, answering, the resident's own upkeep — is real money that
// belongs to no single errand, and so are the jobs below the cap.
func (h *Head) spendWindowLines(since, until time.Time) ([]string, error) {
	window, err := h.store.SpendBetween(since, until)
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("%s: $%.2f over %d %s",
		spendWindowPhrase(window), window.Cost, window.Runs, pluralWord(window.Runs, "run", "runs"))}
	if window.Cost <= 0 {
		return lines, nil
	}
	jobs, err := h.store.SpendByJob(since, until, beltSpendJobCap)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return lines, nil
	}
	now := time.Now()
	lines = append(lines, "what the money went on, heaviest first:")
	named := 0.0
	for _, job := range jobs {
		named += job.Cost
		title := strings.TrimSpace(job.Title)
		if title == "" {
			title = job.JobID
		}
		line := fmt.Sprintf("- $%.2f | %s | %s | %d %s",
			job.Cost, title, job.JobID, job.Runs, pluralWord(job.Runs, "run", "runs"))
		if age := store.AgeLabel(job.Last, now); age != "" {
			line += " | last spent " + age
		}
		lines = append(lines, line)
	}
	if rest := window.Cost - named; rest >= 0.01 {
		lines = append(lines, fmt.Sprintf("- $%.2f | everything else in the window — smaller jobs, planning, answering, upkeep", rest))
	}
	return lines, nil
}

// spendWindowPhrase names the window in the spelling the clock uses, so the
// answer can quote its own bounds. SpendBetween has already resolved an open
// upper bound to now and an open lower bound to the beginning of the journal,
// which is why this reads the resolved window rather than the arguments.
func spendWindowPhrase(window store.SpendWindow) string {
	until := window.Until.Local().Format(nowLineLayout)
	if window.Since.IsZero() || window.Since.Year() <= 1 {
		return "everything recorded up to " + until
	}
	return window.Since.Local().Format(nowLineLayout) + " to " + until
}

// history is a read like every other read here: nothing is journalled and a
// message that only asked what got done carries no command seq. It is the one
// read whose argument is a bound rather than a subject, and the bound is
// composed by the model out of the clock it was given — which is the whole of
// how "yesterday" becomes a query without a single phrase list anywhere.
func (run *beltRun) history(args map[string]any) (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "history is not available on this surface.", false
	}
	since, ok := parseHistoryBound(beltString(args, "since"), false)
	if !ok {
		return `since must be a local date or time, "2026-08-06" or "2026-08-06T09:00"`, true
	}
	until, ok := parseHistoryBound(beltString(args, "until"), true)
	if !ok {
		return `until must be a local date or time, "2026-08-06" or "2026-08-06T09:00"`, true
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return "until is before since — the window is empty as written", true
	}
	lines, err := run.head.historyLines(since, until)
	if err != nil {
		return "that could not be read: " + err.Error(), true
	}
	if len(lines) == 0 {
		return "nothing settled in that window.", false
	}
	return strings.Join(lines, "\n"), false
}

// historyBoundLayouts are the two spellings the tool description promises, in
// the order that reads a bare date as the whole day rather than as midnight.
var historyBoundLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}

// parseHistoryBound reads one end of the window in the user's own timezone,
// which is the only timezone any of their time words mean. A bare date used as
// an upper bound covers that whole day: "until Friday" means through Friday,
// not up to the first instant of it.
func parseHistoryBound(value string, upper bool) (time.Time, bool) {
	if value = strings.TrimSpace(value); value == "" {
		return time.Time{}, true
	}
	value = strings.TrimSuffix(strings.TrimSpace(value), "Z")
	for _, layout := range historyBoundLayouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err != nil {
			continue
		}
		if upper && layout == "2006-01-02" {
			parsed = parsed.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
		return parsed, true
	}
	return time.Time{}, false
}

// historyLines renders the window one job to a line: when it landed, what it
// was, how it ended, what it concluded and what it cost. The order is the one
// the question was asked in — newest first — and the age is spelled the way
// every other age in this package is spelled.
func (h *Head) historyLines(since, until time.Time) ([]string, error) {
	nodes, err := h.store.SettledHistory(since, until, store.SettledHistoryCap)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if !beltAddressable(node) {
			continue
		}
		line := fmt.Sprintf("- %s | %s | %s | %s", node.FinishedAt.Local().Format(nowLineLayout),
			node.ID, node.Status, surgeryTargetLabel(node))
		if age := store.AgeLabel(node.FinishedAt, now); age != "" {
			line += " | " + age
		}
		if impact, err := h.store.Impact(node.ID, now); err == nil && impact.Cost > 0 {
			line += fmt.Sprintf(" | $%.2f", impact.Cost)
		}
		if summary := firstLine(h.jobResult(node)); summary != "" {
			line += " | " + truncateBytes(summary, historyResultBytes)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// historyResultBytes is one job's share of a window. A window is a list of what
// happened, and result reads the one the user then asks about.
const historyResultBytes = 200

// selfWorkLines renders the last few self-work receipts one line each: what the
// inquiry was about, what it cost, and whether anything came of it. "Learned
// nothing" is kept rather than hidden, because a run of them is the honest
// answer to whether the upkeep is worth its money.
func (h *Head) selfWorkLines() []string {
	receipts, err := h.store.SelfReceipts(time.Time{})
	if err != nil || len(receipts) == 0 {
		return nil
	}
	if len(receipts) > beltSelfReceiptCap {
		receipts = receipts[len(receipts)-beltSelfReceiptCap:]
	}
	lines := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		learned := fmt.Sprintf("%d learned", len(receipt.FactIDs)+len(receipt.SkillIDs))
		if receipt.Nothing {
			learned = "learned nothing"
		}
		lines = append(lines, fmt.Sprintf("- %s | $%.2f | %s",
			firstLine(receipt.Origin), receipt.Cost, learned))
	}
	return lines
}

// searchHitCap bounds each of the three memories a search reads. Small on
// purpose: this read exists to find the thread back into something, and eight
// lines of conversation, six beliefs and four jobs is already more than an
// answer can honestly quote.
const (
	searchMessageCap = 8
	searchFactCap    = 6
	searchRecallCap  = 4
)

// search is the read that answers a question about the past honestly.
//
// It asks all three memories at once because the person cannot know which one
// holds their answer: what they said lives in the conversation, what they told
// you lives in the notebook, and what you did lives in folded jobs. Nothing here
// is new machinery — the notebook retrieval and the fold recall have both been
// in the store for months — except the conversation index, which never existed
// and is the reason "remember that pricing analysis from January?" had no
// truthful answer available to it at all.
//
// An empty result is returned as an empty result. That sentence is the point of
// the whole tool: the alternative the prompt used to leave was a fluent
// reconstruction of something that may never have happened.
func (run *beltRun) search(args map[string]any) (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "search is not available on this surface.", false
	}
	query := beltString(args, "q")
	if query == "" {
		return "q must say what to look for", true
	}
	var rendered strings.Builder
	now := time.Now()

	if hits, err := run.head.store.SearchMessages(query, "", searchMessageCap); err == nil && len(hits) > 0 {
		rendered.WriteString("said in conversation:\n")
		for _, hit := range hits {
			who := "you"
			if hit.Role == store.RoleUser {
				who = "they"
			}
			fmt.Fprintf(&rendered, "- %s, %s: %s\n", who, hit.Age, firstLine(hit.Body))
		}
	}
	if facts, err := run.head.store.SearchFacts(store.FactQuery{
		Cues: resident.ExtractCues(query), Terms: query, Limit: searchFactCap,
	}); err == nil && len(facts) > 0 {
		rendered.WriteString("\nin the notebook:\n")
		for _, fact := range facts {
			fmt.Fprintf(&rendered, "- #%d [%s · %s] %s\n", fact.Seq, fact.Scope,
				store.AgeLabel(fact.Time, now), fact.Body)
		}
	}
	if recalled, err := run.head.store.Recall(query, resident.ExtractCues(query), searchRecallCap); err == nil && len(recalled) > 0 {
		rendered.WriteString("\nin work that has already finished:\n")
		for _, hit := range recalled {
			fmt.Fprintf(&rendered, "- %s (%s), id %s", firstLine(hit.Intent), hit.Age, hit.NodeID)
			if digest := strings.TrimSpace(hit.Digest); digest != "" {
				fmt.Fprintf(&rendered, ": %s", firstLine(digest))
			}
			rendered.WriteByte('\n')
		}
	}
	if strings.TrimSpace(rendered.String()) == "" {
		return "nothing remembered matches those words — not in the conversation, not in the notebook, not in finished work. Say that plainly rather than reconstructing it.", false
	}
	return truncateBytes(strings.TrimSpace(rendered.String()), beltGroundingBytes), false
}

// note is durable feedback landing where durable feedback goes. It records
// through the head's own fact writer, so the line it writes is indistinguishable
// from one the router's remember wrote and is read back by the same notebook
// render on every later message. Nothing new is stored and no event kind is
// invented; the gap was never the machinery, it was that this loop had no hands
// for it and answered "from now on" with nothing behind the words.
func (run *beltRun) note(args map[string]any) (string, bool) {
	body := truncateBytes(beltString(args, "body"), beltNoteBytes)
	if body == "" {
		return "body must say the durable thing in one sentence", true
	}
	scope := strings.ToLower(beltString(args, "scope"))
	if scope == "" {
		scope = "user"
	}
	kind := store.FactKind(strings.ToLower(beltString(args, "kind")))
	switch kind {
	case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain:
	default:
		kind = store.FactPreference
	}
	fact, err := run.head.store.RecordFactFrom(store.FactWriterHead, store.RootID, scope, kind, body)
	if err != nil {
		return "that could not be written down: " + err.Error(), true
	}
	// The same supersession the router's remember has. This loop reads the same
	// numbered notebook, so it can see the line the new note makes untrue, and
	// leaving it standing beside the correction is the accumulation failure the
	// consolidator then has to clean up by guessing.
	replaced := beltInt(args, "replaces")
	supersedeBelief(run.head.store, replaced, fact)
	// The receipt is the record, which is the point: a note that failed to
	// journal produces a tool error, and the loop can then only say so.
	//
	// It records the receipt without claiming the message, which is what
	// separates this from every other write on the belt. "Always answer from the
	// result, and rerun the scans" is one durable preference and one piece of
	// work; a note that marked the run as acted would let the loop answer with
	// the receipt and swallow the rest. The fact is journalled either way, the
	// store deduplicates it, and the sentinel stays free to hand the sentence
	// back to the router.
	run.did = append(run.did, "Noted — "+firstLine(fact.Body))
	return fmt.Sprintf("written into the notebook as #%d under %s; it is in front of you on every later message",
		fact.Seq, fact.Scope), false
}

// beltRecordedJob resolves an id for the two reads that are allowed to name work
// that has already finished. It deliberately does not go through beltJob: that
// resolver refuses settled work because the verbs cannot touch it, and settled
// work is precisely what has findings and files.
func (h *Head) beltRecordedJob(id, field string) (store.Node, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Node{}, fmt.Errorf("%s must name one job from a board read", field)
	}
	node, found, err := h.store.Node(id)
	if err != nil {
		return store.Node{}, fmt.Errorf("that job could not be read: %w", err)
	}
	if !found || node.ID == store.RootID || !beltAddressable(node) {
		return store.Node{}, fmt.Errorf("there is no work of the user's with id %q — read the board again", id)
	}
	return node, nil
}

// renderResult is one job's whole account of itself. The finding comes first
// because it is the answer; status and spend trail it because they are context
// for the answer, and the children are there so "what did each part conclude"
// is one read rather than five.
func (h *Head) renderResult(node store.Node) string {
	now := time.Now()
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | %s | %s", node.ID, node.Status, surgeryTargetLabel(node))
	if impact, err := h.store.Impact(node.ID, now); err == nil && impact.Cost > 0 {
		fmt.Fprintf(&rendered, " | $%.2f", impact.Cost)
	}
	if age := store.AgeLabel(node.FinishedAt, now); age != "" {
		rendered.WriteString(" | finished " + age)
	}
	rendered.WriteString("\n")
	body := truncateBytes(h.jobResult(node), beltResultBytes)
	if body != "" {
		rendered.WriteString("result:\n" + body + "\n")
	} else {
		rendered.WriteString("result: nothing recorded yet — this job has not settled.\n")
	}
	if files := unnamedFiles(node, body); len(files) > 0 {
		rendered.WriteString("files: " + strings.Join(files, ", ") + "\n")
	}
	if children := h.resultChildren(node.ID); len(children) > 0 {
		rendered.WriteString("its parts:\n" + strings.Join(children, "\n") + "\n")
	}
	return strings.TrimSpace(rendered.String())
}

const (
	// planStepCap is how many steps one plan read spells out. A plan longer than
	// this is a document, and the question behind this read — what is this, what
	// is left, what is holding it up — is answered by the first two dozen steps
	// and by the counts on the line above them.
	planStepCap = 24
	// planIndentCap bounds how far a step is indented for its place in the
	// dependency order. Past a few layers the indent stops saying anything a
	// reader can hold, and the "after" clause on each line is the exact fact.
	planIndentCap = 6
	// planMethodBytes is how much of a step's working method one line carries.
	// The contract is written for the agent that runs the step; here it is
	// evidence of how the step is being worked, not the instructions themselves.
	planMethodBytes = 120
	// planTruncatedMark is the plan read's own version of the board's marker: a
	// read that stopped saying it stopped, so a short answer is never mistaken
	// for a short plan.
	planTruncatedMark = "(plan truncated)"
)

// journaledPlan finds the plan a store node belongs to. Ids are minted as
// "<prefix>-n<planID>" with the job root taking the bare prefix, so a plan is
// reachable from any node of its job: the row's own id first, its namespace
// second. The separator is the whole of the test — "task-14" is not the
// namespace of "task-142-n1", however much it looks like one.
func (h *Head) journaledPlan(node store.Node) (string, store.PlanGraph, bool) {
	candidates := []string{node.ID}
	if cut := strings.LastIndex(node.ID, "-n"); cut > 0 {
		candidates = append(candidates, node.ID[:cut])
	}
	if parent := strings.TrimSpace(node.Parent); parent != "" && parent != store.RootID {
		candidates = append(candidates, parent)
	}
	for _, prefix := range candidates {
		journaled, found, err := h.store.PlanGraphFor(prefix)
		if err != nil || !found {
			continue
		}
		return prefix, journaled, true
	}
	return "", store.PlanGraph{}, false
}

// renderPlan is one job's structure in plain words: every step, in an order
// that can actually run, with what each is waiting on and how each is going.
//
// The states and the spend are read off the durable rows rather than off the
// journaled document, and that is not belt-and-braces. The journal is written
// when a plan is made and when it is revised, never once per landed leaf, so a
// document read on its own would report a finished job as entirely pending —
// which is the same reason the executor re-derives state from the store when it
// rehydrates a plan, by the same id mapping.
func (h *Head) renderPlan(node store.Node) (string, bool) {
	return h.renderPlanWithin(node, planStepCap, beltResultBytes)
}

// renderPlanWithin is renderPlan with its two bounds passed in, and it exists
// so the lens can borrow the renderer without borrowing the belt's budget. A
// non-positive stepCap spells every step; a non-positive byteCap clips nothing.
// The plan tool's own call above passes exactly the constants it always used,
// so nothing about that read changed.
func (h *Head) renderPlanWithin(node store.Node, stepCap, byteCap int) (string, bool) {
	prefix, journaled, found := h.journaledPlan(node)
	if !found {
		return fmt.Sprintf("%s was taken on as a single step, so there is no breakdown to read — result is what it has to say for itself.",
			surgeryTargetLabel(node)), false
	}
	document, err := plan.Load(journaled.Graph)
	if err != nil {
		return "that job's plan could not be read: " + err.Error(), true
	}
	root := strings.TrimSpace(journaled.Root)
	if root == "" {
		root = prefix
	}
	rows := h.planRows(prefix, root, document)

	now := time.Now()
	titles := make(map[int]string, len(document.Nodes))
	for _, step := range document.Nodes {
		titles[step.ID] = planStepLabel(step)
	}
	done, running, waiting := 0, 0, 0
	capacity := stepCap
	if capacity <= 0 {
		capacity = len(document.Nodes)
	}
	lines := make([]string, 0, capacity)
	truncated := false
	for level, wave := range document.Waves() {
		for _, id := range wave {
			step := document.Node(id)
			if step == nil {
				continue
			}
			word, row, stored := planStepState(*step, rows)
			switch word {
			case surgeryStatusWord(store.Done):
				done++
			case surgeryStatusWord(store.Running), surgeryStatusWord(store.Claimed):
				running++
			case surgeryStatusWord(store.Pending):
				waiting++
			}
			if stepCap > 0 && len(lines) >= stepCap {
				truncated = true
				continue
			}
			indent := level
			if indent > planIndentCap {
				indent = planIndentCap
			}
			line := strings.Repeat("  ", indent) + "- " + titles[step.ID] + " | " + word
			if after := planAfter(*step, titles); after != "" {
				line += " | " + after
			}
			if step.Turns > 0 {
				line += fmt.Sprintf(" | %d turns", step.Turns)
			}
			if stored {
				if impact, err := h.store.Impact(row.ID, now); err == nil && impact.Cost > 0 {
					line += fmt.Sprintf(" | $%.2f", impact.Cost)
				}
				line += " | id " + row.ID
			}
			if method := firstLine(step.Contract); method != "" {
				line += " | method: " + truncateBytes(method, planMethodBytes)
			}
			lines = append(lines, line)
		}
	}

	var rendered strings.Builder
	fmt.Fprintf(&rendered, "plan for %s | %d steps | %d done, %d under way, %d waiting\n",
		surgeryTargetLabel(node), len(document.Nodes), done, running, waiting)
	if goal := firstLine(document.Goal); goal != "" {
		rendered.WriteString("goal: " + goal + "\n")
	}
	if evidence := firstLine(document.Evidence); evidence != "" {
		rendered.WriteString("what counts as done: " + evidence + "\n")
	}
	rendered.WriteString(strings.Join(lines, "\n"))
	if truncated {
		rendered.WriteString("\n" + planTruncatedMark)
	}
	body := strings.TrimSpace(rendered.String())
	if byteCap > 0 {
		body = truncateBytes(body, byteCap)
	}
	return body, false
}

// planRows maps each plan step onto the durable row it was spliced as, by the
// same rule the executor uses when it rehydrates a plan from the journal: the
// minted id, and failing that the job root, which is what the deliverable sink
// is admitted under. A step with no row at all has not been spliced, and that
// is a fact about it rather than a gap.
func (h *Head) planRows(prefix, root string, document *plan.Graph) map[int]store.Node {
	nodes, err := h.store.SubtreeNodes(root)
	if err != nil {
		return nil
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	rows := make(map[int]store.Node, len(document.Nodes))
	for _, step := range document.Nodes {
		stored, ok := byID[fmt.Sprintf("%s-n%d", prefix, step.ID)]
		if !ok {
			if stored, ok = byID[prefix]; !ok || stored.ID != root {
				continue
			}
		}
		rows[step.ID] = stored
	}
	return rows
}

// planStepState prefers the durable row's status and falls back to the
// document's own, so a step that was never spliced still says where it stands
// instead of borrowing somebody else's state.
func planStepState(step plan.Node, rows map[int]store.Node) (string, store.Node, bool) {
	if row, ok := rows[step.ID]; ok {
		if row.Held {
			return "paused", row, true
		}
		return surgeryStatusWord(row.Status), row, true
	}
	switch step.State {
	case plan.StateDone:
		return surgeryStatusWord(store.Done), store.Node{}, false
	case plan.StateRunning:
		return surgeryStatusWord(store.Running), store.Node{}, false
	case plan.StateFailed:
		return surgeryStatusWord(store.Failed), store.Node{}, false
	case plan.StateBlocked:
		return "held up by a step that failed", store.Node{}, false
	default:
		return surgeryStatusWord(store.Pending), store.Node{}, false
	}
}

// planAfter says what a step is waiting for in the plan's own words rather than
// in ids, because the reply may not use ids and the model should not have to
// carry a second naming scheme to write one sentence.
func planAfter(step plan.Node, titles map[int]string) string {
	if len(step.Needs) == 0 {
		return "starts straight away"
	}
	names := make([]string, 0, len(step.Needs))
	for _, need := range step.Needs {
		if title := titles[need]; title != "" {
			names = append(names, title)
		}
		if len(names) == boardWaitsCap {
			break
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "after " + strings.Join(names, ", ")
}

// planStepLabel is a step's own short name, on the same rule the board uses for
// a job: the title if it was given one, the first line of what it is otherwise.
func planStepLabel(step plan.Node) string {
	if title := firstLine(step.Title); title != "" {
		return title
	}
	if summary := firstLine(step.Summary); summary != "" {
		return truncateBytes(summary, boardWaitLabelBytes)
	}
	if brief := firstLine(step.Brief); brief != "" {
		return truncateBytes(brief, boardWaitLabelBytes)
	}
	return fmt.Sprintf("step %d", step.ID)
}

// resultChildren gives each part of a job the board's one line. BoardRowCap
// bounds it for the board's own reason: past a dozen rows this is a log rather
// than a list of parts, and the parent's own result already summarises it.
//
// Folded children are included, and the filter that skipped them was backwards.
// Folding is what happens to a job once it is thoroughly over; "what did each
// part conclude" is a question about exactly those parts, and answering it with
// nothing because the work was tidied away is the same status-instead-of-
// substance failure this whole read exists to end.
//
// It reads the whole subtree rather than the direct children, and the depth
// marker is what makes that legible. A job whose plan put its real work two
// layers down — a container per stage, the steps beneath it — answered "what
// did each part conclude" with a list of containers that concluded nothing,
// which is the flat-list lie the board itself had to be taught out of.
func (h *Head) resultChildren(id string) []string {
	nodes, err := h.store.SubtreeNodes(id)
	if err != nil {
		return nil
	}
	depth := map[string]int{id: 0}
	lines := make([]string, 0, BoardRowCap)
	for _, node := range nodes {
		if node.ID == id {
			continue
		}
		// SubtreeNodes returns admission order, so a node's parent has always
		// been seen by the time the node is. A part whose parent somehow is not
		// in hand sits at the first level rather than being dropped.
		level, ok := depth[node.Parent]
		if !ok {
			level = 0
		}
		depth[node.ID] = level + 1
		line := fmt.Sprintf("%s- %s | %s | %s", strings.Repeat("  ", level),
			node.ID, node.Status, surgeryTargetLabel(node))
		if summary := firstLine(h.jobResult(node)); summary != "" {
			line += " | " + summary
		}
		lines = append(lines, line)
		if len(lines) == BoardRowCap {
			break
		}
	}
	return lines
}

// record is the only way the run learns it acted. The command seq is the last
// one journalled, which is what ties the reply to durable work the way every
// other receipt in this package does.
func (run *beltRun) record(seq int64, receipt string) {
	run.acted = true
	if seq != 0 {
		run.commandSeq = seq
		// And the head remembers it is owed a receipt for this one. That is the
		// whole registration the wake needs: a command nobody in a head turn
		// journaled never wakes anybody (wake.go).
		run.head.expectReceipt(seq)
	}
	if receipt != "" {
		run.did = append(run.did, receipt)
	}
}

// summary is the fallback receipt for a loop that acted and then said nothing
// useful. It is assembled from what the tools reported, never from intent.
func (run *beltRun) summary() string {
	if len(run.did) == 0 {
		return "Done."
	}
	return strings.Join(run.did, " ")
}

// beltJob resolves one job id the model named. Only the user's own live work is
// addressable: the resident's practice and its own internals are not on the
// board, so they can never be named, and a hallucinated id fails here.
func (h *Head) beltJob(id string) (store.Node, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Node{}, fmt.Errorf("job must name an id from a board read")
	}
	node, found, err := h.store.Node(id)
	if err != nil {
		return store.Node{}, fmt.Errorf("that job could not be read: %w", err)
	}
	if !found || node.Folded || node.ID == store.RootID {
		return store.Node{}, fmt.Errorf("there is no live work with id %q — read the board again", id)
	}
	if !beltAddressable(node) {
		return store.Node{}, fmt.Errorf("%q is not the user's work and is not yours to change", id)
	}
	if !classOpen(node.Status) {
		return store.Node{}, fmt.Errorf("%q has already finished", id)
	}
	return node, nil
}

// beltAddressable is the ownership membrane the whole belt sits behind. The
// class path draws the same line with its sweeping flag; here it is absolute,
// because a model composing tools has no user word to weigh against it.
func beltAddressable(node store.Node) bool {
	switch node.Group {
	case store.TerritoryGroup, charterNodeGroup, store.PracticeGroup:
		return false
	}
	return node.Provenance.Origin != store.OriginSelf
}

// beltSet resolves an explicit id set exactly the way the class path resolves a
// status set: expand each named id over its open subtree, keep only what the
// verb may legally touch, and reduce the result to the outermost wholly-legal
// units. Naming a job whose leaf is running under a verb that cannot touch
// running work therefore reaches its queued leaves individually and leaves the
// running one alone — the same safety property, arrived at from ids instead of
// from a status word.
//
// strict is the difference between the tool and the answered question. The tool
// is strict so a wrong id comes back as something the model can read and fix;
// the confirmed replay is lenient, so a unit that settled between the question
// and the answer simply drops out rather than failing the whole set.
func (h *Head) beltSet(ids []string, kind store.CommandKind, strict bool) (classSet, error) {
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return classSet{}, err
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	children := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if _, ok := byID[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}
	matched := make(map[string]bool, len(nodes))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		node, present := byID[id]
		if !present || node.Folded || node.ID == store.RootID {
			if strict {
				return classSet{}, fmt.Errorf("there is no live work with id %q — read the board again", id)
			}
			continue
		}
		if !beltAddressable(node) {
			if strict {
				return classSet{}, fmt.Errorf("%q is not the user's work and is not yours to change", id)
			}
			continue
		}
		if !beltLegal(node, kind) {
			if strict {
				return classSet{}, fmt.Errorf("%q is %s, and %s only applies to %s",
					id, beltStatusWord(node), surgeryVerb(kind), beltAllowedWords(kind))
			}
			continue
		}
		for _, member := range beltSubtree(byID, children, id) {
			if beltLegal(byID[member], kind) {
				matched[member] = true
			}
		}
	}
	return classUnits(nodes, matched), nil
}

// beltSubtree walks one named id and everything open beneath it. Settled work
// is skipped rather than walked through: a finished branch has no bearing on
// whether the branch above it is wholly in the set.
func beltSubtree(byID map[string]store.Node, children map[string][]string, root string) []string {
	seen := make(map[string]bool, len(byID))
	members := make([]string, 0, 8)
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		members = append(members, id)
		for _, child := range children[id] {
			if classOpen(byID[child].Status) || byID[child].Status == store.Failed {
				walk(child)
			}
		}
	}
	walk(root)
	return members
}

// beltLegal is the store's own legality table read ahead of time, so a bad
// combination becomes a sentence the model can act on rather than a rejected
// command the user never hears about.
func beltLegal(node store.Node, kind store.CommandKind) bool {
	if node.Folded || node.ID == store.RootID {
		return false
	}
	for _, status := range surgeryAllowedStatuses(kind) {
		if node.Status == status {
			return surgeryEligible(node, kind)
		}
	}
	return false
}

func beltStatusWord(node store.Node) string {
	if node.Held {
		return "paused"
	}
	return string(node.Status)
}

func beltAllowedWords(kind store.CommandKind) string {
	switch kind {
	case store.CommandRestart:
		return "failed or cancelled work"
	case store.CommandResume:
		return "paused work"
	case store.CommandReprioritize:
		return "work that has not started"
	default:
		return "work that is queued or running"
	}
}

func beltVerbKind(verb string) (store.CommandKind, bool) {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case "cancel":
		return store.CommandCancel, true
	case "pause":
		return store.CommandPause, true
	case "resume":
		return store.CommandResume, true
	case "restart":
		return store.CommandRestart, true
	case "reprioritize":
		return store.CommandReprioritize, true
	}
	return "", false
}

// boardRow is one line of live work: what it is, how it is going, and what it
// has cost. Nothing else fits in a row that is resent on every turn.
type boardRow struct {
	node store.Node
	age  string
	// elsewhere marks a job the user started in a different window. The board is
	// cross-session and the thread is not, so without it one conversation can
	// cancel another's work and neither can say where the job came from.
	elsewhere bool
	running   int
	queued    int
	failed    int
	cost      float64
	// waits is what this row is sitting behind, in the names of the jobs it is
	// waiting on. The edges have been in the snapshot the whole time; without
	// this clause the board could say three steps are queued without saying what
	// they are queued behind, which describes a workforce as a number.
	waits string
	// runningFor is how long the longest-running step at or under this row has
	// been going. A job root usually carries no start time of its own — its parts
	// do the work — so a row that said "running" with no duration was the whole
	// reason the head could describe a job forty-nine minutes in as if it had
	// just been asked for.
	runningFor time.Duration
	// owner is the job a part belongs to, empty on a job root.
	owner string
	// result is the row's own first finding. A settled row's finding is what
	// "what did you find?" gets answered from without spending a call; it is
	// dropped when the same words are already elsewhere in the prompt.
	result string
}

// boardRows is the belt's whole read side. It is the ordinary surgery search
// over job roots, annotated with the counts and spend a person would want
// before deciding anything, and narrowed by beltAddressable and nothing else.
//
// It used to require Origin == OriginUser on top of that membrane, and the extra
// conjunct was a bug with a transcript: a charter-fired job carries
// OriginTrigger, which passes the router's snapshot and passes the deep slice
// and failed only here — so the head described the job, the user said "stop that
// job", and the belt answered that no such work of the user's exists. Trigger
// work IS the user's; it came from a charter they ratified. One membrane now,
// and beltAddressable is the one, because it is the documented one and it is
// what still keeps the resident's own practice and internals off the board.
//
// sessionID is the conversation asking. It marks rather than filters: the board
// is global on purpose, and the row says which window a job came from.
func (h *Head) boardRows(sessionID, query, status, id string) ([]boardRow, error) {
	return h.boardRowsAt(sessionID, query, status, id, time.Now())
}

// boardRowsAt is boardRows with the clock passed in. Two of the board's clauses
// are durations — how long a row has been going, how long ago it landed — and a
// clock a caller cannot move is a clause a test cannot pin.
func (h *Head) boardRowsAt(sessionID, query, status, id string, now time.Time) ([]boardRow, error) {
	class := classAll
	if word := strings.ToLower(strings.TrimSpace(status)); word != "" {
		named, ok := classVocabulary[word]
		if !ok {
			return nil, fmt.Errorf("status must be running, queued, failed, or all")
		}
		class = named
	}
	// The snapshot rather than the node list, because the edges are half of what
	// a board row means: what a row is waiting on is the one structural fact the
	// head could never read, and it has been sitting in the same query all along.
	snapshot, err := h.store.ActiveSnapshot()
	if err != nil {
		return nil, fmt.Errorf("the board could not be read: %w", err)
	}
	nodes := snapshot.Nodes
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	children := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if _, ok := byID[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}

	var candidates []store.SurgeryTarget
	switch {
	case strings.TrimSpace(id) != "":
		node, found, err := h.store.Node(strings.TrimSpace(id))
		if err != nil {
			return nil, fmt.Errorf("the board could not be read: %w", err)
		}
		if !found || node.Folded || !beltAddressable(node) || node.ID == store.RootID {
			return nil, nil
		}
		candidates = []store.SurgeryTarget{{Node: node}}
	case strings.TrimSpace(query) != "":
		// No allowed statuses: a read is never narrowed by what some verb could
		// legally touch, only by what the caller asked to see.
		candidates, err = h.store.SearchSurgeryTargets(strings.TrimSpace(query), false)
		if err != nil {
			return nil, fmt.Errorf("the board could not be read: %w", err)
		}
		if len(candidates) == 0 {
			// The search above ranks over ActiveNodes, and a territory packs the
			// jobs it swallows out of that view entirely — the territory row
			// itself is furniture and is filtered, its members are folded under
			// it and are not returned. So a job the retrospective tidied away a
			// few hours after it landed is unreachable by every snapshot-derived
			// read in this package, which is the whole of why Tuesday's job was
			// invisible on Thursday. Recall is the store's own index over folded
			// memory and it does reach them; it is asked only when the ordinary
			// ranking came back empty, so nothing about a live board changes.
			candidates, err = h.recalledCandidates(strings.TrimSpace(query))
			if err != nil {
				return nil, err
			}
		}
	default:
		// The unqueried board is an enumeration, not a search. Ranking words
		// against nothing scores every job the same and then truncates the tie
		// arbitrarily, and a board that silently omits a job is exactly the
		// reality the model must not be handed.
		candidates = boardEnumeration(nodes, byID)
	}

	// A read the caller aimed — by id, or by the words the user used for the
	// work — reaches settled jobs too. The unqueried board is about what is
	// moving; a named read is almost always about what a job came back with,
	// and findings only exist once a job is over. Refusing them here is what
	// made the belt's result tool unreachable for precisely the questions it
	// was written to answer.
	targeted := strings.TrimSpace(id) != "" || strings.TrimSpace(query) != ""

	waits := boardWaits(snapshot, byID)
	rows := make([]boardRow, 0, len(candidates))
	for _, candidate := range candidates {
		if !beltAddressable(candidate.Node) {
			continue
		}
		age := strings.TrimSpace(candidate.Age)
		if age == "" {
			// The search paths carry an age; the plain enumeration does not, and
			// the head was handed an ordering it could not read as one — asked
			// "what did you do yesterday" with no way to tell yesterday from an
			// hour ago. The age is coarse on purpose, so a row does not rewrite
			// itself between two messages the way a running clock would.
			if settled := store.AgeLabel(candidate.Node.FinishedAt, now); settled != "" {
				age = "finished " + settled
			}
		}
		row := boardRow{node: candidate.Node, age: age,
			elsewhere: crossSession(candidate.Node, sessionID),
			waits:     waits[candidate.Node.ID],
			result:    firstLine(h.jobResult(candidate.Node))}
		for _, member := range beltSubtree(byID, children, candidate.Node.ID) {
			switch byID[member].Status {
			case store.Running, store.Claimed:
				row.running++
			case store.Pending:
				row.queued++
			case store.Failed:
				row.failed++
			}
		}
		if !boardJobRoot(candidate.Node, byID) {
			// A part says whose part it is, by the job's own name — the same name
			// the thread, the cards and the receipts use. Without it, sixteen
			// leaves and four roots read as twenty peers and the model has to
			// invent which of them are the workstreams a person would name.
			row.owner = boardOwnerLabel(candidate.Node, byID)
		}
		row.runningFor = boardRunningFor(candidate.Node, byID, children, now)
		if impact, err := h.store.Impact(candidate.Node.ID, now); err == nil {
			row.cost = impact.Cost
		}
		if !boardRowMatches(row, class) && !(targeted && class == classAll) {
			continue
		}
		rows = append(rows, row)
		if len(rows) == BoardRowCap {
			break
		}
	}
	return rows, nil
}

// recalledCandidates turns folded memory back into board rows. It is the last
// resort and it stays that way on purpose: recall is lexical over digests, so
// it answers a question about a topic rather than about the board, and a live
// board must never be reordered by it. What it buys is that the id comes back —
// and an id is all the result and read tools have ever needed.
func (h *Head) recalledCandidates(query string) ([]store.SurgeryTarget, error) {
	hits, err := h.store.Recall(query, nil, BoardRowCap)
	if err != nil {
		// Recall is an additive hint everywhere else it is used, and a miss and
		// a failure are deliberately indistinguishable there. The board keeps
		// that contract: an index that cannot answer leaves the board empty
		// rather than turning a read into an error.
		return nil, nil
	}
	now := time.Now()
	targets := make([]store.SurgeryTarget, 0, len(hits))
	for _, hit := range hits {
		node, found, readErr := h.store.Node(hit.NodeID)
		if readErr != nil || !found || node.ID == store.RootID || !beltAddressable(node) {
			continue
		}
		targets = append(targets, store.SurgeryTarget{
			Node: node, Score: hit.Score, Age: store.AgeLabel(node.FinishedAt, now),
		})
	}
	return targets, nil
}

// boardEnumeration lists the work, jobs before their parts, live first and
// newest first within each band — the same ordering rule the router's snapshot
// followed, and for the same reason: what is happening now must never be the
// thing the cap drops.
//
// Parts are rows, and that was argued for from both directions. A flat list of
// sixteen leaves and four roots as twenty peers left the model to invent which
// of them were the workstreams a person would name; a roll-up with no parts at
// all left it unable to say which step is queued behind which, which is most of
// what "how is it going" means. So the jobs lead with their counts rolled up,
// and a part says whose part it is.
func boardEnumeration(nodes []store.Node, byID map[string]store.Node) []store.SurgeryTarget {
	roots := make([]store.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.ID == store.RootID || !beltAddressable(node) {
			continue
		}
		// Packed history is not the moving board. A job a territory swallowed is
		// reached by an aimed read, which falls back to the fold index for
		// exactly this case.
		if node.Folded {
			continue
		}
		roots = append(roots, node)
	}
	rank := func(node store.Node) int {
		switch {
		case node.Status == store.Running || node.Status == store.Claimed:
			return 0
		case node.Status == store.Pending:
			return 1
		case node.FoldRoot:
			// Packed history: reachable, but last in line for the budget.
			return 3
		default:
			return 2
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		// Jobs before their parts, always: the flat list was a lie of omission.
		if pi, pj := boardJobRoot(roots[i], byID), boardJobRoot(roots[j], byID); pi != pj {
			return pi
		}
		ri, rj := rank(roots[i]), rank(roots[j])
		if ri != rj {
			return ri < rj
		}
		if ri >= 2 {
			// Settled work is ordered by when it settled, because the question it
			// answers is "what happened", and creation order answers a different
			// one.
			return roots[i].FinishedAt.After(roots[j].FinishedAt)
		}
		return roots[i].CreatedSeq > roots[j].CreatedSeq
	})
	targets := make([]store.SurgeryTarget, 0, len(roots))
	for _, node := range roots {
		targets = append(targets, store.SurgeryTarget{Node: node})
	}
	return targets
}

// boardRowMatches reads the class off the job as a whole rather than off its
// root node, because "the running ones" means jobs with somebody working on
// them, not jobs whose root happens to carry a running status.
// boardRowMatches reads the class off the job as a whole rather than off its
// root node, because "the running ones" means jobs with somebody working on
// them, not jobs whose root happens to carry a running status.
//
// The unnarrowed board is what is MOVING, and cleanly-finished work is left off
// it deliberately. That was tried the other way — settled rows on the plain
// board — and it costs more than it buys: an unmatched job's finding leaks into
// every prompt, which is exactly the pollution the deep slice's relevance floor
// exists to prevent, and the budget then spends itself on history while the job
// running right now competes for what is left. Settled work is reached instead
// by the four reads written for it: an aimed board read by id or by the person's
// own words, result, history, and search. Failed work stays, because a failure
// is not finished business.
func boardRowMatches(row boardRow, class string) bool {
	switch class {
	case classRunning:
		return row.running > 0
	case classQueued:
		return row.queued > 0
	case classFailed:
		return row.failed > 0 || row.node.Status == store.Failed
	default:
		return row.running > 0 || row.queued > 0 || row.failed > 0 ||
			classOpen(row.node.Status) || row.node.Status == store.Failed
	}
}

// dimeUSD spells money for a prompt at the resolution a person actually decides
// on. Position by volatility applies to precision as well as to order: a figure
// is only allowed to be as precise as it is stable, and a cent on a live job
// ticks constantly while nobody cancels a job over three cents. Rounded to a
// dime the line holds still for as long as the decision it informs. The exact
// figure stays exact everywhere it is read as a number rather than said to a
// model: the TUI, the receipts, the store.
func dimeUSD(cost float64) string {
	return fmt.Sprintf("$%.2f", math.Round(cost*10)/10)
}

// renderBoard is THE board renderer. There were two — the router's renderGraph
// over a raw snapshot and the belt's boardRows over ranked job roots — with
// different budgets, different ownership rules, different vocabularies and no
// agreement about what a row even was. They disagreed in production: a
// charter-fired job passed one and failed the other, so the head described the
// job in one sentence and answered "there is no work of the user's with that id"
// to "stop it" in the next. One renderer, one query, one membrane.
func renderBoard(rows []boardRow) string {
	return renderBoardWithin(rows, "", nil, 0)
}

// renderBoardWithin is renderBoard with the prompt's two extra jobs: a byte
// ceiling that says out loud when it bites, and the dedup against what the rest
// of the prompt has already said. A finding the person can read in the thread
// above, or one the depth block below is about to quote in full, is the same
// sentence stated twice — which costs budget and reads to a model as
// corroboration.
func renderBoardWithin(rows []boardRow, thread string, opened map[string]bool, budget int) string {
	var rendered strings.Builder
	for _, row := range rows {
		line := fmt.Sprintf("- %s | %s | %s | %d running, %d queued",
			row.node.ID, surgeryTargetLabel(row.node), boardRowStatus(row), row.running, row.queued)
		if row.failed > 0 {
			line += fmt.Sprintf(", %d failed", row.failed)
		}
		// Dimes, not cents. Within one turn the board is written once and the
		// transcript is append-only, so the damage was at the seam between
		// messages: a single cent ticking on a single live job rewrote the board,
		// and the board is the first volatile thing in the prompt.
		line += " | " + dimeUSD(row.cost)
		if age := strings.TrimSpace(row.age); age != "" {
			line += " | " + age
		}
		if result := strings.TrimSpace(row.result); result != "" &&
			!opened[row.node.ID] && !deepAlreadyInThread(thread, result) {
			line += " | result: " + result
		}
		if row.owner != "" {
			line += " | part of " + row.owner
		}
		if row.waits != "" {
			line += " | waits on " + row.waits
		}
		if row.runningFor > 0 {
			line += " | running " + boardElapsed(row.runningFor)
		}
		if row.elsewhere {
			line += crossSessionMark
		}
		line += "\n"
		if budget > 0 && rendered.Len()+len(line) > budget-len(snapshotTruncatedMark) {
			rendered.WriteString(snapshotTruncatedMark)
			break
		}
		rendered.WriteString(line)
	}
	return strings.TrimSuffix(rendered.String(), "\n")
}

func boardRowStatus(row boardRow) string {
	switch {
	case row.node.Held:
		return "paused"
	case row.running > 0:
		return "running"
	case row.queued > 0:
		return "queued"
	default:
		return string(row.node.Status)
	}
}

// askBeltConfirm is the one question the belt is allowed, and it is the class
// path's question with an id list where the class word was. Both name the count
// before anything moves, because the difference between one node and fourteen
// is the whole reason the user would want to be asked.
func (h *Head) askBeltConfirm(user store.Message, confirm *beltConfirm) error {
	class := classIntent{Class: classAll}
	verb := surgeryVerb(confirm.kind)
	prompt := fmt.Sprintf("%s %s?", upperFirst(verb), classSetPhrase(confirm.set, class))
	if confirm.impact.Cost > 0 || confirm.impact.RunningFor > 0 {
		// The set's own count is already in the prompt, so the loss clause must
		// not repeat it.
		lossOnly := confirm.impact
		lossOnly.OpenNodes, lossOnly.Nodes = 0, 0
		prompt += " " + surgeryLoss(confirm.kind, lossOnly)
	}
	encoded := encodeBeltIDs(confirm.ids)
	return h.askSurgerySetConfirm(user, prompt, confirm.set,
		store.QuestionOption{
			Label: fmt.Sprintf("yes, %s all %d", verb, confirm.set.Affected),
			Value: encodeSurgeryOption(beltConfirmAction, confirm.kind, encoded, user.Body)},
		store.QuestionOption{
			Label: classKeepLabel(confirm.kind, class),
			Value: encodeSurgeryOption(beltKeepAction, confirm.kind, encoded, user.Body)})
}

// applyBeltOption settles a belt confirmation. Like the class answers, it
// re-resolves from the ids as the graph stands now rather than from a frozen
// list of units: the set the user agreed to is the set as it stands when they
// agree.
func (h *Head) applyBeltOption(user store.Message, action string, kind store.CommandKind,
	target, instruction string) (bool, error) {
	switch action {
	case beltKeepAction:
		return true, h.postAgent(user.SessionID, "Keeping them as they are.", 0)
	case beltConfirmAction:
	default:
		return false, nil
	}
	ids, ok := decodeBeltIDs(target)
	if !ok {
		return true, h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	set, err := h.beltSet(ids, kind, false)
	if err != nil {
		return true, err
	}
	if len(set.Units) == 0 {
		return true, h.postAgent(user.SessionID, classEmptyReply(kind, classIntent{Class: classAll}), 0)
	}
	labels, seq := h.journalUnits(user, kind, instruction, set.Units)
	if len(labels) == 0 {
		return true, h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	if len(labels) < len(set.Units) {
		set.Affected = len(labels)
		set.Jobs = 0
	}
	return true, h.postAgent(user.SessionID,
		classReceipt(kind, classIntent{Class: classAll}, set, labels), seq)
}

// encodeBeltIDs hides the separator inside the option's single target field.
// Node ids are opaque strings and the codec splits on colons, so the list is
// encoded rather than joined.
func encodeBeltIDs(ids []string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(ids, "\n")))
}

func decodeBeltIDs(value string) ([]string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, false
	}
	ids := make([]string, 0, 4)
	for _, id := range strings.Split(string(decoded), "\n") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, len(ids) > 0
}

func beltString(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// beltInt reads a numeric argument through the two shapes JSON gives it: a
// number, which decodes to float64, and the same number spelled as a string,
// which several providers emit for integer-typed tool parameters.
func beltInt(args map[string]any, key string) int64 {
	switch value := args[key].(type) {
	case float64:
		return int64(value)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "#")), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}

// beltStrings accepts both shapes providers actually emit for a list argument:
// a JSON array, and a single comma-separated string.
func beltStrings(args map[string]any, key string) []string {
	switch value := args[key].(type) {
	case []any:
		ids := make([]string, 0, len(value))
		for _, item := range value {
			if id, ok := item.(string); ok && strings.TrimSpace(id) != "" {
				ids = append(ids, strings.TrimSpace(id))
			}
		}
		return ids
	case string:
		ids := make([]string, 0, 4)
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	return nil
}
