package tui3

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// AN UNSENT LINE OUTLIVES THE WINDOW, AND IT COMES BACK TO THE READER IT WAS
// WRITTEN FOR.
//
// draft.go states the older half of this law — the half-written message belongs
// to the person — and it kept exactly one sentence: the conversation's, in a
// plain file. Everything else the composer holds died with the process: the
// correction half-typed at task 7, the document behind `[paste 1 · 42 lines]`,
// the screenshot on the tray.
//
// ── ONE RECORD, AND A PLAIN FILE BESIDE IT ──
//
//	draft-<hash>-<n>-<pid>.json   THE RECORD. Every recipient's composer: its
//	                              words, its caret, the documents behind its
//	                              compact tokens, its tray and its uncertain
//	                              messages. It is what a restore reads.
//	draft-<hash>-<n>-<pid>.txt    the conversation's words, and nothing else. It
//	                              is an EXPORT: a build that has never heard of
//	                              the record reads and writes it exactly as it
//	                              always did, and a person who finds one in a
//	                              directory can read their own sentence in it.
//
// The record is authoritative and is written FIRST, atomically, so it is always
// a whole snapshot of one moment. A crash between the two leaves the plain file
// one save behind, which costs nothing here — a restore that finds a record does
// not read it — and keeps an older build's reading honest.
//
// THE PLAIN FILE IS READ ONLY WHERE NO RECORD SPEAKS FOR THIS CONVERSATION: an
// older window's leftovers, or an orphan adopted from a dead window on this
// directory ([app.legacyDraftText]). Those words come over ALONE — no documents,
// no tray, no task pages — because a record beside a stranger's sentence belongs
// to a stranger's conversation.
//
// AND A RECORD THAT SPEAKS FOR THIS CONVERSATION AND SAYS NOTHING ABOUT THE BOX
// IS SAYING THE BOX IS EMPTY. That is why an empty record is kept rather than
// deleted ([draftKeep.forgettable]) and why the fallback above is not reached
// once one has been found: a crash between the record and the export leaves the
// export holding the sentence the record has just spent, and believing it hands
// back a message the person watched leave.
//
// ── WHOSE LINE IS IT ──
//
// The conversation's sentence follows the DIRECTORY, as it always has: an orphan
// left by a dead window is adopted by the next window opened there (draft.go's
// [adoptDraft]).
//
// EVERYTHING IN THE RECORD FOLLOWS THE CONVERSATION AND NOTHING ELSE. `task 7`
// names a node inside one graph; the same three characters in the next
// conversation are a different piece of work. So every slot carries its OWNER —
// the machine, the project and the transcript ([draftOwnerOf]) — and a slot is
// laid out only where that matches exactly. A slot belonging to somebody else,
// or to nobody this build can name, is left exactly as it was found: never
// shown, never deleted, and picked up by the window that opens that conversation
// again ([app.reuniteDrafts]).
//
// ── NOTHING IS LOST QUIETLY ──
//
// There is no ceiling and no truncation: what the person typed is what is
// written. A write that fails says so ([draftKeepFailWord]) rather than
// pretending. A record this build cannot read is moved aside before anything
// replaces it, never overwritten. A restored draft naming a file that has since
// gone keeps naming it, and the surface says which one.

// draftKeepVersion is the record's shape.
const draftKeepVersion = 1

// draftKeep is one window's composer, written down.
type draftKeep struct {
	Version int `json:"version"`

	// Owner is the conversation this window wrote the record for ([draftOwnerOf]).
	// It is a convenience for a person reading the file; what a restore trusts is
	// the owner on each slot, because a record CARRIES SLOTS IT DOES NOT OWN.
	Owner string `json:"owner,omitempty"`

	// At is when it was written, RFC 3339, and it settles which of two records
	// holding the same page's line is the newer one.
	At string `json:"at,omitempty"`

	// Slots are the composers, one per recipient, the conversation's own among
	// them (task 0 and no run).
	Slots []draftKeepSlot `json:"slots,omitempty"`
}

// draftKeepSlot is one recipient's composer.
type draftKeepSlot struct {
	// Owner is whose composer this is, and it is on the SLOT rather than only on
	// the record: a window that finds another conversation's lines under the name
	// it was given writes them straight back down, and they would otherwise be
	// re-stamped with the carrier's own conversation.
	Owner string `json:"owner,omitempty"`

	// Task and Run name the reader; both zero is the conversation itself.
	Task uint64 `json:"task,omitempty"`

	// Run is the adaptive run's own id (roomorch.go).
	Run string `json:"run,omitempty"`

	// Guest is the other conversation a read-only page belongs to
	// (recipient.go's [guestRecipient]), and empty is a page of this one.
	Guest string `json:"guest,omitempty"`

	// Text is the sentence.
	Text string `json:"text,omitempty"`

	// Caret is where the person was in it, as a RUNE index — which is what the
	// editor counts in (input.go).
	Caret int `json:"caret,omitempty"`

	// Pastes are the documents the compact tokens in Text stand for.
	Pastes []draftKeepPaste `json:"pastes,omitempty"`

	// Chips are the tray above the box.
	Chips []draftKeepChip `json:"chips,omitempty"`

	// Sends is every message handed to a crossing that has not settled, and the
	// one this surface was given back to offer again. It is HERE rather than in a
	// store of its own because it is the same fact as the text above it — words
	// the person typed that have not landed anywhere (see [outboxSnapshot]).
	Sends []draftKeepSend `json:"sends,omitempty"`
}

// draftKeepPaste is one compact token's document.
type draftKeepPaste struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

// draftKeepChip is one thing on the tray. THE PATH AND NOT THE BYTES: an
// attachment names a file on this machine, the file is read at the moment enter
// is pressed (attach.go), and copying sixteen megabytes into a draft file every
// time somebody pauses typing would be this surface making a second copy of a
// file it does not own. A chip whose file has gone by the time the window opens
// again is restored anyway and NAMED ([app.noteRestoreTrouble]): the person
// meant to send it, and dropping it quietly would send a different message from
// the one they wrote.
type draftKeepChip struct {
	Path string `json:"path"`
	File bool   `json:"file,omitempty"`
}

