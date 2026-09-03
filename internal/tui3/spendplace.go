package tui3

// THE SPEND PLACE IS A READING, NEVER A SECOND SOURCE OF SPENDING FACTS.
//
// The session package owns the calendar arithmetic and the three groupings.
// This file merely keeps those answers together and lays them onto the page.
// In particular, drawing never opens the machine-wide file and never reads a
// clock: callers hand [readSpend] both the lines and the instant called now.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

const (
	spendModelBarCap = 12
	spendSubjectCap  = 3
)

// spendReading is the complete, immutable answer drawn by one spend page.
// The original lines are deliberately absent: once the window has been read,
// no later draw should be able to count a line outside it by accident.
type spendReading struct {
	window   session.UsageWindow
	now      time.Time
	totals   session.DaySpend
	days     []session.DaySpend
	models   []session.ModelSpend
	subjects []session.SubjectSpend
	loudest  session.DaySpend
	loudFor  session.SubjectSpend
	// names is the join THE LEDGER CANNOT MAKE FOR ITSELF: a task id, a standing
	// id or a conversation id against the word a person calls that thing. The
	// ledger holds ids and nothing else and says so
	// ([session.SubjectSpend.ID]), so the page reads the titles off the records
	// it is already holding — the world's task index, the standing seam — and
	// hands them here ([spendReading.naming]). A subject nobody could name keeps
	// its id, which is a worse row than a title and a better one than a blank.
	names map[string]string
	// crew is the OTHER join the ledger cannot make: a model id against the role
	// this machine has that model BOUND to, and the role slots nothing is bound
	// to at all ([spendReading.crewed], [spendCrew]).
	crew spendCrew
	// rail is the machine's daily limit, for the pointer line at the top of the
	// page ([spendReading.railed]). Zero is no limit.
	rail float64
	// today is WHAT THIS MACHINE HAS SPENT SINCE MIDNIGHT, handed in with
	// everything else this reading answers from ([spendReading.todayed]).
	//
	// IT IS A FACT ABOUT THE MACHINE'S DAY AND NOT ABOUT THIS WINDOW, which is
	// why it is handed in rather than picked out of the buckets below. It used to
	// be read back out of [spendReading.days] — the bucket covering `now` — so a
	// person who paged the fortnight back a month was shown a pointer line with
	// no `today` on it at all, and the top line of the same frame went on drawing
	// the day. The line says what the day has cost and where the limits are set;
	// neither of those two facts moves when the window does.
	today float64
	// unwritten is how many spending records the machine failed to write down
	// ([session.UsageDrops]), handed in on the read like everything else here so
	// that DRAWING stays arithmetic over what was already gathered. Zero is the
	// ordinary case and the line says nothing about it.
	unwritten int64
}

// lost hands the reading the count of rows that never reached the file. It
// answers a copy, for [spendReading.naming]'s reason: a reading is an immutable
// answer.
func (r spendReading) lost(dropped int64) spendReading {
	r.unwritten = dropped
	return r
}

// spendCrew is what SCREEN 2c's model table needs and the ledger does not hold:
// which slot each model is bound to, what each model is CALLED, and which slots
// have nothing behind them.
//
// THE ROLE IS THE BINDING AND NEVER THE CALL. The ledger's own role word is the
// auxiliary name one call gave itself — `title`, `taskname` — and a table headed
// with it answers a question nobody can act on. The design's caption says which
// question this column answers: `by the model, and the role it was bound to`, and
// the point of the column is that reading "execution is 63% of the bill" sends
// you to the one chip that changes it.
type spendCrew struct {
	// role is the slot's plain word — `execution`, `conversation`,
	// `verification`, `naming`, `planning` — by model id, lower-cased, because
	// a model id is matched case-insensitively everywhere else on this surface.
	role map[string]string
	// unbound is every role slot with nothing bound to it, in the ladder's own
	// order. Each becomes a row of its own under the models — `planning ·
	// unbound · follows execution` — because a slot nothing answers for is a
	// fact about the crew that no model's row could carry.
	unbound []config.ModelSlot
}

// crewed hands the reading the crew's own facts. It answers a copy, for
// [spendReading.naming]'s reason: a reading is an immutable answer.
func (r spendReading) crewed(crew spendCrew) spendReading {
	r.crew = crew
	return r
}

// railed hands the reading the day's own limit, for the pointer line at the top
// of the page. Zero is a machine with no daily limit, which the line says in a
// word rather than drawing a denominator nobody set.
func (r spendReading) railed(rail float64) spendReading {
	r.rail = rail
	return r
}

