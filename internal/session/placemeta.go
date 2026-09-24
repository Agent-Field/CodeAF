package session

// placemeta.go keeps meta.json true while the conversation is happening.
//
// A session folder's identity file is what a picker reads instead of parsing
// every journal on the machine (place.go), and the two facts it exists to carry
// — WHEN THE PERSON LAST SPOKE and WHAT THE CONVERSATION IS CALLED — are only
// knowable from inside the running session. So this is where they are written,
// on the two lines that establish them: the person's own message, and the name
// the session gives itself.
//
// IT IS THE JOURNAL'S SEAM, NOT THE SURFACE'S. Every door — the terminal, the
// engine at the far end of an ssh pipe, a headless --once — records a user
// message through one place ([Agent.recordUserLocked]), and stamping there is
// what makes the resume order true whichever door was used. A surface-side
// stamp would have to be repeated in each of them, and the one that was
// forgotten would be a door whose conversations never come back.
//
// EVERY FAILURE IS SILENCE. meta.json is a citation and not the record: the id
// is the folder's name, the title and the last-active stamp are recoverable
// from the transcript, and there is nothing a person mid-sentence could do with
// the news that a lookup file could not be written. [LoadMeta] already answers
// a missing or corrupt file with a blank Meta, so a stamp over one is a stamp
// that rebuilds it.

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/offpath"
)

// metaTitleLimit is how much of the person's first line becomes the folder's
// working name. It is [taskLabelLimit]'s number for taskLabel's reason: this is
// what a picker row shows beside a glyph and an age, and a row is one line.
const metaTitleLimit = 56

// stampUserLocked records that the PERSON has just said something in this
// session. It runs with the agent lock held, from the one place a user message
// reaches the journal.
//
// The title it writes is a PLACEHOLDER — the person's own opening words — and
// only while there is none. The session starts naming itself alongside the answer
// (title.go), and [Agent.stampTitle] replaces this with that. Between the two
// the picker still has a row a person recognizes, which is the whole difference
// between a list of conversations and a list of ids.
// IT IS OWED HERE AND WRITTEN BEHIND THE PATH. The stamp used to be a whole
// [LoadMeta] and [SaveMeta] — a read, a MkdirAll, a CreateTemp, a write and a
// rename — performed with THE AGENT LOCK HELD, in the moment between a person
// pressing Enter and their turn starting. Nothing in that write is something the
// turn needs, and nothing that reads meta.json is running yet: a picker reads it
// when a person opens the home page, not while they are typing. So what happens
// on the path is the snapshot, and the disk is [offpath.Write]'s
// ([Agent.metaStamp]).
//
// The LAST STAMP WINS and that is correct rather than merely tolerable: what is
// being recorded is "the person last spoke just now", so a stamp overtaken by a
// newer one has nothing to say that the newer one does not say better.
func (a *Agent) stampUserLocked(text string) {
	snapshot := a.fillMetaLocked(Meta{})
	dir := a.config.Place.Dir
	at := time.Now()
	title := placeholderTitle(text)
	a.metaStamp().Owe(func() {
		a.updateMeta(dir, snapshot, func(meta *Meta) {
			meta.Model, meta.Effort, meta.Approval = snapshot.Model, snapshot.Effort, snapshot.Approval
			meta.Places, meta.Trees = snapshot.Places, snapshot.Trees
			meta.LastUserAt = at
			if strings.TrimSpace(meta.Title) == "" {
				meta.Title = title
			}
		})
	})
}

// metaStamp is the one deferred write this session owes its own lookup file.
//
// It is built on first use rather than in the constructor because an agent
// assembled by a test may never speak: a session that stamps nothing starts no
// goroutine and has nothing to settle.
func (a *Agent) metaStamp() *stampWriter {
	a.metaStampOnce.Do(func() { a.metaStampWriter = &stampWriter{} })
	return a.metaStampWriter
}

