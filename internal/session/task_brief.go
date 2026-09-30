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

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// The section headings, in the order [composeBrief] lays them out. They are
// SHOUTED because the worker reads this as a document rather than as a sentence
// — the same voice the run's own node briefs already use for their bounds
// (orchestrate.go's [orchestrateBrief]).
const (
	briefAskHeading    = "WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS"
	briefWorkHeading   = "THE WORK"
	briefMakeHeading   = "WHAT TO PRODUCE"
	briefDoneHeading   = "DONE WHEN"
	briefCopyHeading   = "THE FOLDER THIS WORK IS ABOUT, AND YOUR OWN COPY OF IT"
	briefOriginHeading = "THE PERSON'S ORIGINAL MESSAGE"
)

// briefAskRule is the one line that says what the person's words are FOR. A
// worker handed two accounts of the same job needs to be told which one wins,
// and it is not the one the model wrote.
const briefAskRule = "This is the message this work came out of. Where anything below reads differently from it, their words are what was asked for."

// briefRole is whether the person's message above is this worker's WHOLE job or
// the job one piece of it was cut from. It is one enum rather than a bool
// because the two readings are different documents, and the caller that knows
// which is the graph ([TaskNode.spec] carries the parent).
type briefRole uint8

const (
	// briefWhole: nobody stands between this worker and the person's ask.
	briefWhole briefRole = iota
	// briefPiece: this node was handed out by another task, through
	// `propose_task` or `divide_work`.
	briefPiece
)

// briefPieceRule is [briefAskRule] for a node that owns ONE PIECE of the
// message above it.
//
// THE MEASURED READING IT CLOSES. Every descendant inherits the person's whole
// message verbatim (task.go's [Agent.taskRequest]) and used to be handed the
// whole-job rule with it: "where anything below reads differently, their words
// are what was asked for". A part briefed to run one script, under a message
// asking for that script AND a count of something else, can read that sentence
// as instructions to do both — and where the message says the work should go to
// a task, as licence to hand its own piece out again. Neither is what the person
// asked THIS worker for, and nothing else in the document said so.
//
// THE HANDING OUT IT DECLINES IS THE MESSAGE'S, NOT THE PIECE'S OWN. A piece
// above the floor of the tree carries `propose_task` and the fan-out page, and
// may split its own share when that share has parts; what it may not do is read
// the person's "send this to a task" as a second order to hand the whole thing
// out again. The clause says "any handing out it asks for" so that the rule and
// the fan-out page cannot be read as one forbidding what the other teaches.
//
// WHAT IT MUST NOT DO IS SILENCE THEM. Their words still govern this piece: the
// paraphrase above is the model's and theirs is not, so a real disagreement
// about THIS work is still theirs to win, and a piece that cannot be done
// without going against them is a report and not a quiet widening.
const briefPieceRule = "This is the message the whole job came out of, and this task is ONE PIECE of it: do what THE WORK and DONE WHEN below name, and leave the rest of that message to whoever kept it — including any handing out it asks for, which is not yours to do again. Their words still govern your piece: where anything below reads differently from them about it, theirs are what was asked for, and if your piece cannot be done without going against them, say so in your report rather than widening the work."

// askRule is the rule that opens the document, chosen by the role.
func (role briefRole) askRule() string {
	if role == briefPiece {
		return briefPieceRule
	}
	return briefAskRule
}

// briefPlanRule is the sentence that opens a plan-born worker's document. It
// says outright that the tasks above this one in the plan are context rather
// than more work. A child whose document carried the whole run's objective as
// its opening read the run as its own assignment, and this sentence closes
// that reading.
const briefPlanRule = "ancestor tasks are background, not extra assignments"

// briefBackgroundRule is [briefAskRule] and [briefPieceRule] both, for a
// plan-born worker on the bash belt. The person's words below are the run's
// background rather than this node's assignment: what this worker owes was
// fixed from the plan when the node was born, and reading the whole objective
// as its own job would widen it past the one task it owns.
const briefBackgroundRule = "This is the message the run came out of. It is background for this task, not your assignment: what you owe was fixed from the plan when this task was born and is the work above. Read the person's words for context, and do not take any part of the run you were not given."

// briefScope is the one fact that can REORDER a worker's document: the plan
// task a bash-belt node was born from. It is empty for every other worker, and
// an empty scope composes byte for byte what [composeBrief] has always
// composed — which is why the flag-off and non-plan-born roads cannot move.
type briefScope struct {
	// planID is the plan task's bare id (internal/plandb stores ids without
	// the `t-` prefix [planStoreID] prints), non-empty only on a node that is
	// both born from the plan and running the bash belt.
	planID string
}

// planBorn says whether this scope asks for the plan-born ordering.
func (s briefScope) planBorn() bool { return s.planID != "" }

// briefScopeFor is [TaskNode.briefScopeLocked]'s rule as a plain function, so
// that the both-facts test needs no live node. The plan-born ordering applies
// only where the node is born from the plan AND the session is on the bash
// belt: a plan id with the belt off is the document every such node composed
// before this ordering existed.
func briefScopeFor(planID string, bashBelt bool) briefScope {
	if planID == "" || !bashBelt {
		return briefScope{}
	}
	return briefScope{planID: planID}
}

// briefPart is one section of a worker's document before
// [composeBriefScoped] lays it out: the heading, the rule under it, and the
// body. The order of the parts is the order of the document, which is the
// whole of what a plan-born worker's document changes.
type briefPart struct {
	heading, rule, body string
}

// briefOriginRule is the one line that says what the pointer is FOR. The
// restatement above is bounded; this is where the uncut words live, and the
// brief still governs what ships.
const briefOriginRule = "The restatement above is bounded. Their original words are at this path and line — read them if that is not enough. The brief still governs what ships."