// todayed hands the reading what this machine has spent since midnight
// ([spendDayTotal]). It answers a copy, for [spendReading.naming]'s reason: a
// reading is an immutable answer.
func (r spendReading) todayed(usd float64) spendReading {
	r.today = usd
	return r
}

// spendRailsWord is the second half of the pointer line: where the limits are
// set, named with the door that actually works from here.
//
// IT NAMES `/budget` AND NOT A LETTER. The design asks for `b sets the rails`,
// and on this surface every printable key belongs to a text box — this place has
// one at its foot like every other place — so a bare letter advertised here
// would be a letter the box eats (verbstrip.go's first law states it as the
// product and not a compromise). `b` IS bound where a letter can be bound: on
// the verb strip `→` opens over this row, where it is drawn before it works.
const spendRailsWord = "/budget sets the limits"

// modelName is what to call one model on a row: THE WORD A PERSON SAYS OUT LOUD,
// which is the one this whole tree already spells a model with.
//
// [modelui.ModelWord] takes off the four runs that are provably provenance — the
// vendor prefix, the alias marker, the variant suffix, the release date — and
// hands back anything it does not recognise WHOLE, so a slug this build has never
// seen is still drawn exactly as the provider spells it.
//
// THE CATALOG'S OWN DISPLAY NAME IS DELIBERATELY NOT USED, and it was, for one
// build. What the catalog publishes is `DeepSeek V4 Flash Latest` and
// `Google: Gemini 3.6 Flash` — Title Case, with the vendor back on the front and
// the release pointer back on the end — so a page that preferred it would be the
// ONE surface on this machine calling a model something the status line, /model
// and the crew chips do not. That is the same defect as the scope chip spelled
// two ways, and the design's own `opus 4.1` is nearer this word than that one.
func (r spendReading) modelName(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if word := modelui.ModelWord(id); word != "" {
		return word
	}
	return id
}

// modelRole is the slot one model is bound to, and the empty string for a model
// bound to nothing — which draws NO ROLE WORD at all rather than a blank column
// or a guess. A model can be on the bill for a hundred reasons and be nobody's
// crew today; saying so is the emptiness law.
func (r spendReading) modelRole(id string) string {
	return r.crew.role[spendModelKey(id)]
}

// spendModelKey is the ONE SPELLING OF A MODEL'S IDENTITY on this page, so the
// map the place fills and the row that reads it cannot key it two ways.
//
// IT TAKES THE ALIAS MARKER OFF. A binding a person made through the picker
// carries OpenRouter's leading `~` — the status line draws it, and the crew reads
// it back — while the ledger's own line records the id the request actually went
// out on, without it. Keyed raw, the conversation's own model matched nothing and
// the busiest row on the page wore no role word at all.
func spendModelKey(id string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(id), "~"))
}

// spendStop is what one drawn row is ABOUT, so that `enter` opens the thing the
// row named rather than the row's position. Rows with nothing behind them — the
// header, the sparkline, a section heading — carry the zero value.
type spendStop struct {
	subject session.SubjectSpend
	ok      bool
	// rails marks THE POINTER LINE at the top of the page — the one row whose
	// `enter` opens the Spending tab rather than the thing money was spent on
	// (place_spend.go's [app.openSpendRow]). It is a flag and not a fourth
	// subject kind because it is not a subject at all: nothing was spent on it.
	rails bool
}

// naming hands the reading the titles for the ids it is holding. It answers a
// copy, because a reading is an immutable answer and a caller that mutated one
// in place would be changing a frame that has already been drawn.
func (r spendReading) naming(names map[string]string) spendReading {
	r.names = names
	return r
}

// name is what to call one subject: the title the page joined, and the id
// itself when nobody could name it.
func (r spendReading) name(subject session.SubjectSpend) string {
	if title := strings.TrimSpace(r.names[spendSubjectKey(subject)]); title != "" {
		return title
	}
	return spendSubjectName(subject)
}

// spendSubjectKey is the one spelling of a subject's identity, so the page that
// fills the map and the row that reads it cannot key it two ways.
func spendSubjectKey(subject session.SubjectSpend) string {
	return subject.Kind + "\x00" + strings.TrimSpace(subject.ID)
}

