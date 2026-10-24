// Package slashcat is THE ONE TABLE OF SLASH COMMANDS: every name, alias,
// argument form and one-line description the terminal chat answers to, written
// down once, with no terminal in it.
//
// IT EXISTS BECAUSE TWO READERS NEED THE SAME ROWS. internal/tui3 draws them as
// its command list and dispatches on them; internal/appwire serves them to the
// desktop app as `slash.catalog`. A table that lived in tui3 could not be
// imported by appwire without dragging Bubble Tea into the wire, and a copy in
// appwire would be the second table this package exists to prevent. So the rows
// are here, pure data over the standard library, and tui3 adapts them to its own
// row type (internal/tui3/commands.go).
//
// WHAT IS NOT HERE. Which key opens a page on this keyboard (a [Row.Place] names
// the page; the terminal appends its own chord), the order the menu shows rows in,
// what a row does when chosen, and anything about a program: programs are a
// property of the running build and are appended to the terminal's table at launch
// (internal/tui3/delegate.go), and to the wire's answer by the engine.
//
// A CATALOGUE ROW IS NOT AN ENDPOINT. Listing /cost here says the word exists;
// it does not mean the wire can run it. Nothing in this package executes a
// command, and the wire does not either: dispatch is the terminal's `app.slash`.
package slashcat

import (
	"fmt"
	"strings"
)

// DoorTask marks the rows that start work and therefore act inside a sentence
// as well as at the start of one. It is the terminal's send door, spelled as the
// wire spells it.
const DoorTask = "task"

// EffortKey is the terminal's chord for walking the effort ladder. It is in the
// /effort row's description, and tui3 reads this constant back rather than
// spelling it a second time.
const EffortKey = "alt+e"

// Row is one line of the table: what to type, what it takes, and one sentence.
// A command with several forms has several rows under one Name.
type Row struct {
	Name string
	Args string
	Desc string
	// Door names the alternate send road this command owns; "" is none.
	Door string
	// Alias are the other words that reach this command. They are words, not
	// rows: running one runs the canonical name.
	Alias []string
	// Place names the terminal page whose chord the terminal appends to Desc
	// ("teams", "spend", "memory"). The wire omits it: a chord is a fact about
	// one keyboard.
	Place string
}

