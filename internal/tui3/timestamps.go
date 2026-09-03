package tui3

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// THE CLOCK, AS THE TRANSCRIPT CARRIES IT.
//
// This surface had exactly one time on it — the running call's count-up — and
// that is the time of the four seconds you are living in. Everything ABOVE the
// live edge had none: a conversation read back an hour later, or opened again
// the next morning, was a wall of text with no answer to "when was this" and no
// answer to "what did that turn cost me". Both are facts about work the person
// paid for, and neither is derivable from anything else on screen.
//
// So the clock arrives in two shapes, and they are two because they answer two
// different questions:
//
//	 …the reply ends
//	                                       · 14:02 · 2m12s · 3 tools · $0.04 ·
//
//	── Thu, Aug 14 ──────────────────────────────────────────────────────────
//
//	── 14:02 ────────────────────────────────────────────────────────────────
//
//	› and then the person came back
//
//   - THE TURN FOOTER is the receipt: when the turn ended, how long it took,
//     how many calls it made, what it cost. One dim line, RIGHT-ALIGNED, under
//     the turn it is about — right because the conversation is read down the
//     left and a receipt is not part of the reading, and dim because it is
//     furniture until somebody goes looking for it.
//
//   - THE SEPARATORS are the seam: a mark where the conversation was PUT DOWN
//     and picked up again. They are drawn only at a gap of [timeGap] or at a day
//     boundary, because a mark between two messages typed a minute apart says
//     nothing, and a surface that stamps every message is a chat log.
//
// FROZEN AT COMMIT. A footer's four figures are read once, at the moment the
// turn settles ([app.stampTurn]), and never recomputed: the elapsed time of a
// finished turn does not change, and a number that moved after the fact would
// be a receipt that could not be trusted. Only the PAINT follows the clock —
// see [app.stampPaint].
//
// THE FULL DATE IS ONE KEY AWAY. ctrl+o already means "show me the rest of this
// turn" (it unfolds the turn's tool calls), so an unfolded turn spends its
// footer's first field on the whole ISO timestamp instead of on four digits.
// A person who wants the exact instant asks for the detail they were already
// asking for, and a person who does not never sees a 25-column date.

// turnStamp is one finished turn's receipt, as it was true at the moment the
// turn ended. Everything in it is frozen there.
type turnStamp struct {
	// at is when the turn settled, and it is the field the separators are
	// measured from as well.
	at time.Time
	// took is how long the turn ran, from the person's message to the settle.
	took time.Duration
	// tools is how many calls the turn made, and cost what the session's spend
	// grew by while it ran. A turn with no price pair published grows the spend
	// by nothing, and a cost of zero is not drawn: see [app.stampRow].
	tools int
	cost  float64
}

// timeGap is the silence that earns a mark. Ten minutes is where "the same
// sitting" stops being true — a person who came back from lunch is reading a
// conversation they have to place in time, and one who paused to read a diff is
// not.
const timeGap = 10 * time.Minute

// stampFresh is how long a footer stays MUTED before it settles to dim. An hour
// is the age at which a turn stops being part of what is happening and becomes
// part of what happened, which is the same distinction the HUD's fade draws over
// ten seconds (render.go) — the transcript's version of it is just slower.
const stampFresh = time.Hour

// stampsOn and separatorsOn read the ui.timestamps rung (config's
// [config.TimestampModes]). The word is read at boot and at every turn end, in
// the same place the mouse and the gate posture are, so a change made in the
// settings panel lands one turn later.
//
// The rungs are a LADDER: footers implies the separators under it, because a
// footer saying "14:02" with no day mark above it is a time with no date.
func (a *app) stampsOn() bool { return a.timestamps == timestampsFooters }

func (a *app) separatorsOn() bool {
	return a.timestamps == timestampsFooters || a.timestamps == timestampsSeparators
}

// The three words, spelled here so the rendering never quotes a settings key's
// vocabulary from memory.
const (
	timestampsFooters    = "footers"
	timestampsSeparators = "separators"
)