// draftKeepSend is one uncertain message, written down under its own durable
// name so that a repeat after a restart is the SAME message rather than a second
// one. The scope and the sequence are [session.SteerSource]'s two halves; this
// package writes them down and never mints them.
type draftKeepSend struct {
	// Scope and Seq are the message's durable name.
	Scope string `json:"scope,omitempty"`

	// Seq counts a scope's sends from one.
	Seq uint64 `json:"seq,omitempty"`

	// At is when it was handed over, RFC 3339.
	At string `json:"at,omitempty"`

	// State is what was true of it when this was written ([draftSendCrossing],
	// [draftSendUnanswered], [draftSendUndelivered], [draftSendRefused]). It is a
	// storage word and is never drawn.
	State string `json:"state,omitempty"`

	// Line is the sentence as the person typed it, compact tags and all.
	Line string `json:"line"`

	// Caret is where they were in it, as a RUNE index.
	Caret int `json:"caret,omitempty"`

	// Words is the sentence as the far side reads it, tags unfolded.
	Words string `json:"words,omitempty"`

	// Pastes are the documents behind Line's tags.
	Pastes []draftKeepPaste `json:"pastes,omitempty"`

	// Chips is what the message was carrying.
	Chips []draftKeepChip `json:"chips,omitempty"`
}

// draftKeepFile is one record and where it lives — what a reunion took slots
// out of, and what a prune is about to put back.
type draftKeepFile struct {
	path string
	keep draftKeep
	// export is the plain file beside it, when this reunion is what makes that
	// file stale ([app.reuniteDrafts]). Empty leaves it alone.
	export string
}

// ── who a record belongs to ─────────────────────────────────────────────────

// draftOwnerOf is one conversation's durable identity: the machine, the project
// and the transcript.
//
// THE TRANSCRIPT PATH ALONE IS NOT AN IDENTITY. A session opened over a
// connection names a file on the far machine (host.go), and two machines
// mounting the same path mean two different conversations; so does the same
// relative journal reached through two projects. Restoring task 7 across that
// gap is the misdelivery this wave exists to end, one connection wider.
func draftOwnerOf(host, workspace, file string) string {
	if strings.TrimSpace(file) == "" {
		return ""
	}
	return strings.TrimSpace(host) + "\x00" + filepath.Clean(workspace) + "\x00" + filepath.Clean(file)
}

// draftUnnamedOwner marks an identity that is a WINDOW and not a conversation.
const draftUnnamedOwner = "\x00window\x00"

// draftWindowOwnerOf is the identity of a conversation with no transcript to be
// named after: the draft file, which is this window's and this conversation's
// and lives exactly as long as the words do.
//
// EVERY PRODUCTION DOOR NAMES A TRANSCRIPT — cmd/aforge sets `SessionFile` and
// `DraftFile` in the same breath, and the --host welcome carries the engine's own
// path — so this is the seam and not the road. It exists because the surface
// ACCEPTS an empty one, and a launch that took it would otherwise lose the
// documents and the tray of a box the person had already filled.
//
// ONLY MAIN IS WRITTEN UNDER IT ([app.draftKeepBuild]). A task id means something
// inside one graph, and a window that cannot name its conversation cannot promise
// the next one that its `task 7` is the same task 7.
func draftWindowOwnerOf(host, draftFile string) string {
	if strings.TrimSpace(draftFile) == "" {
		return ""
	}
	return strings.TrimSpace(host) + draftUnnamedOwner + filepath.Clean(draftFile)
}

// draftKeepOwner is this surface's own: its conversation where there is one, and
// its window otherwise.
func (a *app) draftKeepOwner() string {
	if owner := draftOwnerOf(a.host, a.workspace, a.file); owner != "" {
		return owner
	}
	return draftWindowOwnerOf(a.host, a.draftFile)
}

// namedConversation reports whether an identity names a conversation rather than
// a window ([draftWindowOwnerOf] says what turns on it).
func namedConversation(owner string) bool {
	return owner != "" && !strings.Contains(owner, draftUnnamedOwner)
}

// ownsSlot reports whether one slot is this conversation's. An UNSTAMPED slot
// belongs to nobody this build can name and is never laid out.
func (a *app) ownsSlot(slot draftKeepSlot) bool {
	owner := a.draftKeepOwner()
	return owner != "" && slot.Owner == owner
}

// speaksFor reports whether a record has anything to say about THIS
// conversation: it was written for it, or it is carrying one of its composers.
//
// IT IS WHAT MAKES AN EMPTY BOX AN ANSWER. A record that speaks for this
// conversation and holds no sentence for the box says the box is empty — which
// is a fact, and is exactly what the plain export beside it cannot be trusted to
// say after a crash between the two writes ([app.layKeptDrafts]).
func (a *app) speaksFor(keep draftKeep) bool {
	owner := a.draftKeepOwner()
	if owner == "" {
		return false
	}
	if keep.Owner == owner {
		return true
	}
	for _, slot := range keep.Slots {
		if slot.Owner == owner {
			return true
		}
	}
	return false
}

// ── the record on disk ──────────────────────────────────────────────────────

// draftKeepPath is the record beside one draft file: the same name with another
// extension, which is what keeps the pair together through every door draft.go
// already has — the glob that finds an orphan matches `*.txt` and never this,
// and [draftWindowAlive] reads the pid out of either.
func draftKeepPath(draftFile string) string {
	if draftFile == "" {
		return ""
	}
	return strings.TrimSuffix(draftFile, filepath.Ext(draftFile)) + ".json"
}

// draftKeepRead is what was found under a record's name.
type draftKeepRead int

const (
	// draftKeepNone is nothing there — an older window, or a fresh directory.
	draftKeepNone draftKeepRead = iota
	// draftKeepFound is a record this build understands.
	draftKeepFound
	// draftKeepStrange is a file that is there and cannot be read: half written,
	// hand edited, or a shape from a later build. It is somebody's unsent words,
	// so it is neither read nor destroyed ([keepStrangeAside]).
	draftKeepStrange
)

func readDraftKeep(path string) (draftKeep, draftKeepRead) {
	if path == "" {
		return draftKeep{}, draftKeepNone
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return draftKeep{}, draftKeepNone
		}
		return draftKeep{}, draftKeepStrange
	}
	var keep draftKeep
	if err := json.Unmarshal(raw, &keep); err != nil {
		return draftKeep{}, draftKeepStrange
	}
	if keep.Version > draftKeepVersion {
		return draftKeep{}, draftKeepStrange
	}
	return keep, draftKeepFound
}

