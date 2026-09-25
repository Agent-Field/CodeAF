package tui3

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// THE COMMAND LIST: type "/" and what you can type appears.
//
// It is the palette gesture again (palette.go) with two differences, and both
// of them come from the same fact — the slash is typed into the DRAFT, not into
// a filter box of its own:
//
//   - it is not modal. The person keeps typing into the box they were already
//     typing in, the list narrows under it, and only ↑/↓/enter/esc are taken.
//   - it opens and closes by itself. A leading "/" opens it; a space closes it,
//     because a line with an argument in it is a line being written rather than
//     a command being chosen.
//
// The table below is the ONE place a command is written down. /help renders the
// same rows (see [helpText]), so a command that exists in one and not the other
// is not possible.
//
// THE OTHER WORDS FOR A COMMAND ARE IN THE TABLE TOO, on the row they belong to
// ([command.alias]). People arrive here from other tools with a vocabulary
// already in their fingers — /clear, /exit, /q, /? — and a surface that answers
// "unknown command" to those is a surface asking somebody to unlearn something
// before it will talk to them. So the words are accepted, and the dispatch
// resolves every one of them through this table ([canonicalCommand]) rather than
// growing a second list of synonyms next to the switch that runs them.

// command is one slash command: what to type, what it takes, and one line.
type command struct {
	name string
	args string
	desc string
	// door names the alternate send road this command owns. THE SEND-DOOR SET
	// LIVES ON THE COMMAND ROWS so dispatch, aliases, chips and tags cannot grow
	// separate lists that disagree about which words act inside a sentence.
	door sendDoor
	// alias are the other words that reach this same row. They are NOT rows of
	// their own: the list shows the canonical name, filtering an alias surfaces
	// that canonical row, and running one runs that canonical command — with the
	// aliases printed dimly beside it, so somebody who typed the word they knew
	// can see both what ran and what this surface calls it.
	alias []string
}

