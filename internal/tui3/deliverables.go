package tui3

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// /files: THE THINGS THAT WERE MADE FOR YOU, AND WHERE THEY WENT.
//
// A conversation produces files — a generated picture, an exported document,
// a report written into a session that owns its own workspace — and every one
// of them lands at a path that scrolled off the screen an hour ago. "The
// report from Tuesday" is then a question nobody can answer without knowing
// which directory that conversation was about, which is the question this list
// exists to remove (docs/CHAT-V3.md, Decision 26).
//
// It reads the GLOBAL index and not a directory, and that is the whole of why
// it is worth having: what was made lives in as many places as there were
// conversations, and the index is the one file that knows all of them
// (internal/session's artifacts.go). The rows are citations — a title, a kind,
// a path and when it landed — so this list costs one small file read and one
// stat per row, and never a walk.
//
// Four decisions, and three of them are borrowed rather than invented:
//
//   - IT IS THE PICKER GESTURE, unchanged (palette.go, resume.go): a filter box
//     in the input line's place, a short list under it, ↑↓ to move, esc to leave
//     everything as it was. A second list with its own manners would be a second
//     thing to learn for a question of the same shape.
//   - TWO GLYPHS AND A NAME. A picture is marked differently from everything
//     else because it is the one deliverable that is not words; the dim tail
//     carries where it went and how long ago, which are the two facts a person
//     is comparing rows on.
//   - A FILE THAT IS GONE SAYS SO AND OFFERS NOTHING. The index is append-only
//     and a person may delete, move or rename anything in it at any time, so a
//     row is checked against the disk when the list opens. A row that no longer
//     names a file keeps its place — "I did make that, and it is not there any
//     more" is the answer somebody is looking for — and every verb steps over
//     it. Removing the row instead would answer the question with silence.
//   - IT NEVER OPENS ON NOTHING. A person who has made nothing gets one
//     sentence and no overlay, for [openResume]'s reason: a modal list with no
//     rows is a trap that has to be dismissed before it can be told it was
//     useless.
//
// THE VERBS ARE CHORDS AND NOT BARE LETTERS, which is the one place this list
// departs from the shape it was drawn from, and it is forced by the filter. A
// list that is typed into gives every plain letter to the box — /connect's
// panel states that law — so a bare "r" on a list a person filters by typing
// "report" would reveal a file on the way to finding it. The model picker
// already spends a chord on its second verb for the same reason (ctrl+t, the
// reasoning effort), so these follow it: ctrl+r reveals and ctrl+y copies,
// where "yank" is what this surface already calls taking a copy of something
// (copymode.go).

// filesRows is how many deliverables are on offer at once. It is the resume
// picker's ten and not the model picker's twelve, for that list's reason: a row
// here carries a title somebody wrote a sentence into, where a model row
// carries an id.
const filesRows = 10

// filesKept is how many rows of the index this list reads back. The file holds
// everything ever made and is capped in the thousands (internal/session's
// artifactRows); two hundred is far past what a person scrolls or filters down
// and is the bound on the stats — one per row, on the keystroke that opens the
// list.
const filesKept = 200

// filesPathRoom bounds the path column, the way [resumeDescription] bounds the
// description: past it a row is a paragraph competing with the nine rows under
// it.
const filesPathRoom = 60

// The keys that are not the picker's own. They are named here rather than
// spelled into the switch so that the hint and the handler cannot disagree
// about what this list does, and so that a merge that prefers other chords
// changes two lines.
const (
	filesRevealKey = "ctrl+r"
	filesCopyKey   = "ctrl+y"
)

// What this list can do, said in the two places a person looks for it: the
// placeholder in the empty filter box, and the hint slot under the frame. The
// walk is left to the lists that teach it (/model, /resume) — the news on this
// one is its verbs.
const (
	filesVerbs = "enter open · " + filesRevealKey + " reveal · " + filesCopyKey + " copy · esc"
	filesHint  = "filter · " + filesVerbs
	// filesCopyHint is the placeholder in the destination box, which takes the
	// filter's place while it is open — one box under the list, answering one
	// question at a time (connectpanel.go's key box).
	filesCopyHint  = "a path to copy it to · enter copies · esc"
	filesCopyVerbs = "enter copies · esc"
)