// rows is the shared catalogue. Its declaration order is retained by the
// terminal's /help; its interactive menu sorts it by name regardless of the
// older placement notes below, which are about THAT menu's eight-row viewport.
var rows = []Row{
	// The picker's OTHER door is named here rather than on a line of its own,
	// because it is the same door: press the model's name in the status line
	// (render.go's [app.identityParts]). It is worth saying because a surface
	// with the mouse turned off (config's ui.mouse) does not have it, and this
	// row is then the only one there is.
	{Name: "model", Desc: "pick a model · or press its name above the message box"},
	// `<slug>` is the whole of what this row offers a person scanning the list,
	// and the three other shapes it takes — `@lane`, `auto`, a filter query —
	// are NOT four more rows here. The list is how somebody finds a command,
	// not where they learn its grammar; the manual's model page has the four
	// forms in a table ([modelArg] at the foot of this file).
	{Name: "model", Args: "<slug>", Desc: "switch the model for the conversation or open task"},
	// /set and /config were already answered by the dispatch before aliases
	// existed, and /connections and /sessions with them. They are written here
	// now because the table is the one place: a word the surface accepts and the
	// table does not mention is exactly the drift this file exists to prevent.
	{Name: "settings", Desc: "open the settings panel · ctrl+,", Alias: []string{"set", "config"}},
	// It sits under /settings because it is the other half of the same errand:
	// one is what this surface may do, the other is what it may reach.
	{Name: "connect", Desc: "providers and accounts · connect another", Alias: []string{"connections"}},
	// THE VOCABULARY OF THE FRESH START IS BORROWED AND NOT INVENTED. /clear is
	// what a terminal person's fingers type, /reset is what a chat person's do,
	// and both of them mean the thing this surface calls /new — so all three land
	// on it rather than on "unknown command: /clear".
	{Name: "new", Desc: "start another conversation in this project", Alias: []string{"clear", "clean", "reset"}},
	{Name: "delete", Desc: "permanently delete this conversation · asks first"},
	{Name: "resume", Desc: "open an earlier conversation", Alias: []string{"sessions"}},
	{Name: "compact", Desc: "shorten the conversation now"},
	{Name: "drafts", Desc: "cleared-but-kept drafts · enter restores one, d lets one go"},
	{Name: "stop", Desc: "stop the open task or selected work · asks first"},
	{Name: "autonomy", Desc: "how questions are handled while you are away"},
	{Name: "autonomy", Args: "<kind> <ask|recommend DURATION|decide>", Desc: "change one project's question rule"},
	// A project-less conversation needs this once, while /compact is a daily
	// command everywhere. Keep the one-shot anchor immediately below the eight
	// always-visible rows so adding it does not hide /compact behind a scroll.
	{Name: "workspace", Desc: "choose a folder to anchor this conversation · or /workspace <path>"},
	{Name: "workspace", Args: "<path>", Desc: "anchor this conversation to a project"},
	// AND THE OTHER HALF OF THE SAME ERRAND, directly under it: /workspace is
	// the one-shot anchor a project-less conversation needs once, and this is
	// "which folder do you mean" asked at any moment, with a picker to answer it
	// (folderpick.go). They sit together because a person hunting either reads
	// both rows on the way past.
	//
	// It is BELOW /compact for the reason /home and /permissions are: [menuRows]
	// shows eight rows at once, position in this table is a claim about
	// frequency, and a row inserted above /compact would push a daily command
	// behind a scroll.
	//
	// THE OTHER WORDS ARE THE OTHER VOCABULARIES. People say "folder", the
	// design says "place", and terminal fingers type "dir" — and none of the
	// three should have to find out which one this build chose.
	{Name: "folder", Desc: "choose a folder to work in · type a path to browse", Alias: []string{"place", "dir"}},
	{Name: "folder", Args: "<path>", Desc: "…open it already pointed at that path"},
	// AND THE OTHER HALF OF THE WORD, SPLIT OFF ON 2026-09-22. /folder gives
	// THIS conversation a folder; this sets the one the next conversation
	// opens in, and home is the only screen that has a next conversation
	// (projectcmd.go). They sit together because a person who types either
	// one meant one of the two and reads both rows on the way past.
	{Name: "project", Desc: "the folder your next conversation opens in · on home"},
	{Name: "project", Args: "<path>", Desc: "…that folder, without opening the browser"},
	// AND ITS OTHER END. Choosing a folder is where work aimed somewhere else
	// starts; this is where it arrives. It sits directly under /folder because
	// nobody reaches for it who has not already done the first — and because
	// the row above the box says the word the moment there is anything to land
	// (landcmd.go), so the list is the second way of finding it and not the
	// first.
	{Name: "land", Desc: "put the changes for another folder into it · says what changed first"},
	{Name: "land", Args: "now", Desc: "…put the waiting changes into their folder"},
	{Name: "land", Args: "<folder>", Desc: "…that folder, when more than one is waiting"},
	// IT BELONGS BESIDE /resume AND SITS UNDER /compact, and the gap is the
	// frequency law this table is ordered by. /resume is "which conversation,
	// here" and this is "what is there at all" — the same question one size up —
	// but the first eight rows are the ones a person reaches for without
	// looking, /compact is one of them, and a row inserted above it would push
	// the daily command behind a scroll (deliverables_test.go pins exactly
	// that). So it lands as close to its pair as the law allows (home.go).
	{Name: "processes", Desc: "open this conversation's processes and logs"},
	{Name: "dismiss", Desc: "hide settled task notifications here"},
	{Name: "dismiss", Args: "undo", Desc: "restore dismissed task notifications"},
	{Name: "home", Desc: "every project and conversation on this machine"},
	// AND THE TWO PLACES THAT HAD NO TYPED DOOR, directly under the one that
	// does. /home, /memory, /automations, /history and /settings each open a
	// place from the box; search and spend were reachable only by their `alt+`
	// digit, `tab`, the tab bar, or typing a word on home — every one of which
	// has to be learned somewhere else first. The digit on each row is read off
	// the bar's order table ([placeChord]).
	//
	// /spend IS A PLACE AND NOT A READING, WHICH IS WHY IT MOVED. It used to be
	// an alias of /cost, so the one word a person guesses for "what has this cost
	// me" printed THIS CONVERSATION's bill and never said the machine-wide place
	// existed. The two answer different questions — /cost is this conversation,
	// spend is every window, task and automation run on the machine — and the
	// word belongs to the bigger one. /cost keeps /usage and /tokens, and says on
	// its own row which question it is answering, so nobody who typed either
	// word lands nowhere.
	{Name: "wall", Desc: "every open conversation, live, and your teams · alt+v or ▦ below the box"},
	// THE TEAMS PAGE, beside the wall it opens onto: the wall is the open
	// conversations big, and this is the team-level view, every member open or
	// not, the manager's conversation and what waits on you (place_teams.go).
	{Name: "teams", Desc: "your teams, their managers and what waits on you", Place: "teams"},
	// AND THE ASK A MANAGER IS GIVEN FOR HELPERS, under the page its team is on
	// (helpers.go): the team is made by the Teams menu's `+ New from this
	// session`, and this sends the manager the words after the command. Two
	// rows, the way /manual has two, because bare it sends a default ask,
	// written out in the transcript, and with words it sends those instead.
	{Name: "helpers", Desc: "asks the team this conversation manages to suggest helpers · make the team with + New from this session in the Teams menu"},
	{Name: "helpers", Args: "<what to ask the manager>", Desc: "…the same, sending your own words instead of the default ask"},
	{Name: "spend", Desc: "what this machine has cost, by the day", Place: "spend"},
	// It sits AFTER /compact and before /help because those two are the pair a
	// person reads together when a conversation has gone wrong: compacting is
	// what you do when the turn was right and too long, rewinding is what you do
	// when the turn was wrong. Putting it last, beside /quit, would file "take
	// back a message" under leaving.
	// The row names what the COMMAND opens — the whole conversation, as a list to
	// pick a point out of (rewindsheet.go) — and then the gesture that takes the
	// last message back without opening anything (rewind.go). Two tiers, one row,
	// in the order a person meets them.
	{Name: "rewind", Desc: "go back to an earlier point · esc esc takes back the last", Alias: []string{"undo", "back"}},
	// WHAT HAS ALREADY BEEN ANSWERED, and the way to take one back
	// (permissions.go). It BELONGS beside /settings and /connect — those two are
	// "what may this thing do" and "what may it reach", and this is "what has it
	// already been told it may do without asking" — and it sits down here
	// instead for the reason /harness does, stated one row below: [menuRows]
	// shows eight rows at once, position in this table is a claim about
	// frequency, and putting this at the top of the list would have pushed
	// /compact into a scroll to make room for a panel a person opens when
	// something has surprised them. /perms is here because it is what fingers
	// type; the row shows the whole word.
	{Name: "permissions", Desc: "what runs without asking · drop one with d", Alias: []string{"perms"}},
	// AND WHAT RUNS ON A CLOCK, beside what may run without asking
	// (place_automations.go). The pair is the same question asked twice — one
	// is what this thing may do when you ask it, the other is what it does when
	// nobody asks — and this row sits under that one for the reason that one
	// sits down here: [menuRows] shows eight rows at once, position in this
	// table is a claim about frequency, and a page a person opens to check on
	// something may not push /compact into a scroll.
	//
	// TWO ROWS FOR ONE COMMAND, the way /export and /crew have two, and for that
	// reason: a single row carrying arguments would make the bare form — the
	// place — unreachable from this list, because [app.runMenu] puts a row that
	// TAKES something into the draft instead of running it. The place leads,
	// because the conversation is where most automations are made and the place
	// is where a person comes to see them; the exact typed form rides under it.
	{Name: "automations", Desc: "what runs on a clock · run, pause, edit or delete one"},
	{Name: "automations", Args: "add <title> …", Desc: "…or add one exactly, read back on a card first"},
	// THE SHAPES OF WORK THIS CONVERSATION HAS SAVED (harnesspanel.go). It
	// belongs topically beside /connect — one is what this surface may reach,
	// the other is what it has learned to do — and it sits here instead for a
	// reason about the LIST rather than about the command: [menuRows] shows
	// eight rows at once, and a new row put in the middle would have pushed
	// /compact, which people reach for daily, into a scroll to make room for one
	// they will open occasionally. Position in this table is a claim about
	// frequency; this is the honest one.
	// The other words are here because a sub-harness and a harness are the same
	// thing under two names (docs/SUBHARNESS.md), and a person who learned the
	// longer one should not have to find out which half of it this build chose.
	// Every one of them opens the same panel bare, and the same picker with a
	// space after it (harnesspick.go).
	{Name: "harness", Desc: "the shapes of work you have saved · a space picks one to run",
		Alias: []string{"harnesses"}},
	// THE PROGRAMS THIS CONVERSATION CAN RUN (subharness.go). It sits directly
	// under /harness because that is the neighbouring question — one is what this
	// surface has learned to do from watching you, the other is the typed programs
	// it can be given — and a person hunting either will read both rows on the way
	// past.
	//
	// `/subharness` AND `/sub` USED TO BE WORDS FOR THE ROW ABOVE and are not any
	// more, because they now name something else. A subharness is a typed program
	// with an input schema, a cost shape and a card you settle before it runs
	// (docs/SUBHARNESS-PRD.md §2 fixes the person-facing name); a harness is the
	// older saved shape of work that row opens. They are two things, so they are
	// two rows — a word that reached whichever of them the table happened to list
	// first is a word nobody could rely on.
	//
	// TWO ROWS FOR ONE COMMAND, the way /model and /export have two: the bare form
	// is the list nearly everybody wants, and a single row carrying <name> would
	// make it unreachable from this list — [app.runMenu] puts a row that TAKES
	// something into the draft instead of running it.
	{Name: "subharness", Desc: "the programs you can run · type to filter · enter opens its card",
		Alias: []string{"sub"}},
	{Name: "subharness", Args: "<name>", Desc: "…straight to that one's card"},
	// THE SKILLS THIS CONVERSATION CAN BE HANDED (skillpick.go). It sits
	// directly under the subharness rows because it is the neighbouring
	// question — those are the programs this conversation can run, and this is
	// what it can be told to know — and on the picker's own terms: a space
	// after it opens the shelf, enter on a row toggles that skill on or off,
	// and a query that looks like a path offers the skill in that folder.
	{Name: "skill", Desc: "what you can hand this conversation · a space picks more than one",
		Alias: []string{"skills"}},
	// WHAT IT KNOWS ABOUT YOU, and the two ways to change it. They sit beside
	// /harness because they answer the neighbouring question — one is what this
	// conversation has learned to DO, these are what it has been told about YOU
	// — and they are three rows rather than one because a person arrives with
	// one of three errands: seeing the list, adding to it, or dropping something
	// off it.
	//
	// /memories has the bare form and the narrowed one, the way /model does: the
	// bare row is the one nearly everybody wants, and a single row carrying
	// [query] would make it unreachable from the list ([app.runMenu] puts a row
	// that TAKES something into the draft instead of running it).
	{Name: "memory", Desc: "open the Memories tab", Place: "memory"},
	{Name: "memory", Args: "<words>", Desc: "print the remembered lines those words name"},
	{Name: "memories", Desc: "open the Memories tab, or print every line kept here"},
	{Name: "memories", Args: "<words>", Desc: "…only the lines those words name"},
	// THERE IS NO /forget: a line is forgotten on the Memories tab, where it is
	// in front of the person rather than guessed at from words (memory.go).
	{Name: "remember", Args: "<text>", Desc: "keep one line for this project · for you everywhere, from home"},
	// AND /always, THE SAME ERRAND AS /remember UNDER THE WORD PEOPLE WERE TAUGHT:
	// a timeless rule is a memory like any other line (anything with a time in it
	// is an automation). Two rows for the /memories reason: the bare form is the
	// list of what holds here, and a single row carrying <text> would make that
	// list unreachable from the menu.
	{Name: "always", Desc: "every line kept here · everywhere, from home"},
	{Name: "always", Args: "<text>", Desc: "…keep one, the same as /remember"},
	// AND WHAT codeaf WORKS WITH, beside what it knows about you: the crew a
	// task runs on — worker, planner, checker — picked per task (crew.go). The
	// bare form is the panel; the four shortcuts are how anything on it
	// changes, one row each, because [app.runMenu] puts a row that TAKES
	// something into the draft instead of running it, and each of these takes
	// something.
	//
	// It sits here, under the memory rows, because those three are "what does it
	// know" and this is "what does it think WITH".
	{Name: "crew", Desc: "the crew tasks run on · picked per task, with your pins, allowed models and daily cap"},
	{Name: "crew", Args: "pin <seat> <model[@provider]>", Desc: "…pin the worker, planner or checker · /model stays"},
	{Name: "crew", Args: "unpin <seat|all>", Desc: "…put a seat back on auto"},
	{Name: "crew", Args: "models <rule>", Desc: "…which models a seat may be picked from · all, open, ≤in/out, ids"},
	{Name: "crew", Args: "cap <dollars|off>", Desc: "…the most tasks' crews may spend in a day"},
	{Name: "crew", Args: "cap task <dollars>", Desc: "…the most one task may spend"},
	// AND HOW HARD THE ONE YOU TALK TO THINKS, under the two rows about WHICH
	// models it thinks with, because that is the order the two questions arrive
	// in: a person picks the model and then decides how much of it to spend.
	//
	// TWO ROWS FOR ONE COMMAND, the way /crew and /model have two: the bare form
	// is the five rungs with what each one buys, which is how somebody chooses
	// between words that all mean "harder"; a single row carrying <rung> would
	// make that list unreachable, since [app.runMenu] puts a row that TAKES
	// something into the draft instead of running it.
	//
	// It is the LADDER'S door and not its only one. The rung is on the seam
	// beside the model, `alt+e` walks it and so does a press on it
	// (effortchip.go) — this is the row for the person who wants to read the
	// five before choosing, and the word people reach for is `thinking`, which
	// is what the settings row calls the same ladder.
	{Name: "effort", Desc: "how hard this conversation thinks · the five rungs, and what each buys",
		Alias: []string{"think", "thinking"}},
	{Name: "effort", Args: "<rung>", Desc: "…set it outright · " + EffortKey + " walks it, or press it on the seam"},
	{Name: "task", Desc: "every task this machine has run · ctrl+.", Door: DoorTask},
	{Name: "task", Args: "<brief>", Desc: "start work you can walk away from", Door: DoorTask},
	{Name: "task", Args: "solo <brief>", Desc: "…with one worker, and no sizing call before it", Door: DoorTask},
	// HOW HARD TO TRY THIS ONE TASK is said in the ask and sticks to nothing
	// (crew.go): --best puts the strongest crew the allowed models make on it,
	// --cheap the cheapest, and neither moves the next task.
	{Name: "task", Args: "--best <brief>", Desc: "…on the strongest crew allowed, this task only", Door: DoorTask},
	{Name: "task", Args: "--cheap <brief>", Desc: "…on the cheapest crew allowed, this task only", Door: DoorTask},
	{Name: "redo", Desc: "the last task again, on a stronger crew"},
	{Name: "redo", Args: "stronger", Desc: "the last task again, on a stronger crew"},
	// THE THIRD ROW IS GONE, AND ITS ABSENCE IS THE FEATURE. It typed
	// `adaptive <brief>`, which opened a planner that drew the whole graph before
	// any of the work had been looked at. The measured road answers that question
	// later and from evidence — one worker starts, and hands parts out only once
	// it has opened the material and found the width is real (internal/splitgate,
	// internal/session's task_divide.go). Leaving the row here would offer a word
	// that now starts an ordinary task, which is a menu lying about what it does.
	// AND THE PAGE THAT SHOWS WHAT THEY ALL CAME TO (taskview.go). It sits with
	// the two rows that START work because that is the pair of errands a person
	// has about tasks — set one going, and go and look at the ones that already
	// did.
	//
	// IT IS NOT SPELLED "/tasks", AND THE NEAR-MISS IS THE REASON. The two rows
	// above both mean GIVE codeaf WORK, and a plural sitting among them shared four
	// characters with every one of them: typing "/task" narrowed the list to both
	// errands at once, so the muscle memory for starting work kept landing on a
	// page that starts none. What a person calls this thing is the record of
	// everything the project has run, and "history" is that word — it collides with
	// nothing, and it is what somebody types when they cannot remember that the
	// chord is ctrl+.
	//
	// No argument form: the page is a list you are shown, and it is TYPED AT once
	// it is up ([app.taskSheetFilter]) rather than queried from the command line.
	// That is /files' rule as well — a task is recognized by a title a model wrote,
	// which is not a thing anybody types back correctly.
	{Name: "history", Desc: "every task this project has run · ctrl+."},
	// THE TWO QUESTIONS THE STATUS LINE IS ALREADY ANSWERING, asked out loud. The
	// line at the bottom of the frame drops whatever does not fit and a phone-width
	// frame keeps two of eleven facts (statusdeck.go), so on any surface these are
	// the commands that say the rest of it — and on a surface with the mouse turned
	// off they are the only way to the sheet at all.
	//
	// They sit HERE, under /harness and above the copy pair, for the reason
	// /harness sits where it does: position in this table is a claim about
	// frequency, [menuRows] shows eight rows at once, and a person reads their bill
	// occasionally while they compact and rewind daily. Neither may push /compact
	// into a scroll.
	//
	// /status is the wider word and goes first, because the spend is one of the
	// lines it prints: somebody who wanted the money and typed the general word
	// still gets their answer, while the reverse is not true.
	{Name: "status", Desc: "everything the status line knows, one fact per line", Alias: []string{"info", "context"}},
	{Name: "status", Args: "--json", Desc: "…everything the status line knows, as one JSON object"},
	// THE ROW NAMES WHOSE BILL IT IS, because the other one is now a command of
	// its own two rows up: /cost is THIS CONVERSATION and /spend is the machine.
	// The word `spend` used to be an alias here and pointed the one guess a
	// person makes at the wrong reading.
	{Name: "cost", Desc: "what this conversation has spent · /spend is the whole machine", Alias: []string{"usage", "tokens"}},
	// AND DIRECTLY UNDER WHAT IT HAS SPENT, WHAT IT MAY. /cost is the reading and
	// this is the editor, and they sit together because a person who has just
	// read a figure is the person deciding whether it is too high (budget.go).
	// Three rows for one command, /export's reason exactly: bare is the shape
	// nearly everybody wants, and the two that WRITE ride under it wearing the
	// "…".
	{Name: "budget", Desc: "what codeaf may spend · every limit on one tab", Alias: []string{"limits"}},
	{Name: "budget", Args: "<amount>", Desc: "…set the day's limit · none removes it"},
	{Name: "budget", Args: "<row> <amount>", Desc: "…set one by name: day, conversation, plan"},
	// THE DISK BESIDE THE MONEY: /cost is what this conversation has spent and
	// this is what the machine is holding for it — the shared build cache task
	// workers fill (internal/cachedir). Two rows for one command, /export's
	// reason exactly: the reading form is the one nearly everybody wants, and
	// [app.runMenu] writes a row that TAKES something into the draft instead of
	// running it, so the destructive form rides second wearing the "…". It is
	// NOT an alias of anything and shares no word with /new's fresh-start set —
	// /clean already means "start another conversation" there, and a word that
	// sometimes cleared the screen and sometimes deleted half a gigabyte would
	// be the most expensive pun on the surface.
	{Name: "cache", Desc: "the shared build cache — how big, and where"},
	{Name: "cache", Args: "clean", Desc: "…delete it to free disk · asks before anything is removed"},
	{Name: "cache", Args: "clean now", Desc: "…delete the cache now"},
	// THE THREE DOORS ONTO GETTING TEXT OUT, and they sit beside /help because
	// that is where a person goes with the question they answer. The keys behind
	// the first two are the least discoverable on the surface — nothing on the
	// screen says either exists — and "why can I not copy this" is the first
	// question this surface gets asked. /copy is the keyboard's way, /select the
	// mouse's (copymode.go).
	//
	// /export is the third and it is a different KIND of answer: those two hand
	// over what is on the screen, and this one writes the whole conversation to a
	// file somebody can send (export.go). It is last of the three because it is
	// the one a person reaches for once, at the end.
	{Name: "copy", Desc: "read the conversation back and copy from it · ctrl+b"},
	{Name: "select", Desc: "drag to select with your mouse · ctrl+s"},
	// TWO ROWS FOR ONE COMMAND, the way /model has two. A single row carrying
	// <path> would make the bare form — which is the one nearly everybody wants —
	// unreachable from the list: [app.runMenu] writes a row that TAKES something
	// into the draft instead of running it, so choosing it would put "/export "
	// in the box and wait for a path nobody had in mind.
	{Name: "export", Desc: "write this conversation to a file", Alias: []string{"save"}},
	{Name: "export", Args: "<path>", Desc: "…and write it there · tab completes the path"},
	// AND THE FOURTH DOOR, which is the other direction: those three take
	// something out of THIS conversation, and this one finds what any of them
	// has already made — a picture, an export, a document — from a list of
	// everything, whatever directory it was made in (deliverables.go). It sits
	// beside them because that is the errand a person is on when they reach for
	// it, and after them because it is the one you type when the making is
	// already done.
	//
	// No argument form and no alias. A deliverable is picked from rows a person
	// recognizes by title, and a title a model wrote is not a thing anybody
	// types back correctly.
	{Name: "files", Desc: "what has been made for you · open, reveal or copy one"},
	// AND THE SAME ERRAND POINTED AT THE OTHER MACHINE. On a `--host` session the
	// bare row above opens the far workspace as a page in this machine's browser,
	// and this one brings ONE file back and opens it in whatever this machine
	// opens that kind of file with (remotefiles.go, remoteopen.go).
	//
	// TWO ROWS FOR ONE COMMAND, the way /model and /export have two, and for
	// their reason: [app.runMenu] puts a row that TAKES something into the draft
	// instead of running it, so a single row carrying <path> would make the bare
	// form — the one nearly everybody wants — unreachable from this list.
	//
	// The row says what it is FOR rather than which flag it needs, because a
	// person on a local session who chooses it is told in one sentence that this
	// form is for a session on another machine ([filesLocalWord]) — which is a
	// better place to learn it than a table nobody reads twice.
	{Name: "files", Args: "<path>", Desc: "…or bring one back from the machine over there and open it"},
	// AND THE ONE THAT GOES THE OTHER WAY: those four take something out of this
	// conversation and this one puts something INTO it — a log, a CSV, a PDF, on
	// the same tray a picture rides and read rather than looked at (attach.go).
	//
	// ITS PLACE IN THIS LIST KEEPS /compact VISIBLE. [menuRows] shows eight
	// rows at once, so position is a claim about frequency. /attach stands with
	// the doors onto moving a file, an errand it shares with /files; putting it
	// higher would push /compact, which people reach for daily, into a scroll.
	// permissions_test.go pins that ordering.
	//
	// A PICTURE HANDED TO /attach STILL GOES ON AS A PICTURE. Its extension
	// decides whether the model looks at it or reads a file, so one command
	// covers both kinds of cargo.
	//
	// /upload is here because it is the word people bring from every chat program
	// they have used. /file is deliberately NOT an alias: it shares four
	// characters with /files one row above, and a word that narrowed the list to
	// both errands at once is the near-miss /history was named to avoid.
	//
	// TWO ROWS, /folder'S REASON EXACTLY: the bare form is the browser and
	// enter on its row opens it at once, where one row with a placeholder
	// left `/attach ` in the box waiting for a path nobody had — a second
	// enter to reach the sheet the word already meant (the owner met it,
	// 2026-09-22).
	{Name: "attach", Desc: "choose a file to attach · the browser opens", Alias: []string{"upload"}},
	{Name: "attach", Args: "<path>", Desc: "…attach that file · tab completes the path"},
	// AND DIRECTLY ABOVE /help, THE OTHER QUESTION SOMEBODY HAS WHEN THEY ARE
	// LOST. /help is what you can TYPE; this is what codeaf DOES, in the writing
	// codeaf is built from (manualcmd.go). They sit together because a person who
	// has just read a list of commands and still does not know what one of them
	// means is one row away from the page that says.
	//
	// Two rows for one command, /export's reason exactly: the bare form is the
	// only one that can be RUN from this list, since [app.runMenu] puts a row
	// that TAKES something into the draft instead of running it. The one that
	// takes something rides under it wearing the "…". Both are turns since
	// 2026-09-22 — the question goes to the model with the manual open — where
	// three rows used to print the pages as written.
	{Name: "manual", Desc: "asks the model what codeaf can do, from its own manual"},
	{Name: "manual", Args: "<question>", Desc: "…puts that question to the model, answered from the manual"},
	// AND THE ROW FOR THE DAY SOMETHING GOES WRONG, directly above /help for the
	// reason /manual sits there: it is the third thing a person reaches for when
	// they are stuck, after the list of commands and the page that explains one.
	//
	// It is this far down the table because [menuRows] shows eight rows at once
	// and position here is a claim about frequency — nobody turns the record on
	// twice in a day, and a row inserted higher would push a daily command
	// behind a scroll.
	{Name: "debug", Desc: "keep the full record of this conversation · says where it goes"},
	{Name: "update", Desc: "install the newest codeaf for the next launch", Alias: []string{"upgrade"}},
	{Name: "update", Args: "<channel or tag>", Desc: "…that channel or exact tag; skip or never answer an open offer"},
	{Name: "help", Desc: "this list", Alias: []string{"?"}},
	{Name: "quit", Desc: "close this conversation", Alias: []string{"exit", "q"}},
}