// commands is the shared catalogue. Its declaration order is retained by /help;
// the interactive menu sorts it by name regardless of the older placement notes.
var commands = []command{
	// The picker's OTHER door is named here rather than on a line of its own,
	// because it is the same door: press the model's name in the status line
	// (render.go's [app.identityParts]). It is worth saying because a surface
	// with the mouse turned off (config's ui.mouse) does not have it, and this
	// row is then the only one there is.
	{name: "model", desc: "pick a model · or press its name above the message box"},
	// `<slug>` is the whole of what this row offers a person scanning the list,
	// and the three other shapes it takes — `@lane`, `auto`, a filter query —
	// are NOT four more rows here. The list is how somebody finds a command,
	// not where they learn its grammar; the manual's model page has the four
	// forms in a table ([modelArg] at the foot of this file).
	{name: "model", args: "<slug>", desc: "switch the model for the conversation or open task"},
	// /set and /config were already answered by the dispatch before aliases
	// existed, and /connections and /sessions with them. They are written here
	// now because the table is the one place: a word the surface accepts and the
	// table does not mention is exactly the drift this file exists to prevent.
	{name: "settings", desc: "open the settings panel · ctrl+,", alias: []string{"set", "config"}},
	// It sits under /settings because it is the other half of the same errand:
	// one is what this surface may do, the other is what it may reach.
	{name: "connect", desc: "providers and accounts · connect another", alias: []string{"connections"}},
	// THE VOCABULARY OF THE FRESH START IS BORROWED AND NOT INVENTED. /clear is
	// what a terminal person's fingers type, /reset is what a chat person's do,
	// and both of them mean the thing this surface calls /new — so all three land
	// on it rather than on "unknown command: /clear".
	{name: "new", desc: "start another conversation in this project", alias: []string{"clear", "clean", "reset"}},
	{name: "resume", desc: "open an earlier conversation", alias: []string{"sessions"}},
	{name: "compact", desc: "summarize the conversation now"},
	{name: "drafts", desc: "cleared-but-kept drafts · enter restores one, d lets one go"},
	{name: "stop", desc: "stop the open task or selected work · asks first"},
	{name: "autonomy", desc: "how questions are handled while you are away"},
	{name: "autonomy", args: "<kind> <ask|recommend DURATION|decide>", desc: "change one project's question rule"},
	// A project-less conversation needs this once, while /compact is a daily
	// command everywhere. Keep the one-shot anchor immediately below the eight
	// always-visible rows so adding it does not hide /compact behind a scroll.
	{name: "workspace", args: "<path>", desc: "anchor this conversation to a project"},
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
	{name: "folder", desc: "choose a folder to work in · type a path to browse", alias: []string{"place", "dir"}},
	{name: "folder", args: "<path>", desc: "…open it already pointed at that path"},
	// AND THE OTHER HALF OF THE WORD, SPLIT OFF ON 2026-09-22. /folder gives
	// THIS conversation a folder; this sets the one the next conversation
	// opens in, and home is the only screen that has a next conversation
	// (projectcmd.go). They sit together because a person who types either
	// one meant one of the two and reads both rows on the way past.
	{name: "project", desc: "the folder your next conversation opens in · on home"},
	{name: "project", args: "<path>", desc: "…that folder, without opening the browser"},
	// AND ITS OTHER END. Choosing a folder is where work aimed somewhere else
	// starts; this is where it arrives. It sits directly under /folder because
	// nobody reaches for it who has not already done the first — and because
	// the row above the box says the word the moment there is anything to land
	// (landcmd.go), so the list is the second way of finding it and not the
	// first.
	{name: "land", desc: "put the changes for another folder into it · says what changed first"},
	{name: "land", args: "<folder>", desc: "…that folder, when more than one is waiting"},
	// IT BELONGS BESIDE /resume AND SITS UNDER /compact, and the gap is the
	// frequency law this table is ordered by. /resume is "which conversation,
	// here" and this is "what is there at all" — the same question one size up —
	// but the first eight rows are the ones a person reaches for without
	// looking, /compact is one of them, and a row inserted above it would push
	// the daily command behind a scroll (deliverables_test.go pins exactly
	// that). So it lands as close to its pair as the law allows (home.go).
	{name: "home", desc: "every project and conversation on this machine"},
	// AND THE TWO PLACES THAT HAD NO TYPED DOOR, directly under the one that
	// does. /home, /memory, /standing, /history and /settings each open a place
	// from the box; search and spend were reachable only by their `alt+` digit,
	// `tab`, the tab bar, or typing a word on home — every one of which has to be
	// learned somewhere else first. The digit on each row is read off the bar's
	// order table ([placeChord]).
	//
	// /spend IS A PLACE AND NOT A READING, WHICH IS WHY IT MOVED. It used to be
	// an alias of /cost, so the one word a person guesses for "what has this cost
	// me" printed THIS CONVERSATION's bill and never said the machine-wide place
	// existed. The two answer different questions — /cost is this conversation,
	// spend is every window, task and standing run on the machine — and the word
	// belongs to the bigger one. /cost keeps /usage and /tokens, and says on its
	// own row which question it is answering, so nobody who typed either word
	// lands nowhere.
	{name: "search", desc: "everything said on this machine · " + placeChord(pageSearch)},
	{name: "wall", desc: "every open conversation, live, and your teams · alt+v or ▦ below the box"},
	// THE TEAMS PAGE, beside the wall it opens onto: the wall is the open
	// conversations big, and this is the team-level view, every member open or
	// not, the manager's conversation and what waits on you (place_teams.go).
	{name: "teams", desc: "your teams, their managers and what waits on you · " + placeChord(pageTeams)},
	{name: "spend", desc: "what this machine has cost, by the day · " + placeChord(pageSpend)},
	// It sits AFTER /compact and before /help because those two are the pair a
	// person reads together when a conversation has gone wrong: compacting is
	// what you do when the turn was right and too long, rewinding is what you do
	// when the turn was wrong. Putting it last, beside /quit, would file "take
	// back a message" under leaving.
	// The row names what the COMMAND opens — the whole conversation, as a list to
	// pick a point out of (rewindsheet.go) — and then the gesture that takes the
	// last message back without opening anything (rewind.go). Two tiers, one row,
	// in the order a person meets them.
	{name: "rewind", desc: "go back to an earlier point · esc esc takes back the last", alias: []string{"undo", "back"}},
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
	{name: "permissions", desc: "what runs without asking · drop one with d", alias: []string{"perms"}},
	// AND WHAT IS ALREADY TRUE HERE, beside what may run without asking
	// (standingpage.go). The pair is the same question asked twice — one is what
	// this thing may do when you ask it, the other is what it keeps doing when
	// nobody asks — and this row sits under that one for the reason that one
	// sits down here: [menuRows] shows eight rows at once, position in this
	// table is a claim about frequency, and a page a person opens when something
	// has surprised them may not push /compact into a scroll.
	//
	// TWO ROWS FOR ONE COMMAND, the way /export and /crew have two, and for that
	// reason: a single row carrying <words> would make the bare form — the page
	// — unreachable from this list, because [app.runMenu] puts a row that TAKES
	// something into the draft instead of running it.
	//
	// THE WORDS COME FIRST. Making an order is the act this command exists for —
	// the owner's own ruling — and the page is the follow-up a person opens to
	// see what their sentences became. So the row that starts an order leads,
	// and the page rides under it wearing the "…or".
	//
	// The words go through the deliberate door (standmark.go's [app.standingSay]),
	// which is what the tail of this row promises: it is the chord's own sentence
	// said in the grammar of the list.
	{name: "standing", args: "<words>", desc: "keep this true · a card, never work done once", door: sendDoorStanding},
	// /orders is here because it is the other word people bring for the thing:
	// a standing order is the concept, and half of them will type the noun they
	// remember rather than the adjective this surface chose.
	{name: "standing", desc: "…or what stands over this conversation · stop, pause or not here",
		alias: []string{"orders"}},
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
	{name: "harness", desc: "the shapes of work you have saved · a space picks one to run",
		alias: []string{"harnesses"}},
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
	{name: "subharness", desc: "the programs you can run · type to filter · enter opens its card",
		alias: []string{"sub"}},
	{name: "subharness", args: "<name>", desc: "…straight to that one's card"},
	// THE SKILLS THIS CONVERSATION CAN BE HANDED (skillpick.go). It sits
	// directly under the subharness rows because it is the neighbouring
	// question — those are the programs this conversation can run, and this is
	// what it can be told to know — and on the picker's own terms: a space
	// after it opens the shelf, enter on a row toggles that skill on or off,
	// and a query that looks like a path offers the skill in that folder.
	{name: "skill", desc: "what you can hand this conversation · a space picks more than one",
		alias: []string{"skills"}},
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
	{name: "memory", desc: "inspect and change what is remembered"},
	{name: "memory", args: "<query>", desc: "print only the memories matching a word"},
	{name: "memories", desc: "print what is remembered about you"},
	{name: "memories", args: "<query>", desc: "…only the ones matching a word"},
	{name: "remember", args: "<text>", desc: "keep one thing across conversations"},
	{name: "forget", args: "<query>", desc: "drop what is remembered about something"},
	// AND WHAT codeaf WORKS WITH, beside what it knows about you: the crew a
	// task runs on — worker, planner, checker — picked per task (crew.go). The
	// bare form is the panel; the four shortcuts are how anything on it
	// changes, one row each, because [app.runMenu] puts a row that TAKES
	// something into the draft instead of running it, and each of these takes
	// something.
	//
	// It sits here, under the memory rows, because those three are "what does it
	// know" and this is "what does it think WITH".
	{name: "crew", desc: "the crew tasks run on · picked per task, with your pins, allowed models and daily cap"},
	{name: "crew", args: "pin <seat> <model[@provider]>", desc: "…pin the worker, planner or checker · /model stays"},
	{name: "crew", args: "unpin <seat|all>", desc: "…put a seat back on auto"},
	{name: "crew", args: "models <rule>", desc: "…which models a seat may be picked from · all, open, ≤in/out, ids"},
	{name: "crew", args: "cap <dollars|off>", desc: "…the most tasks' crews may spend in a day"},
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
	{name: "effort", desc: "how hard this conversation thinks · the five rungs, and what each buys",
		alias: []string{"think", "thinking"}},
	{name: "effort", args: "<rung>", desc: "…set it outright · " + effortKey + " walks it, or press it on the seam"},
	{name: "task", args: "<brief>", desc: "start work you can walk away from", door: sendDoorTask},
	{name: "task", args: "solo <brief>", desc: "…with one worker, and no sizing call before it", door: sendDoorTask},
	// HOW HARD TO TRY THIS ONE TASK is said in the ask and sticks to nothing
	// (crew.go): --best puts the strongest crew the allowed models make on it,
	// --cheap the cheapest, and neither moves the next task.
	{name: "task", args: "--best <brief>", desc: "…on the strongest crew allowed, this task only", door: sendDoorTask},
	{name: "task", args: "--cheap <brief>", desc: "…on the cheapest crew allowed, this task only", door: sendDoorTask},
	{name: "redo", args: "stronger", desc: "the last task again, on a stronger crew"},
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
	{name: "history", desc: "every task this project has run · ctrl+."},
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
	{name: "status", desc: "everything the status line knows, one fact per line", alias: []string{"info", "context"}},
	{name: "status", args: "--json", desc: "…everything the status line knows, as one JSON object"},
	// THE ROW NAMES WHOSE BILL IT IS, because the other one is now a command of
	// its own two rows up: /cost is THIS CONVERSATION and /spend is the machine.
	// The word `spend` used to be an alias here and pointed the one guess a
	// person makes at the wrong reading.
	{name: "cost", desc: "what this conversation has spent · /spend is the whole machine", alias: []string{"usage", "tokens"}},
	// AND DIRECTLY UNDER WHAT IT HAS SPENT, WHAT IT MAY. /cost is the reading and
	// this is the editor, and they sit together because a person who has just
	// read a figure is the person deciding whether it is too high (budget.go).
	// Three rows for one command, /export's reason exactly: bare is the shape
	// nearly everybody wants, and the two that WRITE ride under it wearing the
	// "…".
	{name: "budget", desc: "what codeaf may spend · every limit on one tab", alias: []string{"limits"}},
	{name: "budget", args: "<amount>", desc: "…set the day's limit · none removes it"},
	{name: "budget", args: "<row> <amount>", desc: "…set one by name: day, conversation, plan, practice"},
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
	{name: "cache", desc: "the shared build cache — how big, and where"},
	{name: "cache", args: "clean", desc: "…delete it to free disk · asks before anything is removed"},
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
	{name: "copy", desc: "read the conversation back and copy from it · ctrl+b"},
	{name: "select", desc: "drag to select with your mouse · ctrl+s"},
	// TWO ROWS FOR ONE COMMAND, the way /model has two. A single row carrying
	// <path> would make the bare form — which is the one nearly everybody wants —
	// unreachable from the list: [app.runMenu] writes a row that TAKES something
	// into the draft instead of running it, so choosing it would put "/export "
	// in the box and wait for a path nobody had in mind.
	{name: "export", desc: "write this conversation to a file", alias: []string{"save"}},
	{name: "export", args: "<path>", desc: "…and write it there · tab completes the path"},
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
	{name: "files", desc: "what has been made for you · open, reveal or copy one"},
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
	{name: "files", args: "<path>", desc: "…or bring one back from the machine over there and open it"},
	// AND THE ONE THAT GOES THE OTHER WAY: those four take something out of this
	// conversation and this one puts something INTO it — a log, a CSV, a PDF, on
	// the same tray a picture rides and read rather than looked at (attach.go).
	//
	// ITS PLACE IN THIS LIST KEEPS /compact VISIBLE. [menuRows] shows eight
	// rows at once, so position is a claim about frequency. /attach stands with
	// the doors onto moving a file, an errand it shares with /files; putting it
	// higher would push /compact, which people reach for daily, into a scroll.
	// standingpage_test.go pins that ordering.
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
	{name: "attach", desc: "choose a file to attach · the browser opens", alias: []string{"upload"}},
	{name: "attach", args: "<path>", desc: "…attach that file · tab completes the path"},
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
	{name: "manual", desc: "asks the model what codeaf can do, from its own manual"},
	{name: "manual", args: "<question>", desc: "…puts that question to the model, answered from the manual"},
	// AND THE ROW FOR THE DAY SOMETHING GOES WRONG, directly above /help for the
	// reason /manual sits there: it is the third thing a person reaches for when
	// they are stuck, after the list of commands and the page that explains one.
	//
	// It is this far down the table because [menuRows] shows eight rows at once
	// and position here is a claim about frequency — nobody turns the record on
	// twice in a day, and a row inserted higher would push a daily command
	// behind a scroll.
	{name: "debug", desc: "keep the full record of this conversation · says where it goes"},
	{name: "update", desc: "install the newest codeaf and restart on it", alias: []string{"upgrade"}},
	{name: "update", args: "<channel or tag>", desc: "…that channel or exact tag, then restart"},
	{name: "help", desc: "this list", alias: []string{"?"}},
	{name: "quit", desc: "close this conversation", alias: []string{"exit", "q"}},
}