// briefCopyRule is the one line that says what the mapping is FOR. A worker
// reading two spellings of one directory needs to be told which of them it is
// standing in, and it is never the one the contract was written from.
const briefCopyRule = "Read anywhere on the machine; write only inside your copy."

// briefAskLimit bounds the verbatim ask, and it is generous on purpose: a
// person's request is usually a paragraph and occasionally a page, and the
// whole value of carrying it is that nothing was edited out of it. What the
// bound is really for is the other case — a pasted log, a whole file dropped
// into the chat — where an unbounded copy would put megabytes into every node
// prompt of a run. The cut is marked (see [clip]), so a worker that has been
// given a truncated ask can see that it was.
const briefAskLimit = 6000

// composeBrief lays out one worker's opening message: the person's request in
// their own words, then the contract the conversation groomed out of it. It is
// [composeBriefScoped] with no scope, which is every worker whose document does
// not reorder; the bash-belt, plan-born road reaches the scoped form directly
// from the node ([TaskNode.briefScopeLocked]).
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
//
// AND THE ADDRESSES ARE THE WORKER'S OWN. The half of this document a model
// wrote is bound to the copy the worker was actually given ([taskCopy]) before
// a word of it is laid out, and where that leaves two spellings of one folder in
// the same document — the person's quoted path and the copy's — the mapping is
// said outright in a section of its own rather than smuggled into the quotation.
func composeBrief(role briefRole, request, work, deliverable, acceptance, expects string, heard AdmissionContext, origin taskOrigin, own taskCopy) string {
	return composeBriefScoped(briefScope{}, role, request, work, deliverable, acceptance, expects, heard, origin, own)
}

// composeBriefScoped is [composeBrief] with the one fact that can REORDER the
// document: the plan task a bash-belt node was born from.
//
// THE ORDER IS THE WHOLE OF WHAT A WORKER READS FIRST, and it is decided once,
// here, by [briefScope.planBorn]. Every ordinary worker opens on the person's
// words, because they are the thing that wins and the contract below was groomed
// out of them. A PLAN-BORN WORKER ON THE BASH BELT OPENS ON ITS OWN ASSIGNMENT
// INSTEAD: the node owns one task in the run's plan, its assignment was fixed
// from the plan when it was born, and the whole objective above it is not more
// of its job. So its document names the plan task and says the tasks above it
// are background, lays out the assignment, and only then quotes the person's
// message under a rule that labels it as background rather than as the thing
// that wins.
func composeBriefScoped(scope briefScope, role briefRole, request, work, deliverable, acceptance, expects string, heard AdmissionContext, origin taskOrigin, own taskCopy) string {
	request = briefAskText(request)
	work = briefWorkText(request, work)
	// THE COPY IS STATED ONLY WHERE THE GROUND WAS NAMED, and it is decided
	// here, BEFORE the binding below erases the evidence. A brief that never
	// spelled the folder out has nothing to disambiguate and gets the document
	// it has always got — an unconditional section would rewrite every worktree
	// brief in the system to answer a question nobody in it had asked.
	quoted, evidence := admissionQuotesSection(heard), admissionEvidenceSection(heard)
	stated := ""
	sections := []string{request, work, deliverable, acceptance, expects, quoted, evidence}
	if own.real() && own.names(sections...) {
		stated = own.note(sections...)
	}
	// AND THE MODEL-AUTHORED HALF IS BOUND TO THE COPY, and only that half. The
	// person's request is a quotation and is never edited ([briefAskRule] makes
	// it the thing that wins, which a rewritten quotation could not be), and the
	// origin pointer is a journal address that lives outside every worktree, so
	// binding it would aim a worker at a file that is not there.
	work = own.bind(work)
	deliverable = own.bind(deliverable)
	acceptance = own.bind(acceptance)
	expects = own.bind(expects)
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
	// AND A PLAN-BORN WORKER OPENS ON THE TASK IT OWNS, not on the run it came
	// out of. The first line names the plan task — the same `t-<id>` the CLI
	// prints and the finish command takes — and the line under it is the
	// sentence saying the tasks above it are context rather than more work. It
	// is written here, ahead of every section, because the whole point is that
	// this document OPENS on the id: a worker that read the run's objective
	// first was invited to take all of it.
	if scope.planBorn() {
		out.WriteString(planStoreID(scope.planID) + " is your task in the plan.\n" + briefPlanRule)
	}
	// AND WHOSE JOB THE MESSAGE IS, in the rule over it rather than in a section
	// of its own: their words are printed once and unedited either way, and what
	// changes is what this worker is being told they are FOR.
	ask := briefPart{briefAskHeading, role.askRule(), request}
	// THE ASSIGNMENT, in the order the contract reads: the folder every address
	// means first, because it is what the reader needs BEFORE the first path
	// rather than after the last one, then the work, then what must exist, then
	// what done means.
	assignment := []briefPart{
		{briefCopyHeading, briefCopyRule, stated},
		{briefWorkHeading, "", work},
		{briefMakeHeading, "", deliverable},
		{briefDoneHeading, "", acceptance},
	}
	// AND WHAT WAS SAID AROUND THE WORK, after the contract and never before it
	// (admission.go). The order is the whole of the distinction the two rules
	// draw: what this worker OWES is above, settled and binding; what was SAID is
	// below, quoted, attributed, and true only of the saying. A document that put
	// the conversation first would read as instruction with a contract appended.
	//
	// NEITHER ADMISSION SECTION IS BOUND TO THE COPY, and that is a decision
	// rather than an oversight. Both are RECORDS OF WHAT HAPPENED SOMEWHERE ELSE:
	// a quote is somebody's sentence, and a handle names the journal it can be
	// fetched from, which lives under the ground and has no counterpart inside
	// the worker's tree — a rewrite would aim it at a file that is not there.
	// The copy is stated instead, in the section above, which is what
	// [namesGround] is asked about these two.
	record := []briefPart{
		{admissionQuotesHeading, admissionQuotesRule, quoted},
		{admissionEvidenceHeading, admissionEvidenceRule, evidence},
		// AND WHAT THE HANDOFF PROMISED ABOUT THE WORLD, last but one, because
		// it is the only section about the folder rather than about the job
		// (handoffcontract.go). A handoff that promised nothing has no section,
		// like every other empty one here.
		{briefExpectsHeading, briefExpectsRule, expects},
		// AND WHERE THE UNCUT WORDS LIVE, last, because it is an address rather
		// than an instruction. THE POINTER IS NOT THE SESSION: an empty origin
		// draws nothing, and a worker that never follows the path is still
		// governed by the brief above.
		{briefOriginHeading, briefOriginRule, originPointer(origin)},
	}
	// THE ORDER ITSELF. An ordinary worker opens on the person's words and
	// reads the assignment under them. A plan-born worker reads the assignment
	// first and the person's words after it, relabelled: what it owes was fixed
	// from the plan, and the run's objective is context rather than a second
	// assignment.
	order := append([]briefPart{ask}, assignment...)
	if scope.planBorn() {
		ask.rule = briefBackgroundRule
		order = append(append([]briefPart{}, assignment...), ask)
	}
	order = append(order, record...)
	for _, part := range order {
		section(part.heading, part.rule, part.body)
	}
	return out.String()
}

