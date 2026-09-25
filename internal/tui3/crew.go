package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// /crew — WHAT IS ALLOWED, AND WHAT IS PINNED. NOTHING ELSE STICKS.
//
// A task's crew is three seats — the WORKER that does the work, the PLANNER
// that structures it, the CHECKER that reads the result — and by default all
// three are AUTO: codeaf reads what kind of task it is and picks each seat for
// that task, at the point on the quality-for-money curve past which paying more
// stops buying much (internal/crewroute). The panel is where a person says what
// PERSISTS: which seat is pinned to which model, which models may be picked at
// all, and how much the crews may spend in a day. How hard to try ONE task is
// said in the ask — `/task --best`, `/task --cheap`, or `/redo stronger` after
// the fact — and moves nothing here.
//
// /model IS UNTOUCHED BY ALL OF IT. The model a person talks to is the
// conversation's; the crew is the models codeaf spends on its own behalf.
//
// THE BARE FORM IS THE PANEL (crewpanel.go): the five rows a person changes
// — three seats, the allowed models, the daily cap — edited where they stand.
// The four shortcuts write the same rows through the same writers and then
// open the panel with the tick on the row they changed, so a command and the
// panel cannot disagree about which crew is on:
//
//	/crew pin <seat> <model[@provider]>   /crew unpin <seat|all>
//	/crew models <all|open|≤in/out|list|+model|-model>
//	/crew cap <dollars|off>   /crew cap task <dollars>

// crewUsage is the one line every refused /crew form answers with.
const crewUsage = "/crew · /crew pin <worker|planner|checker> <model[@provider]> · /crew unpin <seat|all> · " +
	"/crew models <all|open|≤in/out|ids…|+id|-id> · /crew cap <dollars|off> · /crew cap task <dollars>"

// runCrew is /crew: the panel, or one of its four shortcuts. It answers the
// tick that takes the panel's undo offer down after a shortcut opened it.
func (a *app) runCrew(arg string) tea.Cmd {
	if a.hosted() {
		a.note(a.remoteProfileWord("the crew"))
		return nil
	}
	arg = strings.TrimSpace(arg)
	word, rest, _ := strings.Cut(arg, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(word) {
	case "":
		a.openCrew()
		return nil
	}
	// EVERY SHORTCUT KEEPS THE ROWS AS THEY STOOD, so the panel it opens can
	// offer the same undo a change made on the panel offers.
	before := config.CrewStateAt(a.profileDir)
	stop := -1
	switch strings.ToLower(word) {
	case "pin":
		stop = a.crewPin(rest)
	case "unpin":
		stop = a.crewUnpin(rest)
	case "models":
		stop = a.crewAllowedModels(rest)
	case "cap":
		stop = a.crewCap(rest)
	default:
		a.note("/crew " + arg + " · not a crew form · " + crewUsage)
	}
	if stop < 0 {
		return nil
	}
	return a.crewNow(stop, before)
}

// crewPin is `/crew pin <seat> <model[@provider]>`. A pin outside the allowed
// models is REFUSED rather than written, because a pin the router would have
// to break is not a pin ([config.SetCrewPin] says the rule once).
//
// Each shortcut answers the panel row it changed, or -1 when it changed
// nothing — a refusal, or a question it answered with a note.
func (a *app) crewPin(rest string) int {
	seatWord, model, _ := strings.Cut(rest, " ")
	seat, ok := config.ParseCrewSeat(seatWord)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		a.note("usage: /crew pin <worker|planner|checker> <model[@provider]>")
		return -1
	}
	if err := config.SetCrewPin(a.profileDir, seat, model); err != nil {
		a.note("could not pin the " + string(seat) + " · " + err.Error())
		return -1
	}
	a.crewApplied()
	pin, _ := config.CrewPinAt(a.profileDir, seat)
	a.noteFacts(string(seat)+" "+a.crewMark(tokens.GPinned)+" "+pin.String()+" · every task until you unpin it · "+
		a.crewUnchangedClause(), pin.String())
	return crewSeatStop(seat)
}