// checkCommands is THE TABLE CHECK, run at init over [commands] and by the tests
// over tables of their own.
//
// AN ALIAS MAY NEVER SHADOW A CANONICAL NAME OR ANOTHER ALIAS. A word that meant
// two things would resolve to whichever row the loop reached first, which makes
// the answer to "what does /clear do" a fact about the table's ORDER — and this
// table is ordered for reading, so it would be an accident waiting on the next
// person who moves a row. A canonical name may repeat, because /model and
// /model <slug> are two forms of one command rather than two commands.
func checkCommands(list []command) error {
	names := make(map[string]bool, len(list))
	for _, c := range list {
		names[c.name] = true
	}
	owner := make(map[string]string, len(list))
	for _, c := range list {
		for _, word := range c.alias {
			switch {
			case word == "":
				return fmt.Errorf("/%s has an empty alias", c.name)
			case names[word]:
				return fmt.Errorf("/%s is an alias of /%s and a command in its own right", word, c.name)
			case owner[word] != "":
				return fmt.Errorf("/%s is an alias of both /%s and /%s", word, owner[word], c.name)
			}
			owner[word] = c.name
		}
	}
	return nil
}

// It fails at startup and not at the first keystroke: a broken table is a
// programming mistake in this file, and the loudest place to say so is before
// anything has been drawn.
func init() {
	if err := checkCommands(commands); err != nil {
		panic("tui3: the command table is broken: " + err.Error())
	}
}

// aliasNote is the dim tail that names the other words for this command, or ""
// when there are none: "also /clear /clean /reset".
func (c command) aliasNote() string {
	if len(c.alias) == 0 {
		return ""
	}
	words := make([]string, 0, len(c.alias))
	for _, word := range c.alias {
		words = append(words, "/"+word)
	}
	return "also " + strings.Join(words, " ")
}

// note is the whole right-hand side of a row: what the command does, and then
// what else it answers to. It is ONE function because the list and /help both
// draw it, and because the height that reserves the rows and the fill that draws
// them have to be counting the same string (see [menu.height]).
//
// AND IT IS WHERE A ROW'S CHORD IS SPELLED FOR THIS KEYBOARD. Two of these
// descriptions carry a place's own chord ([placeChord]), baked in at init where
// no terminal has been detected yet — so on a Mac the list said `/spend … alt+3`
// while the map two keystrokes away said `opt+1…opt+8`. The substitution has to
// happen HERE rather than at either paint, because `⌘` is one cell where `cmd+`
// is four and [menu.fit] counts the lines this string will take before
// [menu.rows] draws it: measuring one spelling and drawing the other is a list
// that pushes the status line off the frame.
func (c command) note(chords chordSpelling) string {
	if tail := c.aliasNote(); tail != "" {
		return chords.say(c.desc + " · " + tail)
	}
	return chords.say(c.desc)
}

// menuNote is [command.note] cut to what is left of a row after the command's
// own name — and it is THE ONE PLACE ON THIS SURFACE WHERE THE TAIL GIVES WAY
// FIRST rather than the label. Everywhere else a truncated label is still
// recognizable and the tail carries the numbers being compared ([overlayRow]),
// but a command name is a thing a person has to type back EXACTLY: "/set…" is
// not a command, while a sentence about it that stops early still reads. The
// alias tail is what pushed this over — "· also /clear /clean /reset" is longer
// than most of these rows' widths to spare — and without the cut a narrow frame
// drew a row wider than the frame.
//
// At [tierPhone] nothing is cut here: the tail has a line of its own there and
// fits itself to it (see [overlayLines]).
func (c command) menuNote(width int, chords chordSpelling) string {
	note := c.note(chords)
	if phoneList(width) {
		return note
	}
	return fit(note, width-2-len(c.typed())-1)
}