// ── the folder the work is about, and the folder the work happens in ─────────

// A CONTRACT IS WRITTEN IN THE WORKER'S OWN DIRECTORY. Every address in the
// model-authored half of a handoff — the work, what to produce, what done means
// — is resolved against the copy the worker was given, never against the folder
// that copy was made of. The person's own words are not rewritten; the copy is
// stated instead.
//
// THE DEFECT THIS IS FOR (#566). A parent standing in the person's checkout
// writes that checkout's absolute paths into `brief`, `deliverable` and
// `acceptance` — which is exactly what prompts/system.md asks it for — while the
// node it hands them to is stood up in `<session>/trees/<id>`. A worker that
// follows the address it was given reads the directory that is still moving
// under it, and its first write at that same address is refused by
// [taskGroundGuard] as "is outside your copy". The isolation is right and the
// refusal is right; what was wrong is the handoff, and this is upstream of both.
//
// A NESTED FAMILY IS THE SAME DEFECT ONE LEVEL DOWN AND TAKES NO SECOND
// MECHANISM. A part admitted by `divide_work` carries no ground of its own, so
// its tree is cut off the PARENT WORKER's directory and its own copy is
// `<session>/trees/<partID>` — the identical mismatch. Every node reaches its
// worker through [Agent.workTaskNode] and [TaskNode.instructionOn], so both
// depths are bound by this one map.
// AND THE CONVERSATION'S OWN FOLDER IS THE SAME DEFECT FROM THE OTHER SIDE.
// A conversation opened nowhere in particular keeps its deliverables in a folder
// of its own — `<session>/work`, which [Place.Work] names and which
// landing.go's [deliverablesDir] hands to anything looking for somewhere to put
// a finished document — so a handoff writing that address into `deliverable` is
// following the system's own answer to "where does this go". That folder is NOT
// under the ground, so the rule above rightly left it alone, and the worker
// standing in `<session>/trees/<id>` was refused at its first write with "is
// outside your copy". Measured 2026-09-10: the worker read the refusal, invented
// a folder of the same name inside its own copy, wrote there, and reported the
// deviation — one wasted round and a deliverable at an address nobody had
// agreed. Binding it is that self-correction made the contract instead.
//
// AND ANOTHER TREE OF THIS CONVERSATION IS THE SAME DEFECT FROM THE THIRD
// SIDE (#839). The conversation itself learns the address of a task's private
// copy — the first worker's report names the directory it stood in, and the
// landing note names it again — so the model that proposes the NEXT piece of
// work writes that address into the contract instead of the person's checkout.
// The path is a real directory under this conversation's own [Place.Trees], it
// is simply somebody else's, and it is under neither the ground nor work/, so
// both rules above left it as written. The worker followed it and
// [taskGroundGuard] answered "is outside your copy" about a directory the
// harness itself had invented. Another tree of the same conversation is a copy
// of the same folder, so the equivalent address inside this copy is the same
// path under it.
type taskCopy struct {
	// ground is the folder the work is ABOUT and dir is the folder the work
	// HAPPENS IN — [taskTree]'s own two fields under their own two names,
	// carried here so that composing a brief needs to know nothing else about a
	// tree.
	ground, dir string
	// work is THE CONVERSATION'S OWN FOLDER, and it is empty for every session
	// that has none — a borrowed session keeps no work/ at all — and for every
	// tree whose ground or copy already holds it, where the rule above is
	// already the whole answer and a second one would move an address twice.
	work string
	// trees is WHERE THIS CONVERSATION KEEPS ITS COPIES, and it is empty for
	// the legacy layout, whose tree carries the zero Place, and whenever the
	// ground rule already covers it — a trees folder at or under the ground is
	// the ground's to bind, and only what that rule leaves behind may be
	// considered as another task's tree.
	trees string
}

