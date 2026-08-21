package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── /status AND /cost: THE STATUS LINE, ASKED OUT LOUD ──────────────────────
//
// The row at the bottom of the frame answers both of these questions already,
// and it answers them by dropping whatever does not fit: at [hudTight] the
// cache share and the forecast are gone, and at [tierPhone] nine of eleven
// facts have moved to a sheet a FINGER opens ([app.deckPress]). A person on a
// keyboard, on a narrow terminal, with the mouse turned off (config's ui.mouse)
// has no door onto any of it. These two commands are that door.
//
// THEY ARE NOTES AND NOT PANELS. Everything else that shows a list on this
// surface takes the screen — the picker, the settings, the sheet — and that is
// right for a thing you ACT on and wrong for a thing you READ: the answer to
// "what has this cost me" is three lines long, it is wanted while a turn is
// running, and it is wanted again in ten minutes with the first answer still
// above it to compare against. A note stays in the conversation and scrolls
// with it; a sheet has to be dismissed before the person can type the next
// thing, which is the one thing they opened it to decide.
//
// AND NEITHER OF THEM ASSEMBLES A METER OF ITS OWN. /status prints
// [app.deckItems] — the same list, built by the same functions, that the phone
// sheet draws — and /cost prints the figures the status line's own formatters
// make of the same fields. A second assembly of the context meter is exactly
// what statusdeck.go's list exists to prevent, and a command is not an
// exception to it.

// statusText is /status: every fact the status line can carry, one per line.
//
// The list is [app.deckItems] whole, because the phone sheet and this command
// are ONE surface asked for in two ways — its head even says "status"
// ([deckSheetTitle]). A row that appears on one and not the other would make
// "what does this session say about itself" a question with two answers.
//
// It differs from the sheet in exactly two places, and both differences are
// about the MEDIUM rather than about the facts:
//
//	the file      a path is not a status-line segment at any width — it is a
//	              thing a person copies into another program — and a note is
//	              selectable text while a sheet at forty-four columns is a path
//	              with its middle cut out. /help prints it for the same reason.
//	a zero bill   the status line keeps "$0.00" because it is a LIVE row, and a
//	              segment that came into existence on the first priced turn
//	              would shove every segment beside it sideways. A note is
//	              written once and read once, so design-law-v2 §16 applies to it
//	              plainly — and /cost's answer to the same session is "nothing
//	              spent yet", which this list has to agree with.
func (a *app) statusText() string {
	// The totals are refreshed FIRST. A command typed between turns must answer
	// from what the session holds now and not from whatever the last event left
	// on these fields ([app.take] keeps the larger of the two, so this can only
	// move figures forward).
	a.refreshUsage()
	sheet := a.deckItems()
	items := make([]deckItem, 0, len(sheet)+1)
	for _, item := range sheet {
		if item.label == deckSegWords[segCost] && a.cost <= 0 {
			continue
		}
		// THE PLACE IS SAID IN FULL HERE, and on a remote session that is the
		// third difference of medium this function makes. The status line and the
		// sheet carry an abbreviated path because they are rows in a frame; this
		// is a note a person reads once and copies out of, and `devbox:/srv/app`
		// is the thing they would paste into scp or into another terminal. The
		// abbreviated form would make them reconstruct it.
		if a.hosted() && item.label == "place" {
			item.value = a.hostedPath(a.workspace)
		}
		// AND AN OWNED SESSION SAYS WHERE IT ACTUALLY IS, here and nowhere else.
		// The frame calls it "aforge" because the path is bookkeeping a person
		// did not ask to read (host.go's [ownedWord]) — but this note is the one
		// surface whose whole job is the full truth, and "where did my files go"
		// is exactly the question somebody opens it to answer.
		if a.owned && !a.hosted() && item.label == "place" {
			item.value = a.workspace
		}
		items = append(items, item)
	}
	// phone lane: `keeping watch` used to be added HERE and nowhere else, which
	// made it the one fact /status carried that the phone's own sheet did not —
	// on the tier where every other fact had moved into that sheet. It is part of
	// [app.deckItems] now, so both surfaces say it and neither says it twice.
	if a.file != "" {
		// AND THE JOURNAL IS ON WHOSE DISK. A session file is the one path on
		// this list a person is actively invited to copy, and over a connection it
		// is the far machine's — so it is named the way they would have to name it
		// to reach it, rather than as a bare path that looks like one of theirs.
		items = append(items, deckItem{label: "file", value: a.hostedPath(a.file)})
	}
	return labelledLines(items)
}