// stampTurn freezes the receipt of the turn that just ended. It is called from
// [app.settle] and from nowhere else.
//
// ONE STAMP PER TURN, and the map is what enforces it: a turn ends TWICE on this
// surface — the session's own EventTurnDone, then the stream closing behind it —
// and the second of those would otherwise overwrite the first with a longer
// duration measured to a later instant (the same defect [app.sampleContext]
// documents against the context ring).
func (a *app) stampTurn() {
	if a.turnBegan.IsZero() || a.turn <= 0 {
		return
	}
	if a.stamps == nil {
		a.stamps = map[int]turnStamp{}
	}
	if _, already := a.stamps[a.turn]; already {
		return
	}
	now := a.now()
	cost := a.cost - a.turnCostAt
	if cost < 0 {
		// A session whose spend was re-read downward mid-turn (a resume, a
		// usage correction) is not a turn that earned money back.
		cost = 0
	}
	a.stamps[a.turn] = turnStamp{
		at:    now,
		took:  now.Sub(a.turnBegan),
		tools: a.turnTools(a.turn),
		cost:  cost,
	}
}

// turnTools counts the calls one turn made. It walks the entries rather than
// keeping a counter because the entries are the truth about what happened — a
// counter would have to be right about interrupts, replays and the calls a
// consent question killed.
func (a *app) turnTools(turn int) int {
	n := 0
	for i := range a.entries {
		if a.entries[i].kind == entryTool && a.entries[i].turn == turn {
			n++
		}
	}
	return n
}

// stampRow is one turn's footer, painted and right-aligned to the width, or ""
// when this turn has no stamp or the row is turned off.
//
//	· 14:02 · 2m12s · 3 tools · $0.04 ·
//
// EVERY FIELD BUT THE TIME IS DROPPABLE, and each one drops for the same reason:
// a figure that is zero is a figure nobody measured. A turn too fast to have a
// duration, a turn that called nothing, a turn on a model with no published
// price — each of those is a field this line does not draw rather than a zero it
// prints.
func (a *app) stampRow(turn, width int) string {
	stamp, ok := a.stamps[turn]
	if !ok || !a.stampsOn() || width < 8 {
		return ""
	}
	fields := []string{a.stampClock(turn, stamp)}
	if word := tookWord(stamp.took); word != "" {
		fields = append(fields, word)
	}
	if stamp.tools > 0 {
		fields = append(fields, toolCallWord(stamp.tools))
	}
	if stamp.cost > 0 {
		fields = append(fields, dollars(stamp.cost))
	}
	plain := "· " + strings.Join(fields, " · ") + " ·"
	if ansi.StringWidth(plain) > width {
		return ""
	}
	painted := a.stampPaint(stamp)(plain)
	return rightAlign(painted, plain, width)
}

// toolCallWord is how many calls a turn made, and it is the ONE spelling of
// that number on this surface.
//
// THE DEFECT IT FIXES: one turn counted its calls in two words six rows apart —
// `1 tool call` on the fold chip (workfold.go) and `1 tool` on the receipt under
// the same turn — and the rewind sheet said `1 tool` for a third time. Two
// spellings of one number invite the reader to check whether they are two
// numbers, which is the whole of the cost.
//
// THE NOUN IS THE CALL AND NOT THE TOOL, because that is what is counted: a
// turn that ran `bash` four times made four calls and used one tool, and
// `4 tools` is the reading that is actually wrong.
func toolCallWord(calls int) string {
	return itoa(calls) + " " + plural("tool call", calls)
}

// stampClock is the footer's first field: four digits, or the whole instant
// while the turn is unfolded. See this file's header for why ctrl+o is the key
// that carries it.
func (a *app) stampClock(turn int, stamp turnStamp) string {
	if a.unfolded[turn] {
		return stamp.at.Format(time.RFC3339)
	}
	return stamp.at.Format(clockFormat)
}

// stampPaint is the footer's age fade, and it is the one thing about a footer
// that is NOT frozen: a receipt under the turn you are still reading is part of
// the present, and an hour later it is furniture.
func (a *app) stampPaint(stamp turnStamp) func(string) string {
	if a.now().Sub(stamp.at) < stampFresh {
		return a.pal.muted
	}
	return a.pal.dim
}

// The two formats the clock is written in. They are here rather than inline
// because the separator and the footer must never be able to disagree about
// what a time looks like.
const (
	clockFormat = "15:04"
	dayFormat   = "Mon, Jan 2"
)

