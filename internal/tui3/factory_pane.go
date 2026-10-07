package tui3

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FACTORY PANE ────────────────────────────────────────────────────────
//
// The right-hand column of the factory page: the item under the rail's cursor,
// drawn by where it stands. There are four drawings and an item is always in
// exactly one of them.
//
//   - THE CARD is a new item (or a dismissed one): what it is, the factory's
//     read of it, the chips a person can turn, the stages it would run, and the
//     one key that starts it.
//   - THE STREAM is an item on a bench, waiting for one, or waiting on the
//     person: its phases, its recipe, and the tail of its log, with the
//     question under it when there is one.
//   - THE PROOF SHEET is a landed item: one row per claim, shown or not, the
//     policy rows the same way, and the sign-off line.
//   - THE SHIPPED LINE is an item that merged: when, what it cost, and the way
//     into its room.
//
// THE PANE IS PROSE AND ROWS, NOT BOXES (docs/DESIGN-LANGUAGE.md). It has no
// border, a 2-cell lead, one blank row between blocks, and headings in muted.
// The one hairline it draws is the rule over a stream's log.
//
// THE PANE IS A FUNCTION OF ITS WIDTH AND ITS ROOM AND NOTHING ELSE, so the
// handover strip (factory_head.go) can stack its own rows above it by asking
// for less room. It reads the snapshot the page folded in and never the disk.
//
// THE KEYS IT DRAWS DO NOTHING YET except the walk the rail already has. They
// are drawn so the layout is settled before the verbs arrive, and the manual's
// "not yet" section says so in as many words.

// factoryPaneLead is the pane's left margin: the spacing ladder's 2-cell lead.
const factoryPaneLead = 2

// factoryPaneGap is the least air between a row's left words and its
// right-aligned meta. Under it the meta is dropped rather than jammed.
const factoryPaneGap = 2

// factoryPane is the item under the cursor, drawn by its state, as EXACTLY
// room rows of EXACTLY width cells. A floor with no items, or no room, is room
// blank rows.
func (a *app) factoryPane(width, room int) []string {
	if room <= 0 {
		return nil
	}
	var lines []string
	if it, ok := a.factoryCursorItem(); ok && width > factoryPaneLead {
		measure := width - factoryPaneLead
		switch it.State {
		case factory.StateRunning, factory.StateQueued, factory.StateNeedsYou:
			lines = a.factoryStream(it, measure, room)
		case factory.StateLanded:
			lines = a.factorySheet(it, measure)
		case factory.StateShipped:
			lines = a.factoryShipped(it, measure)
		default:
			lines = a.factoryCard(it, measure)
		}
	}
	lead := strings.Repeat(" ", factoryPaneLead)
	out := make([]string, room)
	for i := range out {
		row := ""
		if i < len(lines) && lines[i] != "" {
			row = lead + lines[i]
		}
		out[i] = factoryPad(row, width)
	}
	return out
}

// ── the card ────────────────────────────────────────────────────────────────

