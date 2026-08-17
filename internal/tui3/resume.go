package tui3

// THE RESUME PICKER: /resume, and the surface `aforge resume` opens on.
//
// The welcome box already lists the last four conversations (welcome.go), and
// it is the right answer to "where was I" for the ten seconds after a launch:
// it is on screen, it costs no keystroke, and then it is gone for good. What it
// is not is a way BACK. A person three days and six sessions later wants the
// one about the migration, not the fourth-most-recent one, and the box has no
// row for it and no way to be asked.
//
// So this is the same list, opened on purpose and asked a question:
//
//   - IT IS THE PICKER GESTURE, unchanged. A filter box in the input line's
//     place, a short list under it, ↑↓ to move, enter to open, esc to leave
//     everything as it was — the same overlay grammar /model uses (palette.go),
//     because a second list that behaved differently would be a second thing to
//     learn for a question that is the same question.
//   - A ROW IS A NAME, A DESCRIPTION AND AN AGE, in that order of loudness. The
//     name is what the session called itself, made readable ([humanName]); the
//     description is the last thing that happened in it; the age is dim and
//     coarse. Ids and file names appear nowhere: a person remembers "the resume
//     picker one, yesterday", and no part of 20260816-150405_a3f2.jsonl is that.
//   - IT NEVER OPENS ON NOTHING. A directory with no conversations in it gets a
//     sentence saying so and no overlay — a modal list with no rows is a trap
//     that has to be dismissed before it can be told it was useless.

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// resumeRows is how many sessions are on offer at once. Ten is a screenful a
// person reads down without scrolling and stops short of the picker's twelve:
// a session row carries a sentence where a model row carries an id, and a list
// of twelve sentences is a page rather than a list.
const resumeRows = 10

// resumeDescription bounds the description column. Eighty columns is a
// sentence a person reads at a glance; past it a row is a paragraph competing
// with the nine rows under it.
const resumeDescription = 80

// resumeHint is the placeholder in the empty filter box — where this overlay
// explains itself, the same place the model picker does.
const resumeHint = "filter · ↑↓ · enter open · esc cancel"

// The two sentences this surface says about resuming when it cannot show a
// list. The first is the empty state a fresh machine meets; the second is what
// a surface with no door onto the session files says (a headless frame, a test).
const (
	noSessionsWord        = "No sessions yet — start one with aforge chat"
	resumeUnavailableWord = "resuming is unavailable here"
)

// roster is the overlay's whole state. The zero value is closed, which is what
// every surface starts as.
type roster struct {
	open bool

	// all is the list as it was resolved, and lower the same rows folded once at
	// open — name and description together, because a person hunting a
	// conversation types a word out of EITHER of them.
	all   []Session
	lower []string
	score []int

	// hits are indexes into all, in rank order; cursor indexes hits, and top is
	// the first hit drawn.
	hits   []int
	cursor int
	top    int

	// current is the transcript this surface is already in. It is the marked
	// row, the same mark the model picker puts on the model in use, and it is
	// where the cursor opens.
	current string

	filter editor
}

// start opens the roster over list, with the session already open marked.
func (r *roster) start(list []Session, current string) {
	*r = roster{open: true, all: list, current: current}
	r.lower = make([]string, len(list))
	for i, session := range list {
		r.lower[i] = strings.ToLower(humanName(session) + " " + session.Last)
	}
	r.score = make([]int, len(list))
	r.rank()
	// The cursor opens on the conversation this surface is in, for the reason
	// the model picker opens on the model in use: enter is a confirm key, and a
	// list that opened on somebody else's row would make it a change nobody
	// asked for. A surface whose own session is not in the list — a memory-only
	// conversation — opens on the newest, which is row zero.
	for at, i := range r.hits {
		if r.all[i].File == current {
			r.cursor = at
			break
		}
	}
	r.follow(resumeRows)
}

func (r *roster) close() { *r = roster{} }

