package tui3

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE PEEK ────────────────────────────────────────────────────────────────
//
// The right-hand column of the factory floor at [factoryPaneFloor] columns and
// wider: the item under the rows' cursor, as its own short document. The item
// page (factory_item.go) is where an item is seen whole; the peek is the same
// item in the shape a person reads without moving their eyes off the rows.
//
// THE PEEK IS THE ITEM'S DOCUMENT, AND WHITESPACE IS THE MATERIAL (owner
// ruling, 2026-10-08, after a denser sketch was turned down as "lines of
// paragraph"). It is a ladder of blocks, each one thing, with ONE BLANK ROW
// BETWEEN EVERY TWO BLOCKS and none at the top:
//
//	#1 Total double-counts an entry added twice          the title, in ink
//	factory-demo · bug · S · santosh · 8h                 its meta, dim
//
//	? plan is ready · go, or change it?                   needs you only
//	[y] yes · [n] no · [a] in words
//
//	Add appends without checking the id, so Total …       the read, in ink
//
//	touches money      maybe a duplicate of #7      thin  the facts, dim
//
//	gate  ship      cap  $5      effort  —                the chips
//
//	● plan    ◐ write    ○ test    ○ review               the stages
//
//	review 1/2 · 3 findings · fixing                      a running stage's line
//
//	When the same entry id is added twice, Total …       the body, six rows at most
//
//	» talk                                                only when it has one
//
//	enter open · space mark · d hide                      pinned to the last row
//
// AN EMPTY BLOCK VANISHES WITH ITS BLANK ROW, so a block that has nothing to
// say costs nothing, and nothing is drawn for a zero or an empty value
// anywhere (the emptiness law). A landed item's claims stand where the body
// would. The body scrolls with `J` and `K` (`pgdn` and `pgup` a page at a
// time), and a body cut short says so on its last row with a dim `▾ more`.
//
// THE PEEK IS PROSE AND ROWS, NOT BOXES (docs/DESIGN-LANGUAGE.md): no border,
// a 2-cell lead, no rule. THE PEEK IS A FUNCTION OF ITS WIDTH, ITS ROOM AND THE
// SNAPSHOT, and never the disk.
//
// THE KEYS ON ITS LAST ROW ARE DRAWN HERE AND ACTED ON ELSEWHERE: what each one
// does is the verbs' (factory_keys.go), which reads the same item and asks the
// seam's doors.

// factoryPaneLead is the peek's left margin: the spacing ladder's 2-cell lead.
const factoryPaneLead = 2

// factoryPaneGap is the least air between a row's left words and its
// right-aligned meta. Under it the meta is dropped rather than jammed.
const factoryPaneGap = 2

// factoryPeekWrap is the measure the peek's prose is wrapped at, however wide
// the column: a line of sixty cells is one a person reads without losing the
// start of the next, and a wider peek spends the rest as air.
const factoryPeekWrap = 60

// factoryPageWrap is the item page's measure for the whole issue, which is
// read there rather than glanced at.
const factoryPageWrap = 72

// factoryPeekBodyRows is the most rows of the body the peek shows at once.
const factoryPeekBodyRows = 6

// factoryFactGap is the air between two facts, and between two chips, on one
// row: wide enough that a row of three short phrases reads as three things
// without a separator mark between them (docs/DESIGN-LANGUAGE.md's spacing
// ladder names it).
const factoryFactGap = 6

// factoryStripGap is the air between two cells of the stage strip.
const factoryStripGap = 4

// factoryPane is the item under the cursor as EXACTLY room rows of EXACTLY
// width cells. A floor with no items, or no room, is room blank rows. The
// action line takes the last row whenever there are two rows or more, with a
// blank row above it from three.
func (a *app) factoryPane(width, room int) []string {
	if room <= 0 {
		return nil
	}
	lines := make([]string, room)
	if it, ok := a.factoryCursorItem(); ok && width > factoryPaneLead {
		copy(lines, a.factoryPeek(it, width-factoryPaneLead, room))
	}
	lead := strings.Repeat(" ", factoryPaneLead)
	out := make([]string, room)
	for i, line := range lines {
		row := ""
		if line != "" {
			row = lead + line
		}
		out[i] = factoryPad(row, width)
	}
	return out
}

