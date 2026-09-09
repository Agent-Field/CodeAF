package tui3

// CHOOSING, AND THE ONE PLACE IT HAPPENS.
//
// folderpick.go browses. This file is the other half of the owner's law that
// NAVIGATING AND CHOOSING ARE TWO DIFFERENT ACTS: moving the cursor, opening a
// row, turning the wheel and reading a preview change nothing about the
// conversation, and the only gesture that does is the action row — `enter`, or a
// click on the row that says in words what it is about to do.
//
// ── WHAT A CONFIRM CAN BE ───────────────────────────────────────────────────
//
// Three things, and they are told apart by the row under the cursor and by
// whether anything has been MARKED (alt+m):
//
//   - A FOLDER goes to the conversation, through the one seam every road onto a
//     directory comes out of ([app.referPlace]'s door): it is registered as a
//     scoped reference, it appears on the tray above the box, and the next
//     model request carries it. Its contents are never pasted anywhere.
//   - A FILE goes onto the MESSAGE, through the tray attach.go already owns: a
//     picture travels as content and everything else travels as a path the
//     `read` tool can open. Nothing about the conversation changes.
//   - SEVERAL OF EITHER, MIXED, in the order they were marked — which is the
//     order the pictures are then numbered in, because `[image #2]` means the
//     second picture and a tray built in another order would renumber somebody's
//     sentence under them.
//
// ── AND ALL OF ITS WORK RUNS OFF THE LOOP ───────────────────────────────────
//
// Registering a folder is a write on a local engine and a ROUND TRIP over a
// connection; attaching a file is a stat. Neither may happen under Update: a
// keystroke may not wait for a disk any more than it may wait for git
// (homeband_repo.go states that law, folderpick.go's header repeats it, and the
// synchronous version of this function was the last place in this surface still
// breaking it).
//
// So a confirm fires ONE command carrying everything it was asked to do, and
// the answer comes back as [folderTakenMsg]. Two things guard it:
//
//   - THE CONVERSATION IT WAS FOR. The message carries the session file that was
//     current when the person pressed enter. A `/new` in the gap means the
//     folders reached the conversation they were chosen in — the door was
//     captured in the command — and the SENTENCE about them is dropped rather
//     than printed into a conversation that never gained them.
//   - THE OPENING THAT ASKED. The picker's generation is carried for the same
//     reason every read on this surface carries it: what comes back has to be
//     told apart from what a second opening asked for.
//
// What this file CANNOT do is abort a call already inside a syscall or already
// on the wire — Go has no interruptible `Stat`, and `ReferPlace` takes no
// context. The bound that exists is real and is stated rather than dressed up:
// the batch is capped at [folderMarkCap] things, every step re-reads the app's
// own context before starting, and internal/remote's own call deadline bounds
// the wire. The terminal keeps drawing and keeps taking keys throughout.

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// folderMarkCap is how many things one confirm may carry. Twenty-four is far
// more than anybody marks by hand and it is the bound that keeps a confirm a
// bounded piece of work rather than an unbounded one: a person who selected a
// directory of four hundred files did not mean to put four hundred paths on one
// message, and the tray would refuse most of them afterwards anyway
// ([maxAttachedTotalBytes]).
const folderMarkCap = 24

// folderMarkFullWord is what the mark key says once the cap is reached. It
// names the number, because "too many" is not something a person can act on.
const folderMarkFullWord = "24 is as many as one message can carry · send these first"

// folderMark is one row a person has marked for the confirm: the absolute path,
// and which kind of thing it is. The kind is recorded at MARK time rather than
// looked up at confirm time, because the row it came from may have scrolled
// away, been filtered out, or been in another directory entirely.
type folderMark struct {
	path string
	dir  bool
}

// marked reports whether a path is already marked.
func (f *folderPick) marked(path string) bool { return f.markAt(path) >= 0 }

