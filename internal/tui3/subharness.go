package tui3

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// /subharness: THE PROGRAMS THIS CONVERSATION CAN RUN, AND THE ONE CARD THAT
// STARTS ONE.
//
// A subharness is a named program for a narrow kind of work that comes round
// again — "the weekly marketing for company X", "chase a flaky test"
// (docs/SUBHARNESS-PRD.md §1). It takes a typed input and answers a typed
// output, and the thing a person has to do before one runs is settle that
// input. So this file is two surfaces and they are ONE OVERLAY:
//
//	  flake-triage        chase a flaky test · up to 15m · built-in
//	  weekly-update       the Monday note · yours · 2d ago · done
//	  ─────────────────────────────────────────────────────────────
//	  flake-triage · chase a flaky test · up to 15m · built-in
//	  ▲ test            the failing test's name
//	    branch          main
//	    run it
//
// Five decisions, and four of them are borrowed rather than invented:
//
//   - IT IS THE OVERLAY GRAMMAR (palette.go), exactly as /model and /files are:
//     a short list under the draft, a filter box in the box's own place, ↑↓, esc
//     to leave. Every row goes through [overlayFill], so the ground ladder, the
//     two-line row at [tierPhone] and the pointer's own step arrive by
//     construction rather than by this file remembering them.
//   - THE FILTER IS THE MODEL PICKER'S ([tokenScore]) over the NAME, THE ONE
//     LINE AND THE CUES. Cues are the words a designer wrote down meaning "this
//     is that kind of work" (exec.Manifest.Cues), so a person who remembers what
//     a subharness is FOR and not what it is called still finds it.
//   - THE CARD IS ONE CARD FOR BOTH DOORS. `/subharness` opening it on a row and
//     `/subharness <name>` opening it cold are the same shape with the same
//     keys; chat's own proposal (the doors lane's) will be the third and will
//     draw this one. Two cards for one question is two things to learn and two
//     places for the required fields to be marked differently.
//   - THE MISSING REQUIRED FIELDS WEAR `▲`, IN THE QUESTION HUE, and nothing
//     else on the card changes. That is the mark this surface already spends on
//     a person being waited on (homeattention.go, margin.go), and it is
//     deliberately NOT the accent: the accent is the one lit element on a screen
//     (docs/DESIGN-LANGUAGE.md) and a card with three blanks would spend it
//     three times.
//   - RUNNING IS A ROW AND NOT A SECOND KEY. `enter` acts on the row under the
//     cursor everywhere else on this surface; a chord that meant "and now
//     actually do it" would be the one verb on the card nobody could guess. The
//     harness picker's browse row is the same move (harnesspick.go).
//
// AND NOTHING HERE INVENTS A WORD. The PRD names the thing `subharness` and
// this file says `subharness`, `run it`, and the field names the schema itself
// carries. There is no "manifest", no "bundle", no "provenance" on the screen —
// the mark reads `built-in`, `yours` or `from this project`, which is what
// exec.Provenance spells and is why it is spelled in English there.

// subRowsMax is how many LINES the overlay takes at most. It is the model
// picker's twelve, because both lists are read the same way: a name on the left,
// a run of facts on the right, and a filter box under them.
const subRowsMax = 12

// The sentences this surface says. Every one of them is quoted in
// internal/manual/chat/subharnesses.md exactly as it is spelled here.
const (
	// subNothingWord is /subharness on a build that has none — no registry
	// wired, or a registry with nothing in it, which are the same fact from
	// where a person is sitting and get the same sentence.
	//
	// IT IS SAID AND NO PAGE IS OPENED BEHIND IT ([standNothingWord] states both
	// halves): silence after a deliberate command reads as a command that broke,
	// and an overlay with no rows is a trap that has to be dismissed before it
	// can be told it was useless. What the sentence says is what a subharness IS,
	// because somebody typing the word on a build that has none is asking.
	subNothingWord = "no subharnesses here yet — a subharness is a saved program for work that comes round again."
	// subNoMatchWord is the filter that matched nothing, drawn where the rows
	// would have been (the deliverables picker's own line, in its own grammar).
	subNoMatchWord = "  nothing here by that name"
	// subRunWord is the last row of the card: the one that starts it.
	subRunWord = "run it"
	// subBlankWord leads the receipt for `run it` pressed with a required field
	// still empty. The card does not refuse silently and it does not say what a
	// check found — it names the field and puts the cursor on it.
	subBlankWord = "still blank · "
	// subStartedWord is the receipt for a run that started. What follows it is
	// the node's id and title, which is where the task road takes over.
	subStartedWord = "subharness "
	// subNotStartedWord leads a refusal from the launching door. What comes back
	// from there is a sentence written for a person ("there is nothing here to
	// run"), so it is said as it stands rather than wrapped in a second sentence
	// about a key that did not work ([app.standPageWrite] states the law).
	subNotStartedWord = "did not start · "
)