// factoryPeek is the peek's ladder as at most room lines of at most measure
// cells, the action line on the last of them.
//
// THE BODY IS WHAT GIVES WAY. Every other block is a row or two and says what
// the item is; the body is the one that can be long, so it is handed whatever
// the other blocks leave, never more than [factoryPeekBodyRows], and a room too
// short for the rest cuts the ladder from its foot.
func (a *app) factoryPeek(it factory.Item, measure, room int) []string {
	title := a.factoryPeekTitle(it, measure)
	if room == 1 {
		return title[:1]
	}
	action := a.pal.dim(fit(a.factoryActionWords(it), measure))
	avail := room - 1
	if room >= 3 {
		avail = room - 2
	}
	before := [][]string{
		title,
		a.factoryPeekQuestion(it, measure),
		a.factoryPeekRead(it, measure),
		a.factoryPeekFacts(it, measure),
		{a.factoryPeekChips(it, measure)},
		a.factoryPeekStages(it, measure),
		{a.factoryPeekState(it, measure)},
	}
	after := [][]string{{a.factoryTalkRow(it, measure)}}
	used := len(factoryStack(append(append([][]string{}, before...), after...)))
	blocks := before
	if bodyRoom := min(avail-used-1, factoryPeekBodyRows); bodyRoom > 0 {
		blocks = append(blocks, a.factoryPeekBody(it, measure, bodyRoom))
	}
	lines := factoryStack(append(blocks, after...))
	if len(lines) > avail {
		lines = lines[:avail]
	}
	out := make([]string, room)
	copy(out, lines)
	out[room-1] = action
	return out
}

// factoryStack joins blocks top to bottom with ONE BLANK ROW BETWEEN EVERY
// TWO, none at the top and none at the foot. A block with nothing in it, or
// only blank rows, vanishes with its blank row, so A GAP ASKED FOR TWICE IS
// STILL ONE GAP (docs/DESIGN-LANGUAGE.md).
func factoryStack(blocks [][]string) []string {
	var out []string
	for _, b := range blocks {
		if len(nonEmpty(b)) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, b...)
	}
	return out
}

// factoryPeekWidth is the width the peek's prose wraps at: the column, never
// wider than [factoryPeekWrap].
func factoryPeekWidth(measure int) int { return max(min(measure, factoryPeekWrap), 1) }

// factoryPeekTitle is the title block: the ref and the title in ink, and under
// it the meta, dim. A title too wide for the column is cut on its own row;
// the meta drops its facts from the right before it is cut.
func (a *app) factoryPeekTitle(it factory.Item, measure int) []string {
	pal := a.pal
	out := []string{pal.ink(fit(it.Ref()+" "+it.Title, measure))}
	if meta := factoryMetaLine(a.factoryMeta(it), measure); meta != "" {
		out = append(out, pal.dim(meta))
	}
	return out
}

// factoryMeta is what an item is beside its title: the repo, the kind, its
// size, its author and how long ago it arrived. A fact that is not known is
// left out.
func (a *app) factoryMeta(it factory.Item) []string {
	return nonEmpty([]string{factoryRepoShort(it.Repo), string(it.Kind), it.Triage.Size, it.Author, factoryAge(a.fp.snap.Now, it.Created)})
}

// factoryMetaLine is the meta as one line of at most measure cells, its facts
// dropped from the right, whole, until it fits.
func factoryMetaLine(meta []string, measure int) string {
	for n := len(meta); n > 0; n-- {
		if line := strings.Join(meta[:n], rowSep); ansi.StringWidth(line) <= measure {
			return line
		}
	}
	if len(meta) == 0 {
		return ""
	}
	return fit(meta[0], measure)
}

// factoryPeekQuestion is a needs-you item's question block, directly under the
// title: the question led by its mark and its keys under it. THE AMBER IS ON
// THE MARK AND NOWHERE ELSE (docs/DESIGN-LANGUAGE.md's COLOUR IS STROKE, NEVER
// FILL); the words are ink and the keys dim.
func (a *app) factoryPeekQuestion(it factory.Item, measure int) []string {
	if it.State != factory.StateNeedsYou {
		return nil
	}
	pal := a.pal
	var out []string
	if q := strings.TrimSpace(it.Question); q != "" {
		out = a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ink, factoryPeekWidth(measure))
	}
	return append(out, pal.dim(fit(factoryAnswerKeys, measure)))
}