// markAt is where a path sits among the marks, or -1.
func (f *folderPick) markAt(path string) int {
	for at, mark := range f.marks {
		if mark.path == path {
			return at
		}
	}
	return -1
}

// mark toggles the row under the cursor, and answers the sentence to say — ""
// when there is nothing to say, which is the ordinary case.
//
// THE ORDER IS THE ORDER THEY WERE MARKED IN and marks are appended rather than
// sorted, for the reason this file's header gives about `[image #2]`.
func (f *folderPick) mark() string {
	path, dir, ok := f.hereKind()
	if !ok {
		return ""
	}
	if at := f.markAt(path); at >= 0 {
		f.marks = append(f.marks[:at], f.marks[at+1:]...)
		return ""
	}
	if len(f.marks) >= folderMarkCap {
		return folderMarkFullWord
	}
	f.marks = append(f.marks, folderMark{path: path, dir: dir})
	return ""
}

// hereKind is the row under the cursor as a path AND a kind. It is
// [folderPick.here] with the one extra fact every decision on this sheet now
// needs, and the two are one function so they cannot answer about different
// rows.
func (f *folderPick) hereKind() (string, bool, bool) {
	if !f.open {
		return "", false, false
	}
	if !f.browsing {
		// THE CANDIDATE LIST IS DIRECTORIES AND ONLY DIRECTORIES. Every layer it
		// is built from is a layer of places — the conversation's own, the
		// projects home knows, the index under `~` — so a row there is always a
		// folder, and the files only exist once somebody is standing in one.
		if f.cursor < 0 || f.cursor >= len(f.hits) {
			return "", false, false
		}
		return f.all[f.hits[f.cursor]].path, true, true
	}
	at := f.cols.cursor
	if at >= 0 && at < f.cols.here.rows() {
		return filepath.Join(f.cols.dir, f.cols.here.rowName(at)), f.cols.here.isDir(at), true
	}
	// A DIRECTORY WITH NOTHING IN IT IS STILL A CHOICE (folderpick.go says why
	// at length): somebody who has walked into a leaf has arrived.
	if f.cols.dir != "" {
		return f.cols.dir, true, true
	}
	return "", false, false
}

// folderTake is one thing a confirm is about to do.
type folderTake struct {
	path string
	dir  bool
	// drop is a folder the conversation ALREADY holds, coming off. It is the
	// action row's other verb and it is decided where the row is drawn, so what
	// the row says and what the confirm does cannot drift.
	drop bool
}

// takes is what this confirm would do, in order: the marks, or the one row
// under the cursor when nothing is marked.
//
// A MARKED FOLDER THE CONVERSATION ALREADY HOLDS IS LEFT ALONE rather than
// taken off. Removing is a tidy-up gesture on one row at a time (the action row
// says `remove this folder` and this file's [app.folderConfirm] keeps the sheet
// open for it); a mixed confirm that silently un-attached one of the things a
// person had just marked would be the surface doing the opposite of what the
// mark meant.
func (f *folderPick) takes() []folderTake {
	if len(f.marks) > 0 {
		out := make([]folderTake, 0, len(f.marks))
		for _, mark := range f.marks {
			if mark.dir && f.held[mark.path] {
				continue
			}
			out = append(out, folderTake{path: mark.path, dir: mark.dir})
		}
		return out
	}
	path, dir, ok := f.hereKind()
	if !ok {
		return nil
	}
	return []folderTake{{path: path, dir: dir, drop: dir && f.held[path]}}
}

// folderTakenMsg is one confirm's whole answer, coming BACK. See this file's
// header for the two things that guard it.
type folderTakenMsg struct {
	// file is the session journal that was current when the person confirmed,
	// which is this surface's own name for "which conversation was this for".
	file string
	gen  int
	// chips are the files, ready for the tray, in the order they were marked.
	chips []chip
	// added are the folders that actually reached the conversation.
	added []folderAdded
	// notes are the honest failures, one sentence each, naming the thing.
	notes []string
}