// canonicalCommand is the name a typed word RUNS as: an alias resolves to the
// row that owns it, and everything else — including a word nobody defined — is
// returned folded to lower case for the dispatch to answer as it always has.
//
// The aliases are searched and the names are not, which is only safe because
// [checkCommands] has already proved no alias can shadow a name.
func canonicalCommand(word string) string {
	word = strings.ToLower(word)
	for _, c := range commands {
		for _, other := range c.alias {
			if other == word {
				return c.name
			}
		}
	}
	return word
}

// aliasRung is the wall between a name match and an alias match, far above any
// offset a name a dozen characters long can reach. A row found by its own name
// always outranks a row found by a word it merely also answers to, so typing
// "res" selects /resume rather than the /new that carries "reset".
const aliasRung = 1_000

// bareFor says whether this row is the argless form of exactly the word that
// was typed — by its name or by any alias. It is [menu.rank]'s tiebreak: a
// finished word is a command a person is about to run, and the row that runs
// it is the one that takes nothing more.
func (c command) bareFor(needle string) bool {
	if c.args != "" {
		return false
	}
	if c.name == needle {
		return true
	}
	for _, al := range c.alias {
		if al == needle {
			return true
		}
	}
	return false
}

// matchAt is where needle was found in this command's words and whether it was
// found at all — the name first, then the aliases a rung below it. Lower is
// better: this is a plain offset, the one ranking the command list has always
// kept, and not the fuzzy matcher the pickers rank with (internal/fuzzy).
func (c command) matchAt(needle string) (int, bool) {
	if needle == "" {
		return 0, true
	}
	if at := strings.Index(c.name, needle); at >= 0 {
		return at, true
	}
	best, found := 0, false
	for _, word := range c.alias {
		at := strings.Index(word, needle)
		if at < 0 || (found && at >= best) {
			continue
		}
		best, found = at, true
	}
	if !found {
		return 0, false
	}
	return aliasRung + best, true
}

// typed is the command as it is written: "/model <slug>".
func (c command) typed() string {
	if c.args == "" {
		return "/" + c.name
	}
	return "/" + c.name + " " + c.args
}

// menuRows is the preferred minimum viewport, further limited by the actual
// terminal space. Taller frames use the available room above the seam. Hidden
// commands remain reachable by scrolling, and the conversation list counts
// the rows remaining below its viewport.
const menuRows = 8

const commandNoMatchWord = "no commands match"

// menu is the command list's whole state. The zero value is closed.
type menu struct {
	open bool
	// at is the rune index of the '/' this list is filtering under. It used to
	// be implicit — the list only ever opened on a draft whose first character
	// was a slash, so the answer was always zero — and it is written down now
	// that a slash anywhere in a sentence opens it (see [menu.sync]).
	at    int
	query string
	// hits are indexes into commands, in alphabetical order.
	hits   []int
	score  []int
	cursor int
	top    int
	// sealed says a token has been ANSWERED and the list must stay out of it,
	// and sealAt is which one — the rune index its '/' sits at. Two gestures set
	// it, and they are the same gesture from the list's side: esc, which is a
	// person saying they meant the word rather than the list, and a row chosen
	// mid-sentence, whose answer the list would otherwise reopen on top of.
	//
	// It is the completion's `done` under another name (files.go) and it is kept
	// as a POSITION rather than as text because a command is a word a person
	// keeps typing after: "/task" dismissed and then continued into "/tasks of
	// the day" is still the same token, and the list may not come back for it.
	sealed bool
	sealAt int
}

// sync follows the slash token under the caret after edits and caret movement.
// The entire token must be a command word, so moving within a path cannot open
// the list. Unknown command words keep an empty list rather than revealing
// unrelated search results on home. A space or path punctuation leaves command
// mode, and Esc seals a literal token until the caret leaves it.
func (m *menu) sync(e *editor) {
	at, query, ok := slashToken(e.value, e.cursor)
	if ok {
		// Inspect the whole token, including text after the caret, so moving
		// within an absolute path cannot reopen the command list.
		for _, r := range e.value[at+1 : tokenEnd(e.value, at)] {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '?') {
				ok = false
				break
			}
		}
	}
	if !ok {
		m.close()
		return
	}
	if m.sealed && m.sealAt == at {
		// Answered already. The list stays down without forgetting why, so that
		// the next keystroke inside this same word does not reopen it.
		m.open = false
		return
	}
	was := m.open && m.at == at && m.query == query
	m.open, m.at, m.query = true, at, query
	m.rank(strings.ToLower(query))
	if !was {
		m.cursor, m.top = m.best(), 0
	}
}

// close is the full reset, the seal included: the draft this list was filtering
// has been sent, or an overlay took the box, and there is no word left to stay
// out of.
func (m *menu) close() { *m = menu{} }

// dismiss is close plus the memory of which token was dismissed. See [menu.sync]
// for what the seal buys and [app.dismissLists] for who presses it.
func (m *menu) dismiss(at int) {
	m.close()
	m.sealed, m.sealAt = true, at
}

// rank filters names and aliases by substring, then orders canonical rows
// alphabetically. Scores choose the initial selection without rearranging the
// list. Argument variants keep their table order beside their command.
func (m *menu) rank(needle string) {
	if cap(m.score) < len(commands) {
		m.score = make([]int, len(commands))
	}
	m.hits = m.hits[:0]
	for i, c := range commands {
		at, ok := c.matchAt(needle)
		if !ok {
			continue
		}
		m.score[i] = at
		m.hits = append(m.hits, i)
	}
	sort.SliceStable(m.hits, func(a, b int) bool {
		left, right := commands[m.hits[a]], commands[m.hits[b]]
		if left.name != right.name {
			return left.name < right.name
		}
		// A fully typed command still chooses its bare form. Argument variants
		// otherwise retain their order within that command's alphabetic group.
		return left.bareFor(needle) && !right.bareFor(needle)
	})
	m.cursor = moveCursor(m.cursor, 0, len(m.hits))
	m.follow(menuRows)
}

// best selects a name or prefix match without moving it out of alphabetic order.
// An alias remains discoverable without stealing Enter from a matching name.
func (m *menu) best() int {
	best := 0
	for i, hit := range m.hits {
		if m.score[hit] < m.score[m.hits[best]] {
			best = i
		}
	}
	return best
}

func (m *menu) move(delta int) {
	m.cursor = moveCursor(m.cursor, delta, len(m.hits))
	m.follow(menuRows)
}