// stampMarks is the separator block drawn before the message at `at`, given the
// last time this surface drew or knew about. It returns the labels — a day
// boundary, a long silence, or neither — in the order they are drawn.
//
// THE DAY OUTRANKS THE GAP AND REPLACES IT. Crossing midnight is always also a
// gap of more than ten minutes, and drawing both would be the same fact stated
// twice, coarsely and then finely.
func stampMarks(last, at time.Time) []string {
	if last.IsZero() || at.IsZero() || !at.After(last) {
		return nil
	}
	if last.YearDay() != at.YearDay() || last.Year() != at.Year() {
		return []string{at.Format(dayFormat)}
	}
	if at.Sub(last) >= timeGap {
		return []string{at.Format(clockFormat)}
	}
	return nil
}

// stampWalk is what one layout pass carries about the clock: the turn it is
// currently inside, and the last moment it has drawn or been told about. It is a
// struct rather than two variables because [app.stampBlock] has to advance both
// together — a mark measured from a moment the pass had already passed would
// draw a seam at every turn for the rest of the conversation.
type stampWalk struct {
	turn int
	at   time.Time
}

// stampBlock is the clock's block at the seam before entry i — the receipt of
// the turn that just ended, then the mark saying how long ago that was — and it
// reports whether it drew anything at all.
//
// i is -1 at the END of the conversation, where there is a turn to close and no
// entry to open. That is the only case the last turn's footer can be drawn from:
// every other footer is drawn at the seam with the turn that followed it.
//
// It takes [app.layout]'s own gap so that every blank on this surface is still
// emitted by exactly one function (see that function's spacing law): a block
// asks for its gap before it draws, and asking twice is still one blank because
// each block asks once.
func (a *app) stampBlock(d deck, out []row, walk *stampWalk, i, width int, gap func()) ([]row, bool) {
	drew := false
	if line := a.stampRow(walk.turn, width); line != "" {
		gap()
		out = append(out, row{text: line, entry: -1})
		drew = true
	}
	// The receipt's own moment is later than anything inside the turn, so the
	// gap to whatever comes next is measured from it.
	if stamp, ok := a.stamps[walk.turn]; ok && stamp.at.After(walk.at) {
		walk.at = stamp.at
	}
	if i < 0 || i >= len(d.entries) {
		return out, drew
	}
	began := d.entries[i].began
	if a.separatorsOn() && d.entries[i].kind == entryUser {
		// ONLY THE PERSON'S OWN MESSAGE OPENS A SITTING. Every other entry
		// carries a duration or nothing at all, and a seam drawn above a tool
		// call would be marking the moment a machine got round to something.
		for _, label := range stampMarks(walk.at, began) {
			if line := a.timeRule(label, width); line != "" {
				gap()
				out = append(out, row{text: line, entry: -1})
				drew = true
			}
		}
	}
	if !began.IsZero() {
		walk.at = began
	}
	walk.turn = d.entries[i].turn
	return out, drew
}

// timeRule is a mark's line: the label, centred in the dim rule this surface
// already draws for a compaction ([app.divider]) — the same geometry, without
// the glyph, because this rule marks a SEAM in time rather than an edit to the
// conversation.
func (a *app) timeRule(label string, width int) string {
	if width < 8 || label == "" {
		return ""
	}
	text := " " + label + " "
	rest := width - ansi.StringWidth(text) - 4
	if rest < 0 {
		return a.pal.dim(fit("──"+text, width))
	}
	left := rest / 2
	return a.pal.dim(strings.Repeat("─", left+2) + text + strings.Repeat("─", rest-left+2))
}

// tookWord is a finished span in the transcript's own spelling: "0.4s", "12s",
// "2m12s". It is [elapsedWord]'s body, lifted so a turn and a tool call can
// never grow two different ideas of what two minutes looks like.
func tookWord(took time.Duration) string {
	switch {
	case took < elapsedFloor:
		return ""
	case took < 10*time.Second:
		// One decimal under ten seconds: the difference between 1.2s and 1.9s is
		// the difference a person notices, and past ten seconds it is not.
		return strconv.FormatFloat(took.Seconds(), 'f', 1, 64) + "s"
	case took < time.Minute:
		return itoa(int(took.Round(time.Second)/time.Second)) + "s"
	default:
		minutes := int(took / time.Minute)
		seconds := int((took % time.Minute).Round(time.Second) / time.Second)
		return itoa(minutes) + "m" + pad2(seconds) + "s"
	}
}