// The sentences this list says outside itself.
const (
	// filesNothingWord is /files on a machine that has made nothing. It is SAID
	// rather than dropped, for [exportNothing]'s reason: silence after a
	// deliberate command reads as a command that broke.
	filesNothingWord = "nothing made yet."
	// filesGoneWord is the tail of a row whose file is not there any more. It is
	// the whole tail: the path and the age were promises about a file somebody
	// can open, and neither is true of this one.
	filesGoneWord = "gone"
	// filesNoMatchWord is the filter that matched nothing.
	filesNoMatchWord = "  nothing here by that name"
	// filesOpenFailedWord is the handoff to the platform failing. What comes
	// back from it is a sentence about a browser (opener.go), which is not what
	// was being opened, so this list says its own.
	filesOpenFailedWord = "could not open · "
	// filesCopiedWord and the two refusals under it are /export's three answers
	// with the verb changed, because they are the same three answers: it landed,
	// something is already there, or the disk said no.
	filesCopiedWord     = "copied · "
	filesThereWord      = " is already there · give it another name"
	filesCopyFailedWord = "the copy did not happen: "
	// filesRemoteWord opens the list on a remote session. The index is THIS
	// machine's, and what the conversation over there has made is written into
	// the index over there — so the list is honest about whose files it is
	// showing rather than looking like an empty afternoon's work (host.go).
	filesRemoteWord = "these are the files made on this machine — what that session made is written down on the other one"
)

// The two marks. A picture is the one deliverable that is not words, and that
// is the only distinction this list draws with a shape: every other kind —
// an export, a document, a thing a tool wrote — is a file, and the difference
// between them is said by the title beside the mark rather than by an alphabet
// of symbols nobody was taught (styles.go's [glyphHarness] says it first).
const (
	glyphMadePicture      = "▣"
	glyphMadePictureASCII = "p"
	glyphMadeFile         = "▢"
	glyphMadeFileASCII    = "f"
)

// deliverable is one row: a citation out of the index, plus the one thing the
// index cannot promise — whether the file is still there.
type deliverable struct {
	title string
	kind  string
	path  string
	at    time.Time
	// gone marks a row whose file was not on the disk when the list opened. It
	// is a fact about a moment and not about the index: nothing is rewritten,
	// and the same row is offered again the next time /files is typed, because
	// a file may perfectly well come back.
	gone bool
}

