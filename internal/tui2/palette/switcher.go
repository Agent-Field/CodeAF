package palette

import (
	"image"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Switcher is the chats switcher (chat-simplify.md 5.2's J3, 5.3's `switcher`
// row): the quiet overlay that lists the working conversations and moves the
// window into one of them.
//
// It is the THIRD component built on this package's one list, for the reason
// the doc gives for there being two: 5.22's discoverability holds by
// construction only while every list in the product is the same list. What this
// surface adds over [Palette] is what a THREAD is — a name, the line you left it
// on, how long ago that was — and what it removes is everything a catalog of
// everything carries. There is no drill, no settings group, no jobs read.
//
// The five laws of 5.1, as they land here:
//
//   - ONE ORNAMENT. A row draws `●` only where a delivery landed in that thread
//     and nobody has read it. There is no count, no badge, and no second colour:
//     the dot is present or it is not, which is the whole of what a person needs
//     before they decide which conversation to open.
//   - THE LIST IS A SWITCHER, NOT A MANAGER. Open, filter, enter. Nothing here
//     renames, archives, tags or closes a thread, because a thread never closes —
//     it goes quiet and sinks, which is what the caller's ordering already says.
//   - NAMING IS THE SCRIBE'S JOB. A thread with no name yet says so in one word
//     and never in an id (13.3.4). The `new thread` row asks for no name at all.
//   - THE CURRENT THREAD IS LISTED. A switcher that cannot show you where you
//     are is a switcher that can strand you (the same rule scope.go's roomRows
//     keeps for the rail).
//   - NOTHING MOVES. The list inherits this package's stillness: there is no
//     clock here, no spinner, and linear mode changes paint alone — see
//     [Switcher.SetThreads] for the numbering it turns on instead.
type Switcher struct {
	core

	line lineBuf
	buf  strings.Builder
	out  []string

	// bodyTop is how many chrome lines the last render drew above the list, so
	// a click can be translated into a row.
	bodyTop int

	// threads is the last set installed, kept so a re-render after a selection
	// does not ask the caller for the facts again.
	threads []Thread
}

var (
	_ tui2.Pane      = (*Switcher)(nil)
	_ tui2.PaneKeys  = (*Switcher)(nil)
	_ tui2.PaneMouse = (*Switcher)(nil)
)

// Thread is one working conversation as the switcher lists it.
//
// Every field arrives ALREADY RESOLVED, for the reason [Job]'s telemetry does:
// a relative time is formatted against a clock, and a render that reads a clock
// is a render whose output is not a pure function of its state. The wiring
// formats once, at the moment the door opens.
type Thread struct {
	// ID is the session this row switches to. It is never drawn.
	ID string
	// Name is the scribe's name for this thread. Empty is a real state — the
	// scribe names a thread from its first exchange — and renders as
	// [UnnamedThread] rather than as an id.
	Name string
	// LeftAt is the line the conversation was left on, one line, already
	// flattened by the caller or by [clean] here. Empty draws nothing at all:
	// a thread with nothing said in it has no line to quote, and inventing one
	// would be the surface speaking for the head.
	LeftAt string
	// When is how long ago this thread last moved, already formatted by
	// internal/tui2/reltime ("2h", "yesterday", "aug 3"). Empty draws nothing.
	When string
	// Unseen says a delivery landed here that nobody has read. It is the ONE
	// ornament (5.1 law 3) and it is a boolean rather than a count on purpose.
	Unseen bool
	// Current says this is the thread the window is already in.
	Current bool
}

// UnnamedThread is what a thread the scribe has not named yet is called.
//
// NEVER "untitled". A thread nobody has named is NEW, not defective, and the
// naming scribe names it from its first exchange — so this is a transitional
// face and it should say what is transitional about it. Same reasoning 13.3.4
// gives for never falling back to an id.
const UnnamedThread = "new thread"

// NewThreadWord is the affordance at the foot of the list. It starts a fresh
// conversation immediately: there is no naming prompt, because naming is the
// scribe's job and asking for one would make the only deliberate gesture in
// this product something other than typing (5.1 law 2).
const NewThreadWord = "new thread"

// NewThreadKey is the accelerator that reaches it from inside the switcher.
const NewThreadKey = "n"

// leftAtLead opens the second column. It is a WORD and not a glyph because it
// is the one piece of chrome on this surface that has to be read rather than
// recognised: `left at:` says the line beside it is a quotation of the past,
// which is the difference between a thread's last line and a thread's status.
const leftAtLead = "left at: "

// emptySwitcherText is what a window with no threads says. It is a real state —
// a store opened for the first time — and it teaches the door out of it.
const emptySwitcherText = "no threads yet — say something and this fills up"

// noThreadMatchPrefix opens the over-filtered message, naming the query back so
// the failure a reader needs to see is their own typo.
const noThreadMatchPrefix = "no thread matching "

// switcherLabel is the surface's own word, at the head of its search line.
const switcherLabel = "threads"

// NewSwitcher builds the thread switcher. It renders an empty, honest surface
// until threads arrive.
func NewSwitcher(opts Options) *Switcher {
	s := &Switcher{core: core{opts: opts}}
	s.list.setStyle(opts.Styler)
	s.list.linear = opts.Linear
	// The two groups on this sheet — the threads, and the one door that makes a
	// new one — are separated by whitespace and by nothing else. A `threads`
	// header over a surface whose own header already says `threads` would be
	// §19's same-fact-twice at the top of a list four rows long.
	s.list.noHeaders = true
	s.list.emptyText = emptySwitcherText
	return s
}

// SetThreads installs the list, newest-activity first as the caller ordered it.
//
// LINEAR MODE NUMBERS THE ROWS (10.1.5). The ordinal is part of the row's own
// text rather than a column of its own, because a screen reader reads a line and
// a column it cannot see is a column that does not exist. The numbers are
// ORDINALS and not accelerators: a digit typed into this surface is filter text,
// exactly as every other printable character is, and a key that meant one thing
// in one rendering and another in the other would be the surface teaching two
// products.
func (s *Switcher) SetThreads(threads []Thread) {
	s.threads = append(s.threads[:0], threads...)
	s.list.rebuild(func(dst []row) []row { return s.buildThreadRows(dst) })
	s.refreshEmptyText()
	s.invalidate()
}

// Threads is the set currently on screen, for a caller that wants to assert
// what it installed.
func (s *Switcher) Threads() []Thread { return s.threads }

// Reset clears the query and the cursor. The wiring calls it when the door
// opens: a switcher that reopened still holding the last search is one whose
// first keystroke edits a query the reader cannot see the origin of.
func (s *Switcher) Reset() {
	changed := s.list.setQuery("")
	s.list.cursor, s.list.top = 0, 0
	s.refreshEmptyText()
	if changed {
		s.invalidate()
	}
}

// Query is the current filter text.
func (s *Switcher) Query() string { return s.list.query }

// Select puts the cursor on one thread by id, and reports whether it found it.
//
// It exists for the one door that opens this surface pointing somewhere: a task
// page's `for: <thread>` attribution, where the reader has already said which
// conversation they mean. Everywhere else the cursor opens at the top, which is
// the newest thread and the one a switcher is usually opened to leave for.
func (s *Switcher) Select(id string) bool {
	if id == "" {
		return false
	}
	for i := range s.list.hits {
		if result, ok := s.list.rows[s.list.hits[i].idx].result.(SwitchThread); ok && result.ID == id {
			s.list.cursor = i
			s.invalidate()
			return true
		}
	}
	return false
}

// buildThreadRows turns the installed threads into the flat, section-ordered
// row slice the list filters and renders. The append order IS the display
// order, and [filter] depends on it.
func (s *Switcher) buildThreadRows(dst []row) []row {
	dst = dst[:0]
	for i := range s.threads {
		dst = append(dst, s.threadRow(&s.threads[i], i+1))
	}
	// The door is last and it is its own section, so the blank line above it is
	// the list's own group rhythm rather than a spacer somebody remembered to
	// add. It is never filtered away by a typo either: a reader who searched for
	// a thread that does not exist is precisely the reader who wants to start
	// one.
	dst = append(dst, newThreadRow())
	return dst
}

// threadRow is one conversation: the ornament, the name, the line it was left
// on, and how long ago that was.
func (s *Switcher) threadRow(t *Thread, ordinal int) row {
	name := clean(t.Name)
	if name == "" {
		name = UnnamedThread
	}
	if s.list.linear {
		name = strconv.Itoa(ordinal) + ". " + name
	}
	out := row{
		sec:    sectionThreads,
		verb:   name,
		desc:   leftAtLine(t.LeftAt),
		accel:  clean(t.When),
		band:   tokens.Band,
		result: SwitchThread{ID: t.ID},
	}
	if t.Unseen && !t.Current {
		// The dot is CYAN and not amber. 5.16 spends amber on one thing only —
		// a human is actually needed — and a delivery that landed is the
		// opposite of a demand: it is news. A switcher that painted news amber
		// would be spending the product's one alarm colour on good outcomes, and
		// the reader would stop trusting it on the row where it matters.
		out.glyph, out.glyphTok = tokens.GlyphStepDone, tokens.Cyan
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// leftAtLine is the second column: what was said last, quoted, or nothing at
// all. A thread with no line yet draws NO lead word — `left at:` with nothing
// after it would be chrome announcing an absence, which §16 spends no cells on.
func leftAtLine(text string) string {
	line := clean(text)
	if line == "" {
		return ""
	}
	return leftAtLead + line
}

// newThreadRow is the one affordance at the foot of the list.
func newThreadRow() row {
	out := row{
		sec:    sectionNewThread,
		verb:   NewThreadWord,
		desc:   newThreadHint,
		accel:  NewThreadKey,
		band:   tokens.Band,
		result: NewThread{},
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// newThreadHint is what the door promises, in the words of the decision behind
// it: nothing is asked for, and the name arrives on its own.
const newThreadHint = "start one now — it names itself after the first exchange"

// refreshEmptyText keeps the empty-state sentence answering the right question:
// a window with no threads and an over-filtered list are different facts and
// must not share a line.
func (s *Switcher) refreshEmptyText() {
	if s.list.query == "" {
		s.list.emptyText = emptySwitcherText
		return
	}
	s.list.emptyText = noThreadMatchPrefix + s.list.query
}

// Render implements [tui2.Pane]: a search line, a blank, and the list.
//
// The blank is the first thing height pressure takes and the header is the
// last, exactly as [Palette.Render] spends its rows: a list with no search line
// is a list the reader cannot tell they are filtering.
func (s *Switcher) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	s.out = s.out[:0]
	s.out = append(s.out, s.header(width))
	if height >= 3 {
		s.out = append(s.out, blankLine(&s.line, &s.buf, s.list.profile, s.list.focus, width, s.list.ground()))
	}
	s.bodyTop = len(s.out)
	s.out = append(s.out, s.list.render(width, height-s.bodyTop)...)
	if len(s.out) > height {
		s.out = s.out[:height]
	}
	s.out = padSheet(s.out, &s.line, &s.buf, s.list.profile, s.list.focus, width, height, s.list.ground())
	return strings.Join(s.out, "\n")
}

// header names the surface, carries the query, and ends with the count and the
// way out — the palette's own line with one word changed, so a reader who has
// used one of these surfaces has used all three.
func (s *Switcher) header(width int) string {
	l := &s.line
	l.reset(width)
	l.add(switcherLabel, tokens.TextTertiary)
	l.add("  ", tokens.TextTertiary)
	if s.list.query == "" {
		l.add(searchPlaceholder, tokens.TextTertiary)
	} else {
		l.add(s.list.query, tokens.TextPrimary)
	}
	count := strconv.Itoa(s.list.count()) + "/" + strconv.Itoa(len(s.list.rows))
	lead := count + " " + tokens.GlyphSeparator + " "
	if start := width - blocks.Width(lead+escHint); start > l.w+1 {
		l.padTo(start)
		l.add(lead, tokens.TextTertiary)
		addChip(l, closeChip)
	} else {
		addTail(l, width, count)
	}
	return l.emit(&s.buf, s.list.profile, s.list.focus, width, false, tokens.Band, s.list.ground())
}

// Key implements [tui2.PaneKeys].
//
// It is [Palette.Key] minus the drill and plus one binding, and the ordering is
// the palette's for the palette's reason: the editing keys are checked first so
// a query can never swallow esc or enter, and the default arm inserts only what
// the terminal reported as printable text.
//
// ctrl+n MINTS A THREAD and does not move the cursor, which is the one place
// this surface parts company with [core.navigate]'s vocabulary. The reason is
// that `n` is the door this list advertises on its own last row, and a reader
// who reaches for the chorded spelling of a key the surface just taught them
// should not be answered with a scroll. The arrows, tab and ctrl+p still move.
func (s *Switcher) Key(msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "esc":
		return s.close()
	case "enter":
		return s.choose()
	case "ctrl+u":
		s.setQuery("")
	case "ctrl+w":
		s.setQuery(dropWord(s.list.query))
	case "backspace":
		s.setQuery(dropRune(s.list.query))
	case "ctrl+n":
		return s.mint()
	default:
		// THE BARE `n` IS THE DOOR ONLY ON AN EMPTY FILTER, which is the same
		// rule the chat surface keeps for its own bare keys (app.go's
		// helpKeyLive): a letter typed into a search field is a letter, and a
		// key that sometimes filtered and sometimes minted a conversation would
		// make typing here feel dangerous. With nothing typed, `n` can only have
		// been the row the list is advertising two lines down.
		if key == NewThreadKey && s.list.query == "" {
			return s.mint()
		}
		if s.navigate(key) {
			return nil
		}
		if text := msg.Key().Text; text != "" {
			s.setQuery(s.list.query + text)
		}
	}
	return nil
}

// mint is the `new thread` row reached by its key rather than by the cursor.
// It goes through the same [core.choose] path the row itself takes — one door,
// two hands — so a key and a click cannot leave the surface in two states.
func (s *Switcher) mint() tea.Cmd {
	closed := s.close()
	return tea.Batch(closed, s.emit(NewThread{}))
}

// Mouse implements [tui2.PaneMouse]. Every row is a button, the header is
// chrome, and a wheel notch walks the selection — the shared vocabulary, with
// nothing added.
func (s *Switcher) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	return s.mouse(msg, local, s.bodyTop)
}

func (s *Switcher) setQuery(q string) {
	if s.list.setQuery(q) {
		s.refreshEmptyText()
		s.invalidate()
	}
}