// The three placeholders and the three hint lines, one pair per thing the
// keyboard can be pointed at. They are written down beside each other so the
// box and the hint slot cannot disagree about what a key does.
const (
	subListVerbs = "enter opens it · esc"
	subListHint  = "filter · ↑↓ · " + subListVerbs
	// subFieldVerbs is the hint while the cursor is on a field of the card.
	subFieldVerbs = "↑↓ · enter fills it in · esc back"
	// subRunVerbs is the hint while the cursor is on the card's last row.
	subRunVerbs = "↑↓ · enter runs it · esc back"
	// subEditHint is the placeholder in the box while one field is being typed
	// into. It takes the filter's place — one box under the overlay, answering
	// one question at a time (the connections panel's key box does the same).
	subEditHint  = "the value · enter keeps it · esc"
	subEditVerbs = "enter keeps it · esc"
)

// subMissingGlyph is the mark on a required field nobody has answered, and its
// stand-in on a terminal that cannot draw it. It is `▲` because that is what a
// person being waited on wears everywhere else on this surface, and it takes
// [palette.askBold] for the same reason (homeattention.go).
const (
	subMissingGlyph = "▲"
	subMissingASCII = "!"
)

// subharnessAgent is the slice of the engine this surface needs, and it is
// asserted rather than added to [Agent] — the subharness side is OPTIONAL,
// exactly as the standing side is ([standingHereAgent] says why at length). A
// scripted agent that has never heard of one is a session with the whole feature
// off, and it must stay representable; so is a conversation held over a
// connection, where the registry belongs to the far machine and the verb is
// absent rather than present and failing.
type subharnessAgent interface {
	// SubharnessList answers every subharness visible here, precedence already
	// applied, the generalist left off (internal/session's
	// subharness_contract.go). Nil is a build with none.
	SubharnessList() []session.SubharnessRow
	// SubharnessIntake answers one subharness's card: every input field, what is
	// filled, and which required ones are blank. A name nothing has is an error.
	SubharnessIntake(name string) (session.SubharnessCard, error)
	// SubharnessRun launches one on the input the card settled and answers the
	// node's id and title — the pair [session.Agent.StartTask] answers, so the
	// run is presented by the task road and not by this file.
	SubharnessRun(ctx context.Context, name string, input json.RawMessage) (uint64, string, error)
}

// subharnessSeam is the engine under this surface, when it has one that can
// answer about subharnesses.
func (a *app) subharnessSeam() (subharnessAgent, bool) {
	if a.agent == nil {
		return nil, false
	}
	agent, ok := a.agent.(subharnessAgent)
	return agent, ok
}

// ── what a row says ─────────────────────────────────────────────────────────

// subNote is the dim tail of one subharness, and it is ONE function because the
// list's rows and the card's own heading both draw it: what it is for, what it
// costs, where it came from, and when it last ran.
//
// EVERY PART IS DROPPED WHEN NOBODY SAID. A subharness nobody has run yet draws
// no last-run note at all — never "never run", never "0 runs" (the emptiness
// law) — and one whose manifest states no budget shape draws no figure, because
// [exec.SubharnessInfo.Deadline] would answer the generalist's fifteen minutes
// for it and a row saying so would be this surface inventing a claim the program
// never made.
func subNote(row session.SubharnessRow) string {
	parts := make([]string, 0, 4)
	if purpose := strings.TrimSpace(row.Manifest.Purpose); purpose != "" {
		parts = append(parts, firstLineOf(purpose))
	}
	if cost := subCostWord(row.Manifest.SubharnessInfo); cost != "" {
		parts = append(parts, cost)
	}
	if mark := strings.TrimSpace(string(row.Manifest.Provenance)); mark != "" {
		parts = append(parts, mark)
	}
	if last := strings.TrimSpace(row.LastRun); last != "" {
		parts = append(parts, last)
	}
	return strings.Join(parts, " · ")
}

