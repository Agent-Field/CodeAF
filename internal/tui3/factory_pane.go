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
// wider: the item under the rows' cursor, at a glance. The item page
// (factory_item.go) is where an item is seen whole; the peek is the same item
// in the shape a person can read without moving their eyes off the rows.
//
// ONE SHAPE FOR EVERY STATE. Five fixed rows, so a person's eye learns where
// each answer is and finds it there on every item:
//
//  1. the ref and the title, and at the right repo · kind · author · age · origin
//  2. the factory's read of it, led by the thought mark
//  3. the chips: gate, cap and effort
//  4. the phase strip for anything on a bench, or the stages a new item would run
//  5. the one rule
//
// then a tail that is the state's own (the question, the log, the claims, the
// body, the merge), and the line naming the keys that apply PINNED TO THE LAST
// ROW, so it is in the same place whatever the tail held.
//
// THE PEEK IS PROSE AND ROWS, NOT BOXES (docs/DESIGN-LANGUAGE.md): no border, a
// 2-cell lead, and one hairline. THE PEEK IS A FUNCTION OF ITS WIDTH AND ITS
// ROOM AND NOTHING ELSE, and it reads the snapshot the page folded in and never
// the disk.
//
// THE KEYS ON ITS LAST ROW ARE DRAWN HERE AND ACTED ON ELSEWHERE: what each one
// does is the verbs' (factory_keys.go), which reads the same item and asks the
// seam's doors. The hint line under the page is the one that names only the
// keys the seam has; the peek draws the layout whole.

// factoryPaneLead is the peek's left margin: the spacing ladder's 2-cell lead.
const factoryPaneLead = 2

// factoryPaneGap is the least air between a row's left words and its
// right-aligned meta. Under it the meta is dropped rather than jammed.
const factoryPaneGap = 2

// factoryPaneFixed is the peek's fixed rows, the rule included.
const factoryPaneFixed = 5