// newTaskCopy is the ONE PLACE A FOLDER IS SPELLED FOR THIS TYPE, and it exists
// so that no reader of the fields has to know the rule. A session that has
// trees of its own reaches the four-folder form through [newTaskCopyOf]; this
// wrapper is the three-folder shape the existing callers already hold.
//
// A FOLDER IS STORED CLEANED, WITHOUT ITS TRAILING SEPARATOR. Everything below
// reads the ground as a whole path — the byte behind an occurrence has to be
// either the end of the address or the `/` that carries the rest of it — so a
// ground stored as `/x/repo/` would match its own trailing slash and then find a
// filename byte behind it, and bind nothing at all. Normalising once here is one
// rule in one place; normalising at every use would be the same rule written
// four times, which is the drift CLAUDE.md's one-source-of-truth law is about.
// Production grounds already arrive through [canonicalPath], so this is a
// property of the type rather than a repair of any caller.
//
// AND A SESSION FOLDER THE GROUND OR THE COPY ALREADY HOLDS IS DROPPED HERE,
// once, rather than guarded at each of the three places that read it. A task
// whose ground IS the conversation's own workspace — the ordinary shape for a
// session with no project — would otherwise have every address under it moved by
// the ground rule and then moved again by this one, which is an address in a
// folder that does not exist. The question is asked with [withinDir] because
// "at or under" is exactly what it answers, and it answers it about `/x/repo`
// and `/x/repo-old` the way the rest of this file does.
//
// THE COPY SITTING UNDER THE SESSION FOLDER IS DROPPED TOO. The two checks
// above ask whether work/ is inside the ground or the copy. They do not ask
// the other way: a folder of `/s` with a copy at `/s/trees/1` is a parent,
// not a child, so it used to be kept. Binding it after the ground rule had
// already written `/s/trees/1/…` into the contract then rewrote those
// addresses a second time — the worker followed a path inside a folder that
// does not exist, and [taskGroundGuard] answered "is outside your copy"
// about a write that was meant to be inside. "At or under" runs both ways
// because both directions are one folder swallowing the other.
func newTaskCopy(ground, dir, work string) taskCopy {
	return newTaskCopyOf(ground, dir, work, "")
}

// newTaskCopyOf is [newTaskCopy] with the conversation's trees folder spelled
// too. Production reaches it through [taskCopyFor], which reads [Place.Trees]
// from the same record the copy was cut from; tests that do not carry a
// session keep [newTaskCopy] and bind nothing extra.
func newTaskCopyOf(ground, dir, work, trees string) taskCopy {
	// THE TREES FOLDER IS SPELLED THE WAY THE COPIES INSIDE IT ARE, which is
	// canonically. [taskOwnFolder] resolves a copy's directory before git ever
	// registers it, and a worker's own report therefore names that resolved
	// spelling — so a trees folder kept as the session happened to spell it
	// would be a folder that never matched the address the next contract
	// carries, and would compare as a different directory from the copy sitting
	// inside it. The other spelling is not lost: [bindFolder]'s alias reading
	// resolves it against this one.
	own := taskCopy{ground: cleanFolder(ground), dir: cleanFolder(dir), work: cleanFolder(work), trees: canonicalPath(cleanFolder(trees))}
	if own.work == "" || own.work == "/" || withinDir(own.ground, own.work) || withinDir(own.dir, own.work) || withinDir(own.work, own.dir) {
		own.work = ""
	}
	// ANOTHER TREE OF THIS CONVERSATION IS KEPT, and the drop is the inverse
	// of the work folder's. work/ sitting as a parent of the copy used to be
	// kept and then rewrote addresses already inside the copy; trees/ sitting
	// as a parent of the copy is the ordinary layout — [Place.Trees] is
	// exactly that parent — and dropping it would leave every sibling address
	// as written. What IS dropped is a trees folder the ground already
	// covers, or one that already sits inside the copy: THE GROUND RULE WINS
	// where both could apply, and a folder the worker is already standing in
	// is not another tree.
	if own.trees == "" || own.trees == "/" || own.trees == own.ground || own.trees == own.dir || withinDir(own.dir, own.trees) || withinDir(own.ground, own.trees) {
		own.trees = ""
	}
	return own
}

// cleanFolder is [filepath.Clean] with the empty path left empty, because
// nothing resolved must stay nothing rather than become `.` — [taskCopy.real]
// reads the empty string as "no copy" and a relative dot would be a folder.
func cleanFolder(folder string) string {
	if folder == "" {
		return ""
	}
	return filepath.Clean(folder)
}

// real says whether there is a map to apply at all.
//
// A ground or a directory nobody resolved, and every mode whose ground IS its
// directory, are all the identity — and the identity is spelled as "no copy"
// here so that neither the binding below nor the section that explains it draws
// anything at all for them (the emptiness law, applied to a document).
//
// AND A GROUND OF `/` IS NOT A COPY OF ANYTHING. That is a decision rather than
// a consequence of the boundary rule below: the whole machine is not a folder
// this work is about, and binding it would rewrite EVERY absolute address in a
// contract into the worker's own directory — the exact opposite of the law this
// type exists to keep, which is that an address outside the copy stays outside.
func (c taskCopy) real() bool {
	return c.ground != "" && c.ground != "/" && c.dir != "" && c.ground != c.dir
}