// keepStrangeAside moves a record this build cannot read out of the way, under a
// name in the same directory, and REFUSES THE WRITE if it cannot: replacing a
// file whose contents are unknown is deleting words on the strength of not
// understanding them.
func keepStrangeAside(path string) error {
	if _, how := readDraftKeep(path); how != draftKeepStrange {
		return nil
	}
	return os.Rename(path, path+".unreadable-"+strconv.FormatInt(time.Now().UnixNano(), 10))
}

// putDraftKeep replaces one record, or removes it when nothing anybody typed is
// left in it. THE WRITE IS A RENAME, so a reader never sees half a record.
//
// Callers hold that record's writer ([draftWriter]) — which is what makes the
// temporary name safe within this process — and the name carries this window's
// pid because two windows can prune the same dead record at once.
func putDraftKeep(path string, keep draftKeep) error {
	if path == "" {
		return nil
	}
	if err := keepStrangeAside(path); err != nil {
		return err
	}
	if keep.forgettable() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	keep.Version = draftKeepVersion
	raw, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	temp := path + ".writing-" + strconv.Itoa(draftOwner)
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// forgettable reports whether a record can simply be deleted: it holds nothing
// anybody typed AND it speaks for nobody.
//
// A RECORD WITH AN OWNER AND NO SENTENCE IS A TOMBSTONE, AND A TOMBSTONE IS
// KEPT. It is what says "this conversation's box is empty", and that is a fact
// the plain export beside it cannot be trusted to say: the record is written
// first, so a crash between the two leaves an export still holding the sentence
// that was just sent. Deleting the tombstone would let those words come back at
// the next launch — and the launch after a REUNION, in a window with a name of
// its own, where the export is not even the one being read
// ([app.reuniteDrafts]).
//
// It is removed by exactly one thing: a conversation closed for real
// ([commitDropped]), which is the moment there is nothing left to be wrong.
func (k draftKeep) forgettable() bool { return k.Owner == "" && k.empty() }

// empty reports whether a record holds nothing anybody typed. A slot this window
// is only CARRYING counts: it is somebody's sentence, and the file it is in is
// the only place it exists.
func (k draftKeep) empty() bool {
	for _, slot := range k.Slots {
		if !slot.empty() {
			return false
		}
	}
	return true
}

func (s draftKeepSlot) empty() bool {
	return strings.TrimSpace(s.Text) == "" && len(s.Pastes) == 0 &&
		len(s.Chips) == 0 && len(s.Sends) == 0
}

// ── writing, in order ───────────────────────────────────────────────────────

// draftRevs numbers every save this process starts, and the number is taken ON
// THE EVENT LOOP where the state is read.
var draftRevs atomic.Uint64

// draftWrites orders the writes and remembers what actually landed under each
// name.
//
// THE DEBOUNCE HANDS THE WRITE TO A COMMAND, AND COMMANDS DO NOT ARRIVE IN
// ORDER. Two saves of one conversation are two goroutines racing a disk, so a
// save built before a submit could land after it and put the sent sentence back
// on disk — a message the person watched leave, restored into their box at the
// next launch. An atomic rename cannot help with that: both writes are whole,
// and the loser is simply older. So the ORDER is kept here, by the number the
// loop gave each save, and a save that has been overtaken does nothing at all.
//
// ── TWO LOCKS, AND THE EVENT LOOP ONLY EVER TAKES THE SHORT ONE ──
//
// `mu` guards these maps and NOTHING ELSE: it is taken for a few map operations
// and is never held across a disk write. That is what makes the promise in
// steersend.go true — the keyboard does not wait on a disk. A save built while
// another is being written claims its number, retires the older one and returns
// to the loop immediately; the writing happens on the command's own goroutine.
//
// `writers` is one lock PER RECORD, and it serialises the IO itself. It has to
// exist: [putDraftKeep] writes through one temporary name per process, so two
// goroutines writing the same record at once would write the same temporary file
// and one of them would rename a half-written one into place.
var draftWrites = struct {
	mu      sync.Mutex
	at      map[string]uint64
	done    map[string]draftDone
	writers map[string]*sync.Mutex
}{at: map[string]uint64{}, done: map[string]draftDone{}, writers: map[string]*sync.Mutex{}}

// draftDone is the newest record that reached the disk under one name, and it is
// the ONLY proof a send may cross on (steersend.go's [app.sendsKeptAt]).
//
// A NUMBER ALONE IS NOT PROOF. "Some save at or after mine landed" is true of a
// save that CLEARED the record — a box emptied, a conversation closed, a reunion
// pruning what it did not take — and releasing a send on that would be sending
// words that are on no disk anywhere. So what landed is remembered by the durable
// NAMES it contained, and a send asks about its own.
type draftDone struct {
	rev   uint64
	sends map[string]bool
}

// errDraftSuperseded is a save that did nothing because a newer one was claimed
// for the same record. IT IS NOT SUCCESS AND IT IS NOT FAILURE, and it is an
// error value rather than a nil so that no caller can read a skipped write as
// proof that anything reached the disk.
var errDraftSuperseded = errors.New("a newer draft save was claimed for this record")

// draftKeptSend reports whether one message, by its durable name, is in the
// record that is on disk under this name right now — and in a record at or after
// the save that was built to carry it.
//
// BOTH HALVES ARE THE PROOF. The name says these exact words survived; the number
// says the record holding them is not one this window has since superseded with a
// save whose own fate is still unknown.
func draftKeptSend(path string, rev uint64, name string) bool {
	if path == "" || name == "" {
		return false
	}
	draftWrites.mu.Lock()
	defer draftWrites.mu.Unlock()
	landed := draftWrites.done[path]
	return landed.rev >= rev && landed.sends[name]
}

// draftSendName is one message's durable name — [session.SteerSource]'s two
// halves, spelled once. A message with no scope has no name and can never be
// proven to be anywhere.
func draftSendName(scope string, seq uint64) string {
	if strings.TrimSpace(scope) == "" || seq == 0 {
		return ""
	}
	return scope + "\x00" + strconv.FormatUint(seq, 10)
}

// draftSendsIn is every message a record carries, by name. It is what a landing
// is remembered as.
func draftSendsIn(keep draftKeep) map[string]bool {
	var out map[string]bool
	for _, slot := range keep.Slots {
		for _, send := range slot.Sends {
			name := draftSendName(send.Scope, send.Seq)
			if name == "" {
				continue
			}
			if out == nil {
				out = map[string]bool{}
			}
			out[name] = true
		}
	}
	return out
}

// draftSave is one save of one conversation: the record, and the plain-text
// export beside it.
type draftSave struct {
	keepPath string
	textPath string
	keep     draftKeep
	text     string
	rev      uint64
}

// claimDraftRev takes the next number for one name AND retires every save
// already built for it.
//
// THE CLAIM IS THE INTENT AND NOT THE OUTCOME. It is called where the state is
// read — on the event loop — because the thing that makes an older save wrong is
// that the person has since typed, sent or cleared, not that a newer write
// happened to reach the disk. Claiming on success instead left one hole: a newer
// save that FAILED did not retire anything, so a queued older one could land
// afterwards and make words the person had already spent look current.
func claimDraftRev(path string) uint64 {
	rev := draftRevs.Add(1)
	if path == "" {
		return rev
	}
	draftWrites.mu.Lock()
	defer draftWrites.mu.Unlock()
	if rev > draftWrites.at[path] {
		draftWrites.at[path] = rev
	}
	return rev
}

// commit writes it, unless a newer save has been claimed under this name.
//
// THE RECORD GOES FIRST AND THE EXPORT SECOND (this file's header), and a
// failure to write the record stops the export: the two would otherwise
// disagree, with the plain file claiming words the record has never heard of.
//
// A FAILURE LEAVES THE LAST GOOD RECORD WHERE IT IS. Nothing is removed on the
// way in, so what is on disk after a failed save is the newest save that worked.
func (s draftSave) commit() error {
	if s.keepPath == "" {
		return nil
	}
	if draftOvertaken(s.keepPath, s.rev) {
		return errDraftSuperseded
	}
	// ONE WRITER PER RECORD, AND THE LOOP IS NOT ONE OF THEM. Waiting happens
	// here, on this command's own goroutine; the surface is already back at the
	// keyboard.
	writer := draftWriter(s.keepPath)
	writer.Lock()
	defer writer.Unlock()
	// AND THE QUESTION IS ASKED AGAIN AFTER THE WAIT. A save claimed while this
	// one queued for the disk is newer than it, and writing now would put the
	// older record back over the newer one.
	if draftOvertaken(s.keepPath, s.rev) {
		return errDraftSuperseded
	}
	if err := putDraftKeep(s.keepPath, s.keep); err != nil {
		return err
	}
	// THE RECORD IS THE PROMISE AND THE EXPORT IS A CONVENIENCE, so what is
	// remembered as landed is the record and what it carried: a send whose
	// snapshot is in it is on disk whatever the plain file beside it says, and an
	// export that fails afterwards does not make it less written down.
	draftDidLand(s.keepPath, s.rev, s.keep)
	if s.textPath == "" {
		return nil
	}
	return writeDraft(s.textPath, s.text)
}

// draftOvertaken reports whether a newer save has been claimed for this record.
func draftOvertaken(path string, rev uint64) bool {
	draftWrites.mu.Lock()
	defer draftWrites.mu.Unlock()
	return draftWrites.at[path] > rev
}

// draftWriter is the one lock that serialises writes to one record.
func draftWriter(path string) *sync.Mutex {
	draftWrites.mu.Lock()
	defer draftWrites.mu.Unlock()
	writer := draftWrites.writers[path]
	if writer == nil {
		writer = &sync.Mutex{}
		draftWrites.writers[path] = writer
	}
	return writer
}

// draftDidLand remembers what is on disk under this name: the number of the save
// that put it there, and the messages it carries.
//
// AN OLDER LANDING NEVER OVERWRITES A NEWER ONE, which cannot happen while one
// writer holds the record — and is stated here anyway, because this is the fact
// every waiting send is released on.
func draftDidLand(path string, rev uint64, keep draftKeep) {
	draftWrites.mu.Lock()
	defer draftWrites.mu.Unlock()
	if draftWrites.done[path].rev > rev {
		return
	}
	draftWrites.done[path] = draftDone{rev: rev, sends: draftSendsIn(keep)}
}

// commitKeep writes one record on its own — a reunion putting back what it did
// not take, and nothing else.
func commitKeep(path string, keep draftKeep) error {
	return draftSave{keepPath: path, keep: keep, rev: claimDraftRev(path)}.commit()
}

// commitDropped is a conversation being closed for real (draft.go's
// [dropDraftFile]): its drafts go, its uncertain crossings keep their names,
// and everything carried for another conversation stays exactly as it was.
func commitDropped(path string) error {
	keepPath := draftKeepPath(path)
	left := draftKeep{}
	if keep, how := readDraftKeep(keepPath); how == draftKeepFound {
		for _, slot := range keep.Slots {
			if keep.Owner == "" || slot.Owner != keep.Owner {
				left.Slots = append(left.Slots, slot)
				continue
			}
			// A close discards drafts, never proof of an unanswered crossing.
			// Retain only those sends, under their original recipient and name.
			var uncertain []draftKeepSend
			for _, send := range slot.Sends {
				if send.State == draftSendCrossing || send.State == draftSendUnanswered {
					send.State = draftSendUnanswered
					uncertain = append(uncertain, send)
				}
			}
			if len(uncertain) > 0 {
				left.Slots = append(left.Slots, draftKeepSlot{
					Owner: slot.Owner, Task: slot.Task, Run: slot.Run, Guest: slot.Guest, Sends: uncertain,
				})
			}
		}
		// The record no longer owns a live composer. Each retained slot still
		// names its conversation, including unanswered sends from this close.
		// With no such slots, there is nothing left to keep.
		if len(left.Slots) > 0 {
			left.At = keep.At
		}
	}
	return draftSave{keepPath: keepPath, textPath: path, keep: left, rev: claimDraftRev(keepPath)}.commit()
}

// draftKeptMsg is one whole-record save that has finished, on its way back to
// the loop: which record it was for, which number it was claimed under, and what
// happened to it.
//
// IT IS ONE MESSAGE FOR ALL THREE OUTCOMES because a send waiting to cross needs
// all three (steersend.go): a save that landed is what makes the record on disk
// answerable ([draftKeptSend] asks it by name), a save that failed is what
// refuses a send it was carrying, and a save that was superseded is neither — the
// newer one may still land, and its own message answers for both.
type draftKeptMsg struct {
	path string
	rev  uint64
	err  error
}

// draftKeepFailWord is what the conversation says when the draft could not be
// written. IT IS SAID RATHER THAN SWALLOWED: everything else on this surface
// treats the draft as safe once it has been typed, and a disk that is full or
// read-only makes that false without anybody finding out until the words are
// gone.
const draftKeepFailWord = "this draft could not be saved"

func (a *app) noteDraftKeepFailed(err error) {
	if err == nil || errors.Is(err, errDraftSuperseded) {
		return
	}
	a.note(draftKeepFailWord + " · " + strings.TrimSpace(err.Error()))
}

// draftKept is what the loop does with a finished save: say so if it failed, and
// tell the sending lane which sends may now cross ([app.sendsKeptAt]).
func (a *app) draftKept(msg draftKeptMsg) tea.Cmd {
	a.noteDraftKeepFailed(msg.err)
	return a.sendsKeptAt(msg.path, msg.rev, msg.err)
}

// ── between the record and the composer ─────────────────────────────────────

func keptPastes(pastes []pasteChip) []draftKeepPaste {
	if len(pastes) == 0 {
		return nil
	}
	out := make([]draftKeepPaste, 0, len(pastes))
	for _, held := range pastes {
		out = append(out, draftKeepPaste{N: held.n, Text: held.text})
	}
	return out
}

func livePastes(kept []draftKeepPaste) []pasteChip {
	if len(kept) == 0 {
		return nil
	}
	out := make([]pasteChip, 0, len(kept))
	for _, held := range kept {
		out = append(out, pasteChip{n: held.N, text: held.Text})
	}
	return out
}

func keptChips(chips []chip) []draftKeepChip {
	if len(chips) == 0 {
		return nil
	}
	out := make([]draftKeepChip, 0, len(chips))
	for _, held := range chips {
		out = append(out, draftKeepChip{Path: held.path, File: held.file})
	}
	return out
}

// liveChips is the tray as it comes back, WHOLE. A file that has gone since is
// still on it and is named at the door ([app.noteRestoreTrouble]) — see
// [draftKeepChip] for why it is not quietly dropped.
func liveChips(kept []draftKeepChip) []chip {
	if len(kept) == 0 {
		return nil
	}
	out := make([]chip, 0, len(kept))
	for _, held := range kept {
		out = append(out, chip{path: held.Path, file: held.File})
	}
	return out
}

func keptSends(sends []outboxSnapshot) []draftKeepSend {
	if len(sends) == 0 {
		return nil
	}
	out := make([]draftKeepSend, 0, len(sends))
	for _, send := range sends {
		kept := draftKeepSend{
			Scope: send.scope, Seq: send.seq, State: send.state,
			Line: send.line, Caret: send.caret, Words: send.words,
			Pastes: keptPastes(send.pastes), Chips: keptChips(send.chips),
		}
		if !send.at.IsZero() {
			kept.At = send.at.Format(time.RFC3339Nano)
		}
		out = append(out, kept)
	}
	return out
}

func liveSends(kept []draftKeepSend) []outboxSnapshot {
	if len(kept) == 0 {
		return nil
	}
	out := make([]outboxSnapshot, 0, len(kept))
	for _, held := range kept {
		snap := outboxSnapshot{
			scope: held.Scope, seq: held.Seq, state: held.State,
			line: held.Line, caret: held.Caret, words: held.Words,
			pastes: livePastes(held.Pastes), chips: liveChips(held.Chips),
		}
		if held.At != "" {
			if at, err := time.Parse(time.RFC3339Nano, held.At); err == nil {
				snap.at = at
			}
		}
		out = append(out, snap)
	}
	return out
}

// keptSlot writes one recipient's composer down.
func keptSlot(who recipient, state composerState) draftKeepSlot {
	return draftKeepSlot{
		Task: who.task, Run: who.run, Guest: who.guest,
		Text: state.box.String(), Caret: state.box.cursor,
		Pastes: keptPastes(state.pastes),
		Chips:  keptChips(state.chips),
		Sends:  keptSends(state.sends),
	}
}

// composer reads one back, EXACTLY AS IT WAS WRITTEN. The record is one whole
// snapshot, so a tag in the text always has its document beside it here; the one
// place a tag can arrive alone is a plain file with no record
// ([app.legacyDraftText]), and there the words are still the person's and are
// left alone.
func (s draftKeepSlot) composer() (recipient, composerState) {
	state := composerState{
		box:    editor{value: []rune(s.Text)},
		pastes: livePastes(s.Pastes),
		chips:  liveChips(s.Chips),
		sends:  liveSends(s.Sends),
	}
	state.box.cursor = max(0, min(s.Caret, len(state.box.value)))
	return recipient{task: s.Task, run: s.Run, guest: s.Guest}, state
}

// orphanTokens are the compact paste tags in a sentence with no document behind
// them. NOTHING IS REMOVED FOR THEM — the words are the person's — and the
// surface says so instead ([app.noteRestoreTrouble]).
func orphanTokens(text string, pastes []pasteChip) []string {
	if text == "" || !strings.Contains(text, pasteTokenHead) {
		return nil
	}
	held := make(map[string]bool, len(pastes))
	for _, paste := range pastes {
		held[pasteToken(paste.n, pasteLineCount(paste.text))] = true
	}
	var out []string
	for {
		at := strings.Index(text, pasteTokenHead)
		if at < 0 {
			return out
		}
		end := strings.Index(text[at:], "]")
		if end < 0 {
			return out
		}
		end += at + 1
		if token := text[at:end]; !held[token] {
			out = append(out, token)
		}
		text = text[end:]
	}
}

// draftOrphanSendWord is what enter says when the box holds a compact tag whose
// document is gone. THE WORDS STAY IN THE BOX — all of them — because both other
// answers are wrong: unfolding sends `[paste 1 · 42 lines]` to a model as though
// those were the words, and dropping the tag sends a different message from the
// one on the screen.
const draftOrphanSendWord = "a pasted block could not be restored · the words were not sent"

// missingPaste is the first compact tag in a line whose document is gone, or the
// empty string.
//
// IT IS ASKED AT ENTER AND ON EVERY SEND ROAD, because a restore is the only
// thing that can produce this state and a person meets it at the moment they
// press the key — which is where the refusal is useful and where nothing has been
// spent yet. The startup notice ([app.noteRestoreTrouble]) is the warning; the
// refusals at the two send doors are the stop, each said in the voice of the page
// the person is standing on.
func (a *app) missingPaste(line string) string {
	gone := orphanTokens(line, a.pastes)
	if len(gone) == 0 {
		return ""
	}
	return gone[0]
}

// ── the window's own record ─────────────────────────────────────────────────

// draftKeepBuild assembles this window's record: every recipient's composer, and
// the slots this window is only carrying for somebody else
// ([app.readKeptElsewhere]).
//
// THE CONVERSATION'S WORDS ARE PASSED IN because they are not always the box:
// the door out of the program writes the draft with every parked message folded
// in under it (quitarm.go's [app.leavingDraft]), and submit passes the empty
// string because the sentence has gone to the model.
func (a *app) draftKeepBuild(text string) draftKeep {
	a.readKeptElsewhere()
	owner := a.draftKeepOwner()
	keep := draftKeep{
		Version: draftKeepVersion,
		Owner:   owner,
		At:      a.now().UTC().Format(time.RFC3339Nano),
	}
	if owner != "" {
		main := keptSlot(mainRecipient, a.mainComposer())
		main.Owner, main.Text = owner, text
		main.Caret = max(0, min(main.Caret, len([]rune(text))))
		if !main.empty() {
			keep.Slots = append(keep.Slots, main)
		}
	}
	// AND A WINDOW THAT CANNOT NAME ITS CONVERSATION WRITES ITS BOX AND NOTHING
	// ELSE ([draftWindowOwnerOf]). Main follows the window and the directory, as
	// the plain file always has; a page's line follows a graph this window cannot
	// name, and a slot the next conversation might answer to is the misdelivery
	// this file exists to end.
	if namedConversation(owner) {
		for who, state := range a.everyComposer() {
			if who == mainRecipient {
				continue
			}
			slot := keptSlot(who, state)
			slot.Owner = owner
			if !slot.empty() {
				keep.Slots = append(keep.Slots, slot)
			}
		}
	}
	// AND WHAT THIS WINDOW IS ONLY CARRYING GOES BACK DOWN UNTOUCHED. It is
	// another conversation's, this window cannot show it to anybody, and dropping
	// it because it is in the way would be deleting somebody's unsent words.
	keep.Slots = append(keep.Slots, a.keptElsewhere...)
	sortKeptSlots(keep.Slots)
	return keep
}

// readKeptElsewhere looks, ONCE PER CONVERSATION, at what is already under the
// name this conversation's record is about to be written to.
//
// IT EXISTS BECAUSE A NAME COMES BACK AROUND. The record is named after the
// window's process id (draft.go's [DraftFile]), the operating system hands those
// out again, and a conversation opened in a process given a dead window's number
// would otherwise write straight over that window's record. So anything in there
// for another conversation is picked up and written back down untouched, and the
// window that opens that conversation finds it ([app.reuniteDrafts]).
func (a *app) readKeptElsewhere() {
	if a.keptFrom == a.draftFile {
		return
	}
	a.keptFrom = a.draftFile
	a.keptElsewhere = nil
	if a.draftFile == "" {
		return
	}
	prior, how := readDraftKeep(draftKeepPath(a.draftFile))
	if how != draftKeepFound {
		return
	}
	for _, slot := range prior.Slots {
		if !a.ownsSlot(slot) {
			a.keptElsewhere = append(a.keptElsewhere, slot)
		}
	}
}

// sortKeptSlots puts the slots in one order, so that two writes of the same
// state produce the same bytes — a map's own order is not one.
func sortKeptSlots(slots []draftKeepSlot) {
	for i := 1; i < len(slots); i++ {
		for j := i; j > 0 && keptSlotBefore(slots[j], slots[j-1]); j-- {
			slots[j], slots[j-1] = slots[j-1], slots[j]
		}
	}
}

func keptSlotBefore(a, b draftKeepSlot) bool {
	if a.Owner != b.Owner {
		return a.Owner < b.Owner
	}
	if a.Guest != b.Guest {
		return a.Guest < b.Guest
	}
	if a.Task != b.Task {
		return a.Task < b.Task
	}
	return a.Run < b.Run
}

// everyComposer is every recipient this window holds a composer for, the live
// box included. It is the reading [app.draftKeepBuild] and [app.composersAside]
// both need, and having one of them is what stops the two drifting.
func (a *app) everyComposer() map[recipient]composerState {
	out := make(map[recipient]composerState, len(a.composers)+1)
	for who, state := range a.composers {
		out[who] = state
	}
	if live := a.liveComposer(); !live.empty() {
		out[a.composerOwner] = live
	} else {
		delete(out, a.composerOwner)
	}
	// AND THE NEW-CHAT START PAGE'S BOX IS IN NEITHER READING (chatstart.go). It
	// is addressed to no conversation — that is the whole of what the page is for
	// — so there is no identity to write it down under: [app.composersAside]
	// would hand a person's first sentence to the conversation they were leaving,
	// and [keptSlot] spells a recipient as its task, its run and its guest, so a
	// slot minted here would come back off the disk as MAIN and overwrite the
	// sentence that conversation really is holding.
	//
	// SO IT IS NOT KEPT ACROSS A CRASH, and that is the honest state rather than
	// a gap: a start page parked with `esc` keeps its words on the surface for as
	// long as this window lives ([app.startKept]), and a window that dies takes
	// them with it. The conversation's own box, and every task page's line, are
	// written down exactly as they were.
	delete(out, startRecipient)
	return out
}

// draftSaveOf is this window's whole composer, ready to be written. It is built
// ON THE LOOP — the state is read here and the number that orders it is taken
// here — so that whichever goroutine eventually writes it, the saves land in the
// order the person made them ([draftWrites]).
func (a *app) draftSaveOf(text string) draftSave {
	return draftSave{
		keepPath: draftKeepPath(a.draftFile),
		textPath: a.draftFile,
		keep:     a.draftKeepBuild(text),
		text:     text,
		rev:      claimDraftRev(draftKeepPath(a.draftFile)),
	}
}

// keepDrafts is the whole write, off the event loop.
//
// IT ALWAYS ANSWERS. The debounce used to report only its failures; a send that
// may not cross until its words are on disk needs to hear about the successes
// too, and the same message carries both ([draftKeptMsg]).
func (a *app) keepDrafts() tea.Cmd {
	if a.draftFile == "" {
		return nil
	}
	return a.draftSaveOf(a.mainDraftText()).command()
}

// command hands one save to a goroutine and reports what became of it.
func (s draftSave) command() tea.Cmd {
	return func() tea.Msg {
		return draftKeptMsg{path: s.keepPath, rev: s.rev, err: s.commit()}
	}
}

// stowDrafts is the crash insurance for a conversation this window is HOLDING
// AND NOT DRAWING (keeper.go's [app.stow]): its own words with the parked
// messages folded in, and its pages' lines, written under ITS identity rather
// than the identity of the conversation that is now in front.
func (a *app) stowDrafts(conv Conversation, side *aside) {
	if conv.DraftFile == "" || side == nil {
		return
	}
	path := draftKeepPath(conv.DraftFile)
	owner := draftOwnerOf(a.host, conv.Workspace, conv.SessionFile)
	if owner == "" {
		owner = draftWindowOwnerOf(a.host, conv.DraftFile)
	}
	keep := draftKeep{
		Version: draftKeepVersion,
		Owner:   owner,
		At:      a.now().UTC().Format(time.RFC3339Nano),
	}
	if owner != "" {
		main := keptSlot(mainRecipient, composerState{
			box:    side.mainBox(),
			pastes: side.pastes,
			chips:  side.chips,
			sends:  side.sends,
		})
		main.Owner = owner
		if !main.empty() {
			keep.Slots = append(keep.Slots, main)
		}
	}
	if namedConversation(owner) {
		for who, state := range side.composers {
			if who == mainRecipient {
				continue
			}
			slot := keptSlot(who, state)
			slot.Owner = owner
			if !slot.empty() {
				keep.Slots = append(keep.Slots, slot)
			}
		}
	}
	// WHAT THAT FILE WAS CARRYING FOR SOMEBODY ELSE STAYS IN IT. The conversation
	// being stowed read it once while it was in front ([app.readKeptElsewhere]);
	// this is the same reading, done again because the surface has since let go of
	// everything belonging to it.
	if prior, how := readDraftKeep(path); how == draftKeepFound {
		for _, slot := range prior.Slots {
			if owner == "" || slot.Owner != owner {
				keep.Slots = append(keep.Slots, slot)
			}
		}
	}
	sortKeptSlots(keep.Slots)
	save := draftSave{
		keepPath: path,
		textPath: conv.DraftFile,
		keep:     keep,
		text:     side.draft,
		rev:      claimDraftRev(path),
	}
	if err := save.commit(); err != nil {
		a.noteDraftKeepFailed(err)
	}
}

// ── reading it back ─────────────────────────────────────────────────────────

// layKeptDrafts is startup: every recipient's composer back where it was, with
// the conversation's own in the box.
//
// NOTHING IS TAKEN OUT OF ANOTHER RECORD UNTIL THIS WINDOW'S OWN HAS BEEN
// WRITTEN. A reunion MOVES a line — that is what stops the next window laying
// the same one out again — and a move whose second half fails is a deletion. So
// the order is: read, lay out, write ours, and only then prune what we took.
//
// AND A RECORD THAT SPEAKS FOR THIS CONVERSATION IS THE WHOLE ANSWER, including
// the parts of it that say nothing. A record with no sentence for the box means
// THE BOX IS EMPTY; falling through to the plain file there is how a sentence
// that was sent came back, because the export is written after the record and a
// crash between the two leaves it holding the words the record has just spent.
func (a *app) layKeptDrafts() {
	states := map[recipient]composerState{}
	a.keptFrom, a.keptElsewhere = a.draftFile, nil

	// This window's own record. Even this one is asked whose each slot is: the
	// pid in the name comes back around when the operating system reuses it.
	settled := false
	if keep, how := readDraftKeep(draftKeepPath(a.draftFile)); how == draftKeepFound {
		a.keptElsewhere, _ = a.takeKeptSlots(keep, states)
		settled = a.speaksFor(keep)
		// A shared connection reuses this window's store across selections.
		// Its plain export belongs to the record's owner; falling back to it
		// would put the outgoing chat's unsent text into an empty incoming chat.
		if a.shared && namedConversation(keep.Owner) {
			settled = true
		}
	}

	// AND WHAT THIS CONVERSATION LEFT IN OTHER WINDOWS COMES HOME.
	prune, spoken := a.reuniteDrafts(states)
	settled = settled || spoken

	// Only then the plain file, which is all an older build or a stranger's
	// orphan can offer ([app.legacyDraftText]).
	if _, held := states[mainRecipient]; !held && !settled {
		if text := a.legacyDraftText(); text != "" {
			runes := []rune(text)
			states[mainRecipient] = composerState{box: editor{value: runes, cursor: len(runes)}}
		}
	}

	if state, held := states[mainRecipient]; held {
		a.putComposer(state)
		delete(states, mainRecipient)
	}
	for who, state := range states {
		a.keepComposer(who, state)
	}

	if err := a.draftSaveOf(a.mainDraftText()).commit(); err != nil {
		// The words are on the screen and this window is holding them; what failed
		// is the promise that they would outlive it. Nothing is pruned — every line
		// stays where it was found.
		a.noteDraftKeepFailed(err)
		return
	}
	for _, gone := range prune {
		if err := gone.prune(); err != nil {
			a.noteDraftKeepFailed(err)
		}
	}
	a.noteRestoreTrouble()
}

// legacyDraftText is the plain file: this window's own, or the orphan a dead
// window left on this directory (draft.go's [adoptDraft]).
//
// THE WORDS COME OVER ALONE. A record beside an adopted sentence belongs to
// another conversation, and attaching its documents, its tray or its task pages
// to this one would be migrating a recipient because two directories matched.
func (a *app) legacyDraftText() string {
	text := readDraft(a.draftFile)
	if text == "" && draftFirstHere(a.draftFile, a.workspace) {
		text = adoptDraft(a.draftFile, a.workspace)
	}
	return text
}

// takeKeptSlots folds THIS CONVERSATION's slots out of one record into what is
// being assembled, and hands back every slot it did not take so the caller can
// put them down again exactly as they were found.
//
// A composer already assembled is never overwritten and never dropped: the first
// reading wins — this window's own record, then the newest dead one — and the
// older copy stays in the file it is in.
func (a *app) takeKeptSlots(keep draftKeep, into map[recipient]composerState) (left []draftKeepSlot, took bool) {
	for _, slot := range keep.Slots {
		if !a.ownsSlot(slot) {
			left = append(left, slot)
			continue
		}
		who, state := slot.composer()
		if state.empty() {
			took = true
			continue
		}
		if _, already := into[who]; already {
			left = append(left, slot)
			continue
		}
		into[who] = state
		took = true
	}
	return left, took
}

// reuniteDrafts is how a composer survives a restart into a window that is not
// the one it was typed in, and it hands back the records to prune once this
// window's own has been written.
//
// A CONVERSATION IS OPENED BY WHICHEVER WINDOW OPENS IT, and that window has a
// draft file of its own — a new ordinal, a new pid — so the record holding
// yesterday's corrections is under a name this window would otherwise never
// read. It looks for them by OWNER: every record in this directory whose window
// is gone and whose slots name this conversation.
//
// A LIVE WINDOW'S RECORD IS NEVER TOUCHED, which is [adoptDraft]'s own guarantee
// restated: better to leave a sentence on disk for the window that owns it than
// to take one out from under somebody who is typing.
//
// ONLY THE NEWEST RECORD THAT SPEAKS FOR THIS CONVERSATION IS READ, and the
// second answer it gives is the important one: whether this conversation has
// been spoken for at all. A window that cleared its box left a record saying so
// ([draftKeep.forgettable]), and reading an OLDER record underneath it would hand
// back the sentence that clearing spent. Older records are left exactly where
// they are — never read for this conversation again, never deleted.
func (a *app) reuniteDrafts(into map[recipient]composerState) ([]draftKeepFile, bool) {
	if a.draftKeepOwner() == "" || a.draftFile == "" || strings.TrimSpace(a.workspace) == "" {
		return nil, false
	}
	own := draftKeepPath(a.draftFile)
	directory := filepath.Dir(a.draftFile)
	candidates, err := filepath.Glob(filepath.Join(directory, draftPrefix(a.workspace)+"*.json"))
	if err != nil {
		return nil, false
	}
	var speakers []draftKeepFile
	for _, path := range candidates {
		if path == own || draftWindowAlive(path) {
			continue
		}
		// EVERY DEAD RECORD IS ASKED, not only the ones whose own conversation is
		// this one: a window that was carrying somebody else's lines wrote them back
		// down under ITS name, so the answer is per slot ([app.speaksFor]).
		keep, how := readDraftKeep(path)
		if how != draftKeepFound || !a.speaksFor(keep) {
			continue
		}
		speakers = append(speakers, draftKeepFile{path: path, keep: keep})
	}
	if len(speakers) == 0 {
		return nil, false
	}
	sortKeptNewestFirst(speakers)
	newest := speakers[0]
	left, took := a.takeKeptSlots(newest.keep, into)
	// THE EXPORT BESIDE IT IS STALE BY DEFINITION once this record has been read:
	// the record is that window's whole answer, so its plain file is either the
	// same words — now held here — or the ones a crash left behind after they were
	// spent. It is cleared with the prune, and only where that window was holding
	// THIS conversation: a carrier's export is somebody else's sentence.
	if newest.keep.Owner == a.draftKeepOwner() {
		if stale := draftExportPath(newest.path); readDraft(stale) != "" {
			newest.export = stale
		}
	}
	if !took && newest.export == "" {
		return nil, true
	}
	newest.keep.Slots = left
	return []draftKeepFile{newest}, true
}

// draftExportPath is the plain file beside one record — [draftKeepPath] read the
// other way round.
func draftExportPath(keepPath string) string {
	if keepPath == "" {
		return ""
	}
	return strings.TrimSuffix(keepPath, filepath.Ext(keepPath)) + ".txt"
}

// prune puts a record back with what was taken out of it removed, and clears the
// export beside it. It is [draftSave] like every other write, so it takes its
// place in the order under that name.
//
// THE TEXT IS EMPTY BECAUSE THE WORDS ARE HERE NOW. A record this window has read
// has handed its conversation over; anything of its own left in it belongs to
// somebody else, and the export never did.
func (f draftKeepFile) prune() error {
	return draftSave{
		keepPath: f.path,
		textPath: f.export,
		keep:     f.keep,
		rev:      claimDraftRev(f.path),
	}.commit()
}

// sortKeptNewestFirst puts the newest record first, so that two dead windows
// holding the same page's line hand over the LATER one — the stamp is written
// the same way every time, so comparing the strings compares the moments.
func sortKeptNewestFirst(records []draftKeepFile) {
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && records[j].keep.At > records[j-1].keep.At; j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}
}

