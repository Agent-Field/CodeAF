package session

// A QUICK TASK IS WORK YOU WILL READ THE RESULT OF AND CARRY ON.
//
// The big task is unchanged and this is beside it, not inside it: a node of
// kind `quick` runs a worker WHERE THE CALLER WORKS — the caller's own
// workspace, no worktree, no branch, no merge — started by one tool call that
// returns the id at once, with no sizing, no shaping, no countdown card and no
// consent. It has a `line` saying what to do and `items` it works through in
// order, ticking each. It has no audit and no landing: ITS LAST MESSAGE IS ITS
// RESULT, delivered to the caller as the ordinary landing note, and the row
// goes `done`.
//
// WHAT IT REUSES IS EVERYTHING. The id, the row, the room, the ✕, the
// checkpoint and the landing road are the ones every task already has; the kind
// is derived from the spec like every other kind ([taskSpec.kind]); the row's
// state word is replaced by [TaskNode.doingNow], which is how a row reads
// `quick · 2/4 · reading foo.go` with no surface change; and nesting is
// [TaskNotice.Parent], so a quick node started from inside a task hangs under it
// on the rail for free. The only genuinely new machinery in this file is the
// list — the items and the verb that ticks them — and the WRITE CLAIM, which
// is [Config.writeScope] wired to a door instead of to an adaptive run's node.
//
// TWO QUICK NODES THAT CLAIM ONE PATH RUN ONE AFTER THE OTHER, and they do it
// through the edge the graph already has rather than through a lock of their
// own: at admission the second node's `files` are compared against every
// running or queued quick node's ([forkScopesCollide]), and a collision appends
// that node's id to `depends_on`. [TaskGraph.readinessLocked] then does the
// waiting, and nothing in the frontier had to learn a new idea.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// quickWord is the word a quick node's row leads with, and it is the same word
// the tool is named for and the same word [TaskKindWord] answers. Written once
// here because a row saying `quick` while the kind says something else is two
// answers to one question (design-law §ONE SOURCE OF TRUTH).
const quickWord = "quick"

// quickTaskToolName and quickItemsToolName are the two verbs this file puts on
// a belt. They are constants because three readers spell them — the belt, the
// manual gate and the prompt page — and a name typed three times is a name that
// drifts twice.
const (
	quickTaskToolName  = "quick_task"
	quickItemsToolName = "items"
)

// quickTaskSpec is what a quick node is, and it is deliberately four fields:
// what to do, the list it works through, what it said it would write, and how
// far down the list it has got.
//
// IT IS MUTATED WHILE THE NODE RUNS, which no other spec in this package is,
// and that is the one thing to know about reading it. `items` and `done` grow
// and change under the graph's lock — the `items` tool is the only writer
// ([TaskNode.quickItemChange]) — so every reader of either takes
// `graph.mu` first, exactly as the row's own fields are read.
type quickTaskSpec struct {
	// line is what to do, in one sentence. It is also the node's title when the
	// caller named none.
	line string
	// items is the ordered list the worker works through. Empty is ordinary and
	// means the line is the whole job.
	items []string
	// files is the write claim, already through [normalizeWriteScope] so it is
	// in the guard's own form. EMPTY IS UNRESTRICTED and not "writes nothing" —
	// that is [Config.writeScope]'s own law (fork.go), stated here because a
	// reader of this field would otherwise reasonably assume the opposite.
	files []string
	// done is parallel to items: done[i] says item i+1 has been ticked. It is
	// written only by the `items` tool and read only under the graph's lock.
	done []bool
	// waits is WHY THIS NODE WAS QUEUED BEHIND ANOTHER, kept from admission so
	// the receipt the model reads can name the task and the path rather than
	// making it work the collision out for itself. It is a record of the moment
	// of admission and nothing reads it afterwards.
	waits []quickClaim
}

// quickClaim is one collision found at admission: the node already claiming a
// path, and the path both of them claim.
type quickClaim struct {
	id   uint64
	path string
}