// picture reports whether this row gets the other mark.
//
// It reads the KIND the index recorded and falls back to the file's own
// extension, because the two disagree in the ordinary case: a tool that wrote a
// picture and recorded no kind at all is still a row about a picture, and the
// name on disk is the only thing left that says so.
func (d deliverable) picture() bool {
	if strings.EqualFold(strings.TrimSpace(d.kind), "image") {
		return true
	}
	switch strings.ToLower(filepath.Ext(d.path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}

// mark is the row's glyph.
func (d deliverable) mark(pal palette) string {
	switch {
	case pal.linear && d.picture():
		return glyphMadePictureASCII
	case pal.linear:
		return glyphMadeFileASCII
	case d.picture():
		return glyphMadePicture
	}
	return glyphMadeFile
}

// copyTo is one deliverable on its way somewhere else: the row it came from and
// the box the destination is typed into.
//
// THE ROW IS COPIED OUT OF THE LIST and not held as an index, for
// [connectPanel.armed]'s reason: the filter re-ranks the rows on every
// keystroke, and a destination typed against row four would land on whichever
// file row four had become.
type copyTo struct {
	from deliverable
	box  editor
}

// shelf is the overlay's whole state. The zero value is closed, which is what
// every surface starts as.
type shelf struct {
	open bool

	// all is the list as it was read, newest first, and lower the titles folded
	// once at open — the filter is over what a person NAMED the thing, which is
	// the only half of a row they remember. A path is where a machine put it.
	all   []deliverable
	lower []string
	score []int

	// hits are indexes into all, in rank order; cursor indexes hits, and top is
	// the first hit drawn.
	hits   []int
	cursor int
	top    int

	filter editor

	// dest is the destination box open over the list, or nil.
	dest *copyTo

	// home is what "~" abbreviates in the paths these rows draw. It is carried
	// rather than asked for per row, because a paint runs many times a second
	// and where a person's home directory is does not change between two of
	// them. Empty leaves every path spelled in full, which is the emptiness law
	// and not a fallback.
	home string
}

// start opens the shelf over list.
func (s *shelf) start(list []deliverable, home string) {
	*s = shelf{open: true, all: list, home: home}
	s.lower = make([]string, len(list))
	for i, row := range list {
		s.lower[i] = strings.ToLower(row.title)
	}
	s.score = make([]int, len(list))
	s.rank()
}

func (s *shelf) close() { *s = shelf{} }

// rank re-filters against the filter box with the model picker's own ladder
// ([tokenScore]): every token must match, prefix beats substring beats
// subsequence.
func (s *shelf) rank() {
	tokens := strings.Fields(strings.ToLower(s.filter.String()))
	s.hits = s.hits[:0]
	for i, text := range s.lower {
		if len(tokens) == 0 {
			s.hits = append(s.hits, i)
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
		s.score[i] = total
		s.hits = append(s.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(s.hits, func(a, b int) bool { return s.score[s.hits[a]] < s.score[s.hits[b]] })
	}
	// Ties keep source order, which is newest first — so an empty box is the
	// list as it was read and a filtered one is the best match first.
	s.cursor, s.top = 0, 0
}

func (s *shelf) move(delta int) {
	s.cursor = moveCursor(s.cursor, delta, len(s.hits))
	s.follow(filesRows)
}

func (s *shelf) follow(height int) { s.top = listTop(s.cursor, s.top, len(s.hits), height) }

// navigate is every key the shelf owns that is not a decision: the walk, the
// scroll and the filter box.
func (s *shelf) navigate(msg tea.KeyPressMsg) {
	listNavigate(msg, &s.filter, s.move, s.rank, filesRows)
}

// choice is the row under the cursor, and false when the filter matched nothing
// — a verb pressed on an empty list must do nothing at all.
func (s *shelf) choice() (deliverable, bool) {
	if !s.open || s.cursor < 0 || s.cursor >= len(s.hits) {
		return deliverable{}, false
	}
	return s.all[s.hits[s.cursor]], true
}

// live is the row a verb may act on: the one under the cursor, unless its file
// is gone. It is ONE function because all three verbs ask the same question,
// and a verb that asked it its own way would be the row that says "gone"
// answering one of them.
func (s *shelf) live() (deliverable, bool) {
	row, ok := s.choice()
	if !ok || row.gone {
		return deliverable{}, false
	}
	return row, true
}

// height is how many list rows the overlay wants, not counting the box under
// it: one for the "nothing here by that name" line when the filter matched
// nothing, and otherwise as many rows as there are, up to the ceiling — which
// is a ceiling in LINES, so a phone shows fewer deliverables with where they
// went still readable (palette.go).
func (s *shelf) height(width int) int {
	switch {
	case !s.open:
		return 0
	case len(s.hits) == 0:
		return 1
	}
	return overlayWindow(width, s.top, len(s.hits), filesRows, func(at int) string {
		row := s.all[s.hits[at]]
		return madeNote(row, s.home, width, madeWidth(row))
	})
}

func (s *shelf) rows(width, n int, pal palette, hover int) []string {
	if n <= 0 {
		return nil
	}
	if len(s.hits) == 0 {
		return []string{pal.dim(filesNoMatchWord)}
	}
	s.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := s.top; at < len(s.hits) && fill.room(); at++ {
		row := s.all[s.hits[at]]
		if !fill.add(at, madeLabel(row, pal), madeNote(row, s.home, width, madeWidth(row)), at == s.cursor, false) {
			break
		}
	}
	lines, _ := fill.done()
	return lines
}

// madeLabel is the row's own half: the mark and what the thing is called.
func madeLabel(row deliverable, pal palette) string {
	return pal.muted(row.mark(pal)) + " " + row.title
}

// madeWidth is the label's width without painting one, which is what the
// height's arithmetic needs: a measured escape sequence would be columns of a
// row that is not on the screen. Both marks are one cell in every tier, so the
// label is the title and two more.
func madeWidth(row deliverable) int { return ansi.StringWidth(row.title) + 2 }

// madeNote is the dim tail of one row: where it went, then how long ago.
//
// THE AGE IS NEVER THE THING THAT GETS CUT, which is [sessionNote]'s rule
// applied to this list's own two facts: the age is four characters and it is
// half of how a person tells two reports about the same subject apart, while a
// path that ends early still says which directory it was in.
func madeNote(row deliverable, home string, width, label int) string {
	if row.gone {
		return filesGoneWord
	}
	when := ago(row.at)
	// The path is abbreviated the way the status sheet's place is (render.go's
	// [shortPath]): home to "~",
	// every parent to its initial, and the file's own name whole — which is the
	// only part of it that answers "which one is this".
	where := shortPath(row.path, home, 0)
	if where == "" {
		return when
	}
	room := width - 2 - label - 2
	if phoneList(width) {
		room = width - overlayIndent
	}
	if when != "" {
		room -= ansi.StringWidth(when) + 3
	}
	// Too narrow to say anything worth reading. A path clipped to five cells is
	// a row that has spent its width on an ellipsis.
	if room < 12 {
		return when
	}
	if room > filesPathRoom {
		room = filesPathRoom
	}
	where = fit(where, room)
	if when == "" {
		return where
	}
	return where + " · " + when
}

// ── the app's side of the overlay ───────────────────────────────────────────

// The list reads the same global index /export writes to: [app.artifactsIndex]
// (export.go) is the one resolver, going through internal/home so AFORGE_HOME
// moves it with everything else, with the field as the narrower override a
// test sets directly.

// openFiles is /files.
//
// The index is read HERE and not held from boot, on the terms /connect and
// /permissions read theirs: a picture generated four turns ago wrote a row this
// list has to know about, and asking costs one small file read.
func (a *app) openFiles() {
	a.noticeEvent(eventFilesOpened)
	list := readDeliverables(a.artifactsIndex())
	if len(list) == 0 {
		a.note(filesNothingWord)
		return
	}
	a.closeLists()
	a.dismissWelcome()
	a.shelf.start(list, a.tilde)
	if a.hosted() {
		a.note(filesRemoteWord)
	}
	a.touch()
}

// readDeliverables is the index as this list shows it: newest first, one row
// per file, and each row asked whether its file is still there.
//
// THE SAME PATH RECORDED TWICE IS ONE ROW. The index is append-only, so a
// document written, edited and exported again is three rows about one file —
// and the newest of them is the true one, which is the rule the task index
// keeps about its own repeats.
func readDeliverables(index string) []deliverable {
	rows := session.ReadArtifacts(index)
	if len(rows) > filesKept {
		rows = rows[:filesKept]
	}
	seen := make(map[string]bool, len(rows))
	list := make([]deliverable, 0, len(rows))
	for _, row := range rows {
		path := strings.TrimSpace(row.Path)
		if seen[path] {
			continue
		}
		seen[path] = true
		// A directory at the path is not the file that was recorded, so it is
		// treated as the file being gone: every verb here is about a file.
		info, err := os.Stat(path)
		list = append(list, deliverable{
			title: strings.TrimSpace(row.Title),
			kind:  row.Kind,
			path:  path,
			at:    row.Created,
			gone:  err != nil || info.IsDir(),
		})
	}
	return list
}

// filesKey routes one keypress while the shelf owns the keyboard. It is the
// picker's key map with three decisions instead of one, and the two that are
// not enter are chords for the reason the head of this file states.
func (a *app) filesKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.shelf
	if s.dest != nil {
		return a.filesCopyKey(msg)
	}
	switch msg.String() {
	case "esc":
		// ESC UNDOES ONE THING AT A TIME, nearest first: the query narrowing the
		// list, and only then the list. A dismiss key that closed the whole
		// overlay while a person could still see something smaller to dismiss
		// would be throwing away work they can see (connectpanel.go).
		if len(s.filter.value) > 0 {
			s.filter.reset()
			s.rank()
			break
		}
		s.close()

	case "enter":
		// THE LIST STAYS OPEN, which is where this key parts company with the
		// resume picker's enter. That one MOVES a person — the conversation it
		// opened is the screen they are now on, and a list over it would be a
		// list over the answer. This one hands a file to another program and
		// leaves this screen exactly as it was, and "open the picture and then
		// the report it goes with" is one errand, not two.
		if row, ok := s.live(); ok {
			return a.openMediaOriginal(mediaItem{path: row.path})
		}

	case filesRevealKey:
		// The CONTAINING DIRECTORY, opened the same way the file is. There is no
		// second platform gesture for "show me this in a window" that works
		// everywhere, and a directory that opens with the file in it answers the
		// same question on every desktop this surface runs on.
		if row, ok := s.live(); ok {
			return a.openMediaOriginal(mediaItem{path: filepath.Dir(row.path)})
		}

	case filesCopyKey:
		if row, ok := s.live(); ok {
			s.dest = &copyTo{from: row}
		}

	default:
		s.navigate(msg)
	}
	a.touch()
	return nil
}

// filesCopyKey drives the destination box open over the list. enter copies, an
// empty box is not an answer at all, and esc puts the person back on the row
// they pressed it from — which is [connectEntryKey]'s bargain: nothing is
// waiting on this box, so backing out of it declines nothing.
func (a *app) filesCopyKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.shelf
	dest := s.dest
	switch msg.String() {
	case "esc":
		s.dest = nil

	case "enter":
		where := strings.TrimSpace(dest.box.String())
		s.dest = nil
		if where == "" {
			break
		}
		s.close()
		a.touch()
		return a.copyDeliverable(dest.from, where)

	default:
		// The filter box's key map, which is this surface's one way of typing
		// into a one-line box ([keyEntry.typeInto] says it first). There is no
		// list under this box, so the walk and the page are no-ops.
		listNavigate(msg, &dest.box, func(int) {}, func() {}, 1)
	}
	a.touch()
	return nil
}

// copiedMsg is the copy coming back to the loop. The path travels with it so
// the note can name the file even when the error is what happened to it.
type copiedMsg struct {
	path string
	err  error
}

// copyDeliverable is the third verb: a keeper taken OUT of the session that
// made it, deliberately, into a directory the person named.
//
// THIS IS THE PROMOTION AND NOT A CONVENIENCE. Decision 26's whole bargain is
// that an owned session's work/ is cheap to delete, which is only fair if
// getting a keeper out of it is one gesture a person performs on purpose — so
// the destination is typed, the original is left exactly where it was, and
// nothing is written anywhere the person did not name.
//
// Everything that touches a disk happens in what is returned, the way /export's
// write does.
func (a *app) copyDeliverable(row deliverable, where string) tea.Cmd {
	// The same resolution a typed picture path gets: "~" is home, a bare name is
	// under the directory this conversation is about, and an absolute path is
	// left alone (attach.go).
	target := a.resolvePath(where)
	name := filepath.Base(row.path)
	return func() tea.Msg {
		path, err := copyFileTo(row.path, target, name)
		return copiedMsg{path: path, err: err}
	}
}

// copiedFile is the copy's one line in the conversation. It is /export's three
// answers, because it is the same three answers.
func (a *app) copiedFile(msg copiedMsg) {
	short := shortPath(msg.path, a.tilde, 0)
	switch {
	case msg.err == nil:
		a.note(filesCopiedWord + short)
	case errors.Is(msg.err, fs.ErrExist):
		a.note(short + filesThereWord)
	default:
		a.note(filesCopyFailedWord + msg.err.Error())
	}
}

// copyFileTo writes source to target and answers with the path it used.
//
// O_EXCL IS THE REFUSAL ITSELF, exactly as it is in [writeExport]: asking
// whether the file exists and then writing it would be two answers to one
// question with a gap in the middle, and this way the filesystem decides, once.
// A copy that overwrote would be the surface taking a file somebody already had
// — which is the one thing a command whose whole purpose is keeping something
// must not do.
//
// A MISSING PARENT IS NOT CREATED. /export makes one for a path a person typed
// out, because there the alternative is refusing to write a document that has
// nowhere else to go; here the original is untouched at its own path and the
// person is one keystroke from naming a directory that exists, so a typo stays
// a typo rather than becoming a tree.
func copyFileTo(source, target, name string) (string, error) {
	path := target
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		// A DIRECTORY IS A PLACE AND NOT A NAME: "~/Documents" means "put it in
		// there", under the name it already has.
		path = filepath.Join(path, name)
	}
	from, err := os.Open(source)
	if err != nil {
		return path, err
	}
	defer from.Close()
	// The mode is the export's, and for the same reason: what a conversation
	// produced is the most private thing on this machine until its author says
	// otherwise.
	to, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return path, err
	}
	if _, err := io.Copy(to, from); err != nil {
		to.Close()
		return path, err
	}
	return path, to.Close()
}