// factoryPane is the item under the cursor as EXACTLY room rows of EXACTLY
// width cells. A floor with no items, or no room, is room blank rows. The
// action line takes the last row whenever there are two rows or more.
func (a *app) factoryPane(width, room int) []string {
	if room <= 0 {
		return nil
	}
	lines := make([]string, room)
	if it, ok := a.factoryCursorItem(); ok && width > factoryPaneLead {
		measure := width - factoryPaneLead
		top := a.factoryPeekFixed(it, measure)
		action := a.pal.dim(fit(factoryActionWords(it), measure))
		body := room
		if room >= 2 {
			body = room - 1
			lines[room-1] = action
		}
		n := 0
		for _, line := range top {
			if n >= body {
				break
			}
			lines[n] = line
			n++
		}
		for _, line := range a.factoryPeekTail(it, measure, body-n) {
			if n >= body {
				break
			}
			lines[n] = line
			n++
		}
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

// factoryPeekFixed is the peek's five fixed rows. A row with nothing to say is
// blank rather than closed up, so row four is row four on every item.
func (a *app) factoryPeekFixed(it factory.Item, measure int) []string {
	pal := a.pal
	read := ""
	if r := strings.TrimSpace(it.Triage.Read); r != "" {
		read = fit(pal.muted(a.icon(tokens.GThought))+" "+pal.ink(r), measure)
	}
	return []string{
		factorySpread(pal.ink(it.Ref()+" "+it.Title), pal.muted(strings.Join(a.factoryMeta(it), rowSep)), measure),
		read,
		fit(a.factoryChips(it, false), measure),
		a.factoryPeekStrip(it, measure),
		pal.dim(strings.Repeat(a.linearMark("─", "-"), measure)),
	}
}

// factoryMeta is what an item is beside its title: the repo, the kind, the
// author and how far they are trusted, how long ago it arrived, and where it
// came from. A fact that is not known is left out.
func (a *app) factoryMeta(it factory.Item) []string {
	meta := []string{factoryRepoShort(it.Repo), string(it.Kind), factoryAuthor(it)}
	meta = append(meta, factoryAge(a.fp.snap.Now, it.Created), a.factoryOrigin(it))
	return nonEmpty(meta)
}

// factoryChips is the item's chips: the gate, the cap and the effort, muted
// labels and ink values. keys adds the dim key that turns each one, which the
// item page draws and the peek does not. THE EFFORT CHIP READS THE STAGE `e`
// TURNS ([factoryEffortStage]), so the chip and the key are about the same
// stage, and it says a dash when that stage carries no word, which is the
// knee: the crew picks the effort it would pick for this class of work. A cap
// of nothing is no cap chip (the emptiness law).
func (a *app) factoryChips(it factory.Item, keys bool) string {
	pal := a.pal
	chip := func(label, value, key string) string {
		s := pal.muted(label) + " " + pal.ink(value)
		if keys {
			s += " " + pal.dim("["+key+"]")
		}
		return s
	}
	chips := []string{}
	if it.Gate != "" {
		chips = append(chips, chip("gate", string(it.Gate), "t"))
	}
	if c := factoryMoney(it.Cap); c != "" {
		chips = append(chips, chip("cap", c, "c"))
	}
	effort := "—"
	stages := factoryStages(a.fp.snap, it)
	if at := factoryEffortStage(a.fp.snap, it); at >= 0 && at < len(stages) && stages[at].Effort != "" {
		effort = stages[at].Effort
	}
	chips = append(chips, chip("effort", effort, "e"))
	return strings.Join(chips, pal.dim(rowSep))
}

// factoryPeekStrip is row four: for an item on a bench, each phase as its mark
// and its name, `●` done, `◐` running with its round and the minutes it has
// left, `○` pending with its most rounds; for an item with no stream yet, the
// stages it would run as one line of names, a stage switched off or skipped
// dim. SEGMENTS ARE DROPPED FROM THE RIGHT, WHOLE, when the row is too narrow.
func (a *app) factoryPeekStrip(it factory.Item, measure int) string {
	pal := a.pal
	stages := factoryStages(a.fp.snap, it)
	var segs, plains []string
	if it.Stream != nil && len(it.Stream.Phases) > 0 {
		for _, ph := range it.Stream.Phases {
			mark, paint := a.factoryPhaseMark(ph.State)
			words := factoryPhaseWords(ph, factoryStageMax(stages, ph.Name))
			name := pal.muted(words)
			switch ph.State {
			case factory.PhaseRunning:
				name = pal.ink(words)
			case factory.PhasePending:
				name = pal.dim(words)
			case factory.PhaseWaiting:
				name = pal.ask(words)
			}
			segs = append(segs, paint(mark)+" "+name)
			plains = append(plains, mark+" "+words)
		}
		// THE PHASE THAT IS MOVING IS ALWAYS ON THE ROW: the running one, or
		// the one waiting on the person. Phases before it are dropped from the
		// left until it fits, as the item page's stage strip does, because a
		// strip cut from the right lost the very phase a person looks for.
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
				w += ansi.StringWidth(plains[i]) + 2
			}
			if w-2 <= measure {
				break
			}
			from++
		}
		return factoryJoinWhole(segs[from:], plains[from:], "  ", measure)
	}
	for _, st := range stages {
		if st.On && factory.Fits(st, it) {
			segs = append(segs, pal.muted(st.Name))
		} else {
			segs = append(segs, pal.dim(st.Name))
		}
		plains = append(plains, st.Name)
	}
	return factoryJoinWhole(segs, plains, pal.dim(rowSep), measure)
}

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

// factoryPeekTail is the state's own rows under the rule, at most room of
// them.
//
//	needs you  the question, then `[y] yes · [n] no · [a] in words`
//	running    the newest log lines, the activity and spend on the first
//	queued     what frees it
//	landed     a row per claim, then the policy rows
//	new        two lines of the body, the author's questions when thin, the
//	           stranger note when the author is one
//	shipped    when it merged and what it cost
func (a *app) factoryPeekTail(it factory.Item, measure, room int) []string {
	if room <= 0 {
		return nil
	}
	pal := a.pal
	var out []string
	switch it.State {
	case factory.StateNeedsYou:
		if q := strings.TrimSpace(it.Question); q != "" {
			out = append(out, a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ask, measure)...)
		}
		out = append(out, pal.dim(fit(factoryAnswerKeys, measure)))
	case factory.StateRunning:
		out = a.factoryPeekLog(it, measure, room)
	case factory.StateQueued:
		out = append(out, pal.dim(fit("queued · benches full · a bench frees it", measure)))
	case factory.StateLanded:
		for _, c := range it.Proof {
			out = append(out, a.factoryClaimRow(c, "", measure))
		}
		for _, c := range it.Policy {
			out = append(out, a.factoryClaimRow(c, "policy", measure))
		}
	case factory.StateShipped:
		if line := factoryMergedLine(it); line != "" {
			out = append(out, pal.muted(fit(line, measure)))
		}
	default:
		out = a.factoryPeekNew(it, measure)
	}
	if len(out) > room {
		out = out[:room]
	}
	return out
}