func (m *menu) follow(height int) { m.top = listTop(m.cursor, m.top, len(m.hits), height) }

// choice is the command under the cursor, and false when the filter matched
// nothing — enter on an empty list is the typed line's, not the list's.
func (m *menu) choice() (command, bool) {
	if !m.open || m.cursor < 0 || m.cursor >= len(m.hits) {
		return command{}, false
	}
	return commands[m.hits[m.cursor]], true
}

// height is how many rows the list wants. A filter that matches nothing wants
// NONE: the draft under it is a perfectly good "/nonsense" that enter will
// answer, and an overlay saying "no match" over a line that is about to get a
// better answer is two answers to one question.
// THE ROOM IS HANDED IN AS A NUMBER and the list stays pure. What is above this
// line ranks and filters commands and has never known how tall a terminal is;
// what the frame knows is how many rows are left once the status line, the box
// and a row of conversation have taken theirs ([app.overlayHeight] does that
// arithmetic once, for every list). So the frame passes the figure and this
// decides what to do with it, rather than either of them guessing at the other.
func (m *menu) height(width, room int, chords chordSpelling) int {
	if !m.open {
		return 0
	}
	if len(m.hits) == 0 {
		return 1
	}
	// The ceiling is in LINES, so at [tierPhone] the list holds four commands
	// with what they do written under them instead of eight rows that all say
	// "/settings   open the settings pa…" (palette.go).
	ceiling := menuRows
	if room > ceiling {
		ceiling = room
	}
	shown, lines := m.fit(width, ceiling, chords)
	if m.top+shown >= len(m.hits) || ceiling <= 1 {
		return lines
	}
	// SOMETHING IS HIDDEN, SO ONE LINE OF THE CEILING IS THE FOLD'S. It is
	// counted here rather than added on top, because the frame subtracts this
	// figure from the conversation before the rows are drawn: a list that came
	// back a line longer than it promised would push the status line off.
	_, lines = m.fit(width, ceiling-1, chords)
	return lines + 1
}

// fit is how many ROWS and how many LINES this list draws from [menu.top] inside
// a ceiling of screen lines. Two numbers rather than one because the fold has to
// know how many commands were left over, and at [tierPhone] a row is two lines —
// so a count of lines cannot answer that on its own.
func (m *menu) fit(width, ceiling int, chords chordSpelling) (rows, lines int) {
	for at := m.top; at < len(m.hits) && lines < ceiling; at++ {
		take := overlayItemLines(width, commands[m.hits[at]].note(chords))
		if lines+take > ceiling {
			break
		}
		lines += take
		rows++
	}
	return rows, lines
}

func (m *menu) rows(width, n int, pal palette, hover int, chords chordSpelling) []string {
	if n <= 0 {
		return nil
	}
	if len(m.hits) == 0 {
		return []string{pal.dim(fit("  "+commandNoMatchWord, width))}
	}
	m.follow(overlayItems(n, width))
	// The fold's line is taken off the room BEFORE the rows are laid into it, so
	// what is said about the remainder is true of the rows actually drawn. A fold
	// appended after the fact would be counting a row that is on the screen.
	room := n
	if shown, _ := m.fit(width, n, chords); m.top+shown < len(m.hits) && n > 1 {
		room = n - 1
		m.follow(overlayItems(room, width))
	}
	fill := newOverlayFill(width, room, pal, hover)
	past := m.top
	for at := m.top; at < len(m.hits) && fill.room(); at++ {
		c := commands[m.hits[at]]
		if !fill.add(at, c.typed(), c.menuNote(width, chords), at == m.cursor, false) {
			break
		}
		past = at + 1
	}
	lines, _ := fill.done()
	if hidden := len(m.hits) - past; hidden > 0 && room < n {
		// The two cells in front of it are [overlayLead]'s own, so the count hangs
		// under the commands rather than out in the margin beside them.
		lines = append(lines, pal.dim(fit("  "+foldLine(hidden, ""), width)))
	}
	return lines
}

// runMenu is enter while the command list is up: the row under the cursor wins.
//
// WHAT IT DOES WITH THE ROW DEPENDS ON WHERE THE TOKEN IS, and the rule is the
// submit rule read backwards. [app.enter] sends a draft to [app.slash] when its
// FIRST character is a slash and never otherwise, so:
//
//   - The token opens the draft and is all of it. This is a command being
//     chosen, and it behaves exactly as it always has — the row runs, or, if it
//     TAKES something, "/model " goes into the box with the caret after it,
//     because "/model <slug>" with no slug is not a command anybody meant.
//   - Anything else — a slash word inside a sentence, or a command whose
//     argument is already typed — is a MENTION. The token is replaced with the
//     command's word, the caret parks after it, and NOTHING RUNS. A list that
//     ran a command from the middle of a sentence would be running something
//     the same line submitted by hand would not.
//
// Either way the list stops offering: a mention seals its own token, so the
// answer this just wrote does not have the list reopen on top of it.
func (a *app) runMenu() tea.Cmd {
	chosen, ok := a.menu.choice()
	if !ok {
		return nil
	}
	word, ran := chooseCommand(&a.input, &a.menu, chosen)
	if !ran {
		return a.edited()
	}
	// It goes into the recall list exactly as if it had been typed out and
	// entered, because from the person's side it was: the list is a shortcut
	// for typing, not a second door with different rules (see [app.enter]).
	//
	// THIS HALF IS THE CHAT BOX'S OWN and is not in [chooseCommand] with the
	// rest. Home's composer runs the same rows through the same helper and has
	// no recall list and no draft to drop — ↑ there walks the drop-up rather
	// than a history — so the two lines below would be two no-ops and a lie
	// about what that surface keeps (homeslash.go's [app.homeRunCommand]).
	a.remember(word)
	a.dropDraft()
	return a.slash(word)
}

// chooseCommand is the mechanics of [app.runMenu] over WHATEVER box and
// WHATEVER list are handed to it: the token is rewritten with the chosen
// command's word, the list is put away, and the answer says whether what is
// left is a command to run. It is one function because it is one gesture — home
// offers the same rows over its own box (homeslash.go), and a second copy of
// this surgery is a second answer to what choosing a row does.
//
// It returns the word that was written and whether the caller should RUN it.
// False is a token rewritten inside a sentence, or a command left in the box
// waiting for the words it takes; true is a box that now holds the command and
// nothing else, and has been emptied ready for the dispatch. What happens after
// the dispatch is the caller's, because the two surfaces keep different things.
func chooseCommand(e *editor, m *menu, chosen command) (string, bool) {
	// The token's start is CLAMPED to the draft as it stands. The lists follow
	// edits and not caret moves, so there are gestures — a history recall, a
	// draft restored under an open list — that can leave this index pointing
	// past the end of a draft that has since got shorter, and an index into a
	// slice is not a thing to be optimistic about.
	at := min(max(m.at, 0), len(e.value))
	end := tokenEnd(e.value, at)
	// THE NAME AND NEVER [command.typed]. The row on the list reads
	// "/model <slug>" because that is what the command wants said to it, but the
	// placeholder is a thing to READ and never a thing to leave in somebody's
	// box — a draft holding a literal "<slug>" is a command nobody meant and one
	// the dispatcher would refuse.
	word := "/" + chosen.name
	if at != 0 || strings.TrimSpace(string(e.value[end:])) != "" {
		head := append([]rune(nil), e.value[:at]...)
		tail := append([]rune(nil), e.value[end:]...)
		e.value = append(append(head, []rune(word)...), tail...)
		e.cursor = at + len([]rune(word))
		m.dismiss(at)
		return word, false
	}
	m.close()
	if chosen.args != "" {
		e.setText(word + " ")
		return word, false
	}
	e.reset()
	return word, true
}