// nextItemLocked is the item the worker is on: the first one nothing has ticked.
// It answers "" for a list that is finished and for a node with no list at all,
// which the emptiness law then draws as nothing. Called with the graph's lock
// held.
func (s *quickTaskSpec) nextItemLocked() string {
	for index, item := range s.items {
		if index < len(s.done) && s.done[index] {
			continue
		}
		return item
	}
	return ""
}

// doneCountLocked is how many items have been ticked. Called with the graph's
// lock held.
func (s *quickTaskSpec) doneCountLocked() int {
	count := 0
	for _, ticked := range s.done {
		if ticked {
			count++
		}
	}
	return count
}

// quickDoing is the row's state word for a quick node: the kind, how far down
// the list it is, and the item it is on — `quick · 2/4 · reading foo.go`.
//
// IT IS THE WHOLE OF THE SURFACE CHANGE A LIST NEEDED. [TaskNode.doingNow]
// already replaces the word a row draws, so nothing in tui3 had to learn what
// an item is: the engine writes the sentence and the row prints it.
//
// A NODE WITH NO LIST SAYS `quick` AND NOTHING ELSE, and a list that is finished
// says the count without an item after it — the emptiness law, applied to a row:
// `quick · 0/0` and a trailing separator over nothing are both a machine telling
// somebody about a list that is not there. Called with the graph's lock held.
func quickDoing(spec *quickTaskSpec) string {
	if spec == nil || len(spec.items) == 0 {
		return quickWord
	}
	line := fmt.Sprintf("%s · %d/%d", quickWord, spec.doneCountLocked(), len(spec.items))
	if next := spec.nextItemLocked(); next != "" {
		line += " · " + next
	}
	return clip(line, hintLimit)
}

// ── the door ────────────────────────────────────────────────────────────────

// quickTaskDescription is what the model reads before it calls, and the middle
// of it is THE JUDGE — the six sentences that decide between a task, a quick
// task and doing the thing yourself.
//
// THE JUDGE IS WRITTEN ONCE, HERE. It is the only place in the product that
// draws the line between the two kinds of handoff, and a second copy of it in
// prompts/system.md or in the manual would be a second opinion the day one of
// them was edited. What the belt's own bullet says is which verb this is
// (beltfacts.go); what the page says is how to be a quick worker
// (prompts/quick.md); the choice is this paragraph.
//
// AND IT IS THE JUDGE AND NOTHING ELSE, for [taskDescription]'s reason about
// itself: this string rides in the tool block in front of every request of
// every turn, so a sentence here is paid some sixty times over one turn. What
// would otherwise open it — that the id returns at once and its answer starts a
// turn here, so there is nothing to poll — is [taskDescription]'s already, and
// the two verbs are on a belt together or on neither ([Config.mayQuickTask]).
const quickTaskDescription = "A task gets its own copy of the folder, is checked, and lands. A quick task works " +
	"where you are and its last message is its answer. If you will read the result and carry on, it is quick. " +
	"If it must be checked and merged on its own, or survive the window closing, it is a task. One edit, one " +
	"read, one command is a step: do it yourself. Related steps that share what they learn are one quick " +
	"task's items, not several quick tasks."

// quickTaskSchemaJSON is the wire schema. Every field but `line` is optional,
// which is the whole shape of the verb: there is no contract to groom, no
// deliverable to name and nothing to be checked against, so a caller that knows
// only what it wants done can call it with one string.
//
// AND THE FIELDS SAY THEIR RULE ONCE AND NOWHERE ELSE. `depends_on` and `model`
// are `propose_task`'s fields with the same meanings and the same refusals, and
// that schema spells both in full (task.go); the two verbs are on a belt
// together or on neither ([Config.mayQuickTask]), so a second copy here would be
// bytes the person pays for on every request of every turn to be told a rule
// their model is already holding.
const quickTaskSchemaJSON = `{"type":"object","properties":{` +
	`"line":{"type":"string","description":"What to do, one sentence"},` +
	`"items":{"type":"array","items":{"type":"string"},"description":"Ordered steps it works through and ticks off"},` +
	`"files":{"type":"array","items":{"type":"string"},"description":"Paths it will write. Two claiming one path run one after the other; named none, it writes anywhere"},` +
	`"depends_on":{"type":"array","items":{"type":"integer"},"description":"Ids whose result it needs"},` +
	`"title":{"type":"string","description":"Row title; the line is used without one"},` +
	`"model":{"type":"string","description":"ONLY where the person named a model"}` +
	`},"required":["line"],"additionalProperties":false}`