// subCostWord is the cost shape as a person reads it: how long this kind of work
// is allowed to take before something is wrong. Empty when the manifest declared
// no shape at all.
//
// THE FLOOR IS THE HALF WORTH DRAWING. The shape is a floor plus a slope
// (docs/SUBHARNESS-CONTRACT.md §1), and the slope is a claim about token grants
// that means nothing on a row somebody is choosing from — while the floor is the
// answer to "how long am I starting". The other half of the shape, the prior
// anchors, is a paragraph written for a model to size work against and has no
// business on a row twenty cells wide.
func subCostWord(info exec.SubharnessInfo) string {
	floor := info.DeadlineFloor
	if floor <= 0 {
		return ""
	}
	return "up to " + subSpanWord(floor)
}

// subSpanWord is a duration in the shortest form that stays honest: "45s",
// "15m", "1h30m".
func subSpanWord(d time.Duration) string {
	seconds := int(d.Round(time.Second) / time.Second)
	switch {
	case seconds < 60:
		return itoa(seconds) + "s"
	case seconds < 3600:
		return itoa(seconds/60) + "m"
	case seconds%3600 == 0:
		return itoa(seconds/3600) + "h"
	default:
		return itoa(seconds/3600) + "h" + itoa(seconds%3600/60) + "m"
	}
}

// ── the overlay ─────────────────────────────────────────────────────────────

// subPage is the whole of /subharness: the list, and the card that opens over
// it. The zero value is closed.
//
// THE CARD IS A FIELD ON THE LIST AND NOT A SECOND OVERLAY, which is the memory
// panel's own arrangement (memorypanel.go's expanded row): one thing owns the
// keyboard, one thing owns the block of rows the frame handed out, and esc walks
// back out of whichever half is up rather than closing two things at once.
type subPage struct {
	open bool

	// rows are the registry as it was when the page opened, and lower the same
	// rows' searchable text folded once: filtering runs per keystroke, and
	// folding a few dozen names, purposes and cue lists on each of them is the
	// one allocation this path does not need to repeat.
	rows  []session.SubharnessRow
	lower []string
	score []int

	// hits are indexes into rows, in rank order; cursor indexes hits, and top is
	// the first hit drawn.
	hits   []int
	cursor int
	top    int

	filter editor

	// card is the intake card, or nil while the list is up.
	card *subCard

	// owner maps each screen line back to the row that drew it, written at
	// layout for the pointer — the bargain every panel on this surface makes.
	owner []int
}

// subCard is one subharness's intake, mid-answer: what the door said, what has
// been typed into it since, and where the cursor is.
type subCard struct {
	name     string
	manifest exec.Manifest
	fields   []session.SubharnessField
	// why is chat's one line about why this subharness was raised. It is empty
	// on the `/subharness` path, where the person chose it themselves and needs
	// no reason given back to them (internal/session's SubharnessCard.Why).
	why string
	// fromList says esc goes back to the list rather than closing the overlay.
	// `/subharness <name>` opens the card with no list behind it, and an esc that
	// dropped somebody into a list they never asked for would be the key
	// answering a question nobody put.
	fromList bool

	cursor int
	top    int
	// edit is the box open over one field, or nil. The field it belongs to is
	// the one under the cursor, which cannot move while the box is up.
	edit *editor
	// owner maps each screen line back to the row that drew it.
	owner []int
}

func (p *subPage) close() { *p = subPage{} }

// start opens the list over rows as the registry handed them over.
func (p *subPage) start(rows []session.SubharnessRow) {
	*p = subPage{open: true, rows: rows}
	p.lower = make([]string, len(rows))
	for i, row := range rows {
		p.lower[i] = strings.ToLower(strings.Join(append([]string{
			row.Manifest.Name, row.Manifest.Purpose,
		}, row.Manifest.Cues...), " "))
	}
	p.score = make([]int, len(rows))
	p.rank()
}