// readSpend answers only from the supplied facts. A zero-priced line is kept
// out because zero means unpriced, and THE EMPTINESS LAW does not let an
// unknown price become a measured free call on screen.
func readSpend(lines []session.UsageLine, win session.UsageWindow, now time.Time) spendReading {
	win = win.Normalized()
	priced := make([]session.UsageLine, 0, len(lines))
	for _, line := range lines {
		if line.USD > 0 && win.Holds(session.UsageLineDay(line)) {
			priced = append(priced, line)
		}
	}
	if len(priced) == 0 {
		return spendReading{window: win, now: now}
	}
	r := spendReading{
		window: win,
		now:    now,
		totals: session.UsageTotals(priced),
		days:   session.UsageByDay(priced, win),
		models: session.UsageByModel(priced),
	}
	// A subject exists only when the ledger names one of its addresses. The
	// grouping reader's default conversation bucket is useful arithmetic, but
	// an addressless line is not evidence for a person-facing role word.
	var subjectPriced []session.UsageLine
	for _, line := range priced {
		if strings.TrimSpace(line.Task) != "" || strings.TrimSpace(line.Standing) != "" || strings.TrimSpace(line.Session) != "" {
			subjectPriced = append(subjectPriced, line)
		}
	}
	r.subjects = session.UsageBySubject(subjectPriced)
	for _, day := range r.days {
		if day.USD > r.loudest.USD {
			r.loudest = day
		}
	}
	if r.loudest.USD > 0 {
		var onDay []session.UsageLine
		for _, line := range subjectPriced {
			if sameSpendBucket(session.UsageLineDay(line), r.loudest.At, win.Grain) {
				onDay = append(onDay, line)
			}
		}
		if subjects := session.UsageBySubject(onDay); len(subjects) > 0 {
			r.loudFor = subjects[0]
		}
	}
	return r
}

func sameSpendBucket(at, bucket time.Time, grain session.UsageGrain) bool {
	switch grain {
	case session.GrainMonth:
		return at.Local().Year() == bucket.Year() && at.Local().Month() == bucket.Month()
	case session.GrainWeek:
		year, week := at.Local().ISOWeek()
		bucketYear, bucketWeek := bucket.ISOWeek()
		return year == bucketYear && week == bucketWeek
	default:
		a := at.Local()
		return a.Year() == bucket.Year() && a.YearDay() == bucket.YearDay()
	}
}

// rows applies the page's emptiness and width laws after the session reader
// has done the arithmetic. Every returned row is already clipped in terminal
// cells; colour sequences never participate in the width decision.
func (r spendReading) rows(width int, pal palette) []string {
	rows, _ := r.body(width, pal)
	return rows
}

// body is [spendReading.rows] with the door beside each row: what that row is
// about, for the `enter` that opens it. The two are ONE function because a hit
// map written by anything other than the draw is a hit map that resolves a
// keypress against a row the draw did not put there — the law every hit map on
// this surface is held to (home's own says it first).
func (r spendReading) body(width int, pal palette) ([]string, []spendStop) {
	if width < 1 || r.totals.USD <= 0 {
		return nil, nil
	}
	var out []string
	// doors are recorded BY THE INDEX THE ROW LANDED AT, taken as it is appended.
	// [appendPlaceSection] eats a trailing blank before it writes a heading, so a
	// second slice grown in lockstep would come apart by one row exactly where
	// the sections meet — and a hit map off by one row is a `f forget it` on the
	// wrong line.
	doors := map[int]session.SubjectSpend{}
	// THE POINTER LINE LEADS THE PAGE, and it is a reading and not an editor —
	// which is what keeps this place's own law intact ("there is no budget editor
	// here and there will not be one"). It says what the day has cost against
	// what it is allowed and where that second figure is set; `enter` on it walks
	// to the one editor money has.
	rails := len(out)
	out = append(out, r.railsRow(width, pal))
	out = append(out, r.windowHeaderRow(width, pal))

	if spark := r.sparkline(width); spark != "" {
		out = append(out, pal.data(spark))
		if axis := r.sparkAxis(ansi.StringWidth(spark), pal); axis != "" {
			out = append(out, axis)
		}
	}
	if loud, subject, door := r.loudestRow(width, pal); loud != "" {
		// AND THE LOUDEST DAY IS A DOOR, because the row names a thing money was
		// spent on exactly as the rows under `what it was for` do — and the word
		// on its right now says `enter opens it`, which is a key drawn and
		// therefore a key that has to work.
		if door {
			doors[len(out)] = subject
		}
		out = append(out, loud)
	}

	if len(r.models) > 0 || len(r.crew.unbound) > 0 {
		out = appendPlaceSection(out, pal.dim(fit(spendModelsWord, width)))
		for _, model := range r.models {
			out = append(out, r.modelRow(model, width, pal))
		}
		// AND THE SLOTS NOTHING ANSWERS FOR, under the models that do. A slot with
		// no binding has no line in the ledger to be found on and would simply be
		// missing from a table built out of spending — which is the one reading
		// this column must not give, because "planning costs nothing" and "nothing
		// is bound to planning" are opposite facts about the same blank.
		for _, slot := range r.crew.unbound {
			out = append(out, spendUnboundRow(slot, width, pal))
		}
	}
	if len(r.subjects) > 0 {
		out = appendPlaceSection(out, pal.dim(fit("what it was for", width)))
		shown := len(r.subjects)
		if shown > spendSubjectCap {
			shown = spendSubjectCap
		}
		for _, subject := range r.subjects[:shown] {
			doors[len(out)] = subject
			out = append(out, spendSubjectRow(subject, r.name(subject), width, pal))
		}
		if more := len(r.subjects) - shown; more > 0 {
			out = append(out, pal.dim(fit(foldLine(more, ""), width)))
		}
	}
	stops := make([]spendStop, len(out))
	for at, subject := range doors {
		stops[at] = spendStop{subject: subject, ok: true}
	}
	stops[rails] = spendStop{ok: true, rails: true}
	return out, stops
}