// crewUnpin is `/crew unpin <seat|all>`: the seat goes back to auto.
func (a *app) crewUnpin(rest string) int {
	rest = strings.ToLower(strings.TrimSpace(rest))
	if rest == "all" {
		if err := config.ClearCrewPins(a.profileDir); err != nil {
			a.note("could not unpin · " + err.Error())
			return -1
		}
		a.crewApplied()
		a.note("every seat is auto · codeaf picks the worker, planner and checker for each task")
		return 0
	}
	seat, ok := config.ParseCrewSeat(rest)
	if !ok {
		a.note("usage: /crew unpin <worker|planner|checker|all>")
		return -1
	}
	if err := config.ClearCrewPin(a.profileDir, seat); err != nil {
		a.note("could not unpin the " + string(seat) + " · " + err.Error())
		return -1
	}
	a.crewApplied()
	a.note(string(seat) + " is auto · picked for each task")
	return crewSeatStop(seat)
}

// crewAllowedModels is `/crew models <rule>`: the whole rule, or a `+id`/`-id`
// changing the rule in force. A bare `/crew models` says the rule.
func (a *app) crewAllowedModels(rest string) int {
	if rest == "" {
		a.noteFacts("allowed models · "+config.CrewAllowedAt(a.profileDir).String(), config.CrewAllowedAt(a.profileDir).String())
		return -1
	}
	var err error
	switch {
	case strings.HasPrefix(rest, "+") && !strings.Contains(rest, " "):
		err = config.ModifyCrewAllowed(a.profileDir, true, strings.TrimPrefix(rest, "+"))
	case strings.HasPrefix(rest, "-") && !strings.Contains(rest, " "):
		err = config.ModifyCrewAllowed(a.profileDir, false, strings.TrimPrefix(rest, "-"))
	default:
		err = config.SetCrewAllowed(a.profileDir, rest)
	}
	if err != nil {
		a.note("could not set the allowed models · " + err.Error())
		return -1
	}
	a.crewApplied()
	rule := config.CrewAllowedAt(a.profileDir).String()
	a.noteFacts("allowed models · "+rule+" · every seat nobody pinned is picked from these", rule)
	return crewModels
}

// crewCap is `/crew cap <dollars|off>`. The router paces toward it — dearer
// crews cost more of the day's quality as the day's spend climbs — and at it a
// task does not start until the person raises it or asks for `--cheap`.
func (a *app) crewCap(rest string) int {
	if word, figure, _ := strings.Cut(rest, " "); strings.EqualFold(word, "task") {
		return a.crewTaskCap(strings.TrimSpace(figure))
	}
	if rest == "" {
		a.note("daily cap · " + a.crewCapWords())
		return -1
	}
	if err := config.SetCrewCap(a.profileDir, rest); err != nil {
		a.note("could not set the daily cap · " + err.Error())
		return -1
	}
	a.crewApplied()
	a.note("daily cap · " + a.crewCapWords())
	return crewCap
}

// crewTaskCap is `/crew cap task <dollars>`: the most one task may spend.
// A call that would take a task past it is not made.
func (a *app) crewTaskCap(rest string) int {
	if rest == "" {
		a.note("per-task limit · " + config.CrewTaskMoney(config.CrewTaskCapAt(a.profileDir)))
		return -1
	}
	if err := config.SetCrewTaskCap(a.profileDir, rest); err != nil {
		a.note("could not set the per-task limit · " + err.Error())
		return -1
	}
	a.crewApplied()
	a.note("per-task limit · " + config.CrewTaskMoney(config.CrewTaskCapAt(a.profileDir)))
	return crewCap
}

// crewCapWords is the cap and today's spend, or `none` for no cap.
func (a *app) crewCapWords() string {
	spent := config.CrewLogAt(a.profileDir).SpentUSD
	if capUSD := config.CrewCapAt(a.profileDir); capUSD > 0 {
		return crewroute.Money(capUSD) + " · " + crewroute.Money(spent) + " spent today"
	}
	return "none · " + crewroute.Money(spent) + " spent today"
}

// crewApplied is the tail every crew write shares: an open panel re-reads and
// an open settings sheet is rebuilt, because either may be holding rows read
// before the write ([app.crewRefreshed]).
func (a *app) crewApplied() { a.crewRefreshed() }

// crewUnchangedClause is the tail of a pin's confirmation: the model the
// conversation is still on, and the one command that moves it.
func (a *app) crewUnchangedClause() string {
	if id := a.talkingTo(); id != "" {
		return "you are still talking to " + id + " — /model changes that"
	}
	return "the model you talk to is untouched — /model changes that"
}