// SettleWrites waits until everything this session owes a file BEHIND a person's
// path has landed: the meta.json stamp, the delta reading's told.json stamp, the
// fix shelf's counters, and the working copy of any folder referred but not yet
// cut.
//
// IT IS THE ONE EXIT DOOR, and there is one rather than one per owner because a
// caller closing a session should not have to know which parts of it defer a
// write. A deferred write's whole risk is a process that stops while one is
// owed; this is the answer to that risk, and it belongs at [Agent.Close] and in
// every test that reads one of those files back.
//
// IT MUST NOT BE CALLED WITH a.mu HELD, and that is the whole of its contract.
// Every write it waits on takes that lock to read or replace what it is writing
// — the stamp fills a Meta, the cut appends to a.trees — so a call from inside
// the lock waits forever on work that is waiting for the caller. In [Agent.Close]
// the place is beside [Agent.settleDeliveries] (agent.go), BEFORE the
// `a.mu.Lock()` that follows it, and nowhere after.
func (a *Agent) SettleWrites() {
	if a == nil {
		return
	}
	a.metaStamp().settle()
	a.toldStamp().settle()
	a.fixShelfFor().settle()
	a.treesAhead().Settle()
	a.settleTeamEvents()
}

// stampWriter is [offpath.Write] with the patch it is to perform carried beside
// it, because what a stamp writes is decided on the path and performed off it.
//
// THE PATCH IS REPLACED AND NOT QUEUED, which is the coalescing rule
// [offpath.Write] states applied to this particular write: two user messages a
// second apart owe ONE meta.json, holding the later of the two stamps.
type stampWriter struct {
	mu    sync.Mutex
	patch func()
	write *offpath.Write
}

func (s *stampWriter) Owe(patch func()) {
	s.mu.Lock()
	s.patch = patch
	if s.write == nil {
		s.write = offpath.Deferred(s.perform)
	}
	write := s.write
	s.mu.Unlock()
	write.Owe()
}

func (s *stampWriter) perform() {
	s.mu.Lock()
	patch := s.patch
	s.mu.Unlock()
	if patch != nil {
		patch()
	}
}

func (s *stampWriter) settle() {
	s.mu.Lock()
	write := s.write
	s.mu.Unlock()
	write.Settle()
}

// placeholderTitle cuts the folder's working name out of what a person said:
// their first line, one space between the words, to the row's length. It is one
// function because it is asked twice — once when they say it, and once when a
// name that turned out not to be one is thrown away and the words have to come
// back ([openingPlaceholder]).
func placeholderTitle(text string) string {
	return clip(strings.Join(strings.Fields(firstLine(text)), " "), metaTitleLimit)
}

// openingPlaceholder reads the person's opening words back off the journal, for
// the session whose placeholder was overwritten by a name that was not one.
//
// IT IS PAID FOR ONLY BY THE FOLDERS THAT NEED IT. [LoadMeta] is asked about
// every session on the machine when home is drawn, and the whole point of
// meta.json is that answering does not mean opening a transcript — so this runs
// only where the stored title was the namer's own instruction ([healedTitle],
// title.go), and it stops at the first thing the person said rather than
// reading the file. The next message in that session stamps the words back onto
// meta.json and nobody reads a journal for it again.
func openingPlaceholder(dir string) string {
	file, err := os.Open(Place{Dir: dir}.Transcript())
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	// The buffer [Peek] takes, for its reason: one pasted file in an early line
	// would otherwise end the scan before the opening message is reached.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type != "message" || entry.Role != "user" {
			continue
		}
		// A MESSAGE WITH NO WORDS IN IT IS NOT A PLACEHOLDER. A picture and
		// nothing else is an ordinary opening message, and a row named after it
		// would be a blank row with a stamp on it.
		if text := strings.TrimSpace(entry.Content); text != "" {
			return placeholderTitle(text)
		}
	}
	return ""
}

// stampTitle records the earned name. Snapshot agent state first, then serialize
// its metadata patch without holding the lock a foreground turn is waiting on.
func (a *Agent) stampTitle(title string, _ ...string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	dir, snapshot := a.metaSnapshotAt()
	a.updateMeta(dir, snapshot, func(meta *Meta) {
		// Earned names keep the same guards as the journal they mirror. The
		// narrower metadata limit belongs only to the opening-message placeholder.
		meta.Title = clip(title, titleLimit)
		meta.ShortTitle = ""
	})
}

// metaSnapshot reads agent state before taking the metadata lock. The opposite
// order would deadlock stampUserLocked, which already holds the agent lock.
func (a *Agent) metaSnapshot() Meta {
	_, snapshot := a.metaSnapshotAt()
	return snapshot
}

// AnchorWorkspace can replace Place while a title arrives. Read its directory
// under the same lock as the remaining snapshot, even though anchoring keeps it.
func (a *Agent) metaSnapshotAt() (string, Meta) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.config.Place.Dir, a.fillMetaLocked(Meta{})
}