// railsRow is the pointer line: what today has cost, against what the day is
// allowed, and where that second figure is set.
//
//	today $3.42 of $500 · /budget sets the limits
//
// IT IS A READING AND NOT AN EDITOR. This place answers "what did it cost" and
// its own law says the allowance is not edited here; what was missing was the
// POINTER — a person who has just read the bill had nowhere to go — and a
// sentence naming the door is not a second door.
//
// THE EMPTINESS LAW HOLDS ON BOTH FIGURES. A day with nothing on it says nothing
// about today, and a machine with no daily limit says `no limit` rather than
// drawing a fraction with nothing under the line.
func (r spendReading) railsRow(width int, pal palette) string {
	fields := []rowField{}
	if today := r.today; today > 0 {
		// THE POINTER LINE USES [dollars] AND NOT THIS PAGE'S OWN SLIVER WORD.
		// It is the same reading Settings→Spending's `today` row draws and a door
		// onto that row, and issue #269's whole law is that one number reads the
		// same wherever it is met — a line saying `under a cent` beside a tab
		// saying `$0.0003` is the four-numbers defect in miniature. The sliver
		// word stays where it belongs, on this page's own model and subject rows
		// ([spendMoneyWord]), which are a table of many figures rather than one
		// total quoted in two places.
		figure := dollars(today)
		switch {
		case r.rail > 0:
			fields = append(fields, rowSay("today "+figure+" of "+railFigure(r.rail),
				figure+" of "+railFigure(r.rail), figure))
		default:
			fields = append(fields, rowSay("today "+figure+" · "+config.NoLimitWord,
				"today "+figure, figure))
		}
	}
	// AND WHAT THE MACHINE COULD NOT WRITE DOWN, ahead of the door, because it
	// is a fact ABOUT the figure to its left: a ledger short of rows reads as a
	// cheaper day than the one that happened (settingspend.go's
	// [unwrittenReading] states the whole rule). Nothing is said when nothing
	// was lost.
	if r.unwritten > 0 {
		figure := strconv.FormatInt(r.unwritten, 10)
		fields = append(fields, rowSay(figure+" "+spendUnwrittenSaid, figure+" unwritten", figure))
	}
	fields = append(fields, rowSay(spendRailsWord, "/budget"))
	return pal.dim(fit(rowTail(fields, width), width))
}

// spendDayTotal is WHAT ONE DAY COST, summed off ledger lines — and it is THE
// arithmetic behind that figure everywhere it is drawn.
//
// IT IS A PACKAGE FUNCTION AND NOT A METHOD ON THE READING because the two
// surfaces that draw the day do not share a page. The spend place hands it the
// lines it is already holding ([app.rebuildSpend]); the pulse at the top of every
// place hands it the lines it read for the purpose ([app.machineSpentToday]).
// One function, one number: the top line and the body of the spend place cannot
// disagree about the day on the frame a person is looking at, which is exactly
// what they used to do.
//
// A ZERO-PRICED LINE IS LEFT OUT, for [readSpend]'s reason: zero means unpriced,
// and the emptiness law does not let an unknown price become a measured free
// call.
func spendDayTotal(lines []session.UsageLine, now time.Time) float64 {
	total := 0.0
	for _, line := range lines {
		if line.USD > 0 && sameSpendBucket(session.UsageLineDay(line), now, session.GrainDay) {
			total += line.USD
		}
	}
	return total
}