// helpText renders the same table the list draws, plus the two keys that have
// no slash and the session file. One source, two renderings.
// helpKeyRow is one hand-written row of the key sheet: the chord, then the
// sentence at [helpKeyColumn]. A chord wider than the column keeps one space, so
// a Mac's spelling can never run into the words beside it.
func helpKeyRow(key, note string) string {
	gap := helpKeyColumn - ansi.StringWidth(key)
	if gap < 1 {
		gap = 1
	}
	return key + strings.Repeat(" ", gap) + note
}

// helpKeyColumn is where the sentence starts on every hand-written row of the
// key sheet below — five for `@path` plus its ten spaces, six for `ctrl+c` plus
// its nine. It is named because ONE of those rows is spelled for the terminal
// rather than typed out (chords.go), and a padded literal cannot be padded twice.
//
// IT IS NOT THE COMMAND ROWS' COLUMN, which is measured off the longest command
// on the list and sits further right. The two blocks have always been two
// columns; this constant states the second one rather than changing it.
const helpKeyColumn = 15

func helpText(file string, chords chordSpelling) string {
	width := 0
	for _, c := range commands {
		if n := len(c.typed()); n > width {
			width = n
		}
	}
	lines := make([]string, 0, len(commands)+6)
	// The product names itself once, at the top of the one place it explains
	// itself. Everywhere else on this surface it is simply the thing you are
	// already in (styles.go's [product]).
	lines = append(lines, product, "")
	for _, c := range commands {
		// The aliases are printed here as they are on the row, and nothing is cut:
		// /help is prose in the transcript, which wraps, rather than a list drawn
		// into a fixed block (see [command.menuNote]).
		lines = append(lines, c.typed()+strings.Repeat(" ", width-len(c.typed())+2)+c.note(chords))
	}
	lines = append(lines,
		// THE KEY THAT GETS A PERSON HERE IS THE FIRST KEY ON THE SHEET. `?` over
		// an empty box is what opened this list for anybody who did not already
		// know six characters of it, and a sheet that did not name it would be a
		// door with no sign on it (the block at the foot of this file argues the
		// binding).
		helpKeyRow(helpAskKey, helpAskWord),
		"@path          complete a file · a picture attaches",
		// THE DOOR IS NAMED HERE BECAUSE ONE KEY CARRIES TWO MEANINGS
		// (leaving.go): at rest it leaves, mid-turn it stops the model, and a
		// person whose ctrl+c "only interrupted" looks here before anywhere else.
		"ctrl+c         quits everything · mid-turn it interrupts instead, like esc",
		// tab is the seventeenth rung of the key router (input.go) and does
		// nothing at all when this terminal holds one conversation — which is
		// why the line says what it needs rather than promising it always works.
		"tab            go back to the last conversation, with an empty box",
		// ── THE SEVEN PLACES, WHICH THIS SHEET USED TO NAME NO WAY INTO ───────
		//
		// This block lists every chord a person can press, and until this wave it
		// held not one of `alt+1`…`alt+7`, `alt+.` or `tab`'s meaning on a place —
		// so somebody who typed /help from a cold start finished it without
		// learning that the places exist. The map (`alt+.`) is the surface's own
		// chord list and was reachable only from inside a place you already had to
		// know how to open, which is a help sheet behind the thing it explains.
		//
		// The three rows are spelled through [chordSpelling.say] like the
		// `alt+enter` row above them, so a Mac reads `opt+1…opt+8` and a Linux box
		// reads what is authored here — one substitution, one door (chords.go).
		helpKeyRow(chords.say(chordJumpWords), "go to a place · in the tab bar's own order: "+placeWordList()),
		helpKeyRow(chords.say(placeMapKey), "on a place: what else is here · every key that place has, drawn"),
		"               on a place, tab is the next place · esc back",
		// THE CHORD IS SPELLED FOR THIS TERMINAL AND THEN PADDED, in that order,
		// because [helpKeyRow] pads to a fixed column and a literal padded to one
		// spelling would put this row's sentence out of the column every other row
		// on the sheet sits in. `alt+` and `opt+` are the same four cells, so this
		// row is safe either way today; `cmd+` against `⌘` is not, and the order
		// is the sheet's rule rather than this row's luck (chords.go).
		helpKeyRow(chords.say("alt+enter"), "open a line · enter sends"),
		// THE MARKED SEND (standmark.go). It is on this sheet because it is the
		// one key here that changes what a sentence MEANS rather than where it
		// goes, and nothing else on the screen names it until a draft happens to
		// look like a rule.
		standMarkKey+"     keep this true · a standing order's card, never work done once",
		// SPELL IT OUT (spellout.go), on this sheet for the line above's reason:
		// it is about the sentence in the box rather than about the screen, and
		// nothing else names it until a draft happens to look like something to
		// build.
		// AND THE SCOPE IS ON THE ROW, because this chord is on this sheet TWICE.
		// `ctrl+r` is the spell-it-out chord over a draft and the reveal key inside
		// /files (deliverables.go's [filesRevealKey]), thirty-six rows apart, and
		// neither row said the other existed — so the sheet a person opens to learn
		// the keys contradicted itself and gave no way to tell which reading was
		// theirs. The two do not collide in the code, and now they do not collide
		// on the page either: each says where it acts, in the grammar the scoped
		// rows at the foot of this list already use.
		helpKeyRow(spellOutKey, "over a draft: spell it out · what it means · enter adds it to yours"),
		"ctrl+o         expand this turn's tool calls · click one to open it · in a task, scroll up does too",
		"ctrl+b         copy mode · ↑↓ move · v marks · a takes the block · y yanks",
		"ctrl+s         drag to select with your mouse · any key ends it",
		"enter          mid-answer: stops the current reply and steers these words in",
		// AND THE THIRD THING TO DO WITH A SENTENCE TYPED OVER A RUNNING ANSWER
		// (steer.go). It sits directly under the line about the other two because
		// the three are one decision — wait, go in, or stop the answer — and the
		// one that waits is now the secondary choice a person may not guess.
		// AND THIS ROW IS SPELLED FOR THE TERMINAL READING IT, like the `alt+`
		// rows above and below. It used to be a literal `cmd+enter` on every
		// platform, so the sheet on a Linux box named a modifier that keyboard does
		// not have — while the keystroke itself arrives there as `super+enter`
		// (steer.go binds both names). The substitution is chords.go's one door.
		helpKeyRow(chords.say(parkKey), "mid-answer: waits above the box · → sends a waiting one"),
		"ctrl+q         hand this to the session now, to run after the current turn",
		// THE ROW READS IN THE ROUTER'S ORDER. The empty-box key asks the running
		// turn's window first ([app.toggleLatestWorkfold]) and falls through to
		// thinking ([app.toggleLatestThought]); the row was rewritten when the key was.
		"ctrl+e         open the running turn's compact steps · the newest worked chip · or the thinking",
		// THE NEW TAB AND THE ROSTER, IN THAT ORDER AND ON TWO ROWS. They used to be
		// one key: ctrl+t handed the roster the keyboard, and the tab strip's `+` had
		// no chord at all. The strip is drawn as tabs, so the key every browser opens
		// a tab with is the one people press at it — and the roster keeps the letter
		// under the other modifier (task.go's [railHoldChord]), which is the smallest
		// move a hand has to make and the modifier its own widen chord already uses.
		helpKeyRow(newChatChord, "a new chat start page · your draft stays put · esc back"),
		// AND THE OTHER DIRECTION, ON THE ROW UNDER IT. The two chords are one
		// gesture, so they are read together here as they are in input.go, and the
		// row says what the key does NOT do — because "close" is the word people
		// fear on a conversation that has an hour of work in it.
		// AND THE ROW MAY NOT SPELL THE SWITCHER'S OWN CHORD (escword_test.go finds
		// the switcher's row by that prefix, and a second row carrying it is a second
		// answer to the question that test asks). The card is named by what it is
		// instead.
		helpKeyRow(closeTabChord, "close this tab · select the last open chat · keep your draft"),
		// THE TEAM'S TWO CHORDS (teamrail.go). They do something only in a team
		// with a manager, and the rows say so rather than leaving a person to
		// find out by pressing them anywhere else.
		helpKeyRow(chords.say(trafficKey), "with a team's manager in front: show or hide its Traffic"),
		helpKeyRow(chords.say(teamManagerKey), "in a team with a manager: go to the manager"),
		helpKeyRow(reopenTabChord, "reopen the last closed tab · when the terminal sends this distinct chord"),
		helpKeyRow(chords.say(railHoldChord), "the task roster · ↑↓ move · ←→ tasks/traffic · enter opens · esc back"),
		"ctrl+.         every task this project has run · /history · type to filter",
		"ctrl+g         close the roster's column, or bring it back · remembered",
		"ctrl+l         back to the latest · the chip above the box says so too",
		// THE SWITCHER (hop.go). It is named here on every terminal because the
		// binding reaches every terminal; the `ctrl+tab` alias is not on this
		// list, for the reason the manual states — a line that named it would be
		// naming a chord half the terminals reading this cannot send.
		// The line is TRUE IN BOTH MODES of ui.quick_switch on purpose: this list
		// has no reach into the profile, and a clause that named one mode would be
		// wrong in the other. The card's own head and the manual say the rest.
		// IT IS BUILT RATHER THAN TYPED OUT, because the chord wears a modifier
		// with two keycaps and the padding has to be measured after the spelling
		// is chosen ([helpKeyColumn] says why a padded literal cannot be padded
		// twice).
		helpKeyRow(chords.say(hopOpenKey), "choose a conversation · enter open · esc cancel"),
		"               → reaches every other one on this machine · ctrl+w closes one",
		"→ ←            over an empty box: into a running task, and back out",
		// THE WORD "home" USED TO BE HERE AND IS NOW SPENT. This gesture leaves a
		// task room for the conversation; /home is a screen of every project on
		// the machine, and one word meaning two places on the same list is a
		// person pressing ← ← to find out where they end up.
		"← ←            out of a task room · the conversation, at the live edge",
		"space space    over an empty box: home · /home · esc back",
		// THE WORD KILL IS NAMED BY THE KEYS THAT STILL REACH THE BOX. ctrl+w was
		// on this row until it became the close-tab chord above, and a sheet that
		// went on offering it would be teaching a keystroke that shuts the window
		// you are typing in.
		helpKeyRow(chords.say("alt+backspace"), "delete the word behind the caret · ctrl+u the line · ctrl+k the rest of it"),
		"ctrl+,         open settings",
		"d              in /permissions: drop the line under the cursor · press it twice",
		"p s n          in /standing: pause one · stop it · keep it out of here",
		// THE TEAMS PAGE'S LETTERS, each the button of the same word on the
		// selected team, and the chord that puts the keyboard on those buttons
		// while the manager's conversation has the box (teamspagehost.go).
		"s c w n o      in /teams: settings · close · open on the wall · new team · organize",
		helpKeyRow(chords.say("alt+↑↓"), "in /teams: onto the page's buttons while the manager has the box · esc back"),
		"ctrl+r ctrl+y  in /files: reveal the folder it is in · copy it somewhere",
	)
	if file != "" {
		lines = append(lines, "session · "+file)
	}
	return strings.Join(lines, "\n")
}