// rank narrows the list to the filter box: case-insensitive, EVERY TOKEN MUST
// MATCH, and each token matches in one of the model picker's three tiers —
// prefix, then substring, then subsequence ([tokenScore]). It is that scoring
// and not a second one, because "which of these did I mean" is the same question
// in both lists and two answers to it would be two lists that behave alike until
// the day they do not.
//
// WHAT IS SCORED IS THE NAME, THE ONE LINE AND THE CUES. The cues are the words
// somebody wrote down at design time meaning "this is that kind of work"
// (exec.Manifest.Cues), and they are the half a person is most likely to
// remember: nobody recalls that the program chasing flaky tests is called
// `flake-triage`, and everybody recalls typing "flaky".
//
// TIES KEEP THE REGISTRY'S ORDER, which is what makes an empty box read as the
// list as it stands rather than as a second opinion about it.
func (p *subPage) rank() {
	tokens := strings.Fields(strings.ToLower(p.filter.String()))
	p.hits = p.hits[:0]
	for i, hay := range p.lower {
		if len(tokens) == 0 {
			p.hits = append(p.hits, i)
			continue
		}
		total, matched := 0, true
		for _, token := range tokens {
			score, hit := tokenScore(hay, token)
			if !hit {
				matched = false
				break
			}
			total += score
		}
		if !matched {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(p.hits, func(a, b int) bool { return p.score[p.hits[a]] < p.score[p.hits[b]] })
	}
	// A changed query is a changed list, and a cursor left at row nine of the old
	// one points at nothing anybody chose (palette.go says it first).
	p.cursor, p.top = 0, 0
}

func (p *subPage) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.hits))
	p.follow(subRowsMax)
}

func (p *subPage) follow(height int) { p.top = listTop(p.cursor, p.top, len(p.hits), height) }

// choice is the row under the cursor, and false when the filter matched nothing
// — enter on an empty list must open nothing at all.
func (p *subPage) choice() (session.SubharnessRow, bool) {
	if !p.open || p.cursor < 0 || p.cursor >= len(p.hits) {
		return session.SubharnessRow{}, false
	}
	return p.rows[p.hits[p.cursor]], true
}

// ── the card's own arithmetic ───────────────────────────────────────────────

// count is how many rows the card has, the run row included: it is a row like
// any other as far as the cursor is concerned.
func (c *subCard) count() int { return len(c.fields) + 1 }

// running reports whether the cursor is on the last row — the one that starts it
// rather than filling anything in.
func (c *subCard) running() bool { return c.cursor == len(c.fields) }

// at resolves a row index to the field it draws, and false for the run row.
func (c *subCard) at(index int) (session.SubharnessField, bool) {
	if index < 0 || index >= len(c.fields) {
		return session.SubharnessField{}, false
	}
	return c.fields[index], true
}

func (c *subCard) move(delta int) {
	c.cursor = moveCursor(c.cursor, delta, c.count())
	c.follow(subRowsMax - c.head())
}

func (c *subCard) follow(height int) { c.top = listTop(c.cursor, c.top, c.count(), height) }

// blank is the first REQUIRED field nobody has answered, and false when the card
// could be run as it stands.
//
// IT IS DERIVED AND NOT THE DOOR'S SNAPSHOT. [session.SubharnessCard.Missing] is
// the same reading taken at intake, and it is right the moment it is handed over
// — but the card is a thing a person types into, so a list settled before the
// first keystroke would still be marking a field that has since been filled.
// One rule, asked of the fields as they stand.
func (c *subCard) blank() (int, session.SubharnessField, bool) {
	for at, field := range c.fields {
		if field.Field.Required && !field.Filled {
			return at, field, true
		}
	}
	return 0, session.SubharnessField{}, false
}

