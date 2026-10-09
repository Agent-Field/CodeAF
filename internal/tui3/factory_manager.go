package tui3

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE MANAGER'S CONVERSATION, READ FOR THE ITEM PAGE ──────────────────────
//
// The item page's center is the real chat of the selected row
// (factory_host.go); what is left here is what the page itself needs to
// know of the manager's conversation: whether a turn runs on it, so the
// manager row and the floor can say `manager is thinking`, and the manager's
// box, the one typing row the page keeps for a manager that has no chat yet.
// The box sends through the seam's Say door ([factory.Seam.Say]), and the
// chat that makes is the center from then on.
//
// THE CONVERSATION IS READ OFF THE LOOP, on the item page's one-second beat,
// and only what the file grew by is read ([app.factoryTimelineWake]). A
// frame never opens a file.

// factoryProgressKind is the presentation kind the runner writes on the
// progress lines it puts into an item's conversation.
const factoryProgressKind = "factory-progress"

// factoryTLFile is one transcript as the timeline last read it: how far into
// the file, the bytes so far, and their reading.
type factoryTLFile struct {
	off  int64
	buf  []byte
	rec  session.Record
	read bool
}

// factoryTimeline is what the item page keeps of the manager's conversation,
// held as a.fp.tl: what the manager's box held when `esc` gave the keys back,
// the conversation as last read, and whether a turn runs on it.
type factoryTimeline struct {
	// id is the item this state is about; another item starts afresh.
	id int
	// files is every transcript read, by path; reading says a read is out,
	// and readAt when the last one came back.
	files   map[string]*factoryTLFile
	reading bool
	readAt  time.Time
	// draft is the words the manager's box held when `esc` gave the keys
	// back, shown again when the box takes them next.
	draft string
	// thinking is the items whose manager has a turn running as a setter said
	// ([app.factoryManagerThinking]), each with how many entries its
	// conversation held when it was set, so a reply after them ends it.
	// liveTurn is the items whose conversation the last read found open in
	// this process with a turn in flight, and sawRun the flagged items whose
	// turn a read has seen running, so the read that finds it stopped ends
	// the flag. pending is the person's words the box sent that the
	// transcript has not shown yet. ALL FOUR ARE KEPT BY ITEM, across items,
	// because a turn goes on running while the page shows another item.
	thinking map[int]int
	liveTurn map[int]bool
	sawRun   map[int]bool
	pending  map[int][]factoryTLPending
}

// factoryTLPending is one thing the person sent from the box, drawn as `you ·
// …` at once and until the transcript holds it: the words, and how many
// entries the conversation held when they were sent.
type factoryTLPending struct {
	words string
	from  int
}

// factoryTL is the manager's state for item it, started afresh when the
// state was about another item.
func (a *app) factoryTL(it factory.Item) *factoryTimeline {
	tl := &a.fp.tl
	if tl.id != it.ID {
		*tl = factoryTimeline{id: it.ID, files: tl.files, thinking: tl.thinking, liveTurn: tl.liveTurn, sawRun: tl.sawRun, pending: tl.pending}
	}
	return tl
}

// ── the manager is thinking ─────────────────────────────────────────────────
//
// WHILE A TURN RUNS ON THE MANAGER'S CONVERSATION the manager's box says
// `⠋ manager is thinking`, on the factory's spinner, and the page's beat reads
// the conversation every second so the reply streams in as it lands. Two
// things say a turn runs, and either is enough:
//
//   - a setter ([app.factoryManagerThinking]): the box's send, and the item
//     page's own shaping turn when it opens. The flag ends when the setter
//     says so, when the conversation's last entry is a reply written after
//     the flag was set, or when a read that saw the turn running sees it
//     stopped;
//   - the conversation open in THIS process with a turn in flight
//     ([session.LiveTurnRunning]), read off the loop with the transcripts, so
//     a turn started on the chat page or by the runner shows too.