// updateMeta serializes the whole read-modify-write, not just the final rename,
// across every Agent and process that writes this conversation folder.
// Each patch owns its fields; a spend snapshot made before naming cannot write
// an old title back afterward. Snapshot collection must never run inside patch.
func (a *Agent) updateMeta(dir string, snapshot Meta, patch func(*Meta)) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	_ = withMetaLock(dir, func() error {
		meta, err := LoadMeta(dir)
		if err != nil {
			return err
		}
		// Seed identity when rebuilding a missing file, while retaining current disk
		// fields that another transaction may have updated since the snapshot.
		if meta.ID == "" {
			meta.ID, meta.Owned = snapshot.ID, snapshot.Owned
		}
		if meta.Workspace == "" {
			meta.Workspace = snapshot.Workspace
		}
		if meta.Created.IsZero() {
			meta.Created = snapshot.Created
		}
		if meta.Model == "" {
			meta.Model, meta.Effort, meta.Approval = snapshot.Model, snapshot.Effort, snapshot.Approval
		}
		if meta.Places == nil {
			meta.Places = snapshot.Places
		}
		if meta.Trees == nil {
			meta.Trees = snapshot.Trees
		}
		patch(&meta)
		return SaveMeta(dir, meta)
	})
}

// fillMetaLocked fills in everything about a session that does not change while
// it runs, so that a meta.json written by an older build — or lost entirely —
// comes back whole rather than half-filled. The agent lock is held for the
// model, which /model moves.
func (a *Agent) fillMetaLocked(meta Meta) Meta {
	if strings.TrimSpace(meta.ID) == "" {
		meta.ID = a.config.Place.ID()
	}
	if strings.TrimSpace(meta.Workspace) == "" {
		workspace := strings.TrimSpace(a.config.Place.Workspace)
		if workspace == "" {
			workspace = a.config.Workspace
		}
		meta.Workspace = workspace
	}
	meta.Owned = a.config.Place.Owned
	if meta.Created.IsZero() {
		meta.Created = time.Now()
	}
	if model := strings.TrimSpace(a.model); model != "" {
		meta.Model = model
	}
	// The rung is written whatever it is, absence included, because absence is
	// a value a person can choose their way back to: a session dialled to max
	// and then turned off again has to come back off rather than back at max.
	meta.Effort = a.effort.String()
	// And the gate's posture, written whatever it is for the same reason: auto
	// is a value a person chose their way back to, and a conversation opened
	// wide and then handed back has to come back handed back.
	meta.Approval = a.approvalPosture
	// Configuration stamps own the folders this conversation is about. Title
	// and spend patches preserve the latest stored set instead of replacing it
	// with a snapshot captured before a folder was named (places.go).
	meta.Places = a.places
	// And the working copies held on those folders, for the same reason and one
	// sharper: the places are an answer that could be worked out again, and this
	// is work that exists nowhere else (standingtree.go).
	meta.Trees = a.trees
	return meta
}

// stampEffort writes the conversation's rung down the moment it changes, rather
// than waiting for the next turn to seal.
//
// A DIAL IS NOT A COST. [Agent.stampSpend] rides the end of a turn because a
// turn is when a cost exists; a rung exists the instant somebody sets it, and a
// person who dials one and closes the terminal before saying anything else must
// find it there. It is the same read-and-rename of a few hundred bytes, and it
// happens once per deliberate act rather than once per turn.
//
// EVERY FAILURE IS SILENCE, for this file's stated reason: the rung is a
// convenience and the session is the record.
func (a *Agent) stampEffort() { a.stampMeta() }

// stampPlaces writes the folders this conversation is about down the moment the
// set changes, for [Agent.stampEffort]'s reason: a person who names a place and
// closes the terminal must find the conversation still about it, and a ground
// the work resolved is only worth keeping if the NEXT process finds it too
// (places.go).
//
// It is one write per deliberate change and never per turn — [Agent.refer]
// returns without calling this when the set already reads the way it would.
func (a *Agent) stampPlaces() { a.stampMeta() }

// stampTrees writes the working copies down the moment one is cut, one is
// written into, or one lands.
//
// IT IS THE ONE STAMP THAT CANNOT BE LOST. A rung and a set of folders are
// conveniences a next process could live without; a copy holding an hour of
// unlanded work that nothing wrote down is work the person cannot get back to
// (standingtree.go). It is still the same read-and-rename of a few hundred
// bytes, and it happens once per file first written rather than once per write.
func (a *Agent) stampTrees() { a.stampMeta() }