// folderAdded is one folder that reached the conversation: the root the engine
// answered with, and the directory the person actually pointed at, which differ
// exactly when somebody chose a subdirectory of a repository.
type folderAdded struct {
	path  string
	chose string
}

// folderConfirm is THE ACTION ROW — `enter`, and the click on the row that says
// what it would do. It is one function because the key and the row must not be
// able to drift into meaning two different things.
func (a *app) folderConfirm() tea.Cmd {
	takes := a.folder.takes()
	if len(takes) == 0 {
		a.folder.close()
		a.touch()
		return nil
	}
	// A REMOVAL LEAVES THE SHEET OPEN and everything else closes it. Adding is a
	// decision and the sheet has served its purpose; removing is a tidy-up, and
	// the list you are tidying is the one in front of you.
	if len(takes) == 1 && takes[0].drop {
		cmd, ok := a.dropPlace(takes[0].path)
		if ok {
			a.touch()
			return cmd
		}
		return nil
	}
	// AND A FOLDER CANNOT BE OFFERED WHERE THE CONVERSATION CANNOT TAKE ONE. The
	// browser opens for files either way ([app.openContextPick]), so this is the
	// one place the two intents genuinely part: [app.placeRefusal] gives every
	// road the same sentence, said with the person's marks still in their hands
	// rather than after the sheet has gone.
	if word := a.placeRefusal(); word != "" {
		for _, take := range takes {
			if take.dir {
				a.note(word)
				a.touch()
				return nil
			}
		}
	}
	a.folder.close()
	a.touch()
	return a.folderTakeCmd(takes)
}

// folderTakeCmd is the whole of the confirm's work, off the loop.
//
// EVERYTHING IT NEEDS IS CAPTURED HERE AND NOTHING IS READ FROM THE APP INSIDE
// IT. The door, the tilde, the hosted-ness and the session file are read on the
// loop where they are stable; the goroutine touches a disk and a wire and
// nothing else. That is the same bargain [app.askFolderKids] makes and it is
// what makes this safe to run beside a turn.
func (a *app) folderTakeCmd(takes []folderTake) tea.Cmd {
	if len(takes) > folderMarkCap {
		takes = takes[:folderMarkCap]
	}
	door, _ := a.placeDoor()
	ctx, hosted := a.ctx, a.hosted()
	file, gen, tilde := a.file, a.folder.gen, a.tilde
	return func() tea.Msg {
		out := folderTakenMsg{file: file, gen: gen}
		for _, take := range takes {
			// THE APP'S OWN CONTEXT IS RE-READ BEFORE EVERY STEP. A window closing
			// while a batch is part way through should stop asking a disk for more,
			// and this is the only cancellation a blocking syscall admits.
			if ctx != nil && ctx.Err() != nil {
				return out
			}
			if take.dir {
				out = folderTakeDir(out, door, take.path, tilde, hosted)
				continue
			}
			out = folderTakeFile(out, take.path, hosted)
		}
		return out
	}
}

// folderTakeDir registers one directory, or says why it could not be.
func folderTakeDir(out folderTakenMsg, door placeReferrer, path, tilde string, hosted bool) folderTakenMsg {
	if hosted {
		out.notes = append(out.notes, folderRemoteWord)
		return out
	}
	if door == nil {
		out.notes = append(out.notes, folderNoDoorWord)
		return out
	}
	// THE STAT IS HERE AND NOT ON THE KEYSTROKE. The candidate layers are built
	// from what was said and remembered rather than from a walk, so a row can
	// name a directory that has since been moved — and handing that path on as a
	// place would be this surface passing its own staleness downstream.
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		out.notes = append(out.notes, folderGoneWord+tildePath(path, tilde))
		return out
	}
	// The refusal IS the line when it comes, and no pick is kept for a place the
	// conversation did not gain ([app.referPlace] states this).
	ref, err := door.ReferPlace(path, session.PlaceSaid)
	if err != nil {
		out.notes = append(out.notes, err.Error())
		return out
	}
	chose := path
	if scope := placeScope(ref); scope != "" {
		chose = scope
	}
	out.added = append(out.added, folderAdded{path: ref.Path, chose: chose})
	return out
}