// talkingTo is the conversation's model spelled the way the status line's model
// segment spells it: the basename, with the reasoning level riding on it when
// one is set.
func (a *app) talkingTo() string {
	id := modelBase(a.model)
	if id == "" {
		return ""
	}
	if level := a.reasoningFor(a.model); level != "" {
		id += ":" + level
	}
	return id
}

// ── the crew line on a task ──────────────────────────────────────────────────

// crewLine is one task's crew as the card and its landing say it:
//
//	openended · worker glm-5.3-flash (openrouter) · checker (pin) kimi-k3 · $0.108 (est $0.112)
//
// actual below zero is not known yet, and the line then ends on the estimate.
// The pin mark is the vocabulary's own glyph, never a literal.
func (a *app) crewLine(d *crewroute.Decision, actual float64) string {
	if d == nil {
		return ""
	}
	return d.Line(crewPinMark(a.linear), actual)
}

// crewMark is a mark on the crew's own surfaces — the /crew panel, its
// notes, the crew line — in the PLAIN tier whatever the icon setting says: a
// pin, a tick, a ring, a cross, a diamond that every terminal draws. The rich
// tier's private-use codepoints are a blank cell on a terminal without the
// font, and on these surfaces a blank is a pinned seat or a connected
// provider that reads as nothing at all. The linear and ASCII readings keep
// their own spellings.
func (a *app) crewMark(id tokens.GlyphID) string {
	if a.linear || a.pal.ascii {
		return tokens.ASCII.Glyph(id)
	}
	return tokens.Plain.Glyph(id)
}

// crewPinMark is the pin in a crew line: the plain glyph, ⌖, and never the
// rich tier's. The line is TEXT kept in the transcript — read back tomorrow,
// copied out, drawn in a terminal whose font was never asked — and a private-
// use codepoint there is a blank cell, which is how a pinned checker read as
// "checker  kimi-k3". Under the linear reading there is no glyph, and the line
// says the word instead (crewroute's seatModel).
func crewPinMark(linear bool) string {
	if linear {
		return ""
	}
	return tokens.Plain.Glyph(tokens.GPinned)
}

// sayTaskCrew keeps a routed task's crew line in the thread: ONE line, said
// when the task starts with the estimate, and REWRITTEN IN PLACE as the task
// goes — a seat that failed to start and moved to its fallback, and at the end
// what it cost beside the estimate and the one door to asking again harder.
// Two lines for one crew read as two crews; the second said nothing the first
// could not carry.
func (a *app) sayTaskCrew(notice session.TaskNotice) {
	if notice.Crew == nil {
		return
	}
	if a.crewSaid == nil {
		a.crewSaid = map[uint64]crewLineSaid{}
	}
	said := a.crewSaid[notice.ID]
	if said.landed {
		return
	}
	lead := "task " + strconv.FormatUint(notice.ID, 10) + " crew · "
	var text string
	var facts []string
	switch notice.State {
	case session.TaskRunning:
		text = lead + a.crewLine(notice.Crew, -1)
		facts = []string{crewroute.ShortModel(notice.Crew.Seat(crewroute.Worker).Model)}
	case session.TaskFailed:
		// A TASK THAT FAILED ASKS FOR THE NEXT STEP BY NAME: the stronger crew
		// is the one thing on this line a person can do about it.
		// AND THE FAILURE IS SAID FIRST, before any figure: a stopped seat's
		// one action where the cause is known — no stronger crew would get
		// past it — and the stronger crew otherwise. Nothing spent names no
		// money, never a $0.000 that reads as a free success.
		failed := crewFailedWord
		if stop := notice.Crew.Stopped; stop != "" {
			failed = crewStoppedWord + stop
		}
		actual := notice.CostUSD
		if actual <= 0 {
			actual = crewroute.Unspent
		}
		text = lead + failed + " · " + a.crewLine(notice.Crew, actual)
		facts = []string{failed}
		said.landed = true
	case session.TaskDone, session.TaskUnverified:
		text = lead + a.crewLine(notice.Crew, notice.CostUSD) + " · not right? /redo stronger"
		facts = []string{crewroute.Money(notice.CostUSD)}
		said.landed = true
	default:
		return
	}
	if text == said.text {
		return
	}
	if said.text == "" || !a.feed.renote(said.text, text, facts) {
		a.noteFacts(text, facts...)
	}
	said.text, said.facts = text, facts
	a.crewSaid[notice.ID] = said
}