// windowHeaderRow is what the window came to on the left and the window itself
// on the right, drawn by the one head row every place with a time window shares
// ([placeHeadRow], placeprose.go).
//
// THE SPAN IS SAID ONCE, AND IT IS SAID BETWEEN THE ARROWS. It used to lead the
// left field — `aug 12 – aug 25 · $5.94 · 1.1M tokens` — while the right field
// named the keys without the span, so the label a person moves and the label
// they read were two different runs of one line. SCREEN 3d says which of the two
// is right: "the label between the arrows is the control and the reading at
// once". So the left is the FIGURES, which is what the window came to, and the
// control carries the dates.
//
// A FRAME TOO NARROW FOR THE CONTROL DRAWS THE FIGURES ALONE, and the arrows do
// nothing there — one predicate answers the paint and the keys.
func (r spendReading) windowHeaderRow(width int, pal palette) string {
	return placeHeadRow(width, r.headWords(width), r.paintedHead(width, pal), r.window, pal)
}

// headWords is the head line's LEFT FIELD — what the window came to — as plain
// text, and [spendReading.paintedHead] is the same list in its own inks. They
// are built from one sequence so the measured line and the drawn line cannot
// come apart on a narrow frame.
//
// IT SAYS WHICH TOTAL THIS IS, AND IT DID NOT. The page stacked two dollar
// figures — `today $0.13 of $500` and, under it, `$2.05 · 326.5k tokens` — and
// the only thing tying the second to a fortnight was a date span the control
// draws at the OTHER END of the same line: a hundred and forty-five cells away
// at 160 columns, which is not a label. So the field leads with the span in
// words — `14 days came to $2.05 · 326.5k tokens` — and the two money figures on
// this page now each carry the period they are the total of, in one grammar.
//
// THE DATES THEMSELVES ARE STILL NOT IN IT. They are the control's, and SCREEN
// 3d settles that: "the label between the arrows is the control and the reading
// at once". `14 days` is how MANY, which the control does not say and which is
// what makes the figure beside it readable.
//
// IT IS SIZED WITH THE CONTROL'S CELLS ALREADY SPENT, because the head's own
// width is what decides whether the arrows are drawn at all
// ([placeWindowFits]). A lead added without that reservation would have bought
// this sentence at 60 columns by taking the window keys away.
func (r spendReading) headWords(width int) string {
	fields := r.headFields()
	if len(fields) == 0 {
		// A WINDOW THAT CAME TO NOTHING SAYS SO IN WORDS AND NOT AS A ZERO, which
		// is the same edge the tasks place's own head line has: `$0.00` is exactly
		// the figure the emptiness law forbids, and the control beside this
		// sentence already names the fortnight it is about.
		return spendNothingWord
	}
	room := width - ansi.StringWidth(placeWindowWords(r.window)) - placeHeadGap
	if room < 1 {
		room = width
	}
	if words := rowTail(fields, room); words != "" {
		return words
	}
	// A FRAME WITH ROOM FOR NOTHING STILL SAYS THE MONEY. The fitter answers ""
	// when even the shortest spelling is over the room, and a head row with no
	// head on it would leave the control floating against an empty line.
	return spendMoneyWord(r.totals.USD)
}

// headFields is the head's ranked facts: what the window came to, then how many
// tokens it took. The lead spelling carries the span in words and the shorter
// ones give it up before the figure it labels ever goes.
func (r spendReading) headFields() []rowField {
	if r.totals.USD <= 0 && r.totals.Tokens <= 0 {
		return nil
	}
	fields := make([]rowField, 0, 2)
	if r.totals.USD > 0 {
		money := spendMoneyWord(r.totals.USD)
		if span := spendSpanWord(r.window); span != "" {
			fields = append(fields, rowSay(span+" came to "+money, span+" · "+money, money))
		} else {
			fields = append(fields, rowSay(money))
		}
	}
	if r.totals.Tokens > 0 {
		fields = append(fields, rowSay(tokenWord(r.totals.Tokens)+" tokens", tokenWord(r.totals.Tokens)))
	}
	return fields
}

// spendSpanWord is HOW MUCH TIME the head's figure is the total of, in the
// grain's own noun: `14 days`, `8 weeks`, `6 months`.
//
// IT COUNTS BUCKETS AND NOT DAYS, so the word says the same thing the chart
// under it draws — one bar per bucket — and a window zoomed to months does not
// go on calling itself a number of days.
func spendSpanWord(win session.UsageWindow) string {
	buckets := win.Buckets()
	if buckets < 1 {
		return ""
	}
	word := "day"
	switch win.Normalized().Grain {
	case session.GrainMonth:
		word = "month"
	case session.GrainWeek:
		word = "week"
	}
	if buckets != 1 {
		word += "s"
	}
	return groupedInt(buckets) + " " + word
}