// factoryManagerThinking says a turn on item id's manager conversation has
// started (on) or ended (off). It is the one setter every road that starts
// such a turn calls, and the story draws what it says.
func (a *app) factoryManagerThinking(id int, on bool) {
	tl := &a.fp.tl
	if !on {
		delete(tl.thinking, id)
		delete(tl.sawRun, id)
		a.touch()
		return
	}
	if tl.thinking == nil {
		tl.thinking = map[int]int{}
	}
	tl.thinking[id] = a.factoryTLTalkEntries(id)
	a.touch()
}

// factoryTLTalkEntries is how many entries item id's conversation held as
// last read, 0 when it has none or it was not read.
func (a *app) factoryTLTalkEntries(id int) int {
	it, ok := a.factoryItemByID(id)
	if !ok {
		return 0
	}
	rec, ok := a.factoryTLRecord(it.Talk)
	if !ok {
		return 0
	}
	return len(rec.Entries)
}

// factoryTLThinking says whether a turn runs on item it's manager
// conversation now, as far as this window can tell.
func (a *app) factoryTLThinking(it factory.Item) bool {
	tl := &a.fp.tl
	if tl.liveTurn[it.ID] {
		return true
	}
	from, on := tl.thinking[it.ID]
	if !on {
		return false
	}
	if rec, ok := a.factoryTLRecord(it.Talk); ok && factoryTLReplied(rec, from) {
		return false
	}
	return true
}

// factoryTLReplied says whether the conversation's last entry is the
// manager's reply in words, written at or after entry from: the turn that
// was asked for has answered.
func factoryTLReplied(rec session.Record, from int) bool {
	n := len(rec.Entries)
	if n == 0 || n-1 < from {
		return false
	}
	e := rec.Entries[n-1]
	return e.Role == "assistant" && e.Kind != factoryProgressKind && strings.TrimSpace(e.Text) != ""
}

// factoryTLFoldTurn takes what a read found of item id's conversation in this
// process: a turn in flight, or none. A flag whose turn was seen running and
// is not any more is ended.
func (a *app) factoryTLFoldTurn(id int, running bool) bool {
	tl := &a.fp.tl
	was := tl.liveTurn[id]
	if running {
		if tl.liveTurn == nil {
			tl.liveTurn = map[int]bool{}
		}
		tl.liveTurn[id] = true
		if _, on := tl.thinking[id]; on {
			if tl.sawRun == nil {
				tl.sawRun = map[int]bool{}
			}
			tl.sawRun[id] = true
		}
		return !was
	}
	delete(tl.liveTurn, id)
	if tl.sawRun[id] {
		delete(tl.sawRun, id)
		delete(tl.thinking, id)
		return true
	}
	return was
}