// factoryCard is a new item: what it is, what the factory makes of it, the
// chips, the stages it would run, the policy its proof must show, and the line
// that starts it. Each line is at most measure cells.
func (a *app) factoryCard(it factory.Item, measure int) []string {
	pal := a.pal
	snap := a.fp.snap
	var out []string

	// Line 1: what it is, and where it came from on the right.
	meta := []string{factoryRepoShort(it.Repo), string(it.Kind)}
	if who := factoryAuthor(it); who != "" {
		meta = append(meta, who)
	}
	if age := factoryAge(snap.Now, it.Created); age != "" {
		meta = append(meta, age)
	}
	if origin := a.factoryOrigin(it); origin != "" {
		meta = append(meta, origin)
	}
	out = append(out, factorySpread(pal.ink(it.Ref()+" "+it.Title), pal.muted(strings.Join(nonEmpty(meta), " · ")), measure))

	// Up to two lines of the body, the rest marked as cut.
	if body := factoryBodyLines(it.Body, measure); len(body) > 0 {
		cut := len(body) > 2
		if cut {
			body = body[:2]
			body[1] = fit(body[1]+" "+a.icon(tokens.GEllipsis), measure)
		}
		for _, line := range body {
			out = append(out, pal.muted(line))
		}
	}

	// The read: the triage facts, then the one sentence.
	out = append(out, "")
	if facts := factoryFacts(it); facts != "" {
		out = append(out, pal.muted(fit(facts, measure)))
	}
	if read := strings.TrimSpace(it.Triage.Read); read != "" {
		out = append(out, a.factoryLed(pal.muted(a.icon(tokens.GThought)), read, pal.ink, measure)...)
	}
	if qs := nonEmpty(it.Triage.Questions); len(qs) > 0 {
		ask := "thin · it would ask " + factoryOr(it.Author, "the author") + ": " + strings.Join(qs, " / ")
		for _, line := range wrap(ask, measure) {
			out = append(out, pal.ask(line))
		}
		out = append(out, pal.muted(fit("nothing posts without you", measure)))
	}
	if it.Tier == factory.TierStranger {
		for _, line := range wrap(factoryOr(it.Author, "the author")+" is a stranger here · their words may be drafted on, never shipped on, without you", measure) {
			out = append(out, pal.muted(line))
		}
	}

	// The chips.
	out = append(out, "")
	out = append(out, fit(a.factoryChips(it), measure))

	// The stages it would run.
	if rows := a.factoryStageRows(it, measure); len(rows) > 0 {
		out = append(out, "")
		out = append(out, rows...)
	}

	// The policy its proof must show.
	if policy := nonEmpty(factoryRecipe(snap, it).Policy); len(policy) > 0 {
		out = append(out, "")
		for _, line := range wrap("must show: "+strings.Join(policy, " · "), measure) {
			out = append(out, pal.muted(line))
		}
	}

	// The line that starts it.
	out = append(out, "")
	go1 := pal.accent("[enter] go")
	if means := factoryGateMeans(it.Gate); means != "" {
		go1 += "  " + pal.muted(means)
	}
	out = append(out, fit(go1, measure))
	for _, line := range wrap("[s] add a stage in words · [b] bank · [w] chips in words · [g] also on github · [d] hide", measure) {
		out = append(out, pal.dim(line))
	}
	return out
}

// factoryChips is the card's one chip line: the gate, the cap and the effort,
// each with the key that will turn it.
func (a *app) factoryChips(it factory.Item) string {
	pal := a.pal
	chip := func(label, value, key string) string {
		s := pal.muted(label)
		if value != "" {
			s += " " + pal.ink(value)
		}
		return s + " " + pal.dim("["+key+"]")
	}
	// THE EFFORT CHIP SAYS A DASH, which is the knee: no word on the item
	// means the crew picks the effort it would pick for this class of work.
	return strings.Join([]string{
		chip("gate", string(it.Gate), "t"),
		chip("cap", factoryMoney(it.Cap), "c"),
		chip("effort", "—", "e"),
	}, "   ")
}