// ── WHAT THE WORDS AFTER /model MEAN ────────────────────────────────────────
//
// /model has always taken one thing — a slug — and switched to it. It now takes
// three, and they are told apart by SHAPE rather than by a flag, because a
// person types what they want and not what kind of thing it is:
//
//	/model deepseek/deepseek-v4-flash    a name: switch, exactly as before
//	/model @cloudflare                   a machine: pin the lane, stay on the model
//	/model auto                          give the lane choice back
//	/model deep seek                     words: open the list with them typed in
//
// THE SLUG CASE IS UNCHANGED AND IS THE FALL-THROUGH, which is the same
// arrangement the filter box has: anything this grammar does not recognise is
// still the thing it always was.

// modelIntent is what one /model argument asks for.
type modelIntent uint8

const (
	// modelSwitch is a slug, taken as typed.
	modelSwitch modelIntent = iota
	// modelPinLane is `@name`.
	modelPinLane
	// modelAutoLane is `auto`.
	modelAutoLane
	// modelQuery is a query for the picker.
	modelQuery
)

// modelArg reads the words after /model. The second return is the lane for a
// pin and the query for a filter, and is the argument itself for a switch.
func modelArg(rest string) (modelIntent, string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return modelQuery, ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 1 {
		switch {
		case strings.EqualFold(fields[0], config.LaneAuto):
			return modelAutoLane, ""
		case strings.HasPrefix(fields[0], "@") && len(fields[0]) > 1:
			return modelPinLane, strings.TrimPrefix(fields[0], "@")
		}
	}
	// A QUERY IS ANYTHING WITH A SPACE IN IT: a slug has no spaces, so two words
	// were never a name, and the only sensible thing to do with two words is hand
	// them to the box that searches names by their pieces ([picker.rank]).
	//
	// A SINGLE WORD IS A SLUG, whatever it looks like. It used to be checked
	// against the filter box's own query language first — so `/model fast` and
	// `/model <1s` opened a narrowed list — and that language is gone: the box
	// searches names only, and a word this command took as a question would be
	// a word it refused to take as the name of a model.
	if len(fields) > 1 {
		return modelQuery, rest
	}
	return modelSwitch, rest
}