// quickArguments is the wire form.
type quickArguments struct {
	Line      string   `json:"line"`
	Items     []string `json:"items"`
	Files     []string `json:"files"`
	DependsOn []uint64 `json:"depends_on"`
	Title     string   `json:"title"`
	Model     string   `json:"model"`
}

// quickTools is the belt door for `quick_task`.
//
// AT THE FLOOR OF THE TREE THE TOOL IS ABSENT, NOT REFUSING, and it is withheld
// on exactly the predicate `propose_task` is withheld on ([Agent.mayProposeTask],
// task.go): a node standing on the floor has nothing left worth handing out, and
// a capability that cannot work is left off the belt rather than left there to
// fail. The two verbs come and go together because they answer one question — is
// there anywhere for work to go from here — and a belt that carried one without
// the other would be telling a model half a truth about its own depth.
func (a *Agent) quickTools() []bare.Tool {
	if !a.mayProposeTask() {
		return nil
	}
	return []bare.Tool{{
		Name:        quickTaskToolName,
		Description: quickTaskDescription,
		Schema:      json.RawMessage(quickTaskSchemaJSON),
		Execute:     a.startQuickTask,
	}}
}

// startQuickTask is the tool's whole life: parse, build the spec, admit.
//
// THERE IS NO CARD AND NO COUNTDOWN, which is the whole point of the verb, so
// there is nothing between the call and the work: the id is reserved and the
// node is admitted in the same breath, and the frontier has usually started it
// before this function has returned its sentence.
//
// Everything it can answer badly is an ordinary tool result rather than a Go
// error, the way every other door on this belt answers: a line the model forgot
// to write is a call it can make again, and an error would end the turn over a
// missing field.
func (a *Agent) startQuickTask(_ context.Context, args json.RawMessage) (string, bool, error) {
	var parsed quickArguments
	if err := json.Unmarshal(args, &parsed); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	// THE LOOK AND THE ADMIT ARE ONE MOVE. The write claim is read out of the
	// graph by [Agent.newQuickSpec] and written into it by [TaskGraph.admit], and
	// a batch of quick calls runs concurrently (loop.go) — so without this gate
	// two doors claiming one path would each find the graph empty of the other
	// and each start. Any other door that grows a quick node has to hold it over
	// the same pair; nothing else in this package touches it.
	graph := a.graph()
	graph.quickGate.Lock()
	defer graph.quickGate.Unlock()

	spec, refusal := a.newQuickSpec(parsed.Line, parsed.Items, parsed.Files, parsed.DependsOn)
	if refusal != "" {
		return refusal, true, nil
	}
	if title := strings.TrimSpace(parsed.Title); title != "" {
		spec.title = clip(firstLine(title), titleLimit)
	}
	// WHICH HANDS THE WORK LEAVES ON, on [Agent.proposeTask]'s own terms and
	// through the same resolver (taskmodel.go). THE SHORTLIST IS CLOSED HERE
	// rather than carried: a word that fits several models is settled on a card
	// for a task, and a quick task has no card, so the closest match is taken and
	// the work starts — which is the promise the verb makes.
	choice := a.resolveTaskModel(strings.TrimSpace(parsed.Model))
	if choice.problem != "" {
		return choice.problem, true, nil
	}
	spec.model = choice.model
	if len(choice.options) > 0 {
		spec.model = settleTaskModel(choice.options, "")
	}
	// THE SLOT IS TAKEN BEFORE THE NODE IS, so a batch of quick calls emitted
	// together cannot walk through the fan cap ([TaskGraph.claimChild]). Nothing
	// between here and admission can fail, and [TaskGraph.admit] hands the slot
	// back itself as the node starts counting for itself, so there is no release
	// path for this door to remember.
	if refused := graph.claimChild(spec.parent); refused != "" {
		return refused, true, nil
	}
	id := graph.reserve()
	graph.admit(id, spec)
	return quickStartedWord(id, spec), false, nil
}