// Rows is the catalogue in declaration order. The slice is a copy, and so is
// every alias list in it, so a caller may keep or edit it freely.
func Rows() []Row {
	out := make([]Row, len(rows))
	for i, r := range rows {
		r.Alias = append([]string(nil), r.Alias...)
		out[i] = r
	}
	return out
}

// Check is THE TABLE LAW, run at init by the terminal over [Rows] and by tests
// over tables of their own.
//
// AN ALIAS MAY NEVER SHADOW A CANONICAL NAME OR ANOTHER ALIAS. A word that meant
// two things would resolve to whichever row the loop reached first, which makes
// the answer to "what does /clear do" a fact about the table's ORDER — and this
// table is ordered for reading, so it would be an accident waiting on the next
// person who moves a row. A canonical name may repeat, because /model and
// /model <slug> are two forms of one command rather than two commands.
func Check(list []Row) error {
	names := make(map[string]bool, len(list))
	for _, c := range list {
		names[c.Name] = true
	}
	owner := make(map[string]string, len(list))
	for _, c := range list {
		for _, word := range c.Alias {
			switch {
			case word == "":
				return fmt.Errorf("/%s has an empty alias", c.Name)
			case names[word]:
				return fmt.Errorf("/%s is an alias of /%s and a command in its own right", word, c.Name)
			case owner[word] != "":
				return fmt.Errorf("/%s is an alias of both /%s and /%s", word, owner[word], c.Name)
			}
			owner[word] = c.Name
		}
	}
	return nil
}