// factoryPeekRead is the factory's one-sentence read of the item, in ink with
// no mark before it, wrapped at the peek's measure.
func (a *app) factoryPeekRead(it factory.Item, measure int) []string {
	var out []string
	for _, line := range wrap(strings.TrimSpace(it.Triage.Read), factoryPeekWidth(measure)) {
		if strings.TrimSpace(line) != "" {
			out = append(out, a.pal.ink(line))
		}
	}
	return out
}

// factoryPeekFacts is the triage facts on one dim row with no labels and no
// colons, [factoryFactGap] cells apart, dropped from the right whole when the
// row is too narrow for all of them.
func (a *app) factoryPeekFacts(it factory.Item, measure int) []string {
	facts := factoryFacts(it)
	if len(facts) == 0 {
		return nil
	}
	return []string{a.pal.dim(factoryJoinWhole(facts, facts, strings.Repeat(" ", factoryFactGap), measure))}
}

// factoryFacts is what the triage read noticed about an item, in the words a
// person would say: what risky ground it touches, which item it may repeat,
// that it is thin, and that its author is a stranger. A fact nobody knows is
// not drawn.
func factoryFacts(it factory.Item) []string {
	facts := factoryRiskWords(it.Triage.Risk)
	if dup := factoryDupWord(it); dup != "" {
		facts = append(facts, "maybe a duplicate of "+dup)
	}
	if r := it.Triage.Readiness; r > 0 && r < factory.ThinReadiness {
		facts = append(facts, "thin")
	}
	if it.Tier == factory.TierStranger {
		facts = append(facts, "stranger")
	}
	return facts
}

// factoryRiskWords is the triage read's risk as facts. IT READS EITHER SHAPE
// THE READ HAS HAD, because the field is moving under this file: a list of the
// risky things an item touches (`money`, `auth`), each said as `touches money`,
// or the older single word, where only `mid` and `high` are worth a row
// (`high risk`) and `low` is the absence of one.
func factoryRiskWords[R string | []string](risk R) []string {
	var words []string
	switch r := any(risk).(type) {
	case string:
		switch w := strings.TrimSpace(r); w {
		case "", "low", "none":
		default:
			words = append(words, w+" risk")
		}
	case []string:
		for _, w := range r {
			w = strings.TrimSpace(w)
			switch {
			case w == "":
			case strings.Contains(w, " "):
				words = append(words, w)
			default:
				words = append(words, "touches "+w)
			}
		}
	}
	return words
}

// factoryDupWord is the item an item may repeat, as a ref (`#7`), from the
// read's word or its number, and "" when neither names one.
func factoryDupWord(it factory.Item) string {
	if d := strings.TrimSpace(it.Triage.Dup); d != "" {
		if _, err := strconv.Atoi(d); err == nil {
			return "#" + d
		}
		return d
	}
	if it.Triage.DupOf > 0 {
		return "#" + strconv.Itoa(it.Triage.DupOf)
	}
	return ""
}

// factoryChip is one chip: its label, its value and the key that turns it.
type factoryChip struct{ label, value, key string }

// factoryChipList is the item's chips: the gate, the cap and the effort. THE
// EFFORT CHIP READS THE STAGE `e` TURNS ([factoryEffortStage]), so the chip and
// the key are about the same stage, and it says a dash when that stage carries
// no word, which is the knee: the crew picks the effort it would pick for this
// class of work. A cap of nothing is no cap chip (the emptiness law).
func (a *app) factoryChipList(it factory.Item) []factoryChip {
	var chips []factoryChip
	if it.Gate != "" {
		chips = append(chips, factoryChip{"gate", string(it.Gate), "t"})
	}
	if c := factoryMoney(it.Cap); c != "" {
		chips = append(chips, factoryChip{"cap", c, "c"})
	}
	effort := "—"
	stages := factoryStages(a.fp.snap, it)
	if at := factoryEffortStage(a.fp.snap, it); at >= 0 && at < len(stages) && stages[at].Effort != "" {
		effort = stages[at].Effort
	}
	return append(chips, factoryChip{"effort", effort, "e"})
}