// spendNothingWord is the head line over a window nothing was spent in. It is
// NOT [spendTeach]: a machine that has spent nothing is being taught what this
// place is for, and a machine that has simply been paged onto a quiet fortnight
// wants the control that pages it back (place_spend.go's [spendPage.held]).
const spendNothingWord = "nothing spent"

// paintedHead is [spendReading.headWords] in its own inks — the money in the
// money hue, the token count in the reading tier, and the words that say what
// they are dim.
//
// IT PAINTS THE LINE THE FITTER ACTUALLY CHOSE rather than building a second
// one. The head has three spellings and the frame decides which, so a painted
// twin assembled from the longest would measure differently from the string the
// row was laid out against — which is the exact way a right-aligned control ends
// up off the edge.
func (r spendReading) paintedHead(width int, pal palette) string {
	plain := r.headWords(width)
	if plain == spendNothingWord {
		return pal.dim(plain)
	}
	money, toks := spendMoneyWord(r.totals.USD), tokenWord(r.totals.Tokens)
	parts := strings.Split(plain, rowSep)
	for at, part := range parts {
		switch {
		case r.totals.USD > 0 && money != "" && strings.HasSuffix(part, money):
			parts[at] = pal.dim(strings.TrimSuffix(part, money)) + placeMoneyInk(pal)(money)
		case r.totals.Tokens > 0 && toks != "" && strings.HasPrefix(part, toks):
			parts[at] = pal.data(toks) + pal.dim(strings.TrimPrefix(part, toks))
		default:
			parts[at] = pal.dim(part)
		}
	}
	return strings.Join(parts, pal.dim(rowSep))
}

// spendSparkCells is the WIDEST one bucket may be drawn. A day given more cells
// than this stops reading as a bar in a line and starts reading as a block of
// colour, and the axis words under either end are six or seven cells themselves
// — so the chart takes the frame's room up to this and leaves the rest as air.
const spendSparkCells = 8

// sparkline is the window's buckets as one chart, SIZED TO THE FRAME IT IS DRAWN
// ON.
//
// IT USED TO BE ONE CELL PER DAY AT EVERY WIDTH — fourteen cells of braille at
// the far left of a hundred-and-sixty-cell line, 9% of the row, out of which
// nobody can read a fortnight's rhythm. The buckets are what the window says
// they are; how many CELLS each of them gets is the frame's answer, and this is
// where it is given.
func (r spendReading) sparkline(width int) string {
	peak := 0.0
	for _, day := range r.days {
		if day.USD > peak {
			peak = day.USD
		}
	}
	if peak <= 0 || width < 1 || len(r.days) == 0 {
		return ""
	}
	cells := width / len(r.days)
	if cells > spendSparkCells {
		cells = spendSparkCells
	}
	if cells < 1 {
		cells = 1
	}
	var b strings.Builder
	for _, day := range r.days {
		b.WriteString(strings.Repeat(tokens.Sparkline(day.USD/peak), cells))
	}
	return fit(b.String(), width)
}

// sparkAxis is the two ends of the chart above it, AND BOTH OF THEM ARE DATES.
//
// The left was a date and the right was a figure — `aug 20` … `today $0.13` —
// so the axis's two ends were not the same kind of thing and the shape between
// them had no scale; the money on the right was also the figure the pointer line
// two rows above had already given, which is one-source-of-truth broken in the
// smallest way available. The money belongs to the rails row that owns it, and
// what an axis owes a chart is where it starts and where it stops.
//
// IT IS AS WIDE AS THE CHART AND NOT AS THE FRAME, so the right-hand date sits
// under the last bucket rather than out at the edge of a line the chart does not
// reach.
func (r spendReading) sparkAxis(width int, pal palette) string {
	if width < 1 || len(r.days) == 0 {
		return ""
	}
	left := r.days[0].Label
	right := ""
	if last := r.days[len(r.days)-1]; len(r.days) > 1 {
		right = last.Label
		// AND THE LAST BUCKET IS CALLED `today` WHEN IT IS TODAY, which is the
		// word the rest of this surface calls it ([spendTodayWord]) and the one
		// fact a person reads the right end of this chart for.
		if sameSpendBucket(r.now, last.At, r.window.Grain) {
			right = spendTodayWord
		}
	}
	if left == "" && right == "" {
		return ""
	}
	return spendSides(width, left, right, pal.dim, pal.dim)
}