// quickStartedWord is the receipt, and it mirrors `propose_task`'s: the id
// first, because the id is the thing the model may need again, then the name,
// then how much there is to do.
//
// A LIST OF NOTHING IS NOT MENTIONED — the emptiness law, applied to a receipt:
// "· 0 items" is a machine reporting the absence of a list nobody asked for.
// And a node queued behind another says so with the reason, because "started"
// on its own would be a promise this node is not keeping yet.
func quickStartedWord(id uint64, spec taskSpec) string {
	line := fmt.Sprintf("quick task %d started: %s", id, spec.title)
	quick := spec.quick
	switch n := len(quick.items); {
	case n == 1:
		line += " · 1 item"
	case n > 1:
		line += fmt.Sprintf(" · %d items", n)
	}
	for _, claim := range quick.waits {
		line += fmt.Sprintf(" · waits for task %d (both claim %s)", claim.id, claim.path)
	}
	return line
}

// newQuickSpec builds the [taskSpec] for one quick node, or the one sentence
// the model reads back instead.
//
// REFUSALS ARE RESULTS THE MODEL READS, NEVER ERRORS, which is this package's
// law for every door: a scope written the wrong way round or a dependency on a
// number that is not a task is a call the model can make again, correctly.
//
// THE TITLE IS THE LINE AND NOBODY IS ASKED TO NAME IT. `named` is set so the
// naming call never happens ([TaskGraph.nameNode]): a quick task's whole promise
// is that it starts now, and spending a model call on a better row title before
// it does would be the harness charging the person for the promise it just broke.
//
// AND THE WRITE CLAIMS ARE SERIALISED HERE, at admission, because this is where
// the frontier can still be told about it. Every running or queued quick node in
// the same workspace whose claim overlaps this one has its id appended to
// `depends_on`, and [TaskGraph.readinessLocked] does the waiting from there —
// so two quick nodes that claim one path run one after the other with no lock,
// no retry and nothing new in the frontier.
func (a *Agent) newQuickSpec(line string, items, files []string, dependsOn []uint64) (taskSpec, string) {
	line = firstLine(strings.TrimSpace(line))
	if line == "" {
		return taskSpec{}, "`line` is empty. Say in one sentence what this quick task is to do, and call again."
	}
	kept := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			kept = append(kept, item)
		}
	}
	scope, refusal := normalizeWriteScope(a.config.Workspace, files)
	if refusal != "" {
		return taskSpec{}, refusal
	}
	// A DEPENDENCY THAT CAN NEVER RESOLVE IS REFUSED AT THE DOOR, on
	// [Agent.refuseProposedTask]'s terms and through the same reader: an id no
	// node carries or one whose node already failed is a wait that only ever ends
	// in the cascade, and it is cheaper refused here than admitted and killed on
	// the next frontier turn.
	//
	// THE SPAWN FLOOR IS NOT ASKED, and that is deliberate rather than an
	// omission. It exists to stop a one-command ask being translated into a
	// worktree and lost (spawnfloor.go); a quick task IS the answer that floor
	// was protecting, so applying it here would refuse the verb for exactly the
	// work it was built for.
	if missing, failed := a.graph().doomedDependencies(dependsOn); len(missing)+len(failed) > 0 {
		return taskSpec{}, dependencyRefusal(missing, failed)
	}
	quick := &quickTaskSpec{line: line, items: kept, files: scope, done: make([]bool, len(kept))}
	quick.waits = a.graph().quickClaimsOn(a.config.Workspace, scope)
	for _, claim := range quick.waits {
		dependsOn = append(dependsOn, claim.id)
	}
	return taskSpec{
		title: clip(line, titleLimit),
		named: true,
		// THE SUMMARY, THE BRIEF AND THE LINE ARE ONE SENTENCE SAID ONCE. A task's
		// three are three different documents a model groomed; a quick task has no
		// grooming, so the honest answer to all of them is the line — and a
		// deliverable and an acceptance are left EMPTY rather than invented,
		// because nothing will ever check this node and a "DONE WHEN" nobody reads
		// is a contract that is not one.
		summary:   line,
		brief:     line,
		dependsOn: dependsOn,
		parent:    a.config.taskID,
		depth:     a.config.taskDepth + 1,
		owner:     a,
		request:   a.taskRequest(),
		origin:    a.taskOriginRef(),
		admission: a.admissionContext(),
		// WHERE IT RUNS, SAID HONESTLY ON THE CARD. There is no worktree and no
		// copy: the ground IS the workspace and the mode is the person's own "here",
		// so the card's "where" line names the folder the work is actually
		// happening in ([TaskMode] spells the five modes out).
		ground: a.config.Workspace,
		mode:   TaskModeInPlace,
		// "in place" is the person's own word for this (places.go), and it is
		// written here rather than left blank because the index's `where` column
		// is read by somebody looking back at what a row did, and "" there reads
		// as a question nobody answered rather than as an answer.
		where: "in place",
		quick: quick,
	}, ""
}

