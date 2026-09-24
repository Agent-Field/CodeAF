package tui3

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
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
// THE BARE FORM IS THE PANEL, as one page: the seats (auto, or pinned with the
// pin glyph), the providers the crew can route through (derived from the
// connections, never a setting), the allowed-models rule, the daily cap with
// today's spend beside it, and the recent tasks with the crew each one ran on.
// The four shortcuts are how anything on it changes, and every write goes
// through internal/config's own writers so a command and a settings row cannot
// disagree about which crew is on.
//
//	/crew pin <seat> <model[@provider]>   /crew unpin <seat|all>
//	/crew models <all|open|≤in/out|list|+model|-model>
//	/crew cap <dollars|off>

// crewUsage is the one line every refused /crew form answers with.
const crewUsage = "/crew · /crew pin <worker|planner|checker> <model[@provider]> · /crew unpin <seat|all> · " +
	"/crew models <all|open|≤in/out|ids…|+id|-id> · /crew cap <dollars|off>"

// runCrew is /crew: the panel, or one of its four shortcuts.
func (a *app) runCrew(arg string) {
	if a.hosted() {
		a.note(a.remoteProfileWord("the crew"))
		return
	}
	arg = strings.TrimSpace(arg)
	word, rest, _ := strings.Cut(arg, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(word) {
	case "":
		a.closeLists()
		text := a.crewPanel()
		a.noteFacts(text, columnFacts(text, false)...)
	case "pin":
		a.crewPin(rest)
	case "unpin":
		a.crewUnpin(rest)
	case "models":
		a.crewAllowedModels(rest)
	case "cap":
		a.crewCap(rest)
	default:
		a.note("/crew " + arg + " · not a crew form · " + crewUsage)
	}
}

// crewPin is `/crew pin <seat> <model[@provider]>`. A pin outside the allowed
// models is REFUSED rather than written, because a pin the router would have
// to break is not a pin ([config.SetCrewPin] says the rule once).
func (a *app) crewPin(rest string) {
	seatWord, model, _ := strings.Cut(rest, " ")
	seat, ok := config.ParseCrewSeat(seatWord)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		a.note("usage: /crew pin <worker|planner|checker> <model[@provider]>")
		return
	}
	if err := config.SetCrewPin(a.profileDir, seat, model); err != nil {
		a.note("could not pin the " + string(seat) + " · " + err.Error())
		return
	}
	a.crewApplied()
	pin, _ := config.CrewPinAt(a.profileDir, seat)
	a.noteFacts(string(seat)+" "+a.icon(tokens.GPinned)+" "+pin.String()+" · every task until you unpin it · "+
		a.crewUnchangedClause(), pin.String())
}

// crewUnpin is `/crew unpin <seat|all>`: the seat goes back to auto.
func (a *app) crewUnpin(rest string) {
	rest = strings.ToLower(strings.TrimSpace(rest))
	if rest == "all" {
		if err := config.ClearCrewPins(a.profileDir); err != nil {
			a.note("could not unpin · " + err.Error())
			return
		}
		a.crewApplied()
		a.note("every seat is auto · codeaf picks the worker, planner and checker for each task")
		return
	}
	seat, ok := config.ParseCrewSeat(rest)
	if !ok {
		a.note("usage: /crew unpin <worker|planner|checker|all>")
		return
	}
	if err := config.ClearCrewPin(a.profileDir, seat); err != nil {
		a.note("could not unpin the " + string(seat) + " · " + err.Error())
		return
	}
	a.crewApplied()
	a.note(string(seat) + " is auto · picked for each task")
}

// crewAllowedModels is `/crew models <rule>`: the whole rule, or a `+id`/`-id`
// changing the rule in force. A bare `/crew models` says the rule.
func (a *app) crewAllowedModels(rest string) {
	if rest == "" {
		a.noteFacts("allowed models · "+config.CrewAllowedAt(a.profileDir).String(), config.CrewAllowedAt(a.profileDir).String())
		return
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
		return
	}
	a.crewApplied()
	rule := config.CrewAllowedAt(a.profileDir).String()
	a.noteFacts("allowed models · "+rule+" · every seat nobody pinned is picked from these", rule)
}

// crewCap is `/crew cap <dollars|off>`. The router paces toward it — dearer
// crews cost more of the day's quality as the day's spend climbs — and at it a
// task does not start until the person raises it or asks for `--cheap`.
func (a *app) crewCap(rest string) {
	if rest == "" {
		a.note("daily cap · " + a.crewCapWords())
		return
	}
	if err := config.SetCrewCap(a.profileDir, rest); err != nil {
		a.note("could not set the daily cap · " + err.Error())
		return
	}
	a.crewApplied()
	a.note("daily cap · " + a.crewCapWords())
}

// crewCapWords is the cap and today's spend, or `none` for no cap.
func (a *app) crewCapWords() string {
	spent := config.CrewLogAt(a.profileDir).SpentUSD
	if capUSD := config.CrewCapAt(a.profileDir); capUSD > 0 {
		return crewroute.Money(capUSD) + " · " + crewroute.Money(spent) + " spent today"
	}
	return "none · " + crewroute.Money(spent) + " spent today"
}

// crewApplied is the tail every crew write shares: an open settings panel is
// rebuilt, because it may be holding rows read before the write.
func (a *app) crewApplied() { a.refreshSettings() }