// factoryAnswerKeys is the question's keys, as the peek and the item page
// both name them.
const factoryAnswerKeys = "[y] yes · [n] no · [a] in words"

// factoryPeekLog is a running item's newest log lines that fit in room, oldest
// first, with the stream's activity as a sparkline and its spend at the right
// of the first of them.
func (a *app) factoryPeekLog(it factory.Item, measure, room int) []string {
	s := it.Stream
	if s == nil {
		return nil
	}
	pal := a.pal
	log := s.Log
	if len(log) > room {
		log = log[len(log)-room:]
	}
	right := a.factoryActivity(s)
	if m := factoryMoneyFact(it); m != "" {
		right = strings.TrimSpace(right + "  " + placeMoneyInk(pal)(m))
	}
	var out []string
	for i, l := range log {
		if i == 0 && right != "" {
			out = append(out, a.factoryLogSpread(l, right, measure))
			continue
		}
		out = append(out, a.factoryLogLine(l, measure))
	}
	if len(out) == 0 && right != "" {
		out = append(out, factorySpread("", right, measure))
	}
	return out
}

// factoryLogSpread is a log line with right-aligned meta, the line cut first
// so the meta keeps its cells.
func (a *app) factoryLogSpread(l factory.LogLine, right string, measure int) string {
	rw := ansi.StringWidth(right)
	if rw+factoryPaneGap+12 > measure {
		return a.factoryLogLine(l, measure)
	}
	left := a.factoryLogLine(l, measure-rw-factoryPaneGap)
	return left + strings.Repeat(" ", measure-ansi.StringWidth(left)-rw) + right
}

// factoryActivity is a stream's activity, oldest first, as one sparkline cell
// per beat in muted, and nothing for a stream that has done nothing.
func (a *app) factoryActivity(s *factory.Stream) string {
	steps := factorySparkSteps
	if a.linear || a.pal.ascii {
		steps = factorySparkASCII
	}
	top := len(steps) - 1
	any := false
	cells := make([]string, 0, len(s.Activity))
	for _, n := range s.Activity {
		any = any || n > 0
		cells = append(cells, steps[min(top, max(0, n))])
	}
	if !any {
		return ""
	}
	return a.pal.muted(strings.Join(cells, ""))
}

// factoryPeekNew is a new item's tail: two lines of its body, the questions
// the factory would put to the author when it is thin, and the stranger note
// when the author is one.
func (a *app) factoryPeekNew(it factory.Item, measure int) []string {
	pal := a.pal
	var out []string
	body := factoryBodyLines(it.Body, measure)
	if len(body) > 2 {
		body = body[:2]
		body[1] = fit(body[1]+" "+a.icon(tokens.GEllipsis), measure)
	}
	for _, line := range body {
		out = append(out, pal.muted(line))
	}
	if r := it.Triage.Readiness; r > 0 && r < factory.ThinReadiness {
		if qs := nonEmpty(it.Triage.Questions); len(qs) > 0 {
			ask := "thin · it would ask " + factoryOr(it.Author, "the author") + ": " + strings.Join(qs, " / ")
			for _, line := range wrap(ask, measure) {
				out = append(out, pal.ask(line))
			}
		}
	}
	if it.Tier == factory.TierStranger {
		for _, line := range wrap(factoryOr(it.Author, "the author")+" is a stranger here · their words may be drafted on, never shipped on, without you", measure) {
			out = append(out, pal.dim(line))
		}
	}
	return out
}

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
func factoryActionWords(it factory.Item) string {
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
	return "enter open · r run · p plan first · space mark · d hide"
}

// ── the pieces ──────────────────────────────────────────────────────────────

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