// bind rewrites every occurrence of the ground that STANDS AS A WHOLE PATH — at
// a boundary, or with a `/` and the rest of the address behind it — to the same
// address inside the copy, suffix and all.
//
// A PATH THAT IS NOT AT OR BELOW THE GROUND IS LEFT EXACTLY AS WRITTEN. That is
// the law that keeps the guard honest rather than an omission: nothing outside
// the copy may be turned into something writable by a rewrite, so a contract
// that really does name another repository still earns [taskGroundGuard]'s
// refusal and the worker still says in its report what needs doing out there.
//
// THE BYTE IN FRONT IS GUARDED TOO, and it is not pedantry: a ground of
// `/x/repo` must not match inside `/x/repo-old`, which is a different
// repository, nor inside `/y/x/repo`, which is a different folder that happens
// to end with the same name.
//
// A FOLDER SPELLED ANOTHER WAY IS STILL THE FOLDER. The comparison above sees
// only the ground's own spelling, while an address is written the way its author
// was standing — /var/folders and /private/var/folders are one directory on a
// Mac — so [groundAliases] resolves the rest and they are rewritten by the same
// whole-path rule. Without it a contract naming the ground through an alias
// bound nothing, and the worker was left pointing at the person's checkout.
//
// AND THE CONVERSATION'S OWN FOLDER MOVES BY THE SAME RULE, first, into a
// folder of its own name at the copy's root ([taskCopy.workInCopy]). It is
// first rather than second because it is the narrower of the two: park those
// addresses inside the copy before the ground rule writes the copy's own
// spelling into the text. Applying it second — after `/x/repo/a` has become
// `/s/trees/1/a` — rewrites the copy the moment the work folder is an
// ancestor of it, and the worker is then handed a path [taskGroundGuard]
// will refuse. The constructor drops that overlap; the order is the second
// lock on the same door.
//
// ANOTHER TREE OF THIS CONVERSATION MOVES LAST, after the ground has had its
// say. THE GROUND RULE WINS WHERE BOTH COULD APPLY: an address at or under
// the node's own ground binds to the ground's copy, and only what that rule
// leaves behind may be considered as another task's tree. Applying the
// sibling rule first would take `/s/trees/1/a` — which is under a ground of
// `/s` — and write it as this copy's `a`, after which the ground rule would
// see an address still under `/s` and move it again. An address already
// inside this worker's own copy is left as written: it is not somebody
// else's tree.
//
// THE PERSON'S OWN WORDS DO NOT COME THROUGH HERE AT ALL, and that is what makes
// both rules safe: [composeBrief] binds the model-authored half and hands the
// request to the document unedited, so a path in somebody's own sentence is
// still the path they typed and the mapping is STATED to the worker instead
// ([taskCopy.note]).
func (c taskCopy) bind(text string) string {
	if !c.real() {
		return text
	}
	text = bindFolder(text, c.work, c.workInCopy())
	text = bindFolder(text, c.ground, c.dir)
	return bindSiblingTrees(text, c.trees, c.dir)
}

// bindFolder rewrites one folder — its own spelling and every alias of it — into
// the folder it maps onto, and nothing else. It is lifted out of
// [taskCopy.bind] so the two folders that map move by ONE rule rather than by
// two copies of it that can drift apart.
func bindFolder(text, folder, into string) string {
	if folder == "" || into == "" {
		return text
	}
	if strings.Contains(text, folder) {
		text = replaceWholePath(text, folder, into)
	}
	for _, alias := range groundAliases(folder, text) {
		text = replaceWholePath(text, alias.spelling, underCopy(into, alias.under))
	}
	return text
}

// copyRoot is the copy spelled AS THE DIRECTORY A COMMAND IS RUN IN, which is
// the one spelling of it that is true in every copy at once
// ([taskCopy.bindCommand] is its only reason and says why).
const copyRoot = "."

// underCopy joins one path onto the copy it now stands in. It is
// [filepath.Join] with one difference, and the difference is the whole reason
// it has a name: a copy spelled as its own root keeps the `./` in front of what
// is under it, because a bare word is a PROGRAM the shell looks for on PATH
// ([onThePath]) and this is a path. Join alone would clean that prefix away and
// leave a check whose first word names a file in the tree looking like the name
// of something installed on the machine.
func underCopy(into, under string) string {
	if into == copyRoot {
		return copyRoot + "/" + filepath.ToSlash(under)
	}
	return filepath.Join(into, under)
}

// workInCopy is where the conversation's own folder stands inside the copy: a
// directory of THE SAME NAME at the copy's root.
//
// THE NAME IS TAKEN FROM THE FOLDER RATHER THAN SPELLED AGAIN, because place.go
// already owns what that folder is called and a second spelling of it here is
// the drift the one-source-of-truth law is about. Keeping the name is what makes
// the mapping legible from either end: a person reading the report sees the same
// last two components they asked for, and the deliverable is not scattered into
// the root of a working copy that is a checkout of something else.
func (c taskCopy) workInCopy() string {
	if c.work == "" || c.dir == "" {
		return ""
	}
	return underCopy(c.dir, filepath.Base(c.work))
}

// bindCommand is [taskCopy.bind] for A COMMAND THAT WILL BE RUN IN A COPY OF THE
// GROUND, rather than for a document a worker reads.
//
// THE DIFFERENCE IS WHICH COPY. A brief is written for ONE directory — the one
// its worker is standing in — so its addresses are bound to that directory by
// name. A declared check is run in SEVERAL: the checker judges a clean restore
// of what would ship (task_audit.go's [auditGround]), the before-reading runs
// the same commands on the commit the work was cut from (task_baseline.go), and
// the progress reader runs them in the worker's own tree. All three stand at the
// ROOT of a copy of the ground, and every one of them runs the command with that
// directory as its working directory ([runOneCheck] sets it, and the checker's
// own shell is opened there) — so an address at or under the ground is bound to
// the copy the command is being run in, whichever one that is, which is what `.`
// means and the only spelling that means it in all of them.
//
// THE DEFECT THIS IS FOR (#886). A parent standing in the person's checkout
// writes that checkout's absolute paths into `checks`, which is exactly what
// prompts/system.md asks it for, and the checker then ran `grep -q rewritten
// /person/folder/report.txt` in the copy: the cwd is the copy, but an absolute
// argument is not a cwd question, so the check read the untouched original,
// answered red, and correct work landed `your call · nobody could check it`. The
// bind [composeBrief] applies to the contract's other four fields reaches the
// fifth here.
//
// AND THE BEFORE-READING BECOMES HONEST BY THE SAME LINE. That absolute path
// made the base reading read the person's checkout too, so a check the work
// really had broken came back "red before this work and remains red" — a finding
// softened by an address, in the one reading whose whole job is to say who made
// the tree red.
func (c taskCopy) bindCommand(command string) string {
	if !c.real() {
		return command
	}
	// The map itself is built from the REAL directories — which folder swallows
	// which is a question about folders on disk, and [newTaskCopyOf] answers it
	// once — and only the destination is re-spelled here.
	inside := c
	inside.dir = copyRoot
	return inside.bind(command)
}