// loudestRow is the day that cost the most, what it was mostly spent on, and —
// where the sentence leaves room for it — the door onto that thing.
//
// THE DOOR IS SAID THE WAY THIS SURFACE SAYS A DOOR. It was the bare noun
// `tasks`, right-aligned with nothing joining it to the sentence, which reads as
// a fourth fact about the day: a person cannot tell whether `tasks` is a count, a
// category or a place. It names the key now — and because it names the key, the
// row is a stop, so `enter` on it does what the word says.
//
// AND IT IS THE FIRST THING OFF THE ROW. rowfit's law 1: the sentence is the
// identity, the door is a fact about it, so a frame that cannot hold both keeps
// the sentence whole and draws no door at all — never `rebuild-the-frame… tasks`,
// which was the reading at 60 columns.
func (r spendReading) loudestRow(width int, pal palette) (string, session.SubjectSpend, bool) {
	if r.loudest.USD <= 0 {
		return "", session.SubjectSpend{}, false
	}
	name := r.name(r.loudFor)
	left := r.loudest.Label + " was the loudest day — " + spendMoneyWord(r.loudest.USD)
	if name != "" {
		left += ", " + name
	}
	door, opens := "", false
	if word := spendDoorWord(r.loudFor); word.known() && strings.TrimSpace(r.loudFor.ID) != "" {
		door = rowTail([]rowField{word}, width-ansi.StringWidth(left)-rowGutter-1)
		opens = door != ""
	}
	return spendSides(width, left, door, pal.dim, pal.dim), r.loudFor, opens
}

// spendDoorWord is where `enter` on a row goes, in the words of the place it
// opens ([app.openSpendRow] is the same three-way switch). A subject of a kind
// this build has no door for says nothing at all rather than naming a key that
// would do nothing.
func spendDoorWord(subject session.SubjectSpend) rowField {
	switch subject.Kind {
	case session.SubjectTask:
		return rowSay("enter opens it in tasks", "in tasks")
	case session.SubjectStanding:
		return rowSay("enter opens it in standing", "in standing")
	case session.SubjectConversation:
		return rowSay("enter opens it on home", "on home")
	}
	return rowSay()
}

// spendModelsWord is the models table's caption, and it is SCREEN 2c's own. The
// column says the role each model was BOUND to — the crew binding a person can
// go and change — and not the auxiliary word one call gave itself, which is what
// the caption used to promise and what the table used to draw.
const spendModelsWord = "what ran it · by the model, and the role it was bound to"

// spendUnboundRow is one role slot with nothing bound to it:
//
//	· planning · unbound · follows execution
//
// THERE IS NO FIGURE ON IT, and the design's own em-dash is the one thing here
// that is not followed. A slot nothing is bound to has spent nothing THAT CAN BE
// FOUND — every line in the ledger names a model, not a slot — so a figure in
// that column would be a measurement nobody took, and the emptiness law is that
// an unknown renders as nothing rather than as a mark standing in for one.
func spendUnboundRow(slot config.ModelSlot, width int, pal palette) string {
	left := pal.dim(tokens.GlyphProseBullet+" ") + pal.data(slot.Label) + pal.dim(" · "+spendUnboundWord)
	if slot.Follows != "" {
		left += pal.dim(" · follows " + slot.Follows)
	}
	return spendSides(width, left, "", func(s string) string { return s }, pal.dim)
}

// spendUnboundWord is what a role slot with nothing behind it says, and it is
// the design's own word. It is a fact about the settings rather than machinery
// vocabulary: the row a person would bind is empty, and the clause after it says
// what runs in the meantime.
const spendUnboundWord = "unbound"

func (r spendReading) modelRow(model session.ModelSpend, width int, pal palette) string {
	name := r.modelName(model.Model)
	role := strings.TrimSpace(r.modelRole(model.Model))
	money := spendMoneyWord(model.USD)
	left := tokens.GlyphProseBullet
	if name != "" {
		left += " " + name
	}
	if role != "" {
		if name != "" {
			left += " ·"
		}
		left += " " + role
	}
	stats := spendModelStats(model)
	if width >= 80 {
		bar := spendBar(model.USD/r.models[0].USD, spendModelBarCap)
		if bar != "" {
			left += " " + bar
		}
	}
	if width >= 80 && stats != "" {
		left += " " + stats
	}
	return spendSides(width, left, money, func(s string) string {
		// The model is the payload; the role and counts remain quiet even though
		// they share one fitted left field at narrow widths.
		prefix := tokens.GlyphProseBullet + " " + name
		if name != "" && strings.HasPrefix(s, prefix) {
			return pal.dim(tokens.GlyphProseBullet+" ") + pal.data(name) + pal.dim(strings.TrimPrefix(s, prefix))
		}
		return pal.dim(s)
	}, placeMoneyInk(pal))
}