// costText is /cost: what this conversation has spent, and on what.
//
//	spend        $0.42
//	tokens       48.1k in · 3.2k out
//	cache        31.2k read · saved $0.0180
//	model calls  14
//	time         3m12s
//
// EVERY LINE IS DROPPED WHEN ITS FIGURE IS NOT THERE, which is design-law-v2
// §16 said about a command instead of about a row: a session that has not been
// told what it cost must say nothing about money rather than say "$0.00", and a
// provider that publishes no cache accounting must say nothing about caches
// rather than teach a person their cache never hits (session.Usage says the
// same about its own zeroes).
//
// THE ONE THING IT WILL NOT DO IS GO SILENT. A command typed on purpose that
// prints nothing reads as a command that broke, so a session with no figures at
// all gets a sentence saying so — /select's answer to the same problem.
func (a *app) costText() string {
	// This is [app.refreshUsage] with the report KEPT, because two of the five
	// lines are not on the fields it folds into: [app.take] holds the running
	// totals a status line needs and drops the turn count and the wall clock,
	// which are the session's own and are read straight off the report.
	var u session.Usage
	if a.agent != nil {
		u = a.agent.Usage()
	}
	a.take(u)

	items := make([]deckItem, 0, 5)
	add := func(label, value string) {
		if value != "" {
			items = append(items, deckItem{label: label, value: value})
		}
	}

	// The label is the sheet's word for the same figure ([deckSegWords]), and so
	// are "context" and "cache" — a person who has read one of these surfaces has
	// read the vocabulary of the other.
	if a.cost > 0 {
		add(deckSegWords[segCost], dollars(a.cost))
	}
	add("tokens", tokenHalves(a.inputTokens, a.outputTokens, a.tokens))
	add(deckSegWords[segCache], cacheWords(a.cacheRead, a.cacheSaved))
	// "model calls" and not "turns". A turn is what the PERSON took — one message
	// and everything that answered it — and a conversation of nine messages that
	// reports "turns 41" is a surface using a person's word for a machine's
	// count.
	//
	// It is EVERY request that went to the provider: the turn's own calls, the
	// auxiliary ones nobody asked for by name (the session's title, a judge, a
	// picture being looked at), and every call a task's agents made on their own
	// lanes. That is what makes it the honest denominator for the bill above it,
	// which is the sum over exactly this many — Turns counts only the
	// conversation's own steps by law, and would have named a smaller number
	// than the money was spent over.
	if u.Calls > 0 {
		add("model calls", itoa(u.Calls))
	}
	add("time", tookWord(u.Duration))

	if len(items) == 0 {
		return "nothing spent yet — this session has not sent a turn."
	}
	return labelledLines(items)
}

// tokenHalves is the tokens line: what was sent against what was written.
//
// The two halves are what a person can ACT on — a prompt that keeps growing is
// a conversation to compact, and output is what the model was actually asked to
// do — and the combined figure is the fallback for a session that has only ever
// been given the total. It is never "0 in · 0 out": that is a session nobody has
// told anything about, and [tokenWord] spelling its zero is not permission to
// print it.
func tokenHalves(in, out, both int) string {
	switch {
	case in > 0 && out > 0:
		return tokenWord(in) + " in · " + tokenWord(out) + " out"
	case in > 0:
		return tokenWord(in) + " in"
	case out > 0:
		return tokenWord(out) + " out"
	case both > 0:
		return tokenWord(both)
	default:
		return ""
	}
}

// cacheWords is the cache line: what came off a warm prefix, and what that was
// worth.
//
// The cash is printed ONLY when it is real, on the terms [app.cacheSaved] is
// held: it is accumulated per turn at the moment BOTH a prompt price and a
// cache-read price are published, and a session that had neither cannot have it
// worked out afterwards. So a session on a model that publishes no cache-read
// price says how much it read and stops there, rather than learning to say it
// saved the whole prompt price. The count leads and the money follows it, which is
// the order [app.warmSegment] puts the same pair in on the status line.
func cacheWords(read int, saved float64) string {
	if read <= 0 {
		return ""
	}
	word := tokenWord(read) + " read"
	if saved > 0 {
		word += " · saved " + savedWord(saved)
	}
	return word
}

// labelledLines lays a note out in two columns: the label, then the fact,
// aligned down the page so the eye reads the facts rather than hunting them.
//
// It is the sheet's own geometry ([deckLabelWidth]) and /help's own rendering
// (commands.go's [helpText]) — a note is prose in the transcript, so it wraps
// and nothing is cut to a width this function does not know.
func labelledLines(items []deckItem) string {
	width := deckLabelWidth(items)
	lines := make([]string, 0, len(items))
	for _, item := range items {
		pad := width - ansi.StringWidth(item.label)
		lines = append(lines, item.label+strings.Repeat(" ", pad)+item.value)
	}
	return strings.Join(lines, "\n")
}