// standingOn is the map for a checker standing ON THE FOLDER THE WORK IS ABOUT
// rather than on a copy of it: the identity, carrying the directory it stands in
// so that a file check still has somewhere to resolve against
// ([auditDoor.ground]).
//
// IT IS NOT AN EMPTY MAP WITH A FIELD FILLED IN. A ground that IS its own
// directory is exactly what [taskCopy.real] already reads as "no copy" — the
// shape every in-place task has always had — so nothing is bound, and the
// addresses a contract wrote stand as the contract wrote them.
func standingOn(dir string) taskCopy {
	return newTaskCopy(dir, dir, "")
}

// copyOnto is THE ONE READING of "is this directory a copy of that ground, and
// what does it map onto what". It has two callers and they are the two questions
// that must never disagree: [taskCopyFor] asks it about the directory the WORKER
// was given, and [Agent.checkCopy] about the directory a CHECK is about to be
// run in.
//
// ONLY A MODE WHOSE DIRECTORY IS GENUINELY A COPY OF ITS GROUND BINDS, and the
// reason each mode does or does not is stated on [taskCopyFor].
func copyOnto(ground string, mode TaskMode, dir string, place Place) taskCopy {
	switch mode {
	case TaskModeWorktree, TaskModeMirror:
		return newTaskCopyOf(ground, dir, place.Work(), place.Trees())
	}
	return standingOn(dir)
}

// checkCopy is the map ONE DECLARED CHECK IS BOUND THROUGH: the folder the
// node's work is ABOUT, onto the copy of it the check is about to be run in.
//
// IT IS READ OFF THE NODE'S OWN RECORD of where its work stands — which
// [TaskNode.setTree] writes from the tree that was really made — rather than off
// a tree in hand, because the readers that need it hold different directories
// and not all of them hold a tree: the node's audit stands in a clean restore
// and the progress reader stands in the worker's own copy, and both reach one
// door through this.
func (a *Agent) checkCopy(node *TaskNode, dir string) taskCopy {
	if node == nil {
		return standingOn(dir)
	}
	ground, mode := node.standsOn()
	return copyOnto(ground, mode, dir, a.config.Place)
}

// briefScopeLocked is the extra fact [composeBriefScoped] needs about THIS
// node: the plan task it was born from, and nothing unless the node is both
// plan-born and running the bash belt. Both halves are read here rather than
// inside the composer, which is engine-side and holds no config: the plan id
// is on the spec, and the belt is the experiment's switch, which
// [bashBeltAsked] — the one reader of it in this package — answers. A node that
// is either not plan-born (every node outside the experiment, and every quick,
// design or run node under it) or not on the belt composes the document it has
// always composed.
//
// It is read under the graph's lock, as [TaskNode.briefRoleLocked] is, because
// its one caller [TaskNode.instructionLocked] already holds it.
func (n *TaskNode) briefScopeLocked() briefScope {
	return briefScopeFor(n.spec.planID, bashBeltAsked())
}

// standsOn is the node's own record of the folder its work is about and of how
// its copy stands on it, read under the graph's lock.
func (n *TaskNode) standsOn() (string, TaskMode) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.Ground, n.Mode
}

// bindSiblingTrees rewrites every address that stands under another tree of
// this conversation — a child of [Place.Trees] that is not this worker's own
// copy — to the same path under the copy it was given. The trees folder
// itself is not a tree, and an address already inside this copy is left
// alone: it is not somebody else's.
//
// THE FIRST COMPONENT UNDER THE TREES FOLDER IS THE OTHER TREE. That is how
// [taskOwnFolder] names every copy, so `/s/trees/1/internal/widget.go` and
// `/s/trees/1` are this conversation's first tree, and `/s/trees` is the
// parent of every tree and is not rewritten. A lookalike that merely begins
// the same way (`/s/trees-old/…`) is a different directory and [indexWholePath]
// already leaves it alone.
func bindSiblingTrees(text, trees, dir string) string {
	if trees == "" || dir == "" {
		return text
	}
	text = replaceSiblingTree(text, trees, dir)
	for _, alias := range groundAliases(trees, text) {
		id, under, ok := firstRelComponent(alias.under)
		if !ok {
			continue
		}
		sibling := filepath.Join(trees, id)
		if withinDir(dir, sibling) {
			continue
		}
		into := dir
		if under != "" {
			into = underCopy(dir, under)
		}
		text = replaceWholePath(text, alias.spelling, into)
	}
	return text
}

// replaceSiblingTree rewrites the trees folder's own spelling. It walks
// whole-path occurrences of that folder and, where a child tree follows,
// substitutes this worker's copy for that child — suffix and all — the way
// [replaceWholePath] substitutes one address for another.
func replaceSiblingTree(text, trees, dir string) string {
	var out strings.Builder
	for rest := text; ; {
		at := indexWholePath(rest, trees)
		if at < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:at])
		after := rest[at+len(trees):]
		id, n, ok := treeChild(after)
		if !ok || withinDir(dir, filepath.Join(trees, id)) {
			out.WriteString(trees)
			rest = after
			continue
		}
		out.WriteString(dir)
		rest = after[n:]
	}
}

// treeChild reads the first path component after a trees-folder occurrence.
// The leading separator is required: without it the occurrence is the trees
// folder itself, which is not a tree and is not rewritten. `.` and `..` are
// not tree ids — treating `..` as one would walk out of the trees folder
// entirely, which is the opposite of a sibling copy.
func treeChild(after string) (id string, consumed int, ok bool) {
	if !strings.HasPrefix(after, "/") {
		return "", 0, false
	}
	i := 1
	for i < len(after) && pathByte(after[i]) && after[i] != '/' {
		i++
	}
	if i == 1 {
		return "", 0, false
	}
	id = after[1:i]
	if id == "." || id == ".." {
		return "", 0, false
	}
	return id, i, true
}