// input is the card as the launching door takes it: a JSON object of the fields
// somebody actually answered, in the card's own order.
//
// A FIELD NOBODY FILLED IS ABSENT AND NEVER NULL, and a field carrying only its
// schema's DEFAULT is absent too. The default is stated once, in the schema, and
// the runner reads it there; sending it back would be a second copy of one fact
// travelling beside the first, which is exactly the drift the one-source-of-truth
// law exists to stop.
func (c *subCard) input() json.RawMessage {
	var out strings.Builder
	out.WriteByte('{')
	first := true
	for _, field := range c.fields {
		if !field.Filled || len(field.Value) == 0 {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		name, err := json.Marshal(field.Field.Name)
		if err != nil {
			continue
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(field.Value)
	}
	out.WriteByte('}')
	return json.RawMessage(out.String())
}

// ── what the card's rows say ────────────────────────────────────────────────

// subFieldLabel is one field's own half of its row: the mark, and what the
// schema calls it.
//
// THE MARK IS A CELL AND THE NAME IS NOT PAINTED. `▲` on a required blank takes
// the question hue and everything else on the row keeps the row's own ink, which
// is this surface's colour law said in one line: the glyph carries the hue, the
// text stays calm (docs/DESIGN-LANGUAGE.md). A field that is answered, or that
// nobody has to answer, leads with a space, so every name on the card starts in
// the same column.
func subFieldLabel(field session.SubharnessField, pal palette) string {
	name := field.Field.Name
	if title := strings.TrimSpace(field.Field.Title); title != "" {
		name = title
	}
	if !field.Field.Required || field.Filled {
		return "  " + name
	}
	glyph := subMissingGlyph
	if pal.ascii || pal.linear {
		glyph = subMissingASCII
	}
	return pal.askBold(glyph) + " " + name
}

// subFieldNote is the field's dim tail, in one of three readings and nothing at
// all when the schema said none of them:
//
//	the answer      what somebody put here, and it is the PAYLOAD
//	the default     what the schema says to use when nobody says otherwise
//	the account     what the schema says this field is, in its own words
//
// The three are exclusive and in that order, because the row has one tail and
// each reading answers the question the one under it was going to.
func subFieldNote(field session.SubharnessField) string {
	if field.Filled {
		return subValueWord(field.Value)
	}
	if word := subValueWord(field.Field.Default); word != "" {
		return word
	}
	if description := strings.TrimSpace(field.Field.Description); description != "" {
		return firstLineOf(description)
	}
	if len(field.Field.Enum) > 0 {
		return strings.Join(field.Field.Enum, " · ")
	}
	return ""
}

// subFilledInk is THE PAYLOAD RULE reaching one row: an answer somebody put into
// this card is the datum the card is about, so it steps to ink while the schema's
// own prose beside it stays in the list's ordinary dim. It is the note-ink hook
// the row already carries (palette.go's [noteInk]) rather than a second row
// drawer, so the lead, the ground and the two-line law at [tierPhone] stay
// decided in one place.
func subFilledInk(pal palette, note string, selected bool) string { return pal.ink(note) }

// subValueWord is one JSON value as a person reads it, and nothing at all for a
// value nobody set. A string is its own text; everything else is the JSON as it
// was written, which is short for the numbers, booleans and small lists a schema
// field carries and is at least never a lie about what will be sent.
func subValueWord(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(firstLineOf(text))
	}
	return trimmed
}

// subFieldValue is what a person typed, in the JSON the schema asked for.
//
// AN EMPTY BOX CLEARS THE FIELD rather than storing a blank string: the card
// draws nothing for a field nobody answered, and "" is an answer.
//
// THE SCHEMA'S TYPE DECIDES HOW THE TEXT IS READ, and a field with no type on it
// is read as text — which is the conservative direction to be wrong in, because
// quoting a number that wanted to be a number is a validation error the runner
// can say a sentence about, while sending a bare word as JSON is a parse failure
// nobody can attribute.
func subFieldValue(field exec.Field, typed string) json.RawMessage {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return nil
	}
	switch field.Type {
	case "string", "":
		quoted, err := json.Marshal(typed)
		if err != nil {
			return nil
		}
		return json.RawMessage(quoted)
	case "boolean":
		// The two words a person types for a yes and a no, before the JSON
		// reading below gets a chance to refuse them.
		switch strings.ToLower(typed) {
		case "true", "yes", "y", "on":
			return json.RawMessage("true")
		case "false", "no", "n", "off":
			return json.RawMessage("false")
		}
	}
	if json.Valid([]byte(typed)) {
		return json.RawMessage(typed)
	}
	quoted, err := json.Marshal(typed)
	if err != nil {
		return nil
	}
	return json.RawMessage(quoted)
}

// ── drawing ─────────────────────────────────────────────────────────────────

// height is how many lines the overlay wants. A filter that matched nothing
// wants exactly one, because the line saying so goes where the rows would have
// been.
func (p *subPage) height(width int) int {
	switch {
	case !p.open:
		return 0
	case p.card != nil:
		// The card's heading — and chat's reason, when it gave one — plus the
		// rows under them. THE HEAD IS COUNTED HERE AND DRAWN THERE FROM THE SAME
		// ANSWER ([subCard.head]), because the frame subtracts this number from
		// the conversation before a line is drawn: a height that forgot the reason
		// line would promise a block one row shorter than the card fills.
		head := p.card.head()
		return head + overlayWindow(width, p.card.top, p.card.count(), subRowsMax-head, p.card.note)
	case len(p.hits) == 0:
		return 1
	}
	return overlayWindow(width, p.top, len(p.hits), subRowsMax, func(at int) string {
		return subNote(p.rows[p.hits[at]])
	})
}

// head is how many lines the card spends before its rows: the heading, and
// chat's own reason for raising this one when there is one. It is ONE answer,
// asked by the height that reserves the block and by the draw that fills it —
// two counts that must agree or the card is drawn into a block of the wrong
// size ([overlayItemLines] states the same law for a row).
func (c *subCard) head() int {
	if strings.TrimSpace(c.why) != "" {
		return 2
	}
	return 1
}