// runDebugCommand is /debug: keep the full record of THIS conversation from
// here on.
//
// IT IS ONE-WAY, and that is the whole design. A person types it because
// something has already gone wrong, and a switch that could be turned off again
// would only ever produce half a record — the half after the thing they were
// trying to catch. The other two doors mean the same thing (--debug on the
// command line, CODEAF_DEBUG in a shell), and this one exists for the case
// neither of them can serve: the conversation is already open, and the turn
// worth recording is the next one.
//
// IT TURNS THE RECORD ON FOR THIS RUN AND NO OTHER. One process can hold
// several conversations, and a process-wide flip from a command typed inside
// one of them would write another person's prompts and replies into a folder
// they never asked for. The pin and the flag are the process-wide doors,
// because those were handed to the process on purpose.
//
// It ANSWERS every way round. Turning it on says where the record goes, because
// a command that recorded something and did not say where would leave a person
// hunting a folder; typing it twice says it is already on and where; and where
// the whole process is already recording it says that instead, because "it is
// on for everything this codeaf is doing" is a different fact from "it is on
// for you", and a person reading a folder later needs to know which.
func (a *app) runDebugCommand() {
	folder := trace.Dir(trace.RunFrom(a.ctx))
	if folder == "" {
		// No run to record — a surface opened by something that did not begin
		// one. Saying so is better than turning on a record that goes nowhere.
		a.note("this conversation has no run to record.")
		return
	}
	if trace.Enabled() {
		a.note("the record is already on for every conversation this codeaf holds · this one goes to " + folder)
		return
	}
	if trace.EnabledRun(a.ctx) {
		a.note("the record is already on · it goes to " + folder)
		return
	}
	if trace.EnableRun(a.ctx) == "" {
		// The run id reached this surface on nothing but the process's own
		// fallback, so there is no run on the context to switch on by itself.
		a.note("this conversation has no run to record.")
		return
	}
	a.note("recording this conversation · it goes to " + folder)
}

// ── `?` — THE KEY A PERSON PRESSES WHEN THEY ARE LOST ───────────────────────
//
// Until this wave `?` was bound to nothing at all. It was an ALIAS of /help —
// `/?`, six characters and a slash you already had to know about — so the
// shortest honest route to the key sheet was a command a person could only find
// by opening the command list, which is itself a door the greeting names in
// four words at the bottom of the screen. Every program with a key sheet in it
// has answered this key since curses existed, and the first thing a developer
// does when a full-screen program stops making sense is press it.
//
// WHAT IT OPENS, AND WHY THAT IS TWO THINGS. `?` means one sentence — SHOW ME
// THE KEYS FOR WHERE I AM STANDING — and this surface has two answers to it
// because it has two screens:
//
//   - In a conversation it runs /help, which is the key sheet: every command,
//     every chord, in the transcript where it can be scrolled and searched.
//   - On a place it draws THE MAP (`alt+.`), which is that place's own key list
//     drawn in the cells the foot was already using ([placeMapWords], SCREEN
//     3b). Printing the sheet from a place would mean leaving the room a person
//     is standing in to answer a question about it, and the map is the answer
//     the surface already has: the chords, and — since row 14 of this wave —
//     only the ones this place really has.
//
// It is one gesture with one meaning and two renderings, exactly as [helpText]
// is one table with two ([app.slash]'s "One source, two renderings").
//
// AND THE ONE THING THAT WOULD MAKE IT WORSE THAN NOTHING: eating a `?` a
// person is typing. The guard is the offer key's guard word for word (keys.go)
// and it is structural rather than clever — THE BOX MUST BE EMPTY. A question
// mark is nearly always the LAST character of a sentence and never the first,
// so a draft with anything in it keeps the key and it types; and every overlay,
// list, card and modal on this surface is read above this rung, so a `?` typed
// into a filter box, a folder picker or a consent question never reaches here
// at all.
const helpAskKey = "?"

// helpAsk is `?` on the conversation's road: the key sheet, over an empty box.
//
// It answers false for every other key and for a box with something in it, so
// the rung it sits on in [app.key] costs one string comparison.
func (a *app) helpAsk(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != helpAskKey || !a.input.empty() {
		return nil, false
	}
	// THE COMMAND AND NOT A SECOND PRINTING OF THE SHEET. /help is what this
	// key means, so it goes through the dispatcher the typed word goes through
	// and gains whatever that command gains next.
	return a.slash("/help"), true
}

// helpAskWord is the key sheet's own row for the key, and it is the sheet's
// first key row because it is the one a lost person presses before they have
// read any of the others.
const helpAskWord = "the keys · on a place it draws that place's own map"

// ── THE WORD THIS SURFACE DOES NOT HAVE ─────────────────────────────────────

// unknownCommandWord is what a slash word nobody here recognises is answered
// with, and it is A SENTENCE ABOUT THE WORLD.
//
// It read `unknown command: /nosuchthing · try /help` — a compiler's noun and a
// colon, in a lane where every other refusal on this surface is written the way
// a person would say it: `there is no manual page named xyzzy` (manualcmd.go),
// `that conversation is not on this machine any more`. The half that mattered
// was already right — it says what to do — so what changes is the register and
// where it points.
//
// AND IT POINTS AT THE LIST RATHER THAN AT A SECOND COMMAND. `/help` is six
// characters somebody who has just mistyped a command has to type correctly;
// `/` is one keystroke, it is the door the greeting already advertises, and
// since row 8 of this wave the list it opens fills the frame and says how many
// rows it is holding back. `?` opens the sheet itself, in one key ([helpAskKey]).
func unknownCommandWord(name string) string {
	return unknownCommandLead + " /" + name + " · " + unknownCommandDoorWord
}

// unknownCommandLead is that sentence's opening, named so a test can assert the
// refusal without pasting the whole of it — and so that a test asserting a
// refusal did NOT happen cannot go on passing after the wording moves, which is
// exactly what eight of them did while this line said "unknown command".
const unknownCommandLead = "there is no command called"

// unknownCommandDoorWord is that sentence's second clause, named because the
// refusal for a DROPPED path is written from the same two halves (dropkeys.go)
// and a door spelled twice is a door that gets moved once.
const unknownCommandDoorWord = "/ lists them"