// crewPanel is the bare /crew page.
//
//	worker   auto · now glm-5.3-flash via openrouter
//	planner  auto · now glm-5.3-flash via openrouter
//	checker  (pin) moonshotai/kimi-k3@openrouter
//
//	providers  openrouter (metered) · codex (plan)
//	allowed    all
//	daily cap  $5.000 · $0.412 spent today · 3 tasks, 1 on a plan
//
//	recent     openended · worker glm-5.3-flash (openrouter) · checker kimi-k3 · $0.108 (est $0.112) · accepted
//
// THE EMPTINESS LAW on every line: a seat with nothing to route to says so, a
// profile with no connection says so, and a day with no task has no recent
// lines rather than a header over nothing.
func (a *app) crewPanel() string {
	var out strings.Builder
	pins := config.CrewPinsAt(a.profileDir)
	for _, seat := range crewroute.Seats {
		out.WriteString(padRight(string(seat), 9))
		if pin, ok := pins[seat]; ok {
			out.WriteString(a.icon(tokens.GPinned) + " " + pin.String() + "\n")
			continue
		}
		now := config.TierSeatAt(a.profileDir, config.CrewSeatTier(seat)).Model
		if now == "" {
			out.WriteString("auto · nothing allowed can sit this seat — connect a provider or widen /crew models\n")
			continue
		}
		out.WriteString("auto · now " + now + "\n")
	}
	out.WriteString("\n")
	var providers []string
	for _, provider := range config.CrewProvidersAt(a.profileDir) {
		providers = append(providers, provider.ID+" ("+string(provider.Kind)+")")
	}
	if len(providers) == 0 {
		out.WriteString("providers  none connected · /connect adds one\n")
	} else {
		out.WriteString("providers  " + strings.Join(providers, " · ") + "\n")
	}
	out.WriteString("allowed    " + config.CrewAllowedAt(a.profileDir).String() + "\n")
	log := config.CrewLogAt(a.profileDir)
	day := a.crewCapWords()
	if log.Tasks > 0 {
		day += " · " + strconv.Itoa(log.Tasks) + " tasks"
		if log.OnPlan > 0 {
			day += ", " + strconv.Itoa(log.OnPlan) + " on a plan"
		}
		if log.Local > 0 {
			day += ", " + strconv.Itoa(log.Local) + " local"
		}
	}
	out.WriteString("daily cap  " + day + "\n")
	for _, gap := range config.CrewGapsAt(a.profileDir) {
		out.WriteString("gap        " + gap.Line + "\n")
	}
	if len(log.Recent) > 0 {
		out.WriteString("\n")
		for i, task := range log.Recent {
			lead := "recent     "
			if i > 0 {
				lead = "           "
			}
			line := task.Record.TaskClass + " · " + crewRecordSeats(task.Record, a.icon(tokens.GPinned))
			if task.Settled {
				line += " · " + crewroute.Money(task.CostUSD) + " (est " + crewroute.Money(task.Record.EstUSD) + ") · " + task.Outcome
			} else {
				line += " · est " + crewroute.Money(task.Record.EstUSD)
			}
			if title := strings.TrimSpace(task.Record.Title); title != "" {
				line = title + " · " + line
			}
			out.WriteString(lead + line + "\n")
		}
	}
	out.WriteString("\n" + crewUsage + " · /model is untouched")
	return out.String()
}

// crewRecordSeats is a logged task's worker and checker in the card line's
// words.
func crewRecordSeats(record router.CrewRecord, pinMark string) string {
	pinned := map[string]bool{}
	for _, seat := range record.Pinned {
		pinned[seat] = true
	}
	var parts []string
	for _, seat := range []string{string(crewroute.Worker), string(crewroute.Checker)} {
		model := crewroute.ShortModel(record.Seats[seat])
		if model == "" {
			continue
		}
		if pinned[seat] {
			model = pinMark + " " + model
		}
		if provider := record.Providers[seat]; provider != "" && seat == string(crewroute.Worker) {
			model += " (" + provider + ")"
		}
		parts = append(parts, seat+" "+model)
	}
	return strings.Join(parts, " · ")
}

// padRight pads a word to a column.
func padRight(word string, width int) string {
	for len(word) < width {
		word += " "
	}
	return word
}

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
	return d.Line(a.icon(tokens.GPinned), actual)
}

// sayTaskCrew puts a routed task's crew line in the thread twice: when the
// task starts, with the estimate, and when it lands, with what it cost beside
// the estimate and the one door to asking again harder. Each is said once per
// task, whatever number of updates the row goes through.
func (a *app) sayTaskCrew(notice session.TaskNotice) {
	if notice.Crew == nil {
		return
	}
	if a.crewSaid == nil {
		a.crewSaid = map[uint64]string{}
	}
	lead := "task " + strconv.FormatUint(notice.ID, 10) + " crew · "
	switch notice.State {
	case session.TaskRunning:
		if a.crewSaid[notice.ID] != "" {
			return
		}
		a.crewSaid[notice.ID] = "started"
		a.noteFacts(lead+a.crewLine(notice.Crew, -1), crewroute.ShortModel(notice.Crew.Seat(crewroute.Worker).Model))
	case session.TaskDone, session.TaskFailed, session.TaskUnverified:
		if a.crewSaid[notice.ID] == "landed" {
			return
		}
		a.crewSaid[notice.ID] = "landed"
		a.noteFacts(lead+a.crewLine(notice.Crew, notice.CostUSD)+" · not right? /redo stronger",
			crewroute.Money(notice.CostUSD))
	}
}

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