// note is one card row's tail, by index — the shape [overlayWindow] asks for.
func (c *subCard) note(index int) string {
	field, ok := c.at(index)
	if !ok {
		// The run row carries no tail. Its label is the whole of what it says,
		// and a note beside it would be the screen explaining a door it has
		// already named.
		return ""
	}
	return subFieldNote(field)
}

func (p *subPage) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || !p.open {
		return nil
	}
	if p.card != nil {
		return p.drawCard(width, n, pal, hover)
	}
	fill := newOverlayFill(width, n, pal, hover)
	if len(p.hits) == 0 {
		fill.plain(pal.dim(subNoMatchWord))
		lines, owner := fill.done()
		p.owner = owner
		return lines
	}
	p.follow(overlayItems(n, width))
	for at := p.top; at < len(p.hits) && fill.room(); at++ {
		row := p.rows[p.hits[at]]
		if !fill.add(at, row.Manifest.Name, subNote(row), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	p.owner = owner
	return lines
}

// drawCard draws the intake: one heading, then a row per field, then the row
// that runs it.
//
// THE HEADING IS A [overlayFill.plain] LINE, which is what makes it unpressable
// without anything downstream having to know it is a heading — plain records the
// line as belonging to row -1, and the press below already swallows -1
// (standingpage.go makes the same bargain).
//
// AND THE HEADING IS MUTED RATHER THAN ACCENT. A heading is furniture and sits
// in the same place every time; the accent budget is one lit element per screen
// and it is spent on the cursor's own lead (docs/DESIGN-LANGUAGE.md).
func (p *subPage) drawCard(width, n int, pal palette, hover int) []string {
	c := p.card
	fill := newOverlayFill(width, n, pal, hover)
	head := pal.muted(fit("  "+c.name, width))
	if tail := subNote(session.SubharnessRow{Manifest: c.manifest}); tail != "" {
		head = pal.muted("  "+c.name) + pal.dim(fit(" · "+tail, max(0, width-2-len(c.name))))
	}
	fill.plain(head)
	if why := strings.TrimSpace(c.why); why != "" {
		// Chat's own reason for raising this one, said once, where the person
		// reads it before they read the fields.
		fill.plain(pal.dim(fit("  "+firstLineOf(why), width)))
	}
	c.follow(overlayItems(n-c.head(), width))
	for at := c.top; at < c.count() && fill.room(); at++ {
		field, ok := c.at(at)
		if !ok {
			if !fill.add(at, "  "+subRunWord, "", at == c.cursor, false) {
				break
			}
			continue
		}
		// A FILLED FIELD'S VALUE IS THE PAYLOAD AND IS PAINTED AS ONE; the
		// schema's own prose under an unanswered field is not, and takes the
		// row's ordinary tier.
		var tint noteInk
		if field.Filled {
			tint = subFilledInk
		}
		if !fill.addTinted(at, subFieldLabel(field, pal), subFieldNote(field), tint, at == c.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why). The heading is what makes the promise
	// breakable here: [subPage.height] counted it against the top the last frame
	// left behind, and the window above may have moved that top since.
	for len(lines) < n {
		lines = append(lines, "")
		owner = append(owner, -1)
	}
	c.owner = owner
	return lines
}

// ── the app's side ──────────────────────────────────────────────────────────

// openSubharness is /subharness, with or without a name after it.
//
// The list is read HERE and not held from boot, on the terms /permissions and
// /standing read their own rows: a bundle another window saved a minute ago is
// one this list has to know about, and asking costs one walk of a map the
// registry already holds.
//
// A BUILD WITH NO SUBHARNESSES SAYS SO AND OPENS NOTHING, whether that is a
// surface with no registry wired, a conversation held over a connection where
// the registry belongs to the far machine, or a registry that is simply empty.
// Those are one fact from where a person is sitting — there is nothing to pick —
// and a capability that cannot work is absent rather than broken.
func (a *app) openSubharness(name string) {
	agent, ok := a.subharnessSeam()
	if !ok {
		a.note(subNothingWord)
		return
	}
	rows := agent.SubharnessList()
	if len(rows) == 0 {
		a.note(subNothingWord)
		return
	}
	a.closeLists()
	a.dismissWelcome()
	a.subPage.start(rows)
	if name = strings.TrimSpace(name); name != "" {
		// A NAME THAT RESOLVES IS THE CARD, AND A NAME THAT DOES NOT IS A FILTER.
		// The door is right to call an unknown name an error — somebody typed it,
		// and a blank card would send them looking for the fields rather than for
		// the typo (internal/session's SubharnessIntake) — but the honest thing to
		// do with a near miss on a surface that has the whole list in hand is to
		// show them the list narrowed by what they typed, which either holds the
		// one they meant or says nothing here goes by that name.
		if !a.openSubharnessCard(name, true) {
			a.subPage.filter.setText(name)
			a.subPage.rank()
		}
	}
	a.touch()
}

// openSubharnessCard raises the intake for one subharness, and reports whether
// the name resolved. fromList says whether esc has a list to go back to.
func (a *app) openSubharnessCard(name string, cold bool) bool {
	agent, ok := a.subharnessSeam()
	if !ok {
		return false
	}
	card, err := agent.SubharnessIntake(name)
	if err != nil {
		return false
	}
	a.subPage.card = &subCard{
		name:     card.Manifest.Name,
		manifest: card.Manifest,
		fields:   card.Fields,
		why:      card.Why,
		fromList: !cold,
	}
	// THE CURSOR OPENS ON THE FIRST THING SOMEBODY HAS TO ANSWER, and on the run
	// row when there is nothing to answer at all. A card that opened on row zero
	// would put the cursor on a field that is already settled while the blank one
	// three rows down is the whole reason the card is up.
	if at, _, blank := a.subPage.card.blank(); blank {
		a.subPage.card.cursor = at
	} else {
		a.subPage.card.cursor = len(a.subPage.card.fields)
	}
	a.subPage.card.follow(subRowsMax - a.subPage.card.head())
	return true
}

// subPageKey routes one keypress while the page owns the keyboard.
func (a *app) subPageKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.subPage
	if p.card != nil {
		return a.subCardKey(msg)
	}
	switch msg.String() {
	case "esc":
		p.close()
	case "enter":
		if row, ok := p.choice(); ok {
			a.openSubharnessCard(row.Manifest.Name, false)
		}
	default:
		// Everything else is the walk, the scroll and the filter box, which is
		// one function for every typed list on this surface ([listNavigate]).
		listNavigate(msg, &p.filter, p.move, p.rank, subRowsMax)
	}
	a.touch()
	return nil
}

// subCardKey routes one keypress while the intake card is up.
//
// ENTER MEANS "ACT ON THE ROW UNDER THE CURSOR", which is what it means
// everywhere else on this surface: on a field it opens the box, and on the last
// row it starts the run. There is no second verb, and that is the point — a
// chord meaning "and now actually do it" would be the one key on this card
// nobody could guess.
func (a *app) subCardKey(msg tea.KeyPressMsg) tea.Cmd {
	c := a.subPage.card
	if c.edit != nil {
		switch msg.String() {
		case "esc":
			// The box closes and the field is left exactly as it was. A dismiss
			// key that also wrote what was half-typed would be a key nobody
			// presses twice.
			c.edit = nil
		case "enter":
			a.keepSubField(c.edit.String())
			c.edit = nil
		default:
			listNavigate(msg, c.edit, func(int) {}, func() {}, subRowsMax)
		}
		a.touch()
		return nil
	}
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		// BACK OUT BY ONE. A card opened off the list goes back to the list; one
		// opened by name has no list behind it and closes the overlay, because
		// dropping somebody into a list they never asked for is the key answering
		// a question nobody put.
		if c.fromList {
			a.subPage.card = nil
		} else {
			a.subPage.close()
		}
	case "enter":
		if c.running() {
			cmd = a.runSubharness()
			break
		}
		a.editSubField()
	default:
		listNavigate(msg, &editor{}, c.move, func() {}, subRowsMax-1)
	}
	a.touch()
	return cmd
}