// quickClaimsOn is the write-claim collision, read across the whole graph: which
// running or queued quick nodes in this workspace have already claimed a path
// this one wants, and which path each of them collides on.
//
// A NODE THAT HAS LANDED IS NOT A COLLISION. Its writes are on disk and finished,
// and queueing behind it would be waiting for something that is not happening —
// which is why the state is asked rather than merely the kind.
//
// AND A CLAIM OF NOTHING COLLIDES WITH NOTHING, though an empty scope is
// UNRESTRICTED at the guard (fork.go's law). The two are not in conflict: this is
// the SEQUENCING question, and a node that named no files made no promise anybody
// can be sequenced against. A caller that wants to be waited for names its files.
func (g *TaskGraph) quickClaimsOn(workspace string, files []string) []quickClaim {
	if g == nil || len(files) == 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var claims []quickClaim
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.spec.quick == nil {
			continue
		}
		if node.state != TaskRunning && node.state != TaskQueued {
			continue
		}
		if node.Ground != workspace {
			continue
		}
		if path, collides := forkScopesCollide(node.spec.quick.files, files); collides {
			claims = append(claims, quickClaim{id: id, path: path})
		}
	}
	return claims
}

// ── the list ────────────────────────────────────────────────────────────────

// quickItemsDescription is what a quick worker reads about its own list. It is
// short because the page it works from says what the list is FOR
// (prompts/quick.md) and this says only what the call does.
const quickItemsDescription = "Tick an item off your list, or add items to it. Call it with `done` the moment an item is finished — the row the person is watching reads the item you are on — and with `add` when the work turns out to have a step your list does not name."

// quickItemsSchemaJSON is the wire schema: one number, one list, neither
// required, because a call may tick and append in the same breath.
const quickItemsSchemaJSON = `{"type":"object","properties":{` +
	`"done":{"type":"integer","description":"The item that is finished, counting from 1"},` +
	`"add":{"type":"array","items":{"type":"string"},"description":"Steps to append to the end of the list"}` +
	`},"additionalProperties":false}`

// itemsTool is the verb a QUICK WORKER carries and nothing else does.
//
// It hangs off a door wired from the node ([TaskNode.quickDoor]) rather than off
// anything a caller can set, which is `revise_design`'s own shape and is there
// for the same reason: a belt is assembled once, when the agent is constructed
// (agent.go), so the list has to be reachable from the config before the worker
// exists. A conversation, a task worker and a hand all have no door and
// therefore no verb — absent, not refusing.
func (a *Agent) itemsTool() bare.Tool {
	return bare.Tool{
		Name:        quickItemsToolName,
		Description: quickItemsDescription,
		Schema:      json.RawMessage(quickItemsSchemaJSON),
		Execute:     a.tickQuickItems,
	}
}