// factoryStageRows is one row per stage the item would run, numbered. A stage
// switched off draws dim; a stage whose condition does not fit the item draws
// dim with the reason after it ([factory.Fits]).
func (a *app) factoryStageRows(it factory.Item, measure int) []string {
	pal := a.pal
	stages := factoryStages(a.fp.snap, it)
	if len(stages) == 0 {
		return nil
	}
	nameW := 0
	for _, st := range stages {
		nameW = max(nameW, ansi.StringWidth(factoryStageName(st)))
	}
	nameW = min(nameW, max(measure/3, 6))
	var out []string
	for i, st := range stages {
		fits := factory.Fits(st, it)
		lit := st.On && fits
		mark := a.icon(tokens.GStepDone)
		if !st.On {
			mark = a.icon(tokens.GStepPending)
		}
		num := "[" + strconv.Itoa(i+1) + "]"
		// The name column is plain and padded first, then painted in two
		// parts, so the column lines up whatever the paint adds.
		nameCol := factoryPad(fit(factoryStageName(st), nameW), nameW)
		var extra []string
		if st.Max > 1 {
			extra = append(extra, "×"+strconv.Itoa(st.Max))
		}
		if u := strings.TrimSpace(st.Until); u != "" && u != "done" {
			extra = append(extra, "until "+u)
		}
		if !fits {
			extra = append(extra, "· skipped: not "+strings.TrimSpace(st.When))
		}
		tail := strings.Join(extra, " ")
		head := num + " " + mark + " " + nameCol + "  "
		askW := measure - ansi.StringWidth(head)
		if tail != "" {
			askW -= ansi.StringWidth(tail) + 2
		}
		ask := fit(strings.TrimSpace(st.Ask), max(askW, 0))
		if !lit {
			row := head + ask
			if tail != "" {
				row += "  " + tail
			}
			out = append(out, pal.dim(fit(row, measure)))
			continue
		}
		name, kind := nameCol, ""
		if cut := len(st.Name); cut < len(nameCol) && strings.HasPrefix(nameCol, st.Name) {
			name, kind = nameCol[:cut], nameCol[cut:]
		}
		row := pal.dim(num) + " " + pal.muted(mark) + " " + pal.ink(name) + pal.dim(kind) + "  " + pal.muted(ask)
		if tail != "" {
			row += "  " + pal.dim(tail)
		}
		out = append(out, fit(row, measure))
	}
	return out
}

// factoryStageName is the stage's name column: its name, and its kind when the
// kind is not a conversation.
func factoryStageName(st factory.Stage) string {
	if k := factoryStageKindWord(st.Kind); k != "" {
		return st.Name + " " + k
	}
	return st.Name
}

// factoryStageKindWord is a stage's kind as the pane says it, and nothing for
// a conversation, which is what a stage is unless said otherwise.
func factoryStageKindWord(k factory.StageKind) string {
	if k == "" || k == factory.StageChat {
		return ""
	}
	return string(k)
}

// factoryGateMeans is what the gate means for a person, in their words.
func factoryGateMeans(g factory.Gate) string {
	switch g {
	case factory.GateShip:
		return "runs to a PR · you sign off"
	case factory.GatePlan:
		return "comes back with the plan first"
	case factory.GateNone:
		return "self-ships when the proof is green"
	}
	return ""
}

// factoryFacts is the triage on one line: the type, size, area, readiness,
// estimate and risk of an issue, or the diff and checks of a pull request. A
// fact that is not known is left out rather than drawn as a zero.
func factoryFacts(it factory.Item) string {
	t := it.Triage
	if it.Kind == factory.KindPR {
		return strings.Join(nonEmpty([]string{string(it.Kind), it.Diff, it.Checks}), " · ")
	}
	facts := []string{t.Type, t.Size, t.Area}
	if t.Readiness > 0 {
		facts = append(facts, "ready "+strconv.Itoa(t.Readiness)+"%")
	}
	if est := factoryMoney(t.Est); est != "" {
		facts = append(facts, "~"+est)
	}
	if t.Risk != "" {
		facts = append(facts, "risk "+t.Risk)
	}
	return strings.Join(nonEmpty(facts), " · ")
}

// factoryOrigin is where the item came from, and a check after github when
// it is on github too.
func (a *app) factoryOrigin(it factory.Item) string {
	if it.Synced {
		return "github " + a.icon(tokens.GSettled)
	}
	switch it.Origin {
	case factory.OriginForge:
		return "github"
	case factory.OriginChat:
		return "from a chat"
	case factory.OriginTerminal:
		return "terminal only"
	}
	return string(it.Origin)
}

// factoryAuthor is the author and how far they are trusted.
func factoryAuthor(it factory.Item) string {
	if it.Author == "" {
		return ""
	}
	if it.Tier == "" {
		return it.Author
	}
	return it.Author + " (" + string(it.Tier) + ")"
}

