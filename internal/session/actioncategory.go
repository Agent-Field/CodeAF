package session

// ── ONE WORD FOR WHAT A STEP IS DOING ────────────────────────────────────────
//
// A caption says what is happening in a person's own words — "starting the local
// server", "reading the caption renderer". A surface that wants to draw ONE mark
// beside that sentence cannot get it from the sentence: the words are free-form
// on purpose, and a renderer that pattern-matched them would be a second, worse
// narrator built out of substring tests.
//
// So the narrator names its own family. It answers `run | starting the local
// server` — one word from a CLOSED list, a bar, and the same free sentence it
// always wrote — and this file owns that list, the parser that reads it, and the
// deterministic answer for when it is missing.
//
// THE LIST IS CLOSED AND THE SENTENCE IS NOT. Thirteen families cover the belt
// this build carries and leave one honest bucket for everything dynamic; the
// sentence stays whatever the model wanted to say. That split is the whole
// design: a fixed vocabulary is the only thing a glyph table can be keyed on,
// and free prose is the only thing worth reading.
//
// IT COSTS NO SECOND CALL. The category rides the caption narrator that already
// runs (caption.go) — same role, same bound of three per turn, same 500ms dwell,
// same 80-token ceiling. A classifier of its own would have been a second bill
// for a fact one already-paid-for sentence can carry as a prefix.
//
// AND A MISSING ONE IS NEVER AN ERROR. Every rung below the model is
// deterministic: [ActionCategoryForTool] answers from the tool's registered name
// alone, so a batch with no narration, a batch whose narration was malformed, and
// a conversation reopened from a file that predates this field all draw the same
// mark for the same work. Nothing retries and nothing is left blank.

import "strings"

// ActionCategory is the family of work one step belongs to. It is a string
// rather than an integer because it travels on the wire beside the caption, and
// a number would have made an old surface's "3" mean whatever the thirteenth
// constant became.
type ActionCategory string

// The thirteen families. THE ORDER IS THE ORDER THE MODEL IS SHOWN THEM in
// [ActionCategoryWords], so the list a person reads in the manual, the list the
// prompt spells and the list the parser accepts are one list.
//
// Each is a VERB A PERSON WOULD USE about work, never a machinery word: the
// families are what somebody watching would say is going on, which is the same
// bar every caption on this surface is held to.
const (
	// ActionSearch is looking for something whose location is not yet known.
	ActionSearch ActionCategory = "search"
	// ActionRead is opening something already located, and listing what is
	// there — the two are one act to a person watching.
	ActionRead ActionCategory = "read"
	// ActionEdit is changing something that already exists.
	ActionEdit ActionCategory = "edit"
	// ActionCreate is writing content or generating a new artifact.
	ActionCreate ActionCategory = "create"
	// ActionRun is executing a command and waiting for what it prints.
	ActionRun ActionCategory = "run"
	// ActionTest is checking work that has been done. IT HAS NO DEFAULT TOOL and
	// only the narrator ever picks it: a suite is `bash` and so is everything
	// else `bash` does, so the tool name cannot tell them apart. That is honest
	// — the fallback for a shell call is [ActionRun], which is true of a test
	// run as well.
	ActionTest ActionCategory = "test"
	// ActionBrowse is going out to a page or a service and reading what is
	// there.
	ActionBrowse ActionCategory = "browse"
	// ActionTransfer is moving bytes or state from one place to another.
	ActionTransfer ActionCategory = "transfer"
	// ActionCommunicate is saying something to a PERSON — a message, a mail, a
	// spoken line.
	ActionCommunicate ActionCategory = "communicate"
	// ActionCoordinate is work handed out or copied to run beside this one.
	ActionCoordinate ActionCategory = "coordinate"
	// ActionPlan is keeping the account of the work rather than doing it.
	ActionPlan ActionCategory = "plan"
	// ActionWait is standing by for something outside this turn.
	ActionWait ActionCategory = "wait"
	// ActionWork is the honest bucket: a hand this build does not know, a
	// service armed at runtime, a narrator that said nothing usable. It is a
	// real answer rather than an absence, so the gutter never goes blank and
	// never jumps.
	ActionWork ActionCategory = "work"
)

