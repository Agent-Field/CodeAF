package tui3

import (
	"io"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE RUN'S STORY IN THE MIDDLE OF THE ITEM PAGE ──────────────────────────
//
// The centre of the item page is the run told as it happened, one section per
// phase of the stream, top to bottom (owner decision, 2026-10-08):
//
//	✓ plan · 2m · $0.04
//	  Add a reclaim step to session/trees: delete node_modules and target after a
//	  session is terminal, keep .git and receipts. Two files, one test.   ▸ 14 steps
//
//	⠋ write · 4m · $0.27
//	  ✎ edit   internal/session/trees.go
//	  ⠋ bash   go build ./...
//
//	○ test ×2 · ○ review · ○ proof
//	──────────────────────────────────────────────────────────────────────────
//	› enter or click to talk to the manager
//
// A FINISHED STAGE IS FOLDED to its head and what it came to: the phase's own
// note, else the first sentence or two of the last thing its conversation
// said, and at the right a dim `▸ 14 steps` that opens the conversation. A
// RUNNING STAGE IS OPEN, its steps streaming in the chat's step gutter (the
// action family's mark, the verb in a column, what it was done to), newest at
// the bottom, at most [factoryTimelineLiveRows] of them under a dim
// `… 23 more above`. Stages still to come fold into one dim line, a skipped
// stage is one dim line, and a stage waiting on you shows its question and
// its keys under its head.
//
// THE MANAGER SPEAKS BETWEEN THE SECTIONS. The progress lines the runner
// writes into the item's own conversation, marked `factory-progress` by
// their producer ([session.DisplayEntry.Kind]), are drawn as dim
// `manager · …` lines after the section they are about, and the person's
// replies there as `you · …`. An item with no conversation draws none.
//
// THE BOX IS THE PANE'S LAST ROW: `› enter or click to talk to the manager`
// (`› enter or click to talk · r runs it` before a run). THE BOX HAS A FOCUS
// (owner decision, 2026-10-08): `enter` on the `manager` row, `enter` on the
// box's own stop, `tab` from the story, or a click on the box puts the keys
// in it, and while they are there every key types, the verbs' rail on the
// right is dimmed (factory_verbs.go: a dim rail means the keys type now) and
// the bottom bar says `type · enter send · esc back to keys`. Its typing row
// is the floor's own ([factoryAsk], kind [factoryAskManager]), empty it says
// `› say it`, and `esc` gives the keys back with the words kept as the box's
// draft, which the next focus shows again. `enter` opens the item's
// conversation the way `T` does with the words typed in its box, for the
// person to send there: the seam has no door that appends to the
// conversation and wakes it.
//
// DIVE IN: `enter` on a head, or a click on `▸ 14 steps`, shows that stage's
// whole conversation in the centre, through the transcript renderer the
// task page uses, following the file while the stage runs; `esc` comes back
// to the story on the same section, and a second `enter` opens the stage's
// conversation itself ([app.factoryOpenRoom]).
//
// THE TRANSCRIPTS ARE READ OFF THE LOOP, on the item page's one-second beat,
// and only what each file grew by is read ([app.factoryTimelineWake]). A
// frame never opens a file.

// factoryTimelineLiveRows is the most step lines a running stage shows; the
// older ones are counted on one dim line above them.
const factoryTimelineLiveRows = 12

// factoryTimelineResultRows is the most lines a folded stage's result takes.
const factoryTimelineResultRows = 2

// factoryProgressKind is the presentation kind the runner writes on the
// progress lines it puts into an item's conversation.
const factoryProgressKind = "factory-progress"

// factoryTLKind is what one spot of the timeline is, for the pointer.
type factoryTLKind int

const (
	factoryTLNone  factoryTLKind = iota
	factoryTLHead                // a section's head line
	factoryTLSteps               // the `▸ 14 steps` button
	factoryTLBox                 // the manager's box
)

// factoryTLHit is one spot the last draw put on the pane: what it is, the
// phase it belongs to (-1 for the box), its pane row and its cells [x0, x1),
// counted from the first cell past the pane's margin (the item page's
// [app.factoryPaneAt] reads a press the same way).
type factoryTLHit struct {
	kind   factoryTLKind
	phase  int
	row    int
	x0, x1 int
}

// factoryTLFile is one transcript as the timeline last read it: how far into
// the file, the bytes so far, and their reading.
type factoryTLFile struct {
	off  int64
	buf  []byte
	rec  session.Record
	read bool
}

// factoryTLClock is what the surface saw of one phase's time and money: when
// it was first seen running and what the stream had spent then, and when it
// was first seen not running and what had been spent by then. THE RUNNER
// KEEPS NEITHER ON A PHASE, so this is the honest reading of what was
// watched, exactly as [app.factoryPhaseElapsed] is.
type factoryTLClock struct {
	since  time.Time
	spent0 float64
	ended  time.Time
	spent1 float64
}

// factoryTimeline is the timeline's state, held as a.fp.tl.
type factoryTimeline struct {
	// id is the item this state is about; another item starts afresh.
	id int
	// at is the phase the cursor stands on, -1 for the box, and set says the
	// cursor has been placed at all (an unplaced cursor lands where the item
	// is moving).
	at  int
	set bool
	// stops is the cursor's stops as the last layout found them: each head's
	// phase in order, then -1 for the box when one is drawn.
	stops []int
	// flip is the sections a click turned from their default fold.
	flip map[int]bool
	// diving says a stage's conversation stands in the centre, dive is its
	// phase, and diveUp how many rows `K` has moved it up from the newest.
	diving bool
	dive   int
	diveUp int
	// top is the story's first row shown, free says `J` and `K` moved it
	// and the cursor's head need not be kept in view, page is the rows one
	// page is.
	top  int
	free bool
	page int
	// hover is what the pointer rests on, and hits what the last draw put
	// where.
	hover factoryTLHit
	hits  []factoryTLHit
	// files is every transcript read, by path; reading says a read is out,
	// and readAt when the last one came back.
	files   map[string]*factoryTLFile
	reading bool
	readAt  time.Time
	// clocks is what was seen of each phase's time, by [factoryPhaseKey].
	clocks map[string]factoryTLClock
	// diveRows is the dived-into transcript drawn once per growth of the
	// file and width, keyed by diveKey.
	diveRows []string
	diveKey  string
	// draft is the words the manager's box held when `esc` gave the keys
	// back, shown again when the box takes them next.
	draft string
}

// factoryTL is the timeline's state for item it, started afresh when the
// state was about another item.
func (a *app) factoryTL(it factory.Item) *factoryTimeline {
	tl := &a.fp.tl
	if tl.id != it.ID {
		files := tl.files
		*tl = factoryTimeline{id: it.ID, files: files}
	}
	return tl
}

// factoryTimelineAt is the phase the timeline's cursor stands on, and false
// when it stands on the box or has not been placed: the facets lane's left
// column selects the stage row it names.
func (a *app) factoryTimelineAt() (int, bool) {
	tl := &a.fp.tl
	if !tl.set || tl.at < 0 {
		return 0, false
	}
	return tl.at, true
}

// ── reading the transcripts ─────────────────────────────────────────────────

// factoryTLPaths is every transcript the timeline draws from for item it: its
// stages' conversations and the item's own.
func factoryTLPaths(it factory.Item) []string {
	var out []string
	if s := it.Stream; s != nil {
		for _, ph := range s.Phases {
			if chat := strings.TrimSpace(ph.Chat); chat != "" && !slices.Contains(out, chat) {
				out = append(out, chat)
			}
		}
	}
	if talk := strings.TrimSpace(it.Talk); talk != "" && !slices.Contains(out, talk) {
		out = append(out, talk)
	}
	return out
}

// factoryTLLive says whether the item's stream can still move, which is what
// keeps its transcripts read on the beat.
func factoryTLLive(it factory.Item) bool {
	return it.Stream != nil && it.Stream.Ended.IsZero()
}

// factoryTimelineWake is the loop asking, after every message, whether the
// open item page's transcripts are owed a read: one never read is read at
// once, and while the stream can still move each is read again once a beat
// ([factoryReadSoonEvery]). The read is OFF THE LOOP and takes only what each
// file grew by since the last ([factoryTLGrow]). It also folds what the
// surface saw of each phase's clock ([app.factoryTLFoldClocks]).
func (a *app) factoryTimelineWake() tea.Cmd {
	if !a.at(pageFactory) || !a.fp.open {
		return nil
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	tl := a.factoryTL(it)
	a.factoryTLFoldClocks(it)
	if tl.reading {
		return nil
	}
	paths := factoryTLPaths(it)
	if len(paths) == 0 {
		return nil
	}
	fresh := false
	for _, p := range paths {
		if tl.files[p] == nil {
			fresh = true
		}
	}
	due := factoryTLLive(it) && a.now().Sub(tl.readAt) >= factoryReadSoonEvery
	if !fresh && !due {
		return nil
	}
	jobs := make(map[string]factoryTLFile, len(paths))
	for _, p := range paths {
		if f := tl.files[p]; f != nil {
			jobs[p] = *f
		} else {
			jobs[p] = factoryTLFile{}
		}
	}
	tl.reading = true
	id := it.ID
	return a.besideLine(func() func(bool) tea.Cmd {
		got := make(map[string]factoryTLFile, len(jobs))
		for p, f := range jobs {
			got[p] = factoryTLGrow(p, f)
		}
		return func(bool) tea.Cmd {
			tl := &a.fp.tl
			tl.reading = false
			tl.readAt = a.now()
			if tl.files == nil {
				tl.files = map[string]*factoryTLFile{}
			}
			changed := false
			for p, f := range got {
				old := tl.files[p]
				if old == nil || old.off != f.off || old.read != f.read {
					changed = true
				}
				f := f
				tl.files[p] = &f
			}
			if changed && tl.id == id {
				a.touch()
			}
			return nil
		}
	})
}

// factoryTLGrow reads what the file at path grew by since f was read, and the
// whole record again from the bytes in hand: a file that grew is read from
// f's offset and no earlier, a file that shrank (rewritten) from its start,
// and a file that is the same size is f unchanged. A missing file is read and
// empty. It touches the disk, so it runs off the loop.
func factoryTLGrow(path string, f factoryTLFile) factoryTLFile {
	file, err := os.Open(path)
	if err != nil {
		return factoryTLFile{read: true}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return factoryTLFile{read: true}
	}
	size := info.Size()
	if size < f.off {
		f = factoryTLFile{}
	}
	if size == f.off && f.read {
		return f
	}
	grew := make([]byte, size-f.off)
	n, err := file.ReadAt(grew, f.off)
	if err != nil && err != io.EOF {
		f.read = true
		return f
	}
	// THE BYTES IN HAND ARE NEVER WRITTEN INTO: the loop still holds the
	// last reading's slice, so the growth goes onto a copy.
	f.buf = append(slices.Clip(f.buf), grew[:n]...)
	f.off += int64(n)
	f.rec = session.ReadTranscriptBytes(f.buf)
	f.read = true
	return f
}

// factoryTLRecord is the transcript at path as last read, and false when it
// has not been read yet.
func (a *app) factoryTLRecord(path string) (session.Record, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return session.Record{}, false
	}
	f := a.fp.tl.files[path]
	if f == nil || !f.read {
		return session.Record{}, false
	}
	return f.rec, true
}

// factoryTLFoldClocks notes, for the open item, when each phase was first
// seen running and first seen not running, with what the stream had spent at
// each moment, on the floor's own clock.
func (a *app) factoryTLFoldClocks(it factory.Item) {
	s := it.Stream
	if s == nil {
		return
	}
	tl := &a.fp.tl
	now := a.factoryFloorNow()
	if now.IsZero() {
		return
	}
	for i, ph := range s.Phases {
		k := factoryPhaseKey(it.ID, i, ph.Name)
		c, seen := tl.clocks[k]
		switch {
		case ph.State == factory.PhaseRunning && !seen:
			since := now
			if at, ok := a.fp.phaseSince[k]; ok && !at.IsZero() {
				since = at
			}
			if tl.clocks == nil {
				tl.clocks = map[string]factoryTLClock{}
			}
			tl.clocks[k] = factoryTLClock{since: since, spent0: s.Spent}
		case ph.State != factory.PhaseRunning && seen && c.ended.IsZero():
			c.ended, c.spent1 = now, s.Spent
			tl.clocks[k] = c
		}
	}
}

// factoryTLClockWords is how long the phase at index at ran and what it
// spent, as far as the surface saw it: `4m`, `$0.27`, each left out when it
// is not known (the emptiness law).
func (a *app) factoryTLClockWords(it factory.Item, at int) []string {
	s := it.Stream
	ph := s.Phases[at]
	c, seen := a.fp.tl.clocks[factoryPhaseKey(it.ID, at, ph.Name)]
	var out []string
	switch {
	case ph.State == factory.PhaseRunning:
		if e := a.factoryPhaseElapsed(it.ID, at, ph.Name); e != "" {
			out = append(out, e)
		}
		if seen {
			if m := factoryMoney(s.Spent - c.spent0); m != "" {
				out = append(out, m)
			}
		}
	case seen && !c.ended.IsZero():
		if e := factoryShortSpan(c.ended.Sub(c.since)); e != "" {
			out = append(out, e)
		}
		if m := factoryMoney(c.spent1 - c.spent0); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// factoryShortSpan is a span in the running phase's spelling: `12s`, `4m`,
// `1h 5m`, and nothing under a second.
func factoryShortSpan(d time.Duration) string {
	switch {
	case d < time.Second:
		return ""
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	}
	base := time.Unix(0, 0)
	return factoryElapsed(base, base.Add(d))
}

// ── the story's lines ───────────────────────────────────────────────────────

// factoryTLLine is one line of the story before it is laid on the pane: the
// words at the left, painted, and the button at the right with its plain
// width; what the left is for the pointer and the cursor, and the phase both
// belong to.
type factoryTLLine struct {
	left   string
	right  string
	rightW int
	kind   factoryTLKind
	phase  int
	btn    bool
}

// factoryTLManagerLine is one line from the item's conversation: who said it
// and what, and the phase it is drawn after (-1 for after the last that
// started).
type factoryTLManagerLine struct {
	you    bool
	words  string
	anchor int
}

// factoryTLManager is the item's conversation as the timeline draws it. While
// a run stands, the runner's progress lines and the person's words after the
// first of them; before a run, everything the person and the manager said.
// EACH PROGRESS LINE IS DRAWN AFTER THE SECTION IT IS ABOUT, which is the
// phase its first word names (`test failed 1 of 2 · asking you`), else after
// the last section that started; the person's words follow the line before
// them.
func (a *app) factoryTLManager(it factory.Item) []factoryTLManagerLine {
	rec, ok := a.factoryTLRecord(it.Talk)
	if !ok {
		return nil
	}
	run := it.Stream != nil && len(it.Stream.Phases) > 0
	latest := -1
	if run {
		for i, ph := range it.Stream.Phases {
			if ph.State != factory.PhasePending {
				latest = i
			}
		}
	}
	anchorOf := func(words string) int {
		first, _, _ := strings.Cut(strings.TrimSpace(words), " ")
		found := -1
		for i, ph := range it.Stream.Phases {
			if ph.Name == first && (found < 0 || ph.State != factory.PhasePending) {
				found = i
			}
		}
		if found >= 0 {
			return found
		}
		return latest
	}
	var out []factoryTLManagerLine
	progress, anchor := false, latest
	for _, e := range rec.Entries {
		words := strings.TrimSpace(firstLine(e.Text))
		if words == "" {
			continue
		}
		switch {
		case e.Role == "assistant" && e.Kind == factoryProgressKind:
			progress = true
			anchor = latest
			if run {
				anchor = anchorOf(words)
			}
			out = append(out, factoryTLManagerLine{words: words, anchor: anchor})
		case e.Role == "assistant" && !run:
			out = append(out, factoryTLManagerLine{words: words, anchor: -1})
		case e.Role == "user" && (!run || progress):
			out = append(out, factoryTLManagerLine{you: true, words: words, anchor: anchor})
		}
	}
	return out
}

// factoryTLManagerRow is one manager line as the story draws it: `manager ·
// …` dim whole, and `you · …` with the person's words in ink, because they
// are the one line on it a person wrote.
func (a *app) factoryTLManagerRow(l factoryTLManagerLine, measure int) factoryTLLine {
	pal := a.pal
	if l.you {
		return factoryTLLine{left: fit(pal.dim(wordYou+rowSep)+pal.ink(l.words), measure), phase: -1}
	}
	return factoryTLLine{left: pal.dim(fit(wordManager+rowSep+l.words, measure)), phase: -1}
}

// factoryTLDefaultOpen says whether a section stands open before any click:
// the stage running (or held), and the stage waiting on the person.
func factoryTLDefaultOpen(kind factoryMarkKind) bool {
	return kind == factoryMarkRunning || kind == factoryMarkPaused || kind == factoryMarkWaiting
}

// factoryTLOpen says whether the section of phase at stands open now.
func (a *app) factoryTLOpen(it factory.Item, at int) bool {
	return factoryTLDefaultOpen(factoryPhaseKind(it, at)) != a.fp.tl.flip[at]
}

// factoryTLStory is the story's lines top to bottom at measure cells, and the
// cursor's stops in order. AN ITEM WITH NO STREAM is the issue's one line and
// whatever its conversation holds.
func (a *app) factoryTLStory(it factory.Item, measure int) []factoryTLLine {
	pal := a.pal
	mgr := a.factoryTLManager(it)
	var out []factoryTLLine
	gap := func() {
		if len(out) > 0 {
			out = append(out, factoryTLLine{phase: -1})
		}
	}
	// WHAT CHANGED ABOUT THE STAGES STANDS FIRST, dim, one line: the run
	// row's second line, `manager set review: … · added arch after review ·
	// why: …` ([factory.AdaptedLine]), and nothing on an item nobody changed.
	if adapted := factory.AdaptedLine(it); adapted != "" {
		out = append(out, factoryTLLine{left: pal.dim(fit(adapted, measure)), phase: -1})
	}
	if it.Stream == nil || len(it.Stream.Phases) == 0 {
		summary := strings.TrimSpace(it.Triage.Read)
		if summary == "" {
			summary = strings.TrimSpace(firstLine(it.Body))
		}
		if summary == "" {
			summary = strings.TrimSpace(it.Title)
		}
		gap()
		out = append(out, factoryTLLine{left: pal.muted(fit(summary, measure)), phase: -1})
		if len(mgr) > 0 {
			gap()
			for _, l := range mgr {
				out = append(out, a.factoryTLManagerRow(l, measure))
			}
		}
		return out
	}
	phases := it.Stream.Phases
	stages := factoryStages(a.fp.snap, it)
	said := func(at int) {
		first := true
		for _, l := range mgr {
			if l.anchor != at {
				continue
			}
			if first {
				gap()
				first = false
			}
			out = append(out, a.factoryTLManagerRow(l, measure))
		}
	}
	lastOne := false // the line above is a one-line dim section
	for i := 0; i < len(phases); {
		kind := factoryPhaseKind(it, i)
		switch kind {
		case factoryMarkPending:
			var parts []string
			j := i
			for ; j < len(phases) && factoryPhaseKind(it, j) == factoryMarkPending; j++ {
				words := phases[j].Name
				if most := factoryStageMax(stages, phases[j].Name); most > 1 {
					words += " ×" + itoa(most)
				}
				parts = append(parts, a.factoryPendingMark()+" "+words)
			}
			if !lastOne {
				gap()
			}
			out = append(out, factoryTLLine{left: pal.dim(fit(strings.Join(parts, rowSep), measure)), phase: -1})
			lastOne = true
			for k := i; k < j; k++ {
				said(k)
			}
			i = j
			continue
		case factoryMarkSkipped:
			mark, _ := a.factoryKindMark(factoryMarkSkipped)
			if !lastOne {
				gap()
			}
			out = append(out, factoryTLLine{left: pal.dim(fit(mark+" "+phases[i].Name+rowSep+wordSkipped, measure)), phase: -1})
			lastOne = true
			said(i)
			i++
			continue
		}
		gap()
		out = append(out, a.factoryTLSection(it, i, kind, stages, measure)...)
		lastOne = false
		said(i)
		i++
	}
	// A LINE ABOUT NO SECTION THAT STARTED stands at the foot of the story.
	said(-1)
	return out
}

// factoryTLHead is the section head of phase at: its mark (the spinner while
// it runs), its words, and what the surface saw of its time and money. A
// FOLDED HEAD SAYS ITS WORDS AS THE STRIP SAYS THEM, `✓ review 2/2 · 3m ·
// $0.20`; AN OPEN HEAD SAYS ITS LOOP WHOLE ([app.factoryTLLoopWords]),
// `⠋ review · round 1 of 2 · until clean · per finding · 4m · $0.27`.
func (a *app) factoryTLHead(it factory.Item, at int, kind factoryMarkKind, stages []factory.Stage, open bool) string {
	pal := a.pal
	mark, markPaint := a.factoryKindMark(kind)
	if kind == factoryMarkRunning {
		mark = a.factorySpin()
	}
	var parts []string
	if open {
		parts = factoryTLLoopWords(it, at, kind, stages)
	} else {
		parts = []string{phaseWord(it, at, factoryStageMax(stages, it.Stream.Phases[at].Name))}
	}
	parts = append(parts, a.factoryTLClockWords(it, at)...)
	paint := pal.muted
	if kind == factoryMarkRunning || kind == factoryMarkWaiting {
		paint = pal.ink
	}
	return markPaint(mark) + " " + paint(strings.Join(parts, rowSep))
}

// factoryTLLoopWords is an open head's words before its time and money: the
// stage's name (and `paused` or `stopped` on a held or ended one), the round
// it is on over its most where it may take more than one, `round 1 of 2`,
// what it runs until, `until clean`, and how it fans out where it is not one
// task, `per finding`. A KNOB NOBODY SET SAYS NOTHING (the emptiness law).
func factoryTLLoopWords(it factory.Item, at int, kind factoryMarkKind, stages []factory.Stage) []string {
	ph := it.Stream.Phases[at]
	st, _ := factoryStageNamed(stages, ph.Name)
	parts := []string{ph.Name}
	switch kind {
	case factoryMarkPaused:
		parts = append(parts, "paused")
	case factoryMarkStopped:
		parts = append(parts, "stopped")
	}
	if most := max(st.Max, ph.Round); most > 1 && ph.Round > 0 {
		parts = append(parts, wordRound+" "+itoa(ph.Round)+" "+wordOf+" "+itoa(most))
	}
	if u := strings.TrimSpace(st.Until); u != "" {
		parts = append(parts, wordUntil+" "+u)
	}
	if f := strings.TrimSpace(st.Fanout); f != "" && f != "one" {
		parts = append(parts, strings.ReplaceAll(f, "-", " "))
	}
	return parts
}

// factoryStageNamed is the first of the item's stages named name, and false
// when it runs none of that name.
func factoryStageNamed(stages []factory.Stage, name string) (factory.Stage, bool) {
	for _, st := range stages {
		if st.Name == name {
			return st, true
		}
	}
	return factory.Stage{}, false
}

// factoryTLBrief is the dim lines under an open head, before its question or
// its steps: the stage's ask, `ask: thorough on security, code and
// architecture`, and the reason it was set where one was given, `why: touches
// billing`, each one line fit to the measure.
func (a *app) factoryTLBrief(st factory.Stage, inner int) []string {
	var out []string
	if ask := oneLineOf(st.Ask); ask != "" {
		out = append(out, a.pal.dim(fit(wordAskLabel+" "+ask, inner)))
	}
	if why := oneLineOf(stageWhy(st)); why != "" {
		out = append(out, a.pal.dim(fit(wordWhyLabel+" "+why, inner)))
	}
	return out
}

// oneLineOf is text on one line: its runs of space and its line breaks each
// one space.
func oneLineOf(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// factoryTLButton is the section's door into its conversation, `▸ 14 steps`
// (`▸ open` before its transcript is read), and "" for a stage with nothing
// behind it: no conversation, and not running.
func (a *app) factoryTLButton(it factory.Item, at int, kind factoryMarkKind) string {
	ph := it.Stream.Phases[at]
	chat := strings.TrimSpace(ph.Chat)
	if chat == "" && kind != factoryMarkRunning && kind != factoryMarkPaused {
		return ""
	}
	mark := a.linearMark(tokens.GlyphCollapsed, ">")
	if rec, ok := a.factoryTLRecord(chat); ok {
		n := factoryTLCalls(rec)
		return mark + " " + itoa(n) + " " + factoryPlural(n, wordStep, wordSteps)
	}
	return mark + " " + wordOpen
}

// factoryTLCalls is how many tool calls a transcript holds.
func factoryTLCalls(rec session.Record) int {
	n := 0
	for _, e := range rec.Entries {
		if e.Role == "tool" && e.Tool != "" {
			n++
		}
	}
	return n
}

// factoryTLSection is the section of phase at: its head, then, folded, what
// it came to with the button at the right of its last line, or, open, its
// question or its steps with the button at the right of the head.
func (a *app) factoryTLSection(it factory.Item, at int, kind factoryMarkKind, stages []factory.Stage, measure int) []factoryTLLine {
	pal := a.pal
	ph := it.Stream.Phases[at]
	open := a.factoryTLOpen(it, at)
	head := factoryTLLine{left: a.factoryTLHead(it, at, kind, stages, open), kind: factoryTLHead, phase: at}
	button := a.factoryTLButton(it, at, kind)
	inner := max(measure-factoryLeadW, 0)
	indent := factorySpaces(factoryLeadW)
	out := []factoryTLLine{head}
	withButton := func(l *factoryTLLine) {
		if button != "" {
			l.right, l.rightW, l.btn = pal.dim(button), ansi.StringWidth(button), true
		}
	}
	if !open {
		result := strings.TrimSpace(ph.Note)
		if result == "" {
			if rec, ok := a.factoryTLRecord(ph.Chat); ok {
				result = factoryTLSaid(rec)
			}
		}
		if result == "" && kind == factoryMarkFailed {
			result = "failed"
		}
		var lines []string
		if result != "" {
			lines = wrap(factoryTLSentences(result, factoryTimelineResultRows), inner)
		}
		if len(lines) > factoryTimelineResultRows {
			lines = lines[:factoryTimelineResultRows]
		}
		if len(lines) == 0 {
			withButton(&out[0])
			return out
		}
		for _, line := range lines {
			out = append(out, factoryTLLine{left: indent + pal.muted(line), phase: at})
		}
		withButton(&out[len(out)-1])
		return out
	}
	withButton(&out[0])
	if st, ok := factoryStageNamed(stages, ph.Name); ok {
		for _, line := range a.factoryTLBrief(st, inner) {
			out = append(out, factoryTLLine{left: indent + line, phase: at})
		}
	}
	if kind == factoryMarkWaiting {
		q := strings.TrimSpace(it.Question)
		if q == "" {
			q = strings.TrimSpace(ph.Note)
		}
		for _, line := range wrap(q, inner) {
			out = append(out, factoryTLLine{left: indent + pal.ink(line), phase: at})
		}
		if a.factory.Has("answer") {
			keys := strings.Join([]string{factoryHintClause(keyYes, wordYes), factoryHintClause(keyNo, wordNo), factoryHintClause(keyInWords, wordInWords)}, rowSep)
			out = append(out, factoryTLLine{left: indent + pal.dim(fit(keys, inner)), phase: at})
		}
		return out
	}
	steps := a.factoryTLSteps(it, at, kind, inner)
	if len(steps) > factoryTimelineLiveRows {
		more := len(steps) - (factoryTimelineLiveRows - 1)
		steps = append([]string{pal.dim(fit("… "+itoa(more)+" "+wordMoreAbove, inner))}, steps[more:]...)
	}
	for _, line := range steps {
		out = append(out, factoryTLLine{left: indent + line, phase: at})
	}
	return out
}

// factoryTLSteps is the stage's steps, oldest first, each one line in the
// chat's step gutter: from its transcript when it has one, and from the
// stream's log while it runs with none yet (the runner names a stage's
// conversation when its round ends).
func (a *app) factoryTLSteps(it factory.Item, at int, kind factoryMarkKind, measure int) []string {
	ph := it.Stream.Phases[at]
	if rec, ok := a.factoryTLRecord(ph.Chat); ok && len(rec.Entries) > 0 {
		return a.factoryTLStepRows(rec, kind == factoryMarkRunning, measure)
	}
	if kind != factoryMarkRunning && kind != factoryMarkPaused {
		return nil
	}
	pal := a.pal
	var out []string
	for _, l := range it.Stream.Log {
		mark, markPaint, _ := a.factoryTone(l)
		text := pal.muted
		if l.Tone == "thought" {
			markPaint, text = pal.dim, pal.dim
		}
		out = append(out, markPaint(mark)+" "+text(fit(l.Text, max(measure-factoryLeadW, 0))))
	}
	return out
}

// factoryTLStepRows is a transcript's steps: each call as its family's mark
// (the chat's step gutter, [app.actionMarkFor]), its verb in a column of
// [factoryTimelineVerbW] and what it was done to, and each thing the stage
// said as `said` and its first words. A call with no result under it on a
// running stage wears the spinner, because it is the one still going.
func (a *app) factoryTLStepRows(rec session.Record, running bool, measure int) []string {
	pal := a.pal
	rest := max(measure-factoryLeadW-factoryTimelineVerbW-1, 0)
	var out []string
	for _, e := range rec.Entries {
		switch e.Role {
		case "tool":
			// A RESULT IS NOT A STEP: only the entry that names its call is.
			if strings.TrimSpace(e.Tool) == "" {
				continue
			}
			verb := strings.TrimSpace(e.Tool)
			obj := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(e.Hint), verb))
			lead := pal.dim(a.actionMarkFor(session.ActionCategoryForTool(verb)))
			ink := pal.muted
			if running && !e.Answered {
				lead, ink = pal.accent(a.factorySpin()), pal.ink
			}
			out = append(out, lead+" "+ink(factoryPad(verb, factoryTimelineVerbW))+" "+ink(fit(obj, rest)))
		case "assistant":
			words := strings.TrimSpace(firstLine(e.Text))
			if words == "" {
				continue
			}
			lead := pal.dim(a.actionMarkFor(session.ActionCommunicate))
			out = append(out, lead+" "+pal.muted(factoryPad(wordSaid, factoryTimelineVerbW))+" "+pal.dim(fit(words, rest)))
		}
	}
	return out
}

// factoryTLSaid is the last thing a stage's conversation said in words.
func factoryTLSaid(rec session.Record) string {
	for i := len(rec.Entries) - 1; i >= 0; i-- {
		e := rec.Entries[i]
		if e.Role == "assistant" && strings.TrimSpace(e.Text) != "" {
			return strings.TrimSpace(e.Text)
		}
	}
	return ""
}

// factoryTLSentences is the first n sentences of text, on one line.
func factoryTLSentences(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	end, found := 0, 0
	for found < n {
		i := strings.IndexAny(text[end:], ".!?")
		if i < 0 {
			return text
		}
		end += i + 1
		if end < len(text) && text[end] != ' ' {
			continue
		}
		found++
	}
	return strings.TrimSpace(text[:end])
}

// ── the pane ────────────────────────────────────────────────────────────────

// factoryTimelinePane is the centre of the item page while it shows the
// timeline: exactly room rows of exactly width cells. The story, or the
// stage dived into, fills the rows above a rule, and the last row is the
// manager's box, or, dived in, the keys that leave.
func (a *app) factoryTimelinePane(it factory.Item, width, room int) []string {
	out := make([]string, max(room, 0))
	tl := a.factoryTL(it)
	tl.hits = tl.hits[:0]
	if room <= 0 || width <= 0 {
		return out
	}
	for i := range out {
		out[i] = factorySpaces(width)
	}
	pal := a.pal
	// THE PAGE LAYS THE MARGINS: width is the pane's measure, the cells
	// between its margin and the verbs' rule, like every other pane's.
	lead := ""
	measure := width
	if tl.diving && (it.Stream == nil || tl.dive >= len(it.Stream.Phases)) {
		tl.diving = false
	}
	var foot []string
	last, box := a.factoryTLFoot(it, measure)
	if last != "" {
		foot = []string{pal.dim(strings.Repeat(a.linearMark("─", "-"), measure)), last}
	}
	if len(foot) > room {
		foot = foot[len(foot)-room:]
	}
	body := room - len(foot)
	tl.page = max(body, 1)
	var lines []factoryTLLine
	if tl.diving {
		lines = a.factoryTLDive(it, measure, body)
	} else {
		lines = a.factoryTLWindow(it, measure, body, box)
	}
	for i, l := range lines {
		if i >= body {
			break
		}
		out[i] = factoryPad(lead+a.factoryTLLay(l, i, measure), width)
	}
	for i, f := range foot {
		row := body + i
		text := lead + f
		if i == len(foot)-1 && box {
			tl.hits = append(tl.hits, factoryTLHit{kind: factoryTLBox, phase: -1, row: row, x0: 0, x1: measure})
			if !tl.diving && tl.set && tl.at == -1 && a.fp.act.ask == nil {
				text = pal.cursorRow(factoryPad(text, width-factoryMargin), width-factoryMargin)
			} else if tl.hover.kind == factoryTLBox {
				text = pal.cursor(factoryPad(text, width-factoryMargin), width-factoryMargin)
			}
		}
		out[row] = factoryPad(text, width)
	}
	return out
}

// factoryTLFoot is the pane's last row and whether it is the box: the typing
// row while the manager's box has the keys, else `› enter or click to talk
// to the manager` dim (`› enter or click to talk · r runs it` before a run,
// where `r` runs), and,
// dived in, the keys that leave. THE BOX IS DRAWN ONLY WHERE THE TALK DOOR
// IS, because its words go to the item's conversation.
func (a *app) factoryTLFoot(it factory.Item, measure int) (string, bool) {
	pal := a.pal
	if a.fp.tl.diving {
		var parts []string
		if s := it.Stream; s != nil && strings.TrimSpace(s.Phases[a.fp.tl.dive].Chat) != "" {
			parts = append(parts, factoryHintClause(keyOpen, wordOpensTheChat))
		}
		parts = append(parts, factoryHintClause(keyBack, wordBack))
		return pal.dim(fit(strings.Join(parts, rowSep), measure)), false
	}
	if !a.factory.Has("talk") {
		return "", false
	}
	if ask := a.fp.act.ask; ask != nil && ask.kind == factoryAskManager && ask.id == it.ID {
		return a.factoryAskLine(ask, measure), true
	}
	prompt := a.linearMark(tokens.GlyphPromptChat, ">")
	words := wordEnterOrClickToTalk + " " + wordToTheManager
	if it.Stream == nil && a.factoryCanRun() {
		words = wordEnterOrClickToTalk + rowSep + factoryHintClause(keyRun, wordRunsIt)
	}
	return pal.accent(prompt) + " " + pal.dim(fit(words, max(measure-ansi.StringWidth(prompt)-1, 0))), true
}

// factoryTLWindow is the story's rows that fit in room: the cursor's head
// kept in view unless `J` and `K` moved the story, and the cursor's stops
// noted from the whole story.
func (a *app) factoryTLWindow(it factory.Item, measure, room int, box bool) []factoryTLLine {
	tl := &a.fp.tl
	lines := a.factoryTLStory(it, measure)
	tl.stops = tl.stops[:0]
	for _, l := range lines {
		if l.kind == factoryTLHead {
			tl.stops = append(tl.stops, l.phase)
		}
	}
	if box {
		tl.stops = append(tl.stops, -1)
	}
	if !tl.set || !slices.Contains(tl.stops, tl.at) {
		tl.at, tl.set = a.factoryTLLanding(it, tl.stops), len(tl.stops) > 0
	}
	most := max(len(lines)-room, 0)
	if !tl.free {
		headAt := -1
		for i, l := range lines {
			if l.kind == factoryTLHead && l.phase == tl.at {
				headAt = i
			}
		}
		end := headAt
		for headAt >= 0 && end+1 < len(lines) && lines[end+1].phase == tl.at {
			end++
		}
		switch {
		case tl.at == -1:
			tl.top = most
		case headAt < 0:
		case headAt < tl.top:
			tl.top = headAt
		case end >= tl.top+room:
			// THE WHOLE SECTION COMES INTO VIEW when it fits, so a running
			// stage shows its newest step under its head; when it does not
			// fit, the head stays at the top.
			tl.top = min(headAt, end-room+1)
		}
	}
	tl.top = max(min(tl.top, most), 0)
	return lines[tl.top:]
}

// factoryTLLanding is where an unplaced cursor lands: the stage waiting on
// the person, else the running one, else the last head, else the box.
func (a *app) factoryTLLanding(it factory.Item, stops []int) int {
	if s := it.Stream; s != nil {
		for _, want := range []factory.PhaseState{factory.PhaseWaiting, factory.PhaseRunning} {
			for _, at := range stops {
				if at >= 0 && s.Phases[at].State == want {
					return at
				}
			}
		}
	}
	for i := len(stops) - 1; i >= 0; i-- {
		if stops[i] >= 0 {
			return stops[i]
		}
	}
	return -1
}

// factoryTLLay is one story line on pane row row: the words, and the button
// flush right at measure; the cursor's head on the cursor's ground, and the
// head or the button the pointer rests on on the pointer's. It notes where
// the head and the button were put.
func (a *app) factoryTLLay(l factoryTLLine, row, measure int) string {
	pal := a.pal
	tl := &a.fp.tl
	leftW := measure
	if l.right != "" {
		leftW = max(measure-l.rightW-factoryGutter, 0)
	}
	left := fit(l.left, leftW)
	usedW := ansi.StringWidth(left)
	cursor := !tl.diving && l.kind == factoryTLHead && tl.set && tl.at == l.phase
	if l.kind == factoryTLHead {
		tl.hits = append(tl.hits, factoryTLHit{kind: factoryTLHead, phase: l.phase, row: row, x0: 0, x1: usedW})
		if !cursor && tl.hover.kind == factoryTLHead && tl.hover.phase == l.phase {
			left = pal.cursor(left, usedW)
		}
	}
	line := left
	if l.right != "" {
		right := l.right
		x0 := measure - l.rightW
		if l.btn {
			tl.hits = append(tl.hits, factoryTLHit{kind: factoryTLSteps, phase: l.phase, row: row, x0: x0, x1: x0 + l.rightW})
			if tl.hover.kind == factoryTLSteps && tl.hover.phase == l.phase {
				right = pal.cursor(right, l.rightW)
			}
		}
		line = factoryPad(left, measure-l.rightW) + right
	}
	if cursor {
		line = pal.cursorRow(factoryPad(line, measure), measure)
	}
	return line
}

// factoryTLDive is the stage dived into as lines of the pane: its head, a
// blank, and its whole conversation through the task page's transcript
// renderer, the newest at the bottom unless `K` moved it up. A running stage
// with no conversation yet shows the stream's log, and a stage with nothing
// to show says so in the task page's own words.
func (a *app) factoryTLDive(it factory.Item, measure, room int) []factoryTLLine {
	pal := a.pal
	tl := &a.fp.tl
	at := tl.dive
	kind := factoryPhaseKind(it, at)
	stages := factoryStages(a.fp.snap, it)
	head := factoryTLLine{left: a.factoryTLHead(it, at, kind, stages, true), phase: at}
	ph := it.Stream.Phases[at]
	var rows []string
	if rec, ok := a.factoryTLRecord(ph.Chat); ok && len(rec.Entries) > 0 {
		rows = a.factoryTLDiveRows(ph.Chat, rec, kind == factoryMarkRunning, measure)
	} else if steps := a.factoryTLSteps(it, at, kind, measure); len(steps) > 0 {
		rows = steps
	} else {
		words := roomYetWord
		if kind != factoryMarkRunning && kind != factoryMarkPaused {
			words = factoryNoRoomWords(factoryStageView{stage: factory.Stage{Name: ph.Name, Kind: ph.Kind}, state: ph.State, phase: ph, ran: true})
		}
		rows = []string{pal.dim(fit(words, measure))}
	}
	out := []factoryTLLine{head, {phase: -1}}
	window := max(room-len(out), 0)
	most := max(len(rows)-window, 0)
	tl.diveUp = max(min(tl.diveUp, most), 0)
	from := most - tl.diveUp
	for _, r := range rows[from:min(from+window, len(rows))] {
		out = append(out, factoryTLLine{left: r, phase: -1})
	}
	return out
}

// factoryTLDiveRows is a stage transcript drawn by the task page's renderer
// ([app.roomRecord], [app.deckRows]) at measure cells, read-only: drawn again
// only when the file grew or the width changed, and every frame while the
// stage runs, so its spinner turns.
func (a *app) factoryTLDiveRows(path string, rec session.Record, running bool, measure int) []string {
	tl := &a.fp.tl
	off := int64(0)
	if f := tl.files[strings.TrimSpace(path)]; f != nil {
		off = f.off
	}
	key := path + "|" + itoa(int(off)) + "|" + itoa(measure)
	if !running && key == tl.diveKey {
		return tl.diveRows
	}
	entries, turns := a.roomRecord(rec, 0)
	d := deck{entries: entries, unfolded: map[int]bool{}, workOpen: map[int]bool{}, capOpen: map[int]bool{}, lens: transcriptLens}
	if running {
		d.runningTurn = turns
	}
	drawn, _ := a.deckRows(d, measure)
	rows := make([]string, 0, len(drawn))
	for _, r := range drawn {
		rows = append(rows, fit(r.text, measure))
	}
	tl.diveRows, tl.diveKey = rows, key
	return rows
}

// ── keys and the pointer ────────────────────────────────────────────────────

// factoryTimelineKey is a key while the centre is the timeline. On the story
// `↑` and `↓` walk the heads and the box, `enter` dives into the section
// under the cursor (on the box it opens the box), and `J`, `K`, `pgdown` and
// `pgup` scroll. Dived in, `esc` comes back to the story on the same section,
// `enter` opens the stage's conversation where it has one, and the scroll
// keys move the transcript. It answers false for every other key.
func (a *app) factoryTimelineKey(it factory.Item, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	tl := a.factoryTL(it)
	k := msg.String()
	step := 0
	switch k {
	case "J", "shift+j":
		step = 1
	case "K", "shift+k":
		step = -1
	case "pgdown":
		step = max(tl.page, 1)
	case "pgup":
		step = -max(tl.page, 1)
	}
	if tl.diving {
		switch {
		case k == "esc":
			tl.diving, tl.diveUp, tl.free = false, 0, false
			tl.at, tl.set = tl.dive, true
			a.touch()
			return nil, true
		case k == "enter":
			if s := it.Stream; s != nil && tl.dive < len(s.Phases) && strings.TrimSpace(s.Phases[tl.dive].Chat) != "" {
				ph := s.Phases[tl.dive]
				view := factoryStageView{stage: factory.Stage{Name: ph.Name, Kind: ph.Kind}, state: ph.State, phase: ph, ran: true}
				return a.factoryOpenRoom(it, view), true
			}
			return nil, true
		case step != 0:
			tl.diveUp = max(tl.diveUp-step, 0)
			a.touch()
			return nil, true
		}
		return nil, false
	}
	switch {
	case k == "up" || k == "ctrl+p":
		a.factoryTLMove(-1)
		return nil, true
	case k == "down" || k == "ctrl+n":
		a.factoryTLMove(1)
		return nil, true
	case k == "enter":
		if !tl.set {
			return nil, false
		}
		if tl.at == -1 {
			a.factoryTLOpenBox(it)
			return nil, true
		}
		// A STAGE WITH NOTHING TO SHOW YET (not started, no conversation)
		// is not dived into: the page's own `enter` says why it has none.
		if !factoryTLCanDive(it, tl.at) {
			return nil, false
		}
		a.factoryTLDiveIn(tl.at)
		return nil, true
	case step != 0:
		tl.top = max(tl.top+step, 0)
		tl.free = true
		a.touch()
		return nil, true
	}
	return nil, false
}

// factoryTLMove walks the cursor delta stops along the last layout's.
func (a *app) factoryTLMove(delta int) {
	tl := &a.fp.tl
	if len(tl.stops) == 0 {
		return
	}
	at := slices.Index(tl.stops, tl.at)
	if at < 0 {
		at = 0
	}
	at = max(min(at+delta, len(tl.stops)-1), 0)
	tl.at, tl.set, tl.free = tl.stops[at], true, false
	a.factoryRowFollowTL()
	a.touch()
}

// ONE CURSOR, TWO COLUMNS: the left column's stage row and the story's
// section head name the same stage, so moving either moves the other.
// factoryTLFollowRow puts the story's cursor on the section the left column's
// row names, climbing out of a dive into another stage; a row that is not a
// stage leaves the story where it was.
func (a *app) factoryTLFollowRow() {
	if !a.fp.open {
		return
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return
	}
	r, ok := a.factoryPageRowAt(it)
	if !ok || r.kind != factoryPageStage {
		return
	}
	tl := a.factoryTL(it)
	if tl.diving && tl.dive != r.at {
		tl.diving, tl.diveUp = false, 0
	}
	tl.at, tl.set, tl.free = r.at, true, false
}

// factoryRowFollowTL selects the left column's stage row for the section the
// story's cursor stands on, without touching the story.
func (a *app) factoryRowFollowTL() {
	it, ok := a.factoryCursorItem()
	if !ok {
		return
	}
	ph, ok := a.factoryTimelineAt()
	if !ok {
		return
	}
	for i, r := range a.factoryItemRows(it) {
		if r.kind == factoryPageStage && r.at == ph {
			if i != a.fp.stage {
				a.fp.stage, a.fp.scroll, a.fp.said, a.pageMsg = i, 0, false, ""
			}
			return
		}
	}
}

// factoryTLDiveIn shows the conversation of phase at in the centre.
func (a *app) factoryTLDiveIn(at int) {
	tl := &a.fp.tl
	tl.diving, tl.dive, tl.diveUp = true, at, 0
	tl.at, tl.set = at, true
	tl.hover = factoryTLHit{}
	a.touch()
}

// factoryTLOpenBox puts the keys in the manager's box: the floor's typing
// row, kind [factoryAskManager], drawn on the pane's last row with the draft
// `esc` kept. A box that has them already keeps its words.
func (a *app) factoryTLOpenBox(it factory.Item) {
	if !a.factory.Has("talk") || a.factoryBoxFocused() {
		return
	}
	tl := a.factoryTL(it)
	tl.diving, tl.diveUp, tl.free = false, 0, false
	tl.at, tl.set = -1, true
	a.factoryOpenAsk(factoryAsk{kind: factoryAskManager, id: it.ID, label: a.linearMark(tokens.GlyphPromptChat, ">"), example: wordSayIt, text: tl.draft})
}

// factoryBoxFocused says whether the manager's box has the keys.
func (a *app) factoryBoxFocused() bool {
	ask := a.fp.act.ask
	return ask != nil && ask.kind == factoryAskManager
}

// factoryTimelineSend is `enter` in the manager's box: the item's own
// conversation opens the way `T` opens it ([app.factoryTalkSaying]), with the
// words typed in its box for the person to send there.
func (a *app) factoryTimelineSend(id int, words string) tea.Cmd {
	it, ok := a.factoryItemByID(id)
	if !ok || !a.factory.Has("talk") {
		return nil
	}
	return a.factoryTalkSaying(it, words)
}

// factoryTLHitAt is the spot the last draw put at pane cell (x, y).
func (a *app) factoryTLHitAt(x, y int) factoryTLHit {
	for _, h := range a.fp.tl.hits {
		if h.row == y && x >= h.x0 && x < h.x1 {
			return h
		}
	}
	return factoryTLHit{}
}

// factoryTimelinePress is a click at pane cell (x, y), x counted from the
// first cell past the margin and y from the pane's first line: on a head it opens or folds that section and puts the
// cursor on it, on `▸ 14 steps` it dives into the stage, and on the box it
// opens the box. A click on a step line, or anywhere else, does nothing and
// answers false.
func (a *app) factoryTimelinePress(it factory.Item, x, y int) (tea.Cmd, bool) {
	tl := a.factoryTL(it)
	h := a.factoryTLHitAt(x, y)
	switch h.kind {
	case factoryTLHead:
		if tl.flip == nil {
			tl.flip = map[int]bool{}
		}
		tl.flip[h.phase] = !tl.flip[h.phase]
		tl.at, tl.set, tl.free = h.phase, true, false
		a.factoryRowFollowTL()
		a.touch()
		return nil, true
	case factoryTLSteps:
		a.factoryTLDiveIn(h.phase)
		return nil, true
	case factoryTLBox:
		if tl.diving {
			return nil, false
		}
		a.factoryTLOpenBox(it)
		return nil, true
	}
	return nil, false
}

// factoryTimelineHover is the pointer resting at pane cell (x, y): a head,
// the steps button or the box under it wears the pointer's ground. It
// answers whether what wears it changed.
func (a *app) factoryTimelineHover(it factory.Item, x, y int) bool {
	tl := a.factoryTL(it)
	h := a.factoryTLHitAt(x, y)
	next := factoryTLHit{kind: h.kind, phase: h.phase}
	if next == tl.hover {
		return false
	}
	tl.hover = next
	a.touch()
	return true
}

// factoryTLCanDive says whether phase at has a story to dive into: a
// conversation of its own, or a round still running whose log is the story.
func factoryTLCanDive(it factory.Item, at int) bool {
	s := it.Stream
	if s == nil || at < 0 || at >= len(s.Phases) {
		return false
	}
	ph := s.Phases[at]
	return strings.TrimSpace(ph.Chat) != "" || ph.State == factory.PhaseRunning || ph.State == factory.PhaseWaiting
}