func spendModelStats(model session.ModelSpend) string {
	var parts []string
	if model.Calls > 0 {
		parts = append(parts, fmt.Sprintf("%s calls", groupedInt(model.Calls)))
	}
	if model.Tokens > 0 {
		parts = append(parts, tokenWord(model.Tokens))
	}
	return strings.Join(parts, " · ")
}

func spendBar(fraction float64, cap int) string {
	if !(fraction > 0) || cap < 1 {
		return ""
	}
	cells := int(fraction*float64(cap) + 0.5)
	if cells < 1 {
		cells = 1
	}
	if cells > cap {
		cells = cap
	}
	return strings.Repeat("█", cells)
}

func spendSubjectRow(subject session.SubjectSpend, name string, width int, pal palette) string {
	tag := filepath.Base(strings.TrimSpace(subject.Workspace))
	if subject.Kind == session.SubjectStanding && subject.Calls > 0 {
		tag = fmt.Sprintf("standing · %d firings", subject.Calls)
	}
	kind := subject.Label
	switch {
	case subject.Kind == session.SubjectStanding && subject.Calls > 0 && subject.USD/float64(subject.Calls) < 0.005:
		kind = "under a cent a run"
	case subject.Kind == session.SubjectStanding && strings.HasPrefix(tag, subject.Label):
		// A PROMISE'S TAG ALREADY SAYS WHAT KIND OF THING IT IS — `standing · 14
		// firings` — so the label after it is that word a second time on one row,
		// which is the drift the one-source-of-truth law is about. The design's own
		// row spends that field on something a person did not already know.
		kind = ""
	}
	var left strings.Builder
	left.WriteString(pal.dim(tokens.GlyphProseBullet + " "))
	left.WriteString(pal.data(name))
	for _, word := range nonempty(tag, kind) {
		left.WriteString(pal.dim(" · " + word))
	}
	return spendSides(width, left.String(), spendMoneyWord(subject.USD), func(s string) string { return s }, placeMoneyInk(pal))
}

// spendMoneyWord keeps tui3's one dollar formatter while applying the page's
// extra rule for a measured sliver: a table of many rows reads better with a
// word than with a column of four-decimal fractions
// (docs/design/home-rethink/SCREENS.txt states the rule).
//
// IT IS FOR THIS PAGE'S ROWS AND NOT FOR A TOTAL ANOTHER SURFACE ALSO QUOTES. The
// pointer line quotes the same day figure the Spending tab does, so it uses
// [dollars] like every other surface that quotes it ([spendReading.railsRow]).
func spendMoneyWord(usd float64) string {
	if usd > 0 && usd < 0.005 {
		return "under a cent"
	}
	return dollars(usd)
}

func spendSubjectName(subject session.SubjectSpend) string {
	if id := strings.TrimSpace(subject.ID); id != "" {
		return id
	}
	return subject.Label
}

func nonempty(words ...string) []string {
	kept := words[:0]
	for _, word := range words {
		if strings.TrimSpace(word) != "" {
			kept = append(kept, word)
		}
	}
	return kept
}

// spendSides is the page's one right-flush seam. It fits in printable cells
// before painting, so ANSI sequences cannot steal or create layout space.
func spendSides(width int, left, right string, leftInk, rightInk func(string) string) string {
	if width < 1 {
		return ""
	}
	right = fit(right, width)
	room := width - ansi.StringWidth(right)
	if right != "" && room > 0 {
		room--
	}
	left = fit(left, room)
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if right == "" {
		gap = 0
	}
	if gap < 0 {
		gap = 0
	}
	return leftInk(left) + strings.Repeat(" ", gap) + rightInk(right)
}

// step gives the four drawn arrow chords their complete grammar. Unknown keys
// leave the reading alone because an undrawn key never acts on this surface.
func (r spendReading) step(win session.UsageWindow, key string) session.UsageWindow {
	return placeWindowStep(win, key)
}

// THE EMPTY SPEND PAGE IS THE PLACE'S OWN TEACHING AND NOT A SECOND ONE. A
// ledger with nothing priced in the window draws no rows at all
// ([spendReading.body] answers nil), and the place's body then draws the three
// sentences saying what spend is for ([spendTeach], place_spend.go). This file
// used to carry a near-identical trio of its own; two teachings for one place is
// two places for the wording to drift, and that one is what the manual quotes.