// actionCategories is THE list, in prompt order. Everything that needs to know
// what the families are walks this — the parser, the prompt, the manual gate and
// the icon table — so a fourteenth family is one line in one place.
var actionCategories = []ActionCategory{
	ActionSearch, ActionRead, ActionEdit, ActionCreate, ActionRun,
	ActionTest, ActionBrowse, ActionTransfer, ActionCommunicate,
	ActionCoordinate, ActionPlan, ActionWait, ActionWork,
}

// ActionCategories returns every family in prompt order.
//
// IT IS A COPY, and the copy is load-bearing rather than tidy. The prompt is
// built ONCE, at package initialization, out of this same table; a caller
// handed the backing array could write into it — a test tightening the list, a
// surface sorting it for a menu — and from then on the parser would accept a
// vocabulary the model was never shown, and refuse the one it was. A capped
// slice prevents an append from reaching the array and does not prevent that,
// so this allocates. It is called at construction time and never on a frame.
func ActionCategories() []ActionCategory {
	out := make([]ActionCategory, len(actionCategories))
	copy(out, actionCategories)
	return out
}

// ActionCategoryWords is the list as the prompt spells it: `search, read, edit,
// …`. It is built from the table rather than typed into the prompt so a family
// cannot exist that the model is never told about.
func ActionCategoryWords() string {
	words := make([]string, 0, len(actionCategories))
	for _, category := range actionCategories {
		words = append(words, string(category))
	}
	return strings.Join(words, ", ")
}

// ParseActionCategory reads one word as a family, reporting whether it is one.
//
// IT IS AN EXACT MATCH after case and surrounding punctuation, and there is no
// synonym table on purpose. A model that answers "running" instead of "run" is
// not guessed at — it falls to [ActionCategoryForTool], which is right about the
// work by construction. A synonym list would be a second vocabulary that drifts
// from the one the prompt names, and it would turn a wrong guess into a
// confident wrong icon rather than into the deterministic one.
func ParseActionCategory(word string) (ActionCategory, bool) {
	word = strings.ToLower(strings.Trim(strings.TrimSpace(word), "*_`\"'“”[](){}.:,;!?"))
	for _, category := range actionCategories {
		if string(category) == word {
			return category, true
		}
	}
	return "", false
}

// actionWordMax bounds what may sit before the bar and still be READ as a
// category attempt. The longest family is "communicate" at eleven characters;
// sixteen leaves room for a model that decorates its answer without letting a
// real sentence's opening clause be mistaken for a label.
const actionWordMax = 16

// SplitActionLine takes the narrator's raw answer apart into the family it named
// and the sentence it wrote.
//
// THE PROSE SURVIVES EVERY FAILURE AND THE LABEL NEVER DOES. A line with no bar,
// or a bar with a PHRASE in front of it, is prose that happens to contain the
// character and comes back whole — so a caption reads exactly as it would have
// before this format existed, and the mark comes from the tools.
//
// A LABEL ATTEMPT, THOUGH, IS CONSUMED WHATEVER BECOMES OF IT. One bare word
// before a bar is a model reaching for this format: `investigate | reading the
// renderer` gives back "reading the renderer" with no family, because leaving
// the word in would put machinery into the one line on the frame that may not
// carry any. The rule is narrow — ONE word, no spaces, at most [actionWordMax]
// characters — because a real five-to-ten-word caption never opens that way.
//
// AND AN ATTEMPT WITH NOTHING AFTER THE BAR IS REFUSED OUTRIGHT: both halves come
// back empty. `run |` is a model that produced the format and no sentence, and
// the two alternatives are both worse than nothing — handing back the raw line
// draws the label and the bar as though they were the work ("run |" on the
// frame), and handing back the family alone would put a mark beside a caption
// the narrator never actually replaced. Empty is the caller's signal to keep the
// deterministic composite already standing in the slot, which is a better line
// than either.
//
// NOTHING HERE RETRIES. Every outcome is one pass over one answer; the caller
// has already spent its call and does not ask again.
func SplitActionLine(raw string) (ActionCategory, string) {
	line := stripMarkup(strings.TrimSpace(firstLine(raw)))
	bar := strings.IndexByte(line, '|')
	if bar < 0 {
		return "", raw
	}
	label := strings.TrimSpace(line[:bar])
	rest := strings.TrimSpace(line[bar+1:])
	if label == "" || strings.ContainsAny(label, " \t") || len(label) > actionWordMax {
		// Not a label attempt at all: prose with a bar in it, or a bar that
		// opens the line. Either way the sentence is the whole line.
		return "", raw
	}
	if rest == "" {
		// The format, and no work named by it.
		return "", ""
	}
	category, ok := ParseActionCategory(label)
	if !ok {
		// The shape was right and the word was not. The label goes; the family
		// is decided by the tools, which cannot be wrong about themselves.
		return "", rest
	}
	return category, rest
}