// rank re-filters against the filter box with the model picker's own ladder
// ([tokenScore]): every token must match, prefix beats substring beats
// subsequence. The rows are ranked over the NAME AND THE DESCRIPTION as one
// string, which is the whole reason a person can type "migration" and land on
// the session they never named.
func (r *roster) rank() {
	tokens := strings.Fields(strings.ToLower(r.filter.String()))
	r.hits = r.hits[:0]
	for i, text := range r.lower {
		if len(tokens) == 0 {
			r.hits = append(r.hits, i)
			continue
		}
		total, matched := 0, true
		for _, token := range tokens {
			score, hit := tokenScore(text, token)
			if !hit {
				matched = false
				break
			}
			total += score
		}
		if !matched {
			continue
		}
		r.score[i] = total
		r.hits = append(r.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(r.hits, func(a, b int) bool { return r.score[r.hits[a]] < r.score[r.hits[b]] })
	}
	// Ties keep source order, which is newest first — so an empty box is the
	// list as it was handed over and a filtered one is the best match first.
	r.cursor, r.top = 0, 0
}

func (r *roster) move(delta int) {
	r.cursor = moveCursor(r.cursor, delta, len(r.hits))
	r.follow(resumeRows)
}

func (r *roster) follow(height int) { r.top = listTop(r.cursor, r.top, len(r.hits), height) }

// navigate is every key the roster owns that is not a decision: the walk, the
// scroll and the filter box. enter and esc are the app's, for the reason
// [picker.navigate] leaves them to whoever opened the list.
func (r *roster) navigate(msg tea.KeyPressMsg) {
	listNavigate(msg, &r.filter, r.move, r.rank, resumeRows)
}

// choice is the session under the cursor, and false when the filter matched
// nothing — enter on an empty list must change nothing at all.
func (r *roster) choice() (Session, bool) {
	if !r.open || r.cursor < 0 || r.cursor >= len(r.hits) {
		return Session{}, false
	}
	return r.all[r.hits[r.cursor]], true
}

// height is how many list rows the overlay wants, not counting the filter box:
// one for the "no session matches" line when the filter matched nothing, and
// otherwise as many rows as there are sessions, up to the ceiling — which is a
// ceiling in LINES, so a phone shows fewer conversations with what happened in
// them readable rather than ten rows of clipped sentence (palette.go).
func (r *roster) height(width int) int {
	switch {
	case !r.open:
		return 0
	case len(r.hits) == 0:
		return 1
	}
	return overlayWindow(width, r.top, len(r.hits), resumeRows, func(at int) string {
		session := r.all[r.hits[at]]
		return sessionNote(session, width, humanName(session))
	})
}

func (r *roster) rows(width, n int, pal palette, hover int) []string {
	if n <= 0 {
		return nil
	}
	if len(r.hits) == 0 {
		return []string{pal.dim("  no session matches")}
	}
	r.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := r.top; at < len(r.hits) && fill.room(); at++ {
		session := r.all[r.hits[at]]
		name := humanName(session)
		if !fill.add(at, name, sessionNote(session, width, name), at == r.cursor, session.File == r.current) {
			break
		}
	}
	lines, _ := fill.done()
	return lines
}

// sessionNote is the dim tail of one row: what was last happening, then how
// long ago. Both are facts about the conversation rather than about the row, so
// they sit where every other list on this surface puts its facts.
//
// THE AGE IS NEVER THE THING THAT GETS CUT. It is four characters and it is
// half the reason a person can tell two sessions about the same subject apart;
// the description is a sentence, and a sentence that ends early still says what
// it was about. So the age is reserved first and the description takes what is
// left — the same rule the row itself keeps between its label and its note
// ([overlayRow]), applied once more inside the note.
func sessionNote(session Session, width int, name string) string {
	when := ago(session.At)
	description := strings.TrimSpace(session.Last)
	if description == "" {
		return when
	}
	// What the row has left after the lead, the name, the gap before the note,
	// and the age with its own separator.
	//
	// A WRAPPED ROW HAS THE WHOLE LINE, less its indent: at [tierPhone] the note
	// sits under the name rather than beside it (palette.go's [overlayLines]),
	// so the name costs it nothing and the description is what a phone has room
	// for rather than the eight cells left over from a session called "Port the
	// Resume Picker to tui3".
	room := width - 2 - ansi.StringWidth(name) - 2
	if phoneList(width) {
		room = width - overlayIndent
	}
	if when != "" {
		room -= ansi.StringWidth(when) + 3
	}
	// Too narrow to say anything worth reading. A description clipped to five
	// cells is a row that has spent its width on an ellipsis.
	if room < 12 {
		return when
	}
	if room > resumeDescription {
		room = resumeDescription
	}
	description = fit(description, room)
	if when == "" {
		return description
	}
	return description + " · " + when
}

// ── the name on a row ───────────────────────────────────────────────────────

// humanName is what a session is CALLED, and it is the one place the ladder
// down from a name to a machine's idea of one is climbed:
//
//	the title the session gave itself   "port the resume picker"
//	the first thing the person said     "port the resume picker to tui3"
//	the transcript's own file name      "20260816-150405_a3f2"
//
// and then it is made readable. A stored name is a model's eight lowercase
// words, or — for a session written before the namer existed, or one derived
// from a file — a single machine-shaped token; both are turned into something
// a person reads rather than parses.
//
// It is a DISPLAY rule and deliberately not a migration. Deriving a name here
// costs one pass over a string that is already in memory, rewrites no
// transcript, and works the same on a file written by a build that had no
// titles at all — where a migration would have to open, rewrite and re-lock
// every session file on the machine to answer the same question.
func humanName(session Session) string {
	name := strings.TrimSpace(session.Title)
	if name == "" {
		name = openingName(session.Opening)
	}
	if name == "" {
		// The transcript's own name, which under Decision 26 is the session
		// FOLDER's rather than the file's — every folder session's transcript is
		// called the same thing (sessionrows.go).
		name = strings.TrimSuffix(sessionStem(session.File), ".jsonl")
	}
	return titleCase(unpackName(name))
}

// openingWords is how much of a first message becomes a name. Seven words is
// the length of the titles the namer itself writes (session's title.go asks for
// eight), so a derived name and a given one sit at the same width in the list.
const openingWords = 7

// openingName is a name derived from the first thing the person said: its
// opening words, with the punctuation a sentence ends on taken off.
func openingName(opening string) string {
	words := strings.Fields(strings.TrimSpace(opening))
	if len(words) == 0 {
		return ""
	}
	if len(words) > openingWords {
		words = words[:openingWords]
	}
	return strings.TrimRight(strings.Join(words, " "), " .,:;!?-—")
}

// unpackName opens a machine-shaped name out into words. It fires ONLY on a
// name that has no spaces in it, which is the whole of the rule: a title that
// is already a sentence may legitimately hold a hyphen ("state-of-the-art"),
// and splitting that one would be inventing words where the model wrote a
// compound. A single token never carries that meaning — it is a slug, a
// filename or an identifier — and unpacking it is the only way to read it.
func unpackName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t") {
		return name
	}
	unpacked := strings.Map(func(r rune) rune {
		switch r {
		case '_', '-', '.':
			return ' '
		}
		return r
	}, name)
	return strings.Join(strings.Fields(unpacked), " ")
}