// factoryBodyLines is the body wrapped to measure with its blank lines taken
// out, so two lines of it are two lines of words.
func factoryBodyLines(body string, measure int) []string {
	var out []string
	for _, line := range wrap(strings.TrimSpace(body), measure) {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// ── the stream ──────────────────────────────────────────────────────────────

// factoryStream is an item on a bench, queued for one, or waiting on the
// person: its head line, its phases, its recipe, the tail of its log, and the
// question or the queue line under it. THE LOG TAKES WHAT ROOM IS LEFT and
// keeps its newest lines, so the question is never pushed off the bottom by
// the grain above it.
func (a *app) factoryStream(it factory.Item, measure, room int) []string {
	pal := a.pal
	snap := a.fp.snap
	s := it.Stream

	// Line 1: the state, the item, and the bench on the right.
	var lead string
	switch {
	case it.State == factory.StateNeedsYou:
		lead = pal.ask(a.icon(tokens.GNeedsHuman))
	case it.State == factory.StateQueued:
		lead = pal.muted(a.icon(tokens.GQueued))
	case s != nil && s.Paused:
		lead = pal.muted(a.icon(tokens.GPaused))
	default:
		lead = pal.accent(a.icon(tokens.GWorking))
	}
	meta := []string{factoryRepoShort(it.Repo)}
	if s != nil && s.Bench > 0 {
		meta = append(meta, "bench "+strconv.Itoa(s.Bench))
	}
	if s != nil {
		meta = append(meta, factoryElapsed(s.Started, factoryEnd(s, snap.Now)))
	}
	meta = append(meta, factorySpend(s, it.Cap))
	head := []string{factorySpread(lead+" "+pal.ink(it.Ref()+" "+it.Title), pal.muted(strings.Join(nonEmpty(meta), " · ")), measure)}

	// The phase strip, then the recipe.
	head = append(head, a.factoryPhaseStrip(it, measure)...)
	if recipe := factoryRecipeWords(factoryStages(snap, it)); recipe != "" {
		head = append(head, pal.muted(fit(recipe, measure)))
	}

	// What waits under the log.
	var foot []string
	switch it.State {
	case factory.StateNeedsYou:
		foot = append(foot, "")
		if q := strings.TrimSpace(it.Question); q != "" {
			foot = append(foot, a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ask, measure)...)
		}
		for _, line := range wrap("[y] yes · [n] no · [a] answer in words · it waits; the other benches do not", measure) {
			foot = append(foot, pal.dim(line))
		}
	case factory.StateQueued:
		foot = append(foot, "", pal.dim(fit("queued · benches full · a bench frees it", measure)))
	}

	// The rule and the log, in the room between.
	var log []string
	if s != nil {
		for _, l := range s.Log {
			log = append(log, a.factoryLogLine(l, measure))
		}
	}
	out := head
	if left := room - len(head) - len(foot) - 1; len(log) > 0 && left > 0 {
		out = append(out, pal.dim(strings.Repeat(a.linearMark("─", "-"), measure)))
		if len(log) > left {
			log = log[len(log)-left:]
		}
		out = append(out, log...)
	}
	return append(out, foot...)
}

// factoryPhaseStrip is the stream's phases on one line, or more when the pane
// is too narrow for them: each one its mark and its name, its round and task
// count when it has them, the running one in ink with the minutes it has left.
// An item with no stream yet draws its stages as pending.
func (a *app) factoryPhaseStrip(it factory.Item, measure int) []string {
	pal := a.pal
	stages := factoryStages(a.fp.snap, it)
	var phases []factory.Phase
	if it.Stream != nil {
		phases = it.Stream.Phases
	}
	if len(phases) == 0 {
		for _, st := range stages {
			if st.On && factory.Fits(st, it) {
				phases = append(phases, factory.Phase{Name: st.Name, Kind: st.Kind, State: factory.PhasePending})
			}
		}
	}
	maxOf := func(name string) int {
		for _, st := range stages {
			if st.Name == name {
				return max(st.Max, 1)
			}
		}
		return 0
	}
	var segs []string
	for _, ph := range phases {
		words := ph.Name
		if ph.Round > 0 {
			if m := maxOf(ph.Name); m > 0 {
				words += " " + strconv.Itoa(ph.Round) + "/" + strconv.Itoa(m)
			} else {
				words += " " + strconv.Itoa(ph.Round)
			}
		}
		if ph.Tasks > 1 {
			words += " ×" + strconv.Itoa(ph.Tasks)
		}
		var seg string
		switch ph.State {
		case factory.PhaseRunning:
			if ph.Left > 0 {
				words += " · " + factoryMinutes(ph.Left) + " left"
			}
			seg = pal.accent(a.icon(tokens.GStepRunning)) + " " + pal.ink(words)
		case factory.PhaseDone:
			seg = pal.muted(a.icon(tokens.GStepDone) + " " + words)
		case factory.PhaseWaiting:
			seg = pal.ask(a.icon(tokens.GNeedsHuman) + " " + words)
		case factory.PhaseFailed:
			seg = pal.bad(a.icon(tokens.GFailed) + " " + words)
		default:
			seg = pal.dim(a.icon(tokens.GStepPending) + " " + words)
		}
		segs = append(segs, seg)
	}
	return factoryPack(segs, "   ", measure)
}

// factoryRecipeWords is the stage names in order, joined, each with its effort
// word when one is set. A stage switched off is not in it.
func factoryRecipeWords(stages []factory.Stage) string {
	var names []string
	for _, st := range stages {
		if !st.On {
			continue
		}
		name := st.Name
		if e := strings.TrimSpace(st.Effort); e != "" {
			name += " (" + e + ")"
		}
		names = append(names, name)
	}
	return strings.Join(names, " · ")
}

// factoryLogLine is one line of a stream's grain: the time, a mark for what
// kind of line it is, coloured by its tone, and the words.
func (a *app) factoryLogLine(l factory.LogLine, measure int) string {
	pal := a.pal
	mark, paint, text := a.factoryTone(l)
	stamp := ""
	if !l.At.IsZero() {
		stamp = pal.dim(l.At.Format("15:04")) + " "
	}
	return fit(stamp+paint(mark)+" "+text(l.Text), measure)
}

// factoryTone is the mark for a log line's tone, through the vocabulary's door,
// and the paints for the mark and the words. A tone the vocabulary has no slot
// for keeps the mark the line arrived with.
func (a *app) factoryTone(l factory.LogLine) (string, func(string) string, func(string) string) {
	pal := a.pal
	switch l.Tone {
	case "thought":
		return a.icon(tokens.GThought), pal.muted, pal.muted
	case "shell":
		return a.icon(tokens.GShell), pal.muted, pal.muted
	case "test":
		return a.icon(tokens.GActionTest), pal.muted, pal.muted
	case "write":
		return a.icon(tokens.GWrite), pal.muted, pal.muted
	case "said":
		return a.icon(tokens.GActionCommunicate), pal.ink, pal.ink
	case "ask":
		return a.icon(tokens.GNeedsHuman), pal.ask, pal.ask
	case "fail":
		return a.icon(tokens.GFailed), pal.bad, pal.muted
	case "ok":
		return a.icon(tokens.GSettled), pal.add, pal.muted
	}
	mark := strings.TrimSpace(l.Glyph)
	if mark == "" || a.linear {
		mark = a.icon(tokens.GSeparator)
	}
	return mark, pal.muted, pal.muted
}

// ── the proof sheet ─────────────────────────────────────────────────────────

// factorySheet is a landed item: a row per claim, shown or not, the policy rows
// the same way, what the run cost, and the sign-off line. A FAILED CLAIM MAKES
// THE BLOCKING ACTION THE DEFAULT KEY: enter sends the work back naming the
// first claim nothing showed, and shipping it anyway takes a different key on
// purpose.
func (a *app) factorySheet(it factory.Item, measure int) []string {
	pal := a.pal
	snap := a.fp.snap
	note := "its claims"
	if it.Kind == factory.KindPR {
		note = "their claims, from the PR body"
	}
	out := []string{factorySpread(pal.muted("sign-off · "+it.Ref()+" · "+it.Title), pal.dim(note), measure)}

	failed := ""
	for _, c := range it.Proof {
		out = append(out, a.factoryClaimRow(c, "", measure))
		if !c.OK && failed == "" {
			failed = c.Text
		}
	}
	if len(it.Policy) > 0 {
		out = append(out, "", pal.muted(fit("policy · every PR on "+factoryRepoShort(it.Repo)+" must show", measure)))
		for _, c := range it.Policy {
			out = append(out, a.factoryClaimRow(c, "policy", measure))
			if !c.OK && failed == "" {
				failed = c.Text
			}
		}
	}

	// What the run took.
	var took []string
	if s := it.Stream; s != nil {
		took = append(took, factoryElapsed(s.Started, factoryEnd(s, snap.Now)))
	}
	if words := factoryEffortWords(factoryStages(snap, it)); words != "" {
		took = append(took, words)
	}
	if it.Stream != nil {
		took = append(took, factoryMoney(it.Stream.Spent))
	}
	if it.Diff != "" {
		took = append(took, "diff "+it.Diff+" — the appendix")
	}
	if line := strings.Join(nonEmpty(took), " · "); line != "" {
		row := pal.muted(line)
		if it.Diff != "" {
			row += "  " + pal.dim("[d] open diff")
		}
		out = append(out, "", fit(row, measure))
	}

	// The sign-off.
	out = append(out, "")
	if failed == "" {
		out = append(out, fit(pal.accent("[enter] ship")+"  "+pal.dim("[c] send back · [o] check again · [s] the stream"), measure))
		return out
	}
	out = append(out, pal.warn(fit("[enter] send back — \"prove "+failed+"\"", measure)))
	out = append(out, pal.dim(fit("[a] ship anyway · [o] check again", measure)))
	out = append(out, pal.dim(fit("a failed claim makes the blocking action the default key", measure)))
	return out
}

// factoryClaimRow is one claim: its mark, its words, and its evidence on the
// right, with the way to look at a screenshot after one. A claim nothing showed
// says so in its own row, in the words a person uses: NOT SHOWN.
func (a *app) factoryClaimRow(c factory.Claim, tag string, measure int) string {
	pal := a.pal
	left := pal.add(a.icon(tokens.GSettled)) + " " + pal.ink(c.Text)
	if !c.OK {
		left = pal.bad(a.icon(tokens.GFailed)) + " " + pal.ink(c.Text) + pal.bad(" — not shown")
	}
	right := pal.muted(c.Evidence)
	if tag != "" {
		right = pal.muted(strings.Join(nonEmpty([]string{c.Evidence, tag}), " · "))
	}
	if c.Medium == "screenshot" {
		right += " " + pal.accent("["+a.linearMark("▦", "#")+" view]")
	}
	return factorySpread(left, right, measure)
}

// factoryEffortWords is the effort words the stages carried, each beside its
// stage, and nothing when every stage ran at the knee.
func factoryEffortWords(stages []factory.Stage) string {
	var words []string
	for _, st := range stages {
		if e := strings.TrimSpace(st.Effort); e != "" && st.On {
			words = append(words, e+" "+st.Name)
		}
	}
	return strings.Join(words, ", ")
}

// ── the shipped line ────────────────────────────────────────────────────────

// factoryShipped is an item that merged: when, what it cost, and the way into
// the room that did it.
func (a *app) factoryShipped(it factory.Item, measure int) []string {
	pal := a.pal
	out := []string{fit(pal.ink(it.Ref()+" "+it.Title), measure)}
	when := it.Changed
	var spent float64
	if s := it.Stream; s != nil {
		if !s.Ended.IsZero() {
			when = s.Ended
		}
		spent = s.Spent
	}
	var facts []string
	if !when.IsZero() {
		facts = append(facts, "merged "+when.Format("15:04"))
	}
	facts = append(facts, factoryMoney(spent))
	if line := strings.Join(nonEmpty(facts), " · "); line != "" {
		out = append(out, pal.muted(fit(line, measure)))
	}
	return append(out, "", pal.dim(fit("[enter] the room", measure)))
}

// ── the pieces ──────────────────────────────────────────────────────────────

// factoryStages is the stages the item runs: its own copy when it has one, and
// otherwise what its repo banked for its kind.
func factoryStages(snap factory.Snapshot, it factory.Item) []factory.Stage {
	if len(it.Stages) > 0 {
		return it.Stages
	}
	return factoryRecipe(snap, it).For(it.Kind)
}

// factoryRecipe is the recipe the item's repo banked, and the zero recipe for
// a repo the snapshot does not hold.
func factoryRecipe(snap factory.Snapshot, it factory.Item) factory.Recipe {
	r, _ := snap.RepoNamed(it.Repo)
	return r.Recipe
}

// factoryLed is text wrapped under a one-cell mark with a hanging indent, so
// the second line sits under the first line's words and not under its mark.
func (a *app) factoryLed(mark, text string, paint func(string) string, measure int) []string {
	var out []string
	for i, line := range wrap(text, max(measure-2, 4)) {
		if i == 0 {
			out = append(out, fit(mark+" "+paint(line), measure))
			continue
		}
		out = append(out, fit("  "+paint(line), measure))
	}
	return out
}

// factorySpread is left words and right-aligned meta on one line of measure
// cells. When both do not fit with air between them the meta goes first, then
// the left words are cut: what the row is matters more than its metadata.
func factorySpread(left, right string, measure int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if right == "" || lw+factoryPaneGap+rw > measure {
		if right != "" && rw+factoryPaneGap+12 <= measure {
			left = fit(left, measure-rw-factoryPaneGap)
			lw = ansi.StringWidth(left)
		} else {
			return fit(left, measure)
		}
	}
	return left + strings.Repeat(" ", measure-lw-rw) + right
}

// factoryPack lays segments on as few lines of measure cells as they fit,
// joined by sep, cutting a segment wider than a whole line.
func factoryPack(segs []string, sep string, measure int) []string {
	var out []string
	line, lineW := "", 0
	sepW := ansi.StringWidth(sep)
	for _, seg := range segs {
		w := ansi.StringWidth(seg)
		if lineW > 0 && lineW+sepW+w > measure {
			out = append(out, line)
			line, lineW = "", 0
		}
		if lineW > 0 {
			line += sep
			lineW += sepW
		}
		if w > measure-lineW {
			seg = fit(seg, measure-lineW)
			w = ansi.StringWidth(seg)
		}
		line += seg
		lineW += w
	}
	if lineW > 0 {
		out = append(out, line)
	}
	return out
}

// factoryMoney is an amount as the pane says it: whole dollars without cents,
// and NOTHING for zero (the emptiness law).
func factoryMoney(usd float64) string {
	if usd <= 0 {
		return ""
	}
	if usd == float64(int(usd)) && usd < 1e6 {
		return "$" + strconv.Itoa(int(usd))
	}
	return dollars(usd)
}

// factorySpend is spent over the cap, or either alone when the other is not
// known.
func factorySpend(s *factory.Stream, cap float64) string {
	spent := ""
	if s != nil {
		spent = factoryMoney(s.Spent)
	}
	capWord := factoryMoney(cap)
	switch {
	case spent != "" && capWord != "":
		return spent + " / " + capWord
	case spent != "":
		return spent
	case capWord != "":
		return "cap " + capWord
	}
	return ""
}

// factoryEnd is when a stream stopped running, or now while it runs.
func factoryEnd(s *factory.Stream, now time.Time) time.Time {
	if !s.Ended.IsZero() {
		return s.Ended
	}
	return now
}

// factoryElapsed is how long from start to end, and nothing when either is
// unknown or the span is under a minute.
func factoryElapsed(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return ""
	}
	d := end.Sub(start)
	if d < time.Minute {
		return ""
	}
	if d < time.Hour {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	h, m := int(d/time.Hour), int((d%time.Hour)/time.Minute)
	if m == 0 {
		return strconv.Itoa(h) + "h"
	}
	return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
}

// factoryAge is how long ago the item arrived, and nothing when it is not
// known.
func factoryAge(now, then time.Time) string {
	if now.IsZero() || then.IsZero() {
		return ""
	}
	return nextUpAge(now.Sub(then))
}

// factoryMinutes is a duration left, in whole minutes, never under one.
func factoryMinutes(d time.Duration) string {
	return strconv.Itoa(max(int(d/time.Minute), 1)) + "m"
}

// factoryOr is s, or the fallback when s is blank.
func factoryOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// nonEmpty is the strings that say something.
func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