// crewAfterStarted keeps a task's two lines in one order: `started` first,
// then its crew. The two arrive on different roads — the start answer from
// the command, the crew on the task's first row — and either can come first;
// when the crew line is already in the thread, it is rewritten into the
// started line and said again after it, so the thread reads the same every
// time. It answers whether it wrote the started line.
func (a *app) crewAfterStarted(id string, started string, startedFacts []string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return false
	}
	said, ok := a.crewSaid[n]
	if !ok || said.text == "" || !a.feed.renote(said.text, started, startedFacts) {
		return false
	}
	a.noteFacts(said.text, said.facts...)
	return true
}

// crewLineSaid is one task's crew line as the thread holds it.
type crewLineSaid struct {
	text   string
	facts  []string
	landed bool
}

// crewFailedWord ends a failed task's crew line.
const crewFailedWord = "failed — /redo stronger runs it again on a stronger crew"

// crewStoppedWord leads a failed line whose seat stopped on a cause a person
// can fix; the action follows it.
const crewStoppedWord = "failed — "

// ── the one reading every crew surface answers from ─────────────────────────

// crewReading is the profile's crew as this surface last read it: the word
// /status prints, the status line's segment, the welcome box's clause and the
// pinned models, read at the door and not at the draw.
type crewReading struct {
	word    string
	segment string
	clause  string
	// models are the pinned ids, which /status lifts as its facts.
	models     []string
	dir        string
	generation uint64
	taken      bool
}

// crewReading is THE ONE READING every crew surface answers from, and reports
// false for a window that has no crew of its own — the hosted one.
//
// IT IS READ AT THE DOOR AND NOT AT THE DRAW. The status line asks for the
// segment on every frame; the snapshot is invalidated by the one counter
// every persisted write bumps ([config.SettingsGeneration]).
func (a *app) crewReading() (crewReading, bool) {
	if a.hosted() {
		return crewReading{}, false
	}
	// UNDER `--one-model` THE CREW SEATS NOTHING (#444): every text call rides
	// the conversation's model, so the reading names the flag.
	if a.oneModel {
		return crewReading{word: crewOneModelWord, segment: crewOneModelSegment, clause: crewOneModelSegment, dir: a.profileDir, taken: true}, true
	}
	if generation := config.SettingsGeneration(); !a.crew.taken || a.crew.generation != generation || a.crew.dir != a.profileDir {
		pins := config.CrewPinsAt(a.profileDir)
		word, segment := "auto · codeaf picks the worker, planner and checker for each task", "crew auto"
		var models []string
		if len(pins) > 0 {
			var said []string
			for _, seat := range crewroute.Seats {
				if pin, ok := pins[seat]; ok {
					said = append(said, string(seat)+" "+pin.String())
					models = append(models, pin.String())
				}
			}
			word = "auto · pinned " + strings.Join(said, ", ")
			segment = "crew auto · " + strconv.Itoa(len(pins)) + " pinned"
		}
		if rule := config.CrewAllowedAt(a.profileDir).String(); rule != "all" {
			word += " · allowed " + rule
		}
		a.crew = crewReading{word: word, segment: segment, clause: "auto crew", models: models,
			dir: a.profileDir, generation: generation, taken: true}
	}
	return a.crew, true
}

// crewWord is the crew as a page states it (/status).
func (a *app) crewWord() string {
	crew, ok := a.crewReading()
	if !ok {
		return ""
	}
	return crew.word
}

// crewSegment is the status line's crew segment: `crew auto`, or
// `crew auto · 1 pinned`.
func (a *app) crewSegment() string {
	crew, ok := a.crewReading()
	if !ok {
		return ""
	}
	return crew.segment
}

// crewHint is [app.crewSegment] for the hint slot under the model picker.
func (a *app) crewHint() string { return a.crewSegment() }

// The crew as `--one-model` leaves it, spelled once.
const (
	crewOneModelSegment = "one model"
	crewOneModelWord    = "one model · every call rides the model you are talking to"
)

// refreshSettings rebuilds an open settings panel from the registry. A command
// that wrote a row while the panel was open would otherwise leave the panel
// showing what it read when it opened.
func (a *app) refreshSettings() {
	if !a.at(pageSettings) || a.sheet.registry == nil {
		return
	}
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
}
