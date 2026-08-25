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
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
}

// spendStop is what one drawn row is ABOUT, so that `enter` opens the thing the
// row named rather than the row's position. Rows with nothing behind them — the
// header, the sparkline, a section heading — carry the zero value.
type spendStop struct {
	subject session.SubjectSpend
	ok      bool
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
	out = append(out, r.windowHeaderRow(width, pal))

	if spark := r.sparkline(); spark != "" {
		out = append(out, pal.data(fit(spark, width)))
		left := ""
		if len(r.days) > 0 {
			left = r.days[0].Label
		}
		right := ""
		for _, day := range r.days {
			if sameSpendBucket(r.now, day.At, r.window.Grain) && day.USD > 0 {
				right = "today " + spendMoneyWord(day.USD)
				break
			}
		}
		if left != "" || right != "" {
			out = append(out, spendSides(width, left, right, pal.dim, placeMoneyInk(pal)))
		}
	}
	if loud := r.loudestRow(width, pal); loud != "" {
		out = append(out, loud)
	}

	if len(r.models) > 0 {
		out = appendPlaceSection(out, pal.dim(fit("what ran it · by the model, and the role it named", width)))
		for _, model := range r.models {
			out = append(out, r.modelRow(model, width, pal))
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
	return out, stops
}

func (r spendReading) windowHeader() string {
	parts := []string{r.window.Label()}
	if r.totals.USD > 0 {
		parts = append(parts, spendMoneyWord(r.totals.USD))
	}
	if r.totals.Tokens > 0 {
		parts = append(parts, tokenWord(r.totals.Tokens)+" tokens")
	}
	return strings.Join(parts, " · ")
}

func (r spendReading) windowHeaderRow(width int, pal palette) string {
	var left strings.Builder
	left.WriteString(pal.ink(r.window.Label()))
	if r.totals.USD > 0 {
		left.WriteString(pal.dim(" · "))
		left.WriteString(placeMoneyInk(pal)(spendMoneyWord(r.totals.USD)))
	}
	if r.totals.Tokens > 0 {
		left.WriteString(pal.dim(" · "))
		left.WriteString(pal.data(tokenWord(r.totals.Tokens)))
		left.WriteString(pal.dim(" tokens"))
	}
	return spendSides(width, left.String(), "shift+←→ window · shift+↑ coarser", func(s string) string { return s }, pal.dim)
}

func (r spendReading) sparkline() string {
	peak := 0.0
	for _, day := range r.days {
		if day.USD > peak {
			peak = day.USD
		}
	}
	if peak <= 0 {
		return ""
	}
	var b strings.Builder
	for _, day := range r.days {
		b.WriteString(tokens.Sparkline(day.USD / peak))
	}
	return b.String()
}

func (r spendReading) loudestRow(width int, pal palette) string {
	if r.loudest.USD <= 0 {
		return ""
	}
	name := r.name(r.loudFor)
	left := r.loudest.Label + " was the loudest day — " + spendMoneyWord(r.loudest.USD)
	if name != "" {
		left += ", " + name
	}
	door := ""
	if r.loudFor.Kind == session.SubjectTask {
		door = "tasks"
	}
	return spendSides(width, left, door, pal.dim, pal.dim)
}

func (r spendReading) modelRow(model session.ModelSpend, width int, pal palette) string {
	name := strings.TrimSpace(model.Model)
	role := strings.TrimSpace(model.Role)
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
	if subject.Kind == session.SubjectStanding && subject.Calls > 0 && subject.USD/float64(subject.Calls) < 0.005 {
		kind = "under a cent a run"
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
// extra rule for a measured sliver: rounding it to zero would read as free.
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

// THE EMPTY SPEND PAGE IS THE ROUTER'S TEACHING AND NOT A SECOND ONE. A ledger
// with nothing priced in the window draws no rows at all ([spendReading.body]
// answers nil), and the frame then falls through to the three sentences every
// place-with-no-body says — [page.explain], drawn by teachplace.go. This file
// used to carry a near-identical trio of its own; two teachings for one place is
// two places for the wording to drift, and the router's is the one the manual
// already quotes.