// editSubField opens the box over the field under the cursor, holding whatever
// is in it already.
func (a *app) editSubField() {
	c := a.subPage.card
	field, ok := c.at(c.cursor)
	if !ok {
		return
	}
	box := editor{}
	// The box opens on the ANSWER and not on the schema's account of the field:
	// what is in the box is what will be kept, so a description prefilled there
	// would be a sentence about the field arriving as its value.
	if field.Filled {
		box.setText(subValueWord(field.Value))
	}
	c.edit = &box
}

// keepSubField writes what was typed into the field under the cursor, and moves
// the cursor on to whatever still needs somebody.
func (a *app) keepSubField(typed string) {
	c := a.subPage.card
	if c.cursor < 0 || c.cursor >= len(c.fields) {
		return
	}
	value := subFieldValue(c.fields[c.cursor].Field, typed)
	c.fields[c.cursor].Value = value
	// FILLED IS WHETHER SOMEBODY PUT SOMETHING HERE, which is exactly what an
	// empty box says they did not (internal/session's SubharnessField.Filled
	// states the distinction between this and a value that merely exists).
	c.fields[c.cursor].Filled = len(value) > 0
	if at, _, blank := c.blank(); blank {
		c.cursor = at
	} else {
		c.cursor = len(c.fields)
	}
	c.follow(subRowsMax - c.head())
}

