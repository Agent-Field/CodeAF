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
//	budget  $5  thinking  —                              the chips
//
//	● plan    ◐ write    ○ test    ○ review               the stages
//
//	review 1/2 · 3 findings · fixing                      a running stage's line
//
//	When the same entry id is added twice, Total …       the body, six rows at most
//
//	» chat                                                only when it has one
//
//	enter open · r run · T chat · space select            the strip, last row
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

// factoryPeekBodyRows is the most rows of the body the peek shows at once.
const factoryPeekBodyRows = 6

// factoryPeekCommentRows is the most rows one comment's words take in the
// peek; the item page shows each whole.
const factoryPeekCommentRows = 3

// The forge blocks' bounds: the last three comments, six files, five events.
const (
	factoryCommentsShown = 3
	factoryFilesShown    = 6
	factoryEventsShown   = 5
)

// factoryPane is the item under the cursor as EXACTLY room rows of EXACTLY
// width cells. A floor with no items, or no room, is room blank rows. The
// action line takes the last row whenever there are two rows or more, with a
// blank row above it from three.
func (a *app) factoryPane(width, room int) []string {
	if room <= 0 {
		return nil
	}
	lines := make([]string, room)
	// THE PEEK STOPS [factoryMargin] BEFORE THE FRAME'S EDGE, as it starts
	// [factoryMargin] past the divider (factory_grid.go's THE RIGHT MARGIN).
	if it, ok := a.factoryCursorItem(); ok && width > factoryMargins {
		copy(lines, a.factoryPeek(it, width-factoryMargins, room))
	}
	lead := factorySpaces(factoryMargin)
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
	action := a.pal.dim(a.factoryVerbLine(it, measure))
	avail := room - 1
	if room >= 3 {
		avail = room - factoryActionRows
	}
	before := [][]string{
		title,
		a.factoryPeekQuestion(it, measure, max(avail-len(title), factoryShapingMinRows)),
		a.factoryPeekRead(it, measure),
		a.factoryPeekFacts(it, measure),
		{a.factoryPeekChips(it, measure)},
		a.factoryPeekStages(it, measure),
		{a.factoryPeekState(it, measure)},
	}
	talk := [][]string{{a.factoryTalkRow(it, measure)}}
	used := len(factoryStack(append(append([][]string{}, before...), talk...)))
	blocks := before
	if bodyRoom := min(avail-used-1, factoryPeekBodyRows); bodyRoom > 0 {
		blocks = append(blocks, a.factoryPeekBody(it, measure, bodyRoom))
	}
	// THE FORGE'S BLOCKS FOLLOW THE BODY AND THE TALK ROW (comments, checks,
	// files, activity, links), and they are what a short peek cuts first:
	// the body and the conversation were handed their rows before them.
	blocks = append(append(blocks, talk...), a.factoryForgeBlocks(it, factoryPeekWidth(measure), factoryPeekCommentRows)...)
	lines := factoryStack(blocks)
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
// wider than [factoryProseW].
func factoryPeekWidth(measure int) int { return max(min(measure, factoryProseW), 1) }

// factoryPeekTitle is the title block: the ref and the title in ink, and under
// it the meta, dim. A title too wide for the column is cut on its own row;
// the meta drops its facts from the right before it is cut.
//
// AN ITEM WITH A PAGE ON ITS FORGE says so twice: its ref is a hyperlink to
// that page ([app.factoryRefLink]), and its meta ends `github ↗`, which is a
// link too and is the one fact the meta never drops.
func (a *app) factoryPeekTitle(it factory.Item, measure int) []string {
	pal := a.pal
	ref := it.Ref()
	title := fit(ref+" "+it.Title, measure)
	if strings.HasPrefix(ansi.Strip(title), ref) {
		title = a.factoryRefLink(it, pal.ink(ref)) + pal.ink(strings.TrimPrefix(title, ref))
	} else {
		title = pal.ink(title)
	}
	out := []string{title}
	tail, tailW := a.factoryGitHubMark(it)
	room := measure
	if tailW > 0 {
		room -= tailW + ansi.StringWidth(rowSep)
	}
	meta := factoryMetaLine(a.factoryMeta(it), max(room, 0))
	switch {
	case meta != "" && tailW > 0:
		out = append(out, pal.dim(meta+rowSep)+tail)
	case meta != "":
		out = append(out, pal.dim(meta))
	case tailW > 0 && tailW <= measure:
		out = append(out, tail)
	}
	return out
}

// factoryGitHubMark is `github ↗`, dim, as a hyperlink to the item's page on
// its forge, and its width; nothing for an item with no page there.
func (a *app) factoryGitHubMark(it factory.Item) (string, int) {
	if it.URL == "" {
		return "", 0
	}
	words := "github " + a.icon(tokens.GActionBrowse)
	painted := a.pal.dim(words)
	if a.pathLinks {
		painted = linkify(painted, it.URL)
	}
	return painted, ansi.StringWidth(words)
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
func (a *app) factoryPeekQuestion(it factory.Item, measure, room int) []string {
	if it.State != factory.StateNeedsYou {
		return nil
	}
	pal := a.pal
	if factoryIsShaping(it) {
		return a.factoryShapingBlock(it, measure, room)
	}
	var out []string
	if q := strings.TrimSpace(it.Question); q != "" {
		out = a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ink, factoryPeekWidth(measure))
	}
	if a.factory.Has("answer") {
		out = append(out, pal.dim(fit(factoryAnswerKeys, measure)))
	}
	return out
}

// factoryPeekRead is the factory's one-sentence read of the item, in ink with
// no mark before it, wrapped at the peek's measure.
//
// A READ STILL BEING MADE says so in its place, `⠋ reading…`, rather than
// leaving a hole the person would read as a floor that has nothing to say.
func (a *app) factoryPeekRead(it factory.Item, measure int) []string {
	if strings.TrimSpace(it.Triage.Read) == "" {
		if a.factoryRowWaits(it.ID) {
			return []string{a.pal.dim(fit(factoryWaitWords, measure))}
		}
		if word, busy := a.factoryBusy(it.ID); busy {
			return []string{a.pal.accent(a.factorySpin()) + " " + a.pal.dim(fit(word+"…", max(measure-factoryLeadW, 0)))}
		}
	}
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
	return []string{a.pal.dim(factoryJoinWhole(facts, facts, factorySpaces(factoryFactGap), measure))}
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

// factoryChip is one chip: its label, its value, the key that turns it, and
// the widest value it can hold, so its slot is one width on every item.
type factoryChip struct {
	label, value, key string
	valueW            int
}

// factoryChipList is the item's chips: the budget and the thinking, ALWAYS
// TWO, in that order, so each stands in its own slot on every item
// ([app.factoryChipRow]). Where the run holds for the person is an approve
// step among the stages, never a chip. THEIR WORDS ARE THE VOCABULARY'S
// (factory_words.go): `budget  $5`, `thinking  —`. THE
// THINKING CHIP READS THE STAGE `e` TURNS ([factoryEffortStage]), so the chip
// and the key are about the same stage, and it says a dash when that stage
// carries no word, which is the knee: the crew picks how hard to think for
// this class of work. A budget of nothing has an empty value, and its slot is
// drawn as air (the emptiness law).
func (a *app) factoryChipList(it factory.Item) []factoryChip {
	thinking := "—"
	stages := factoryStages(a.fp.snap, it)
	if at := factoryEffortStage(a.fp.snap, it); at >= 0 && at < len(stages) && stages[at].Effort != "" {
		thinking = stages[at].Effort
	}
	return []factoryChip{
		{wordBudget, factoryMoney(it.Cap), keyBudget, factoryChipValueW},
		{wordThinking, thinking, keyThinking, factoryChipValueW},
	}
}

// factoryChipRow is the chips as one row of FIXED SLOTS: each a muted label,
// [factoryLabelGap] of air, the value in ink padded to [factoryChipValueW],
// and with keys the dim key that turns it; slots [factoryFactGap] apart. A
// LABEL AND ITS VALUE START AT ONE CELL ON EVERY ITEM, whatever the chips
// before them say, so a column of items read down the peek reads as one
// table. A slot with no value is air of its own width. Slots are dropped from
// the right, whole, when the row is too narrow.
func (a *app) factoryChipRow(it factory.Item, keys bool, measure int) string {
	pal := a.pal
	var segs, plains []string
	for _, c := range a.factoryChipList(it) {
		valueW := c.valueW
		plain := c.label + factorySpaces(factoryLabelGap) + c.value
		seg := pal.muted(c.label) + factorySpaces(factoryLabelGap) + pal.ink(c.value)
		if keys {
			// The key follows the value it turns, and the cell is padded
			// after it, so the values still start at one cell.
			plain += " [" + c.key + "]"
			seg += " " + pal.dim("["+c.key+"]")
			valueW += len(" [") + len(c.key) + len("]")
		}
		if pad := ansi.StringWidth(c.label) + factoryLabelGap + valueW - ansi.StringWidth(plain); pad > 0 {
			plain, seg = plain+factorySpaces(pad), seg+factorySpaces(pad)
		}
		if strings.TrimSpace(c.value) == "" {
			seg = factorySpaces(ansi.StringWidth(plain))
		}
		segs, plains = append(segs, seg), append(plains, plain)
	}
	// The last slot's padding is trailing air nobody reads.
	return strings.TrimRight(factoryJoinWhole(segs, plains, factorySpaces(factoryChipGap), measure), " ")
}

// factoryChips is the item page's chips, with the key that turns each.
func (a *app) factoryChips(it factory.Item, keys bool) string {
	return a.factoryChipRow(it, keys, factoryPageProseW+factoryRailW)
}

// factoryPeekChips is the peek's chips, without keys: the peek is read, and
// the keys are on its last row and the hint line.
func (a *app) factoryPeekChips(it factory.Item, measure int) string {
	return a.factoryChipRow(it, false, measure)
}

// factoryPeekStages is the stages block: the strip, and under it what the
// plan stage changed when it changed something ([app.factoryAdaptedRow]).
func (a *app) factoryPeekStages(it factory.Item, measure int) []string {
	out := []string{a.factoryPeekStrip(it, measure)}
	// THE PHASE THAT STOPPED SAYS WHY UNDER THE STRIP, dim: a waiting phase's
	// note, or the latest failed one's (factory_run.go).
	if note := factoryPhaseNoteLine(it, factoryStages(a.fp.snap, it)); note != "" {
		out = append(out, a.pal.dim(fit(note, measure)))
	}
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

// factoryPeekStrip is the stage strip: for an item on a bench, each phase as
// its mark and its words, the running cell in the accent, done cells ink and
// the rest dim, a phase waiting on the person with its mark in amber; for an
// item with no stream yet, the stages it would run, each with the pending
// mark, dim. A STAGE SWITCHED OFF OR SKIPPED IS NOT ON THE STRIP: it will not
// run, and the item page says why.
//
// EVERY CELL OF ONE STRIP IS ONE WIDTH ([factoryStripCellW]), [factoryStripGap]
// apart, so the marks stand at even steps and the first cell is as wide as
// the rest (owner ruling, 2026-10-08); a name longer than the cell ends in an
// ellipsis. Cells are dropped whole when the row is too narrow, and A STRIP
// THAT DROPPED CELLS SAYS SO with an ellipsis at the end it lost them from.
// THE PHASE THAT IS MOVING IS ALWAYS ON THE ROW: the cells before it go first.
func (a *app) factoryPeekStrip(it factory.Item, measure int) string {
	pal := a.pal
	stages := factoryStages(a.fp.snap, it)
	var cells []factoryStripPart
	moving := -1
	if it.Stream != nil && len(it.Stream.Phases) > 0 {
		for i, ph := range it.Stream.Phases {
			// THE CELL'S MARK AND WORDS ARE THE MARKS' (factory_marks.go), so a
			// held, stopped or skipped stage reads as that here too; the minutes
			// left are the running line's, not the cell's.
			c := a.factoryPhasePart(it, i, factoryStageMax(stages, ph.Name))
			if moving < 0 && (ph.State == factory.PhaseRunning || ph.State == factory.PhaseWaiting) {
				moving = i
			}
			cells = append(cells, c)
		}
	} else {
		mark := a.factoryPendingMark()
		for _, st := range stages {
			if !st.On || !factory.Fits(st, it) {
				continue
			}
			words := st.Name
			if st.Max > 1 {
				words += " ×" + strconv.Itoa(st.Max)
			}
			cells = append(cells, factoryStripPart{mark: mark, words: words, markPaint: pal.dim, wordPaint: pal.dim})
		}
	}
	return a.factoryStripRow(cells, moving, measure)
}

// factoryStripPart is one cell of a stage strip before it is laid out: its
// mark and its words, and the paint of each.
type factoryStripPart struct {
	mark, words          string
	markPaint, wordPaint func(string) string
}

func (c factoryStripPart) plain() string { return c.mark + " " + c.words }

// factoryStripCellW is one strip's cell width: its longest cell, never wider
// than [factoryStripCellMax].
func factoryStripCellW(plains []string) int {
	w := 0
	for _, p := range plains {
		w = max(w, ansi.StringWidth(p))
	}
	return min(w, factoryStripCellMax)
}

// factoryStripRow lays the cells out in measure cells on one width
// ([factoryStripCellW]), the cell at moving (-1 for none) kept on the row.
func (a *app) factoryStripRow(cells []factoryStripPart, moving, measure int) string {
	if len(cells) == 0 || measure <= 0 {
		return ""
	}
	plains := make([]string, len(cells))
	for i, c := range cells {
		plains[i] = c.plain()
	}
	cellW := factoryStripCellW(plains)
	more := a.icon(tokens.GEllipsis)
	moreW := ansi.StringWidth(more)
	n := len(cells)
	// shown is how many cells from `from` fit, with room kept for the
	// ellipsis at each end that lost cells.
	shown := func(from int) int {
		lead := 0
		if from > 0 {
			lead = moreW + factoryStripGap
		}
		k := n - from
		for ; k > 1; k-- {
			w := lead + k*cellW + (k-1)*factoryStripGap
			if from+k < n {
				w += factoryStripGap + moreW
			}
			if w <= measure {
				break
			}
		}
		return k
	}
	from, k := 0, shown(0)
	for from < moving && moving >= from+k {
		from++
		k = shown(from)
	}
	gap := factorySpaces(factoryStripGap)
	var parts []string
	if from > 0 {
		parts = append(parts, a.pal.dim(more))
	}
	for _, c := range cells[from : from+k] {
		words := c.words
		if room := cellW - ansi.StringWidth(c.mark) - 1; ansi.StringWidth(words) > room {
			words = ansi.Truncate(words, max(room, 0), more)
		}
		seg := c.markPaint(c.mark) + " " + c.wordPaint(words)
		parts = append(parts, seg+factorySpaces(cellW-ansi.StringWidth(c.mark+" "+words)))
	}
	if from+k < n {
		parts = append(parts, a.pal.dim(more))
	}
	return strings.TrimRight(fit(strings.Join(parts, gap), measure), " ")
}

// factoryPeekState is the one line an item's state adds under its stages:
// the running stage's own line, what frees a queued item, and when a shipped
// one merged and what it cost. Every other state adds nothing.
func (a *app) factoryPeekState(it factory.Item, measure int) string {
	pal := a.pal
	switch it.State {
	case factory.StateRunning:
		if line := a.factoryStateLine(it); line != "" {
			return pal.muted(fit(line, measure))
		}
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
// `review 1/2 · 3 findings · fixing · 4m left`. It is the stage and its round,
// the phase's own note, the newest thing the stream said, with what the said
// line repeats of the first two left out, and the minutes it has left.
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
	if r := factoryRoundWords(ph.Round, factoryStageMax(stages, ph.Name)); r != "" {
		head += " " + r
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
	if ph.Left > 0 {
		add(factoryMinutes(ph.Left) + " left")
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
		out := a.factorySheetRows(it, measure)
		if len(out) > room {
			out = out[:room]
		}
		return out
	}
	return a.factoryScrolled(it, a.factoryMarkdown(it.Body, factoryPeekWidth(measure)), factoryPeekWidth(measure), room)
}

// factoryScrolled is lines, already painted, seen through a window of room
// rows from the item's scroll, the last row marked when more is below.
// The scroll is the item's: a cursor that moved to another item starts that
// item at its top. The window it drew is kept, so every road to the rest
// knows how far there is to go ([app.factoryScrollBy]).
//
// THE DRAW NEVER WRITES THE SCROLL BACK. A re-read that drew the body shorter
// for one beat, or a room that grew for one frame, shows the window at the
// end; the scroll a person made stands, and comes back when the body does. Only
// a cursor on another item starts it at the top.
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
	at := max(min(a.fp.scroll, a.fp.scrollMax), 0)
	window := lines[at : at+n]
	out := make([]string, 0, n)
	for i, line := range window {
		if i == n-1 && at+n < len(lines) {
			more := a.pal.dim(a.icon(tokens.GExpanded) + " more")
			cut := fit(line, max(measure-ansi.StringWidth(more)-factoryGutter-factoryLeadW, 1))
			// A CUT THAT FALLS ON A BLANK ROW puts the ellipsis at the margin,
			// not one cell into the air after it.
			if strings.TrimSpace(ansi.Strip(cut)) == "" {
				cut = ""
			} else {
				cut += " "
			}
			out = append(out, factorySpread(cut+a.pal.ink(a.icon(tokens.GEllipsis)), more, measure))
			continue
		}
		out = append(out, line)
	}
	return out
}

// factoryHasMore says whether a painted line ends with the dim `▾ more` that
// [app.factoryScrolled] puts on a cut body's last row: the row a press on the
// word scrolls a page from.
func (a *app) factoryHasMore(line string) bool {
	return strings.HasSuffix(strings.TrimRight(ansi.Strip(line), " "), a.icon(tokens.GExpanded)+" more")
}

// factoryTalkRow is the item's conversation, one row, only when the item has
// one: its mark, the word, and what is known about it.
func (a *app) factoryTalkRow(it factory.Item, measure int) string {
	if strings.TrimSpace(it.Talk) == "" {
		return ""
	}
	pal := a.pal
	row := pal.muted(a.icon(tokens.GActionCommunicate)) + " " + pal.ink(wordChat)
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
// round over its most where a stage may run more than one (`review 2/2`,
// factory_run.go's [factoryRoundWords]), how many tasks it split into, the
// minutes a running one has left, and a pending one's most rounds.
func factoryPhaseWords(ph factory.Phase, most int) string {
	words := ph.Name
	if ph.Round > 0 {
		if r := factoryRoundWords(ph.Round, most); r != "" {
			words += " " + r
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

// factoryActionWords is the peek's key line, whole: [app.factoryVerbRail]
// joined, the ONE list the bottom line also draws (factory_marks.go).
func (a *app) factoryActionWords(it factory.Item) string {
	return strings.Join(a.factoryVerbRail(it), rowSep)
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

// factoryClaimRow is one row of the proof sheet: its mark (`✓` shown, `✕` not,
// through the vocabulary's door), its words in ink, and at the right its
// evidence, dim, then its MEDIUM AS A CHIP in a slot of [factoryMediumW], so
// the evidence ends in one column down the sheet. A row with no medium wears
// tag (`policy`) in the slot, or air when it has no tag either. A claim
// nothing showed says so in its own row, in the words a person uses: NOT
// SHOWN.
func (a *app) factoryClaimRow(c factory.Claim, tag string, measure int) string {
	pal := a.pal
	left := pal.add(a.icon(tokens.GSettled)) + " " + pal.ink(c.Text)
	if !c.OK {
		left = pal.bad(a.icon(tokens.GFailed)) + " " + pal.ink(c.Text) + pal.bad(" — not shown")
	}
	medium := strings.TrimSpace(c.Medium)
	if medium == "" {
		medium = tag
	}
	var parts []string
	if ev := factoryEvidence(c.Evidence, medium); ev != "" {
		parts = append(parts, pal.dim(ev))
	}
	if c.Medium == "screenshot" {
		parts = append(parts, pal.accent("["+a.linearMark("▦", "#")+" view]"))
	}
	right := strings.Join(parts, " ")
	// A ROW WITH NO MEDIUM KEEPS THE CHIP'S SLOT AS AIR, so its evidence ends
	// in the column every other row's does.
	chip := factorySpaces(factoryMediumW)
	if medium != "" {
		chip = pal.muted(fit(medium, factoryMediumW)) + factorySpaces(factoryMediumW-ansi.StringWidth(fit(medium, factoryMediumW)))
	}
	switch {
	case right != "" && (medium != "" || ansi.StringWidth(left)+ansi.StringWidth(right)+factoryGutter*2+factoryMediumW <= measure):
		// The air is spent only where the row has room for it: a narrow
		// peek keeps the words `not shown` before it keeps a column.
		right += factorySpaces(factoryGutter) + chip
	case right == "" && medium != "":
		right = chip
	}
	return factorySpread(left, right, measure)
}

// factoryEvidence is a claim's evidence as the sheet draws it, WITHOUT THE
// MEDIUM IT REPEATS: the medium has its own column, so `test · 0.3s` under a
// `test` chip is `0.3s`, and evidence that is only the medium's name is none.
func factoryEvidence(evidence, medium string) string {
	ev := strings.TrimSpace(evidence)
	if medium == "" {
		return ev
	}
	if strings.EqualFold(ev, medium) {
		return ""
	}
	if head, rest, ok := strings.Cut(ev, rowSep); ok && strings.EqualFold(strings.TrimSpace(head), medium) {
		return strings.TrimSpace(rest)
	}
	return ev
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
	for i, line := range wrap(text, max(measure-factoryLeadW, factoryLedMin)) {
		if i == 0 {
			out = append(out, fit(mark+" "+paint(line), measure))
			continue
		}
		out = append(out, fit(factorySpaces(factoryLeadW)+paint(line), measure))
	}
	return out
}

// factorySpread is left words and right-aligned meta on one line of measure
// cells. When both do not fit with air between them the meta goes first, then
// the left words are cut: what the row is matters more than its metadata.
func factorySpread(left, right string, measure int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if right == "" || lw+factoryGutter+rw > measure {
		if right != "" && rw+factoryGutter+12 <= measure {
			left = fit(left, measure-rw-factoryGutter)
			lw = ansi.StringWidth(left)
		} else {
			return fit(left, measure)
		}
	}
	return left + factorySpaces(measure-lw-rw) + right
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
		return wordBudget + " " + capWord
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
