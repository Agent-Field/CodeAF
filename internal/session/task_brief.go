package session

// WHAT A WORKER IS ACTUALLY TOLD, and who decides its shape.
//
// A task node and an adaptive run's node both open on ONE user message and
// never see the conversation that commissioned them. For a long time that
// message was whatever the chat model typed into `brief`: a paraphrase, written
// from memory, of something a person had said in their own words a moment
// earlier. The person's sentence was nowhere in the thread. When the paraphrase
// dropped a requirement — a path, a format, a "don't touch the tests" — nothing
// downstream could notice, because there was nothing to compare it against.
//
// THE PERSON'S WORDS ARE CAPTURED BY THIS PACKAGE, NOT ASKED FOR. The message
// that triggered the work is already in hand at propose time ([Agent.personAsk]
// records it where the transcript records it), so it travels as a field on the
// spec and this file lays it out. A model cannot forget to include what it was
// never asked to include, and it cannot "helpfully" tidy it on the way past.
//
// THE SHAPE LIVES HERE AND NOWHERE ELSE. The schema descriptions and
// prompts/system.md say what each FIELD is for; they do not spell the layout,
// because a format written in two places is a format that will disagree with
// itself (the law CLAUDE.md states about interpolated numbers, applied to
// prose). [composeBrief] is the one place the sections and their order are
// decided, and both shapes of work go through it.

import "strings"

// The section headings, in the order [composeBrief] lays them out. They are
// SHOUTED because the worker reads this as a document rather than as a sentence
// — the same voice the run's own node briefs already use for their bounds
// (orchestrate.go's [orchestrateBrief]).
const (
	briefAskHeading  = "WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS"
	briefWorkHeading = "THE WORK"
	briefMakeHeading = "WHAT TO PRODUCE"
	briefDoneHeading = "DONE WHEN"
)

// briefAskRule is the one line that says what the person's words are FOR. A
// worker handed two accounts of the same job needs to be told which one wins,
// and it is not the one the model wrote.
const briefAskRule = "This is the message this work came out of. Where anything below reads differently from it, their words are what was asked for."

// briefAskLimit bounds the verbatim ask, and it is generous on purpose: a
// person's request is usually a paragraph and occasionally a page, and the
// whole value of carrying it is that nothing was edited out of it. What the
// bound is really for is the other case — a pasted log, a whole file dropped
// into the chat — where an unbounded copy would put megabytes into every node
// prompt of a run. The cut is marked (see [clip]), so a worker that has been
// given a truncated ask can see that it was.
const briefAskLimit = 6000

// composeBrief lays out one worker's opening message: the person's request in
// their own words, then the contract the conversation groomed out of it.
//
// AN EMPTY SECTION IS ABSENT, not an empty heading — the emptiness law, applied
// to a document. A node restored from a checkpoint written before requests were
// carried, a graph a test scripted by hand, a person-authored task with no
// deliverable named: each simply has fewer sections, and none of them gets a
// heading over nothing.
//
// A REQUEST THAT IS ALSO THE WORK IS PRINTED ONCE. When a person writes the
// brief themselves (task_person.go's [Agent.StartTask]) there is no paraphrase
// to put under THE WORK — their words are the whole of it — and printing the
// same paragraph twice under two headings would read as two instructions that
// happen to agree.
func composeBrief(request, work, deliverable, acceptance, expects string) string {
	request = clip(strings.TrimSpace(request), briefAskLimit)
	work = strings.TrimSpace(work)
	if work == request {
		work = ""
	}
	var out strings.Builder
	section := func(heading, rule, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(heading)
		if rule != "" {
			out.WriteString("\n" + rule)
		}
		out.WriteString("\n\n" + body)
	}
	section(briefAskHeading, briefAskRule, request)
	section(briefWorkHeading, "", work)
	section(briefMakeHeading, "", deliverable)
	section(briefDoneHeading, "", acceptance)
	// AND WHAT THE HANDOFF PROMISED ABOUT THE WORLD, last, because it is the
	// only section that is about the folder rather than about the job
	// (handoffcontract.go). A handoff that promised nothing has no section, like
	// every other empty one here.
	section(briefExpectsHeading, briefExpectsRule, expects)
	return out.String()
}

// ── the person's words, as the session hears them ───────────────────────────

// rememberAskLocked keeps the last thing THE PERSON said, so that work handed
// out later in the turn can carry it verbatim.
//
// It is the same test the other lanes in this package make of a user message —
// routeJudge and routeHarness both ask "did somebody actually type this" the
// same way — and it is made here for the same reason: a
// wake note is the session talking to itself, and a task briefed with "task 4
// has finished" as the person's request would be quoting a sentence nobody
// said.
//
// The caller holds a.mu: this runs where the message reaches the transcript
// ([Agent.startTurnLocked] and the steering drain), so what a tool reads mid-turn
// is the newest thing the person has typed, steering included.
func (a *Agent) rememberAskLocked(user userMessage) {
	if user.empty() || user.wake || user.authored {
		return
	}
	if text := strings.TrimSpace(user.text()); text != "" {
		a.personAsk = text
		// AND THE SESSION'S GOAL OWNER IS TOLD THE SAME THING, in the same
		// place, on the same test (principal.go). It is one writer rather than
		// two for the reason stated directly below: a second recorder of the
		// person's words is a second answer to what was asked, and the two
		// answers drift on exactly the sessions where it matters.
		a.hearAsk(text)
	}
}

// THERE IS NO SECOND RECORDER ANY MORE. `rememberAsk` sat here for words that
// never became a chat message — what somebody typed into a command that starts
// work — and its only caller was the planner door in task_person.go, which went
// with `/task adaptive`. Every road left records the ask where the message
// itself is recorded, above, so this is the one writer of [Agent.personAsk] and
// there is nowhere a second one could disagree with it.

// taskRequest is the person's ask AS THIS AGENT KNOWS IT, and the two answers
// are the two kinds of agent there are.
//
// In a CONVERSATION it is what they typed. In a NODE there is nobody to type
// anything — the node's whole world is the brief it was given — so it INHERITS
// the request of the task it was handed out by, which is how a sub-task three
// levels down is still working against the sentence that started all of it
// rather than against a paraphrase of a paraphrase.
func (a *Agent) taskRequest() string {
	if a.config.InTask {
		if parent := a.graph().node(a.config.taskID); parent != nil {
			return parent.request()
		}
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.personAsk
}