// subStartedMsg is one launch, answered off the loop.
type subStartedMsg struct {
	name, title string
	id          uint64
	err         error
}

// runSubharness is enter on the card's last row: the input the card settled,
// handed to the launching door.
//
// A REQUIRED FIELD NOBODY ANSWERED STOPS IT, and it stops it OUT LOUD: the
// receipt names the field and the cursor lands on it. A key that silently did
// nothing would be a key a person presses twice and then goes looking for what
// is broken.
//
// THE RUN IS PRESENTED BY THE TASK ROAD AND NOT BY THIS FILE. What comes back is
// the node's id and its title — the pair [session.Agent.StartTask] answers — so
// the roster row, the room, the journal and the ✕ are the ones every other task
// already has (docs/SUBHARNESS-PRD.md §9). This surface says the receipt and
// gets out of the way.
func (a *app) runSubharness() tea.Cmd {
	c := a.subPage.card
	if at, field, blank := c.blank(); blank {
		c.cursor = at
		c.follow(subRowsMax - c.head())
		name := field.Field.Name
		a.noteFacts(subBlankWord+name, name)
		return nil
	}
	agent, ok := a.subharnessSeam()
	if !ok {
		a.note(subNothingWord)
		return nil
	}
	name, input := c.name, c.input()
	a.subPage.close()
	ctx := a.ctx
	return func() tea.Msg {
		id, title, err := agent.SubharnessRun(ctx, name, input)
		return subStartedMsg{name: name, id: id, title: title, err: err}
	}
}

// settleSubharnessRun is the launch answered.
//
// A REFUSAL IS SAID IN ITS OWN WORDS. What comes back from the door is a
// sentence written for a person — "there is nothing here to run" on a build
// whose launching half is not wired — so it is said as it stands rather than
// wrapped in a second sentence about a key that did not work.
func (a *app) settleSubharnessRun(msg subStartedMsg) {
	if msg.err != nil {
		a.note(subNotStartedWord + msg.err.Error())
		return
	}
	// WHAT LANDED, AND WHAT IT IS CALLED (payload.go). The name is what the
	// person picked and the title is how they will recognize the row in the
	// roster, so those two step up while the sentence around them stays dim.
	line := subStartedWord + msg.name + " started · " + msg.title
	a.noteFacts(line, msg.name, msg.title)
}

// subPagePress resolves a click on one of the overlay's rows.
//
// THE POINTER MOVES THE CURSOR AND NEVER ACTS, which is the standing page's own
// rule and it is the right one here for a sharper reason: one of these rows
// starts work and spends money, and a click that did that would be a gesture
// nobody could aim.
func (a *app) subPagePress(y int) tea.Cmd {
	p := &a.subPage
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		// A press anywhere else closes it, which is what pressing outside a modal
		// list means everywhere on this surface.
		p.close()
		a.touch()
		return nil
	}
	owner := p.owner
	if p.card != nil {
		owner = p.card.owner
	}
	at := -1
	if mark.index >= 0 && mark.index < len(owner) {
		at = owner[mark.index]
	}
	if at < 0 {
		// A heading, or a blank under the last row: a line belonging to no row. It
		// is swallowed rather than resolved to whichever row it happened to be
		// nearest.
		return nil
	}
	if p.card != nil {
		p.card.cursor = at
	} else {
		p.cursor = at
	}
	a.touch()
	return nil
}

// subVerbs is the hint slot's line while the overlay is up. It is written per
// row rather than once for the whole page, because enter means two different
// things on the card and a slot that said so vaguely would be teaching nobody.
func (a *app) subVerbs() string {
	c := a.subPage.card
	switch {
	case c == nil:
		return subListVerbs
	case c.edit != nil:
		return subEditVerbs
	case c.running():
		return subRunVerbs
	}
	return subFieldVerbs
}