// noteRestoreTrouble says what a restored draft is missing, once, at the door.
//
// IT NAMES RATHER THAN REPAIRS. A file that has gone since it was attached and a
// compact tag whose document never made it are both the person's own intent, and
// the surface's job is to hand it back and say what is wrong with it — not to
// send a different message from the one they wrote.
func (a *app) noteRestoreTrouble() {
	var gone, tags []string
	for _, state := range a.everyComposer() {
		for _, held := range state.chips {
			if _, err := os.Stat(held.path); err != nil && !heldName(gone, held.name()) {
				gone = append(gone, held.name())
			}
		}
		tags = append(tags, orphanTokens(state.box.String(), state.pastes)...)
	}
	// The composers are walked as a map, so the sentence is put in one order —
	// the same restore must not say two different things.
	sort.Strings(gone)
	sort.Strings(tags)
	if len(gone) > 0 {
		a.note(draftGoneWord + " · " + strings.Join(gone, ", "))
	}
	if len(tags) > 0 {
		a.note(draftLostPasteWord + " · " + tags[0])
	}
}

func heldName(names []string, name string) bool {
	for _, held := range names {
		if held == name {
			return true
		}
	}
	return false
}

// draftGoneWord is a restored tray naming a file this machine no longer has. The
// chip stays: the person put it there, and `enter` names it again if they send
// it anyway (attach.go's [readAttachments]).
const draftGoneWord = "a restored draft still names a file that is gone"

// draftLostPasteWord is a compact tag with no document behind it, which only a
// plain file written by a build that kept none can leave.
const draftLostPasteWord = "a pasted block could not be restored · its tag is still in the draft"