// smallWords stay lowercase inside a name. Title case that capitalizes every
// word reads as a headline being shouted, and a list of ten headlines is a list
// with no shape; the small words are the shape.
var smallWords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "but": true,
	"by": true, "for": true, "from": true, "in": true, "into": true, "of": true,
	"on": true, "or": true, "the": true, "to": true, "via": true, "vs": true,
	"with": true,
}

// titleCase raises the first letter of each word, leaving the small ones alone
// unless they lead.
//
// IT ONLY EVER RAISES. Nothing here is lowercased, so a word that arrived with
// capitals in it — OpenAI, a file name, an acronym the person typed — keeps
// exactly the shape they gave it. A title-caser that normalized would be a
// surface correcting somebody's spelling of their own subject.
func titleCase(name string) string {
	words := strings.Fields(name)
	for i, word := range words {
		if i > 0 && smallWords[word] {
			continue
		}
		words[i] = raiseFirst(word)
	}
	return strings.Join(words, " ")
}

// raiseFirst raises the first LETTER of a word, which is not always its first
// character: a word that opens with a quote or a bracket is raised inside it,
// and one whose first letter is already a capital is returned untouched.
func raiseFirst(word string) string {
	for i, r := range word {
		if !unicode.IsLetter(r) {
			continue
		}
		upper := unicode.ToUpper(r)
		if upper == r {
			return word
		}
		return word[:i] + string(upper) + word[i+utf8.RuneLen(r):]
	}
	return word
}

// ago is [since] with the word a person says out loud. A span reads "2h ago"; a
// date — which is what [since] falls back to past a month — reads as itself,
// because "16 Aug ago" is not a thing anybody says.
func ago(at time.Time) string {
	word := since(at)
	if word == "" || word == "now" || strings.ContainsRune(word, ' ') {
		return word
	}
	return word + " ago"
}

// ── the app's side of the overlay ───────────────────────────────────────────

// openResume is /resume, and the list `aforge resume` opens on.
//
// It resolves the list HERE rather than holding one from boot: a session picker
// opened an hour into a conversation must show the conversation that was had in
// the next terminal since, and the walk is a directory read and a bounded
// number of file scans (internal/session's Recent), which is the same order of
// work the file completion does on every "@".
func (a *app) openResume() {
	if a.resume == nil {
		a.note(resumeUnavailableWord)
		return
	}
	var list []Session
	if a.recentSessions != nil {
		list = a.recentSessions()
	}
	if len(list) == 0 {
		a.note(noSessionsWord)
		return
	}
	a.closeLists()
	// The welcome box is the same list in its short form, and the two must not
	// be on screen at once saying it twice. Opening the picker is also work
	// beginning, which is what dismissal means (welcome.go).
	a.dismissWelcome()
	a.roster.start(list, a.file)
	a.touch()
}

// resumeKey routes one keypress while the roster owns the keyboard. It is the
// picker's key map with one decision instead of the other: enter opens a
// conversation where /model's switches a model.
//
// It returns work for exactly one of those keys: enter on a row that is not the
// row we are standing on swaps the agent, and the conversation that arrives owes
// itself the two standing lanes (welcome.go's [app.resumeSession]).
func (a *app) resumeKey(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		a.roster.close()

	case "enter":
		chosen, ok := a.roster.choice()
		a.roster.close()
		switch {
		case !ok:
		case chosen.File != "" && chosen.File == a.file:
			// The row that was already marked. Closing the agent and reopening the
			// same file would drop the lock, replay the journal and land exactly
			// here — a second of work to arrive where the person already was.
			a.note("already here · " + humanName(chosen))
		default:
			cmd = a.resumeSession(chosen)
		}

	default:
		a.roster.navigate(msg)
	}
	a.touch()
	return cmd
}