// firstRelComponent splits a path that [insideWorkspace] already decided sits
// under the trees folder into the other tree's id and whatever remains under
// it. An empty or `.` relative is the trees folder itself.
func firstRelComponent(rel string) (id, rest string, ok bool) {
	if rel == "" || rel == "." {
		return "", "", false
	}
	rel = filepath.ToSlash(rel)
	id, rest, found := strings.Cut(rel, "/")
	if id == "" || id == "." || id == ".." {
		return "", "", false
	}
	if !found {
		return id, "", true
	}
	return id, rest, true
}

// replaceWholePath rewrites every occurrence of one address that stands as a
// whole path, and nothing else. It is lifted out of [taskCopy.bind] so the
// ground's own spelling and an alias of it move by one rule.
func replaceWholePath(text, address, with string) string {
	var out strings.Builder
	for rest := text; ; {
		at := indexWholePath(rest, address)
		if at < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:at])
		out.WriteString(with)
		rest = rest[at+len(address):]
	}
}

// note is the mapping said outright, for the one case a rewrite cannot cover:
// the person's own sentence, which is quoted and never edited. It says the two
// folders in the two roles they actually hold, and then what that makes true of
// every address under them.
//
// AND IT SAYS "AN EXCEPTION" RATHER THAN "THE ONE EXCEPTION", because there is
// now a second one below it and a document that counts its own exceptions
// wrongly is a document a worker is right to stop trusting.
//
// AND THE CONVERSATION'S OWN FOLDER IS NAMED WHERE THERE IS ONE, because the
// sentence in front of it would otherwise be FALSE about exactly one directory
// on the machine: "a path that is not under the ground stands as written" was
// the whole rule until this folder began to bind too, and a worker that trusted
// it would go on writing at an address it is still refused. A limit stated
// wrongly is worse than one not stated at all.
//
// AND SO IS ANOTHER TREE OF THIS CONVERSATION. The same sentence is false
// about `<session>/trees/<someone else>` the moment that address begins to
// bind, and a worker told nothing about it would follow the path it can still
// see in the person's quotation — or trust "stands as written" and aim a
// write at a copy it may not touch.
//
// IT IS SAID ONLY WHERE ONE WAS ACTUALLY NAMED, and that is the difference
// between it and the two sentences above it. The ground and the conversation's
// own folder are facts about every worktree task alive — the note is drawn at
// all only because one of them was spelled in the contract — while another
// task's copy is an address most briefs never carry, and a paragraph
// announcing that other copies EXIST would be false in the ordinary case of
// the first task in a fresh conversation. A worker reads this document once
// and acts on it; a sentence about a folder nothing in its contract mentions
// is one more address for it to wander to, which is the failure this whole
// section is here to prevent.
func (c taskCopy) note(sections ...string) string {
	note := "The work is about " + c.ground + ".\n" +
		"Your own copy of it is " + c.dir + ", and that is where you are standing.\n\n" +
		"Every address below is written as its address in your copy. The person's own message is quoted as they typed it, so a path in it that begins " + c.ground +
		" means the same path under " + c.dir + ". A path that is not under " + c.ground + " is somewhere else on the machine and stands as written."
	if work := c.workInCopy(); work != "" {
		note += "\n\nThis conversation's own folder, " + c.work + ", is an exception. You cannot write there either, so an address under it is written below as the same path under " +
			work + ", and what you leave there comes home with the rest of your work."
	}
	if c.trees != "" && namesGround(c.trees, sections...) {
		note += "\n\nAnother task in this conversation has a copy of its own under " + c.trees + ", and that is an exception too. You cannot write in one, so an address under one of them is written below as the same path under " +
			c.dir + "."
	}
	return note
}

// names answers whether EITHER folder this map moves is spelled as a whole path
// in what is about to be composed, which is the question the section that
// explains the mapping is drawn on.
//
// IT IS EVERY FOLDER THE NOTE EXPLAINS. A contract whose only address is in
// the conversation's own folder, or under another tree of this conversation,
// has had an address moved, and a worker handed that document with nothing
// said about why would be reading a path it never saw written down anywhere.
func (c taskCopy) names(sections ...string) bool {
	if namesGround(c.ground, sections...) {
		return true
	}
	// The empty folder is asked about nowhere: [namesGround] reads an empty
	// spelling as standing at the end of every text, which would draw the
	// section over every brief in the system.
	if c.work != "" && namesGround(c.work, sections...) {
		return true
	}
	return c.trees != "" && namesGround(c.trees, sections...)
}

// namesGround answers whether the folder is spelled AS A WHOLE PATH anywhere in
// what is about to be composed. It reads the sections AS HANDED OVER, which is
// the only moment the question has an answer: binding is what removes the ground
// from four of them.
//
// IT ASKS EXACTLY THE QUESTION [taskCopy.bind] ANSWERS, through the same
// [indexWholePath], and that identity is the point rather than a convenience. A
// plainer substring test says yes to a contract naming `/x/repo-old` under a
// ground of `/x/repo` — where bind rightly rewrites nothing — and the worker is
// then handed a section telling it that addresses were mapped when none were.
func namesGround(ground string, sections ...string) bool {
	for _, section := range sections {
		if indexWholePath(section, ground) >= 0 || len(groundAliases(ground, section)) > 0 {
			return true
		}
	}
	return false
}

// groundAlias is one address in a contract that names the ground, or something
// under it, through a different spelling of the same folder.
type groundAlias struct {
	// spelling is the address as the text writes it, which is what a rewrite has
	// to find; under is the path it names beneath the ground, "." for the ground.
	spelling string
	under    string
}