// factoryTLFoldPending lets go of the words the box sent that item id's
// conversation now holds, read as the person's line at or after the moment
// they were sent.
func (a *app) factoryTLFoldPending(id int, rec session.Record) {
	tl := &a.fp.tl
	kept := tl.pending[id][:0]
	for _, p := range tl.pending[id] {
		if !factoryTLHolds(rec, p) {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		delete(tl.pending, id)
		return
	}
	tl.pending[id] = kept
}

// factoryTLHolds says whether the conversation holds the sent words p as a
// person's line at or after the entry they were sent at.
func factoryTLHolds(rec session.Record, p factoryTLPending) bool {
	for i := max(p.from, 0); i < len(rec.Entries); i++ {
		e := rec.Entries[i]
		if e.Role == "user" && strings.TrimSpace(e.Text) == p.words {
			return true
		}
	}
	return false
}

// ── reading the manager's conversation ───────────────────────────────────────

// factoryTimelineWake is the loop asking, after every message, whether the
// open item's manager conversation is owed a read: never read, it is read at
// once, and then again once a beat
// ([factoryReadSoonEvery]). The read is OFF THE LOOP and takes only what the
// file grew by since the last ([factoryTLGrow]).
func (a *app) factoryTimelineWake() tea.Cmd {
	if !a.at(pageFactory) || !a.fp.open {
		return nil
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	tl := a.factoryTL(it)
	if tl.reading {
		return nil
	}
	talk := strings.TrimSpace(it.Talk)
	if talk == "" {
		return nil
	}
	paths := []string{talk}
	fresh := false
	for _, p := range paths {
		if tl.files[p] == nil {
			fresh = true
		}
	}
	// THE MANAGER'S CONVERSATION IS READ ON THE BEAT whenever the item has
	// one, not only while a run moves: it is where the replies land, and a
	// turn started on the chat page shows here too.
	due := a.now().Sub(tl.readAt) >= factoryReadSoonEvery
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
		running := talk != "" && session.LiveTurnRunning(talk)
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
			if talk != "" {
				if a.factoryTLFoldTurn(id, running) {
					changed = true
				}
				if f, ok := got[talk]; ok && f.read {
					a.factoryTLFoldPending(id, f.rec)
				}
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

// ── the manager's box ───────────────────────────────────────────────────────

// factoryTLOpenBox puts the keys in the manager's box: the floor's typing
// row, kind [factoryAskManager], drawn on the pane's last row with the draft
// `esc` kept. A box that has them already keeps its words.
func (a *app) factoryTLOpenBox(it factory.Item) {
	if !a.factory.Has("talk") || a.factoryBoxFocused() {
		return
	}
	tl := a.factoryTL(it)
	a.factoryOpenAsk(factoryAsk{kind: factoryAskManager, id: it.ID, label: a.linearMark(tokens.GlyphPromptChat, ">"), example: wordSayIt, text: tl.draft})
}

// factoryBoxFocused says whether the manager's box has the keys.
func (a *app) factoryBoxFocused() bool {
	ask := a.fp.act.ask
	return ask != nil && ask.kind == factoryAskManager
}

// factoryTimelineSend is `enter` in the manager's box. WITH A SAY DOOR THE
// WORDS ARE SENT IN PLACE ([factory.Seam.Say]): the page stays, the manager
// row says `manager is thinking`, and the chat the words made is brought
// into the center (factory_host.go). Without one, the item's own conversation opens the way `T`
// opens it ([app.factoryTalkSaying]), with the words typed in its box for
// the person to send there.
func (a *app) factoryTimelineSend(id int, words string) tea.Cmd {
	it, ok := a.factoryItemByID(id)
	if !ok || !a.factory.Has("talk") {
		return nil
	}
	if !a.factory.Has("say") {
		return a.factoryTalkSaying(it, words)
	}
	words = strings.TrimSpace(words)
	tl := &a.fp.tl
	if tl.pending == nil {
		tl.pending = map[int][]factoryTLPending{}
	}
	tl.pending[id] = append(tl.pending[id], factoryTLPending{words: words, from: a.factoryTLTalkEntries(id)})
	a.factoryManagerThinking(id, true)
	seam := a.factory
	return a.offLoop(func() func(bool) tea.Cmd {
		err := seam.Say(context.Background(), id, words)
		// THE FLOOR IS READ IN THE SAME ASK: the first words to an item with
		// no conversation made one, and the page learns its transcript here.
		var snap factory.Snapshot
		var lerr error
		if seam.Load != nil {
			snap, lerr = seam.Load()
		}
		return func(bool) tea.Cmd {
			switch {
			case lerr != nil:
				a.fp.err = lerr
			case seam.Load != nil:
				a.factoryFold(snap)
			}
			if err != nil {
				a.factoryManagerThinking(id, false)
				tl := &a.fp.tl
				if ps := tl.pending[id]; len(ps) > 0 {
					tl.pending[id] = ps[:len(ps)-1]
				}
				a.pageMsg = strings.TrimSpace(err.Error())
				a.touch()
				return nil
			}
			// AND THE CONVERSATION IS READ AT ONCE, not on the next beat.
			a.fp.tl.readAt = time.Time{}
			a.touch()
			return a.factoryTimelineWake()
		}
	})
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