// Form is one way to type a command: its arguments, as the row spells them, and
// what that form does. The bare form has empty Args.
type Form struct {
	Args string
	Desc string
}

// Command is one canonical command with every form and alias the table gives it.
type Command struct {
	Name    string
	Aliases []string
	Door    string
	Forms   []Form
}

// Commands folds rows into canonical commands: one per distinct Name, in the
// order a name first appears, with its forms in row order and its aliases in
// the order they were written. It is the shape both the wire and a count of
// "how many commands are there" want, and the only place rows are grouped.
func Commands(list []Row) []Command {
	var out []Command
	at := map[string]int{}
	for _, r := range list {
		i, seen := at[r.Name]
		if !seen {
			i = len(out)
			at[r.Name] = i
			out = append(out, Command{Name: r.Name})
		}
		c := &out[i]
		if c.Door == "" {
			c.Door = r.Door
		}
		c.Aliases = append(c.Aliases, r.Alias...)
		c.Forms = append(c.Forms, Form{Args: r.Args, Desc: r.Desc})
	}
	return out
}

// Typed is the command as it is written: "/model <slug>".
func (r Row) Typed() string {
	if r.Args == "" {
		return "/" + r.Name
	}
	return "/" + r.Name + " " + r.Args
}

// Words is every word the list answers to, names and aliases, without the slash.
func Words(list []Row) map[string]bool {
	words := map[string]bool{}
	for _, r := range list {
		words[r.Name] = true
		for _, a := range r.Alias {
			words[a] = true
		}
	}
	return words
}

// Resolve is the canonical name a typed word runs as: an alias resolves to the
// command that owns it, and anything else is returned folded to lower case.
func Resolve(list []Row, word string) string {
	word = strings.ToLower(word)
	for _, r := range list {
		for _, a := range r.Alias {
			if a == word {
				return r.Name
			}
		}
	}
	return word
}