// groundAliases answers path identity where a byte comparison cannot: the
// addresses in one text that resolve to the ground or below it while being
// spelled another way, most often through a symlinked ancestor.
//
// The reading is composed out of the helpers that already own each half —
// [pathTokens] for what could be a path, [canonicalPath] for one spelling of a
// path that need not exist yet, [insideWorkspace] for a comparison at component
// boundaries, so a ground of /x/repo never swallows /x/repo-old. A relative name
// is not an alias: it is already an address in the directory the worker stands
// in, and resolving it would move a path that was right.
func groundAliases(ground, text string) []groundAlias {
	ground = canonicalPath(cleanFolder(ground))
	if ground == "" || ground == "/" {
		return nil
	}
	var out []groundAlias
	for _, token := range pathTokens(text) {
		// The ground's own spelling is not an alias of itself, and both callers
		// have already asked [indexWholePath] about it without a syscall.
		if !filepath.IsAbs(token) || indexWholePath(token, ground) >= 0 {
			continue
		}
		under, inside := insideWorkspace(ground, canonicalPath(token))
		if !inside {
			continue
		}
		out = append(out, groundAlias{spelling: token, under: under})
	}
	return out
}

// indexWholePath is THE ONE READING OF "THIS OCCURRENCE IS A WHOLE PATH", and
// both the rewrite and the section that explains it go through it so the rule
// cannot be stated twice and drift. It answers where the folder first stands as
// an address of its own, or -1.
//
// In front of an occurrence there must be nothing a name could be made of;
// behind it there must be either the end of the text, a `/` carrying the rest of
// the address, or a byte no name continues through — a quote, a comma, a space,
// a newline. An occurrence that fails either half is a longer name that merely
// contains the folder's spelling, and the scan steps past it to the next one.
func indexWholePath(text, folder string) int {
	for from := 0; from <= len(text)-len(folder); {
		at := strings.Index(text[from:], folder)
		if at < 0 {
			return -1
		}
		at += from
		end := at + len(folder)
		if (at == 0 || !pathByte(text[at-1])) && (end == len(text) || text[end] == '/' || !pathByte(text[end])) {
			return at
		}
		from = at + 1
	}
	return -1
}

// pathByte says whether a byte can be part of a file or directory name.
//
// IT IS DELIBERATELY GENEROUS, because generosity here is the safe direction: a
// byte this calls part of a name only ever makes [taskCopy.bind] LEAVE
// something alone, and a path left alone is a path the guard still judges on
// its merits. A continuation byte of somebody's own alphabet counts too — a
// multi-byte rune cannot be read one byte at a time, and a name in Greek is
// still a name.
func pathByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b >= 0x80:
		return true
	}
	return strings.IndexByte("/.-_~+", b) >= 0
}

// briefAskText is the person's request exactly as [composeBrief] prints it.
// The bound belongs to that printed account, so every reader of the seam sees
// the same words rather than clipping a second way.
func briefAskText(request string) string {
	return clip(strings.TrimSpace(request), briefAskLimit)
}

// briefWorkText is the work's own account exactly as [composeBrief] prints it.
// THE SAME ACCOUNT IS PRINTED ONCE: a person-authored task has no paraphrase,
// so work equal to the bounded ask is absent rather than repeated under a
// second heading.
func briefWorkText(ask, work string) string {
	work = strings.TrimSpace(work)
	if work == ask {
		return ""
	}
	return work
}

// originPointer is the fold-marker idiom applied to one line: the tools that
// open the file, the path, and the line, in that order, because a
// `path:12` token is what neither `read` nor `grep` takes. A path with no
// line still stands; a line with no path does not.
func originPointer(origin taskOrigin) string {
	if origin.empty() {
		return ""
	}
	if origin.line > 0 {
		return fmt.Sprintf("grep or read %s, line %d", origin.journal, origin.line)
	}
	return "grep or read " + origin.journal
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
		// AND WHICH TURN THEY TYPED IT INTO, which is the difference between words
		// the person is saying now and words they said before the last thing that
		// woke this session. Only the first can be forwarded into a running task
		// under their own authority (task_forward.go).
		a.personHeard = a.turnSeq
		a.personAt = time.Now()
		// AND THE TURNS BEFORE THIS ONE ARE KEPT TOO, in the same place, on the
		// same test (admission.go). A constraint the person typed two turns ago
		// and never repeated is not in `personAsk` and is not recoverable from
		// the transcript, where their words and the session's own notes are
		// both user-role.
		a.rememberPersonTurnLocked(user.message, text)
		// AND WHICH PROGRAM THEY NAMED, read from every message of the turn
		// rather than the newest, so a steer does not unsay it
		// (delegate_asked.go). It is read after the line above has numbered the
		// message, because the bounce is counted against that number.
		a.hearProgramsLocked(text)
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

// taskOriginRef is WHERE THE PERSON'S TURN LIVES, as this agent knows it.
//
// In a CONVERSATION it is the journal path and the line of the message that
// opened this turn — [Agent.lastTurnStartLocked] finds the message,
// [sessionFile.messageLines] names the file and the line. In a NODE there is
// no person typing, so it INHERITS the origin of the task it was handed out
// by. THE POINTER IS AN ADDRESS, NOT INHERITED CONTEXT: a nested task still
// points at the human's journal, never at its own, and THE BRIEF REMAINS THE
// CONTRACT. A standing firing has a journal and no person turn, so its spec
// carries an empty origin and every part under it inherits that emptiness
// rather than a guessed pointer at the run folder.
func (a *Agent) taskOriginRef() taskOrigin {
	if a.config.InTask {
		if parent := a.graph().node(a.config.taskID); parent != nil {
			return parent.origin()
		}
		return taskOrigin{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	start, ok := a.lastTurnStartLocked()
	if !ok || a.file == nil {
		return taskOrigin{}
	}
	journal, line, _ := a.file.messageLines(a.messages[start], a.messages[start])
	if journal == "" {
		return taskOrigin{}
	}
	return taskOrigin{journal: journal, line: line}
}