// factoryChips is the item page's chips, muted labels and ink values with the
// dim key that turns each when keys is set.
func (a *app) factoryChips(it factory.Item, keys bool) string {
	pal := a.pal
	var parts []string
	for _, c := range a.factoryChipList(it) {
		s := pal.muted(c.label) + " " + pal.ink(c.value)
		if keys {
			s += " " + pal.dim("["+c.key+"]")
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, pal.dim(rowSep))
}

// factoryPeekChips is the peek's chips: `label  value`, the label dim and the
// value ink, [factoryFactGap] cells apart, and no key hints — the peek is read,
// and the keys are on its last row and the hint line.
func (a *app) factoryPeekChips(it factory.Item, measure int) string {
	pal := a.pal
	var segs, plains []string
	for _, c := range a.factoryChipList(it) {
		segs = append(segs, pal.dim(c.label)+"  "+pal.ink(c.value))
		plains = append(plains, c.label+"  "+c.value)
	}
	return factoryJoinWhole(segs, plains, strings.Repeat(" ", factoryFactGap), measure)
}

// factoryPeekStages is the stages block: the strip, and under it what the
// plan stage changed when it changed something ([app.factoryAdaptedRow]).
func (a *app) factoryPeekStages(it factory.Item, measure int) []string {
	out := []string{a.factoryPeekStrip(it, measure)}
	if adapted := a.factoryAdaptedRow(it, measure); adapted != "" {
		out = append(out, adapted)
	}
	return out
}

// factoryAdaptedRow is what the plan stage changed about the item's stages,
// one dim line drawn under the stages on the peek and the item page:
// `plan added security · skipped neaten · why: touches billing`. AN ITEM PLAN
// NEVER CHANGED DRAWS NOTHING, not a line saying so (the emptiness law).
func (a *app) factoryAdaptedRow(it factory.Item, measure int) string {
	line := factory.AdaptedLine(it)
	if line == "" || measure <= 0 {
		return ""
	}
	return a.pal.dim(fit(line, measure))
}

// factoryPeekStrip is the stage strip, cells [factoryStripGap] apart: for an
// item on a bench, each phase as its mark and its words, the running cell in
// the accent, done cells ink and the rest dim, a phase waiting on the person
// with its mark in amber; for an item with no stream yet, the stages it would
// run, each with the pending mark, dim. A STAGE SWITCHED OFF OR SKIPPED IS NOT
// ON THE STRIP: it will not run, and the item page says why. Cells are dropped
// from the right, whole, when the row is too narrow, and THE PHASE THAT IS
// MOVING IS ALWAYS ON THE ROW.
func (a *app) factoryPeekStrip(it factory.Item, measure int) string {
	pal := a.pal
	stages := factoryStages(a.fp.snap, it)
	gap := strings.Repeat(" ", factoryStripGap)
	var segs, plains []string
	if it.Stream != nil && len(it.Stream.Phases) > 0 {
		for _, ph := range it.Stream.Phases {
			mark, paint := a.factoryPhaseMark(ph.State)
			words := factoryPhaseWords(ph, factoryStageMax(stages, ph.Name))
			var seg string
			switch ph.State {
			case factory.PhaseRunning:
				seg = pal.accent(mark + " " + words)
			case factory.PhaseDone:
				seg = paint(mark) + " " + pal.ink(words)
			case factory.PhaseWaiting:
				seg = pal.ask(mark) + " " + pal.ink(words)
			case factory.PhaseFailed:
				seg = paint(mark) + " " + pal.ink(words)
			default:
				seg = pal.dim(mark + " " + words)
			}
			segs = append(segs, seg)
			plains = append(plains, mark+" "+words)
		}
		// Phases before the moving one are dropped from the left until it
		// fits, because a strip cut from the right lost the very phase a
		// person looks for.
		from, at := 0, -1
		for i, ph := range it.Stream.Phases {
			if ph.State == factory.PhaseRunning || ph.State == factory.PhaseWaiting {
				at = i
				break
			}
		}
		for from < at {
			w := 0
			for i := from; i <= at; i++ {
				w += ansi.StringWidth(plains[i]) + factoryStripGap
			}
			if w-factoryStripGap <= measure {
				break
			}
			from++
		}
		return factoryJoinWhole(segs[from:], plains[from:], gap, measure)
	}
	mark := a.factoryPendingMark()
	for _, st := range stages {
		if !st.On || !factory.Fits(st, it) {
			continue
		}
		words := st.Name
		if st.Max > 1 {
			words += " ×" + strconv.Itoa(st.Max)
		}
		segs = append(segs, pal.dim(mark+" "+words))
		plains = append(plains, mark+" "+words)
	}
	return factoryJoinWhole(segs, plains, gap, measure)
}

// factoryPeekState is the one line an item's state adds under its stages:
// the running stage's own line, what frees a queued item, and when a shipped
// one merged and what it cost. Every other state adds nothing.
func (a *app) factoryPeekState(it factory.Item, measure int) string {
	pal := a.pal
	switch it.State {
	case factory.StateRunning:
		if line := factoryRunningLine(it, factoryStages(a.fp.snap, it)); line != "" {
			return pal.muted(fit(line, measure))
		}
	case factory.StateQueued:
		return pal.dim(fit("queued · benches full · a bench frees it", measure))
	case factory.StateShipped:
		if line := factoryMergedLine(it); line != "" {
			return pal.muted(fit(line, measure))
		}
	}
	return ""
}

// factoryRunningLine is what the running stage is doing, as one line:
// `review 1/2 · 3 findings · fixing`. It is the stage and its round, the
// phase's own note, and the newest thing the stream said, with what the said
// line repeats of the first two left out.
func factoryRunningLine(it factory.Item, stages []factory.Stage) string {
	s := it.Stream
	if s == nil {
		return ""
	}
	var ph factory.Phase
	found := false
	for _, p := range s.Phases {
		if p.State == factory.PhaseRunning {
			ph, found = p, true
			break
		}
	}
	if !found {
		return ""
	}
	head := ph.Name
	if ph.Round > 0 {
		head += " " + strconv.Itoa(ph.Round) + "/" + strconv.Itoa(max(factoryStageMax(stages, ph.Name), 1))
	}
	parts := []string{head}
	seen := map[string]bool{head: true}
	add := func(words string) {
		for _, w := range strings.Split(words, rowSep) {
			if w = strings.TrimSpace(w); w != "" && !seen[w] {
				seen[w] = true
				parts = append(parts, w)
			}
		}
	}
	add(ph.Note)
	if n := len(s.Log); n > 0 {
		said := strings.TrimSpace(s.Log[n-1].Text)
		said = strings.TrimSpace(strings.TrimPrefix(said, head+":"))
		add(said)
	}
	return strings.Join(parts, rowSep)
}

// factoryPeekBody is the body block, at most room rows: a landed item's
// claims and policy rows, and every other item's description wrapped at the
// peek's measure, from where `J` and `K` left it. A description longer than the
// room ends its last row with the ellipsis and a dim `▾ more` at the right.
func (a *app) factoryPeekBody(it factory.Item, measure, room int) []string {
	if room <= 0 {
		return nil
	}
	if it.State == factory.StateLanded {
		var out []string
		for _, c := range it.Proof {
			out = append(out, a.factoryClaimRow(c, "", measure))
		}
		for _, c := range it.Policy {
			out = append(out, a.factoryClaimRow(c, "policy", measure))
		}
		if len(out) > room {
			out = out[:room]
		}
		return out
	}
	return a.factoryScrolled(it, factoryBodyLines(it.Body, factoryPeekWidth(measure)), factoryPeekWidth(measure), room)
}

// factoryScrolled is lines seen through a window of room rows from the
// item's scroll, each painted ink, the last row marked when more is below.
// The scroll is the item's: a cursor that moved to another item starts that
// item at its top. The window it drew is kept, so `J`, `K` and the page keys
// know how far there is to go ([app.factoryScroll]).
func (a *app) factoryScrolled(it factory.Item, lines []string, measure, room int) []string {
	if len(lines) == 0 || room <= 0 {
		a.fp.scrollMax = 0
		return nil
	}
	if a.fp.scrollID != it.ID {
		a.fp.scrollID, a.fp.scroll = it.ID, 0
	}
	n := min(room, len(lines))
	a.fp.scrollMax, a.fp.scrollPage = len(lines)-n, n
	a.fp.scroll = max(min(a.fp.scroll, a.fp.scrollMax), 0)
	window := lines[a.fp.scroll : a.fp.scroll+n]
	out := make([]string, 0, n)
	for i, line := range window {
		if i == n-1 && a.fp.scroll+n < len(lines) {
			more := a.pal.dim(a.icon(tokens.GExpanded) + " more")
			cut := fit(line, max(measure-ansi.StringWidth(more)-factoryPaneGap-2, 1))
			out = append(out, factorySpread(a.pal.ink(cut+" "+a.icon(tokens.GEllipsis)), more, measure))
			continue
		}
		out = append(out, a.pal.ink(line))
	}
	return out
}

// factoryTalkRow is the item's conversation, one row, only when the item has
// one: its mark, the word, and what is known about it.
func (a *app) factoryTalkRow(it factory.Item, measure int) string {
	if strings.TrimSpace(it.Talk) == "" {
		return ""
	}
	pal := a.pal
	row := pal.muted(a.icon(tokens.GActionCommunicate)) + " " + pal.ink("talk")
	if facts := a.factoryTalkFacts(it); len(facts) > 0 {
		row += pal.dim(rowSep + strings.Join(facts, rowSep))
	}
	return fit(row, measure)
}

// factoryTalkFacts is what is known about an item's conversation beyond its
// being there — how many turns, how long since the last — and nothing while
// the snapshot does not carry it. The talk row draws whatever this says.
func (a *app) factoryTalkFacts(it factory.Item) []string { return nil }

// factoryPhaseWords is a phase's name with what is countable about it: its
// round over its most when it has one, how many tasks it split into, the
// minutes a running one has left, and a pending one's most rounds.
func factoryPhaseWords(ph factory.Phase, most int) string {
	words := ph.Name
	if ph.Round > 0 {
		if most > 0 {
			words += " " + strconv.Itoa(ph.Round) + "/" + strconv.Itoa(most)
		} else {
			words += " " + strconv.Itoa(ph.Round)
		}
	} else if ph.State == factory.PhasePending && most > 1 {
		words += " ×" + strconv.Itoa(most)
	}
	if ph.Tasks > 1 {
		words += " ×" + strconv.Itoa(ph.Tasks)
	}
	if ph.State == factory.PhaseRunning && ph.Left > 0 {
		words += " · " + factoryMinutes(ph.Left) + " left"
	}
	return words
}

// factoryStageMax is the most rounds the stage named name may take, and 0 when
// the item runs no stage of that name.
func factoryStageMax(stages []factory.Stage, name string) int {
	for _, st := range stages {
		if st.Name == name {
			return max(st.Max, 1)
		}
	}
	return 0
}

// factoryJoinWhole joins painted segments with sep, keeping as many WHOLE
// segments from the left as fit in measure cells. plains are the segments'
// own widths' source, so the paint never changes what fits.
func factoryJoinWhole(segs, plains []string, sep string, measure int) string {
	sepW := ansi.StringWidth(sep)
	out, used := "", 0
	for i, seg := range segs {
		w := ansi.StringWidth(plains[i])
		if i > 0 {
			w += sepW
		}
		if used+w > measure {
			if i == 0 {
				return fit(seg, measure)
			}
			break
		}
		if i > 0 {
			out += sep
		}
		out += seg
		used += w
	}
	return out
}

// factoryAnswerKeys is the question's keys, as the peek and the item page
// both name them.
const factoryAnswerKeys = "[y] yes · [n] no · [a] in words"

// factoryMergedLine is a shipped item's one line: when it merged and what it
// cost, and nothing when neither is known.
func factoryMergedLine(it factory.Item) string {
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
	if spent > 0 {
		facts = append(facts, dollars(spent))
	}
	return strings.Join(facts, rowSep)
}

// factoryActionWords is the one dim line that names the keys that apply to an
// item where it stands, in the hint grammar (`key verb · key verb`). THE KEYS
// ARE THE VERBS' (factory_keys.go); this line only says them. `enter` OPENS
// THE ITEM PAGE ON EVERY ROW and never launches; a new item runs on `r` or `p`,
// and a landed one ships from its proof on the item page.
//
// `r run` AND `p plan first` ARE SAID ONLY WHERE A LAUNCH STANDS BEHIND THEM,
// asked of the same predicate the hint line asks ([app.factoryCanRun]). The
// person's own floor has no engine door yet (factory.go's LocalSeam), and a
// peek that offered `r run` there named a key that does nothing, in the place
// a person looks for what to press next.
func (a *app) factoryActionWords(it factory.Item) string {
	switch it.State {
	case factory.StateNeedsYou:
		return "enter open · y n answer · a in words · x stop"
	case factory.StateRunning:
		return "enter open · s steer · p pause · x stop"
	case factory.StateQueued:
		return "enter open · x stop"
	case factory.StateLanded:
		if factoryFirstFailed(it) != "" {
			return "enter open · a ship anyway · c send back · o check again"
		}
		return "enter open · c send back · o check again"
	case factory.StateShipped:
		return "enter open"
	}
	words := []string{"enter open"}
	if a.factoryCanRun() {
		words = append(words, "r run", "p plan first")
	}
	return strings.Join(append(words, "space mark", "d hide"), " · ")
}

// ── the pieces ──────────────────────────────────────────────────────────────

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
