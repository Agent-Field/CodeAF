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
//
// AND THE ADDRESSES ARE THE WORKER'S OWN. The half of this document a model
// wrote is bound to the copy the worker was actually given ([taskCopy]) before
// a word of it is laid out, and where that leaves two spellings of one folder in
// the same document — the person's quoted path and the copy's — the mapping is
// said outright in a section of its own rather than smuggled into the quotation.
func composeBrief(request, work, deliverable, acceptance, expects string, origin taskOrigin, own taskCopy) string {
	request = briefAskText(request)
	work = briefWorkText(request, work)
	// THE COPY IS STATED ONLY WHERE THE GROUND WAS NAMED, and it is decided
	// here, BEFORE the binding below erases the evidence. A brief that never
	// spelled the folder out has nothing to disambiguate and gets the document
	// it has always got — an unconditional section would rewrite every worktree
	// brief in the system to answer a question nobody in it had asked.
	stated := ""
	if own.real() && namesGround(own.ground, request, work, deliverable, acceptance, expects) {
		stated = own.note()
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
	section(briefAskHeading, briefAskRule, request)
	// AND WHICH FOLDER EVERY ADDRESS UNDER IT MEANS, second, because it is what
	// the reader needs BEFORE the first path rather than after the last one.
	section(briefCopyHeading, briefCopyRule, stated)
	section(briefWorkHeading, "", work)
	section(briefMakeHeading, "", deliverable)
	section(briefDoneHeading, "", acceptance)
	// AND WHAT THE HANDOFF PROMISED ABOUT THE WORLD, last, because it is the
	// only section that is about the folder rather than about the job
	// (handoffcontract.go). A handoff that promised nothing has no section, like
	// every other empty one here.
	section(briefExpectsHeading, briefExpectsRule, expects)
	// AND WHERE THE UNCUT WORDS LIVE, last, because it is an address rather
	// than an instruction. THE POINTER IS NOT THE SESSION: an empty origin
	// draws nothing, and a worker that never follows the path is still
	// governed by the brief above.
	section(briefOriginHeading, briefOriginRule, originPointer(origin))
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
type taskCopy struct {
	// ground is the folder the work is ABOUT and dir is the folder the work
	// HAPPENS IN — [taskTree]'s own two fields under their own two names,
	// carried here so that composing a brief needs to know nothing else about a
	// tree.
	ground, dir string
}

// newTaskCopy is the ONE PLACE A FOLDER IS SPELLED FOR THIS TYPE, and it exists
// so that no reader of the two fields has to know the rule.
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
func newTaskCopy(ground, dir string) taskCopy {
	return taskCopy{ground: cleanFolder(ground), dir: cleanFolder(dir)}
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
func (c taskCopy) bind(text string) string {
	if !c.real() {
		return text
	}
	if strings.Contains(text, c.ground) {
		text = replaceWholePath(text, c.ground, c.dir)
	}
	for _, alias := range groundAliases(c.ground, text) {
		text = replaceWholePath(text, alias.spelling, filepath.Join(c.dir, alias.under))
	}
	return text
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
func (c taskCopy) note() string {
	return "The work is about " + c.ground + ".\n" +
		"Your own copy of it is " + c.dir + ", and that is where you are standing.\n\n" +
		"Every address below is written as its address in your copy. The person's own message is quoted as they typed it, so a path in it that begins " + c.ground +
		" means the same path under " + c.dir + ". A path that is not under " + c.ground + " is somewhere else on the machine and stands as written."
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