// stampMeta updates the configuration and working context those three stamps
// share. The transaction reads the latest title and spending from disk and
// preserves them; this snapshot owns only model, effort, approval, places and trees.
// EVERY FAILURE IS SILENCE, for this file's stated reason.
func (a *Agent) stampMeta() {
	dir, snapshot := a.metaSnapshotAt()
	a.updateMeta(dir, snapshot, func(meta *Meta) {
		meta.Model, meta.Effort, meta.Approval = snapshot.Model, snapshot.Effort, snapshot.Approval
		meta.Places, meta.Trees = snapshot.Places, snapshot.Trees
	})
}

// stampSpend records the conversation's running total — what the talking has
// cost so far, and what it weighed — on the session's own meta.json.
//
// IT RIDES THE END OF A TURN AND NOTHING ELSE. [Agent.sealTurn] is the one
// place a turn's cost reaches the journal, for the reason stated there: every
// turn shape in the package ends through it. So it is also the one place the
// total can be written down without a shape of turn silently keeping no record,
// and stamping anywhere else would be a second answer to "what has this cost".
//
// THE FIGURE IS THE SESSION'S OWN AND NEVER ITS TASKS'. [Agent.usage] is this
// agent's talking: its turns, and the auxiliary calls beside them (the namer,
// the guardian, a look at a picture). A task node runs on an agent of its own
// with no Place at all (task_run.go's newTaskAgent), so its spending reaches
// its own row of the project's index and cannot reach this file — which is
// exactly the separation home's spend band adds back up in one place
// (internal/tui3's homeFacts).
//
// A STANDING FIRING HAS A PLACE OF ITS OWN and so stamps its own run folder
// (standing_run.go's standingRunConfig). That is the right file for it: the
// folder lives under the standing store rather than under v3/projects, nothing
// home reads ever scans it, and what an item has spent is the ledger's answer
// (internal/standing) and not this one.
//
// ONE SMALL ATOMIC WRITE PER TURN. There is no other end-of-turn write to a
// session's meta.json to ride — [Agent.stampUserLocked] runs at the START of a
// turn, before the cost exists — so this is a read and a temp-and-rename of a
// file of a few hundred bytes, once per turn of a conversation, beside a
// provider call that took seconds. EVERY FAILURE IS SILENCE, for this file's
// stated reason: the total is a citation and the transcript is the record.
func (a *Agent) stampSpend() {
	a.mu.Lock()
	dir := a.config.Place.Dir
	snapshot := a.fillMetaLocked(Meta{})
	spent, tokens := a.usage.CostUSD, a.usage.Input+a.usage.Output
	a.mu.Unlock()
	if spent <= 0 && tokens <= 0 {
		return
	}
	a.writeSpendSnapshot(dir, snapshot, spent, tokens)
}

// stampRestoredSpend fills the total in for a conversation resumed from a
// journal written before this field existed.
//
// IT FOLDS NOTHING AND READS NOTHING EXTRA. The resume already replayed the
// file and already summed its usage lines ([sessionFile.RestoredUsage], which
// is what the agent's live counters are restored from — agent.go), so this
// takes that sum and writes it once. A session with nothing restored, or one
// whose meta already carries a figure, writes nothing at all: the stamp above
// keeps it true from here on, and a rewrite per open would be a file touched by
// every window that merely looked.
func (a *Agent) stampRestoredSpend(restored Usage) {
	dir, _ := a.metaSnapshotAt()
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	tokens := restored.Input + restored.Output
	if restored.CostUSD <= 0 && tokens <= 0 {
		return
	}
	meta, err := LoadMeta(dir)
	if err != nil || meta.SpentUSD > 0 || meta.Tokens > 0 {
		return
	}
	a.writeSpend(dir, restored.CostUSD, tokens)
}

// writeSpend is the write both stamps share, so the two can never disagree
// about which fields a total is.
func (a *Agent) writeSpend(dir string, spent float64, tokens int) {
	if spent <= 0 && tokens <= 0 {
		return
	}
	a.writeSpendSnapshot(dir, a.metaSnapshot(), spent, tokens)
}

// The snapshot can predate a title commit; only the spend belongs to this patch.
func (a *Agent) writeSpendSnapshot(dir string, snapshot Meta, spent float64, tokens int) {
	a.updateMeta(dir, snapshot, func(meta *Meta) {
		meta.SpentUSD, meta.Tokens = spent, tokens
	})
}