// tickQuickItems is the `items` call: it does nothing itself and hands the
// change to the node's own door, which is the only thing that may touch a list
// under the graph's lock.
func (a *Agent) tickQuickItems(_ context.Context, args json.RawMessage) (string, bool, error) {
	door := a.config.quickItems
	if door == nil {
		// Unreachable through the belt — the tool is absent without a door — and
		// answered rather than panicked because a control plane that rebuilt a
		// belt at the wrong moment must not take the turn down with it.
		return "there is no list here", true, nil
	}
	var parsed struct {
		Done int      `json:"done"`
		Add  []string `json:"add"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	return door(parsed.Done, parsed.Add), false, nil
}

// quickScope is the write bound this node's worker runs under: the claim it
// made, or nothing at all for every node that is not a quick one.
//
// NOTHING AT ALL IS UNRESTRICTED at the guard (orchestrate.go's [writeGuard]
// returns early on a scope of length zero), which is exactly right in both
// cases and worth saying because it reads backwards: a task in a worktree is
// unbounded because the worktree IS the bound, and a quick task that named no
// files is unbounded because it promised nothing.
func (n *TaskNode) quickScope() []string {
	if n == nil || n.spec.quick == nil {
		return nil
	}
	return n.spec.quick.files
}

// quickDoor is the closure the worker's config carries, and it is nil for every
// node that is not a quick one — which is what keeps `items` off every other
// belt in this build.
func (n *TaskNode) quickDoor() func(int, []string) string {
	if n == nil || n.spec.quick == nil {
		return nil
	}
	return n.quickItemChange
}

// quickItemChange is the one writer of a quick node's list: it ticks, it
// appends, and it moves the row in the same breath.
//
// THE ROW IS MOVED OUTSIDE THE LOCK, because [TaskNode.doingNow] takes the
// graph's lock itself and announces under it. The word is composed while the
// list is held still and said afterwards, which is this package's ordinary
// shape for "read a fact, then tell the world about it".
func (n *TaskNode) quickItemChange(done int, add []string) string {
	spec := n.spec.quick
	n.graph.mu.Lock()
	var reply string
	switch {
	case done >= 1 && done <= len(spec.items):
		spec.done[done-1] = true
		reply = fmt.Sprintf("items %d/%d done", spec.doneCountLocked(), len(spec.items))
	case done != 0:
		// A NUMBER OFF THE END IS ANSWERED WITH THE RANGE, so the next call is the
		// corrected one rather than a guess. A list that is empty is its own
		// sentence: "there is no item 2: this list has 0" would be true and useless.
		if len(spec.items) == 0 {
			reply = "there is no list on this quick task. Add the steps with `add`, or just do the work and say what you found."
		} else {
			reply = fmt.Sprintf("there is no item %d: this list has %d. Tick one of those, or add a step with `add`.", done, len(spec.items))
		}
	}
	for _, item := range add {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		spec.items = append(spec.items, item)
		spec.done = append(spec.done, false)
		reply = fmt.Sprintf("items: %d now", len(spec.items))
	}
	if reply == "" {
		reply = "nothing to do: say `done` with the item you finished, or `add` with the steps to append."
	}
	word := quickDoing(spec)
	n.graph.mu.Unlock()
	n.doingNow(word)
	return reply
}

// ── the body ────────────────────────────────────────────────────────────────

// quickBrief is the worker's opening message: what to do, the list in order, the
// claim it made about files, and what was said around the work.
//
// IT IS NOT [composeBrief], AND THE DIFFERENCE IS THE POINT. A task's brief is a
// contract — the ask, the copy, the work, what to produce, DONE WHEN — because a
// task is checked against it by somebody who was not there. Nothing checks a
// quick task, so a document with an acceptance clause in it would be a promise
// with no reader, and the shaping that writes one is exactly the delay the verb
// exists to avoid. What survives is the two halves that are true of any worker:
// the job, and the record the job came out of (admission.go).
//
// THE LAWS ARE NOT HERE. What it means to BE a quick worker — you work where the
// caller works, you tick as you go, your last message is the answer — is
// prompts/quick.md, rendered into this worker's system prompt on the same
// predicate that puts `items` on its belt. Saying it in both places would be two
// copies of one page, and the day one of them was edited it would be two pages.
func quickBrief(spec taskSpec) string {
	quick := spec.quick
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
	section(briefWorkHeading, "", quick.line)
	if len(quick.items) > 0 {
		var list strings.Builder
		for index, item := range quick.items {
			fmt.Fprintf(&list, "%d. %s\n", index+1, item)
		}
		section(quickItemsHeading, quickItemsRule, strings.TrimRight(list.String(), "\n"))
	}
	if len(quick.files) > 0 {
		section(quickFilesHeading, quickFilesRule, strings.Join(quick.files, "\n"))
	}
	// AND WHAT WAS SAID AROUND THE WORK, after the job and never before it, on
	// [composeBrief]'s own terms: what this worker is to DO is above, and what was
	// SAID is below, quoted and attributed and true only of the saying.
	section(admissionQuotesHeading, admissionQuotesRule, admissionQuotesSection(spec.admission))
	section(admissionEvidenceHeading, admissionEvidenceRule, admissionEvidenceSection(spec.admission))
	return out.String()
}

const (
	quickItemsHeading = "THE ITEMS, IN ORDER"
	quickItemsRule    = "Do them in order and tick each one with `items` the moment it is finished. The person is watching the row this moves."
	quickFilesHeading = "THE FILES YOU SAID YOU WOULD WRITE"
	quickFilesRule    = "These are the only paths you can write. Another quick task claiming one of them is waiting for you to finish."
)

// runQuickNode is the body [Agent.runTaskNode] dispatches to when the spec
// carries a quick task, and it is [Agent.designHarnessNode]'s shape with
// [runTaskChild] in the middle: a real worker built by [Agent.newTaskAgent] in
// the caller's own workspace, a room opened before the first call, and one
// landing at the end.
//
// EVERY ENDING IS A STATE AND A REPORT RATHER THAN AN ERROR, which is the law
// [Agent.workTaskNode] is written to and the reason this signature looks the way
// it does: everything that can go wrong here is something the person who asked
// for the work has to be told in words.
//
// THERE IS NO GATE UNDER THIS AND THERE IS NOT MEANT TO BE. No worktree, so
// nothing to merge; no auditor, so nothing to verify; no settle policy, so a
// quick node is never [TaskUnverified] and never asks anybody anything. Its last
// message is its result, and the road from here — complete, announce, the
// landing note — is the road every task already takes.
func (a *Agent) runQuickNode(ctx context.Context, node *TaskNode, listed *job) TaskState {
	quick := node.spec.quick
	log := taskLog(listed)
	fmt.Fprintf(log, "task %d · %s\nquick, in %s\n", node.id, node.title(), a.config.Workspace)

	// THE ROW SAYS WHAT THIS IS BEFORE THE FIRST CALL. A person can walk into
	// this room the instant the row appears, and a quick node whose word had not
	// been written yet would draw as an ordinary task working — which is the one
	// thing a quick row must never look like, because a task has a branch coming
	// home and this has nothing of the kind.
	node.graph.mu.Lock()
	word := quickDoing(quick)
	node.graph.mu.Unlock()
	node.doingNow(word)

	// IT WORKS WHERE THE CALLER WORKS. No worktree is prepared and none is
	// wanted: the whole difference between this and a task is that what it writes
	// is already in the person's own copy when it says so.
	//
	// AND A QUICK NODE HOLDS ITS FILES, NEVER THE TREE — which is why
	// [TaskNode.setTree] is not called here and must never be. An in-place node
	// whose `worktree` is set claims the WHOLE directory the moment it starts
	// running (treehold.go's [TaskGraph.claimOver]), and every write from the
	// conversation and from every sibling into that directory is refused with its
	// name on the refusal. That is the right law for a task the person deliberately
	// pointed at their own folder, and it is the exact opposite of what a quick
	// task is for: two of them run at once, and the person goes on typing and
	// editing while they do. What bounds this worker instead is the claim it made
	// — [Config.writeScope] on the belt's two writers — and the serialisation at
	// admission that puts two nodes claiming one path one after the other. So the
	// node carries Ground and Mode for the CARD, and no worktree at all.
	child, err := a.newTaskAgent(ctx, a.config.Workspace, node, "")
	if err != nil {
		node.finish("the quick task could not be started: "+err.Error(), nil, "", "")
		return TaskFailed
	}
	defer func() {
		_ = child.Close()
		a.foldTaskUsage(node, child)
	}()

	// THE ROOM IS OPEN BEFORE THE FIRST CALL, on [Agent.workTaskNode]'s terms:
	// from here until the node lands its events reach whoever is watching and the
	// person's words reach this child's steering lane. The bill is separate from
	// the speaker on purpose — the speaker leaves when the worker's reading is
	// over, the bill stands until the money is folded.
	room := node.openRoom()
	room.speaking(child)
	room.bill(child)

	wrote, stopped, runErr := runTaskChild(ctx, child, node, quickBrief(node.spec), a.config.Workspace, a.taskLimits(node), room, log)
	// The answer itself, whole, kept before the transcript is closed — this is
	// the last moment anybody can read it, and everything downstream that wants
	// the work rather than the card reads the node (task_result.go).
	said := lastSaid(child)
	node.keepWorkerConclusion(said, log)
	report := composeTaskReport(said)

	// A PERSON'S ✕ IS NOT A FAILURE OF THE WORK, and it is asked first because
	// [TaskGraph.stop] sets `stopped` before it cuts the context: everything below
	// would otherwise read a cancelled run as one that ran out of road.
	if node.stoppedByPerson() {
		return a.landQuickNode(node, report, wrote, TaskFailed)
	}
	switch {
	case stopped != "":
		// OUT OF ROUNDS, AND THE LAST SENTENCE QUOTED. It stopped MID-WORK: it was
		// told to do a thing, it was part way through doing it, and its budget ended
		// between one call and the next. A report that opened with what it managed
		// is a report the caller reads as done — the hand's own measured failure
		// (fork.go's [handReport]) — so this opens on the words and quotes the last
		// thing it said it was doing, because that sentence is the only description
		// in existence of the change that may be half made.
		report = withReport(quickOutOfRoundsWord(said, stopped), report)
		return a.landQuickNode(node, report, wrote, TaskFailed)
	case runErr != nil:
		report = withReport("the quick task ended on an error: "+runErr.Error(), report)
		return a.landQuickNode(node, report, wrote, TaskFailed)
	case ctx.Err() != nil:
		// The session is closing under this node. Returning the empty state is how
		// [Agent.runTaskNode] is told to leave the row RUNNING for recovery to pick
		// up, and it is the one road out of here that does not land.
		return ""
	}
	// AND ITS LAST MESSAGE IS ITS RESULT. A quick node that said nothing at all
	// still finished, and saying so is more honest than a blank card: a person
	// reading a row with an empty body cannot tell it from one that is broken.
	if strings.TrimSpace(report) == "" {
		report = "it finished and said nothing"
	}
	return a.landQuickNode(node, report, wrote, TaskDone)
}

// quickOutOfRoundsWord is the lead on a quick node that ran out of road: what
// ended it, and the last thing it said it was doing, in its own words.
func quickOutOfRoundsWord(said, stopped string) string {
	lead := "out of rounds — " + strings.TrimSpace(stopped)
	intent := strings.TrimSpace(lastLine(said))
	if intent == "" {
		return lead + ". It said nothing before it ended; its log is the only account."
	}
	return lead + ". What it last said it was doing, in its own words:\n“" + intent + "”"
}

// landQuickNode settles the node with the report a person reads, and it mirrors
// [Agent.landSubharnessNode]: the state is the caller's to name, and the one
// thing decided here is the STOP.
//
// NO BRANCH AND NO MERGE ARE WRITTEN, ever, and that is the emptiness law rather
// than a gap: a quick task has neither and could never have either, so a card
// naming one would be pointing at work that does not exist. What it wrote is
// carried when it wrote something and is nothing when it did not.
func (a *Agent) landQuickNode(node *TaskNode, report string, changed []string, state TaskState) TaskState {
	node.doingNow("")
	node.finish(report, changed, "", "")
	if node.stoppedByPerson() {
		return TaskFailed
	}
	return state
}