// folderTakeFile stats one file and turns it into a chip, or says why not.
//
// A PICTURE IS A PICTURE WHICHEVER ROW IT CAME OFF (attach.go's law about
// /attach handed a PNG): it goes on the tray as content to be looked at, and
// everything else goes on as a path the `read` tool can open.
func folderTakeFile(out folderTakenMsg, path string, hosted bool) folderTakenMsg {
	info, err := os.Stat(path)
	if err != nil {
		out.notes = append(out.notes, "no such file: "+filepath.Base(path))
		return out
	}
	if info.IsDir() {
		// The row said file and the disk says folder, which means it changed under
		// the cursor. Saying so is more use than silently doing the other thing.
		out.notes = append(out.notes, folderTurnedWord+filepath.Base(path))
		return out
	}
	if isImagePath(path) {
		out.chips = append(out.chips, chip{path: path})
		return out
	}
	// THE CEILING IS ONLY A CEILING WHERE THE BYTES CROSS A CONNECTION
	// (attach.go's header): a local session is about to be handed a path to a
	// file it can already open.
	if hosted && info.Size() > maxAttachedFileBytes {
		out.notes = append(out.notes, oversizeFile(filepath.Base(path), info.Size()))
		return out
	}
	out.chips = append(out.chips, chip{path: path, file: true})
	return out
}

// folderTurnedWord is a row that said file and a disk that says folder.
const folderTurnedWord = "this is a folder now, not a file · "

// tookFolderTaken files a confirm's answer.
//
// THE FILES GO ON WHATEVER THE PERSON IS STANDING IN FRONT OF, through the one
// door that knows which composer that is ([app.atMainComposer], recipient.go):
// a task page opened in the gap owns the box, and handing the chips to whatever
// tray is on screen would attach a conversation's files to a message being
// written to a worker.
//
// THE SENTENCE ABOUT THE FOLDERS IS DROPPED ON A CONVERSATION THAT MOVED. See
// this file's header: the folders reached the conversation they were chosen in,
// and a line claiming them in another one would be a claim about the wrong
// place. The FILES are not dropped, because the tray has always travelled with
// the person and not with the conversation (app.go's /new).
func (a *app) tookFolderTaken(msg folderTakenMsg) {
	if len(msg.chips) > 0 {
		a.atMainComposer(func(state *composerState) {
			for _, held := range msg.chips {
				attachChipTo(&state.chips, held)
			}
		})
	}
	if msg.file != a.file {
		a.touch()
		return
	}
	for _, note := range msg.notes {
		a.noteFacts(note)
	}
	for _, added := range msg.added {
		a.placeChosen = added.path
		a.keepFolderPick(added.path)
		shown := tildePath(added.path, a.tilde)
		line := folderChoseWord + shown
		if added.chose != added.path {
			line += folderInsideWord + tildePath(added.chose, a.tilde)
		}
		a.noteFacts(line, shown)
	}
	// AND THE FILES ARE SAID TOO, because a chip appearing on a row above the box
	// is easy to miss when four of them appeared at once and the person was
	// looking at the browser. One line, naming what the message now carries.
	if len(msg.chips) > 0 {
		a.noteFacts(folderAttachedWord + folderChipWords(msg.chips))
	}
	a.markFolderHeld()
	a.touch()
}

// folderAttachedWord leads the one line a confirmed file selection says.
const folderAttachedWord = "attached · "

// folderChipWords names what went on the tray, by base name, in order.
func folderChipWords(chips []chip) string {
	names := make([]string, 0, len(chips))
	for _, held := range chips {
		names = append(names, held.name())
	}
	return strings.Join(names, " · ")
}