// ActionCategoryForTool is THE DETERMINISTIC FLOOR: the family of one call,
// from its registered name alone.
//
// IT IS EXHAUSTIVE OVER THE BELT THIS BUILD CARRIES, and a test walks the belt
// to say so — a hand that landed without a family here would draw the generic
// mark forever and nothing would report it. Everything else answers
// [ActionWork]: a tool armed at runtime by a connected service has a name this
// binary has never seen, and a bucket is the honest answer to a name rather than
// a guess spelled out of its substrings.
//
// It takes the name and nothing else. The arguments are not read, deliberately:
// a shell call that runs a suite is [ActionRun] here and [ActionTest] only when
// the narrator says so, because a second rule about `go test` living in a second
// package is exactly the drift this file exists to prevent.
func ActionCategoryForTool(tool string) ActionCategory {
	switch strings.TrimSpace(tool) {
	// Looking for something not yet located.
	case "grep", "find", "web_search", "search_conversations",
		"gmail_search", "slack_search":
		return ActionSearch

	// Opening what is already located, and listing what is there.
	case "read", "read_document", "ls", "manual", "view_image", "settings",
		"use_skill", "tasks", "jobs", "list_harnesses", "list_subharnesses", "services",
		"gmail_read", "slack_read_thread", "slack_list_channels",
		"calendar_list", "workspace_snapshots",
		// A manager looking at its team: the states, and a member's page.
		"team_status", "team_read":
		return ActionRead

	// Changing something that exists.
	case "edit", "edit_video", "change_setting", "revise_assignment",
		"revise_design", "forget", "workspace":
		return ActionEdit

	// Writing content or generating an artifact.
	case "write", "generate_image", "generate_music", "generate_video",
		"remember", "propose_subharness", "build_harness", "calendar_create":
		return ActionCreate

	// Executing and waiting on what it prints.
	case "bash", "load_capability":
		return ActionRun

	// Going out to a page or a service.
	case "web_fetch", "use_service":
		return ActionBrowse

	// Moving bytes or state from one place to another.
	case "workspace_restore", "workspace_merge":
		return ActionTransfer

	// Saying something to a person.
	case "slack_send", "gmail_send", "speak", "ask",
		// A line into a team's traffic, from its manager or one of its members.
		"team_send", "team_post":
		return ActionCommunicate

	// Work handed out, or this mind copied to run beside itself.
	case "propose_task", "quick_task", "divide_work", "workspace_fork", "stand",
		// A manager starting a member or ending its turn.
		"team_start", "team_stop":
		return ActionCoordinate

	// Keeping the account of the work rather than doing it. `items` is a quick
	// task ticking its own list (task_quick.go): nothing about the work moves,
	// and what changes is the row saying where it has got to.
	case "track", "items", "commit", "recall":
		return ActionPlan

	// Standing by for something outside this turn.
	case "watch":
		return ActionWait
	}
	return ActionWork
}

// ActionCategoryForTools is one batch's family: the calls in a step, in the
// order they were made.
//
// THE FIRST NAMED FAMILY WINS, and the rule is deliberately that plain. A step is
// a run of calls the model made together, and what a person watching calls it is
// what it OPENED with — "reading three files and then editing one" is a reading
// step that went on to edit, not a two-icon step. Counting instead would let a
// batch of four reads and one edit be titled by the reads while a batch of four
// edits and one read flipped, which is a mark that moves for a reason nobody can
// see.
//
// [ActionWork] never wins while anything more specific is present: it is the
// bucket, so a batch of one unknown service call and one `read` is a reading
// step. An empty batch, and a batch of nothing but unknowns, is [ActionWork].
func ActionCategoryForTools(tools []string) ActionCategory {
	for _, tool := range tools {
		if category := ActionCategoryForTool(tool); category != ActionWork {
			return category
		}
	}
	return ActionWork
}
