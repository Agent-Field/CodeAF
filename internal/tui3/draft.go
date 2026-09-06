package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

// THE DRAFT IS THE PERSON'S, NOT THE SESSION'S.
//
// That one sentence decides everything in this file. The half-written message
// in the box belongs to whoever typed it, so it survives a closed window, a
// crash, a /new, and a session file that has moved on without it — a draft
// older than the session's last message is STILL restored, because the person
// who typed it did not stop meaning it when a turn finished. It is keyed by
// directory rather than by session for the same reason: the sentence was about
// this project.
//
// It is cleared on submit, and only on submit.
//
// ── AND IT BELONGS TO ONE BOX ──
//
// A draft is the text in a window's input, and two windows open on one project
// are two inputs. Keyed by directory alone they were one file: each window's
// debounce overwrote the other's half-sentence, whichever quit last decided what
// survived, a submit in either deleted what the other was still typing, and both
// restored the leftover at startup — the person's own words appearing in a
// window they had never typed them into. So the name carries the WINDOW as well
// as the directory, and the window is this process.
//
// What that costs is the restore, and paying it is [adoptDraft] below: a
// window's own file is gone the next time aforge starts, because the pid is. A
// draft whose window is dead is an ORPHAN, and an orphan is the person's
// sentence with nobody holding it — so the next window opened on that directory
// takes it over. A file whose pid is still running is never touched, which is
// the one guarantee that matters here: it is better to leave a sentence on disk
// for the next window than to lift it out of a live one.

// draftOwner is what this process's drafts are named after. A pid is unique
// among the processes alive at one moment, which is exactly the property the
// name needs — two windows open at once cannot share it — and its reuse after a
// window dies is harmless, because a name that comes back around is a name whose
// original holder is gone and whose file was an orphan anyway.
var draftOwner = os.Getpid()

// draftDebounce is how long the box has to be still before the draft is
// written. A file write per keystroke would be a syscall per character to save
// a sentence nobody has finished; three hundred milliseconds is the pause
// between words.
const draftDebounce = 300 * time.Millisecond

// draftSaveMsg is the debounce firing, carrying THE FILE IT WAS ARMED FOR.
//
// THE NAME IS ON THE MESSAGE BECAUSE THE SURFACE CAN HAVE MOVED. A person who
// types half a sentence and switches to another project inside the three
// hundred milliseconds of the debounce would otherwise have this tick land on a
// surface whose box holds the OTHER conversation's words and whose draftFile
// names the other conversation's file — one sentence written under a name that
// does not belong to it, and the real one lost. The conversation being left
// writes its own box on the way out (switcher.go), so a tick that no longer
// matches has nothing left to do.
type draftSaveMsg struct{ file string }

// draftOrdinals is how many drafts this process has named on each workspace,
// keyed by the canonical directory.
//
// IT EXISTS BECAUSE ONE TERMINAL CAN HOLD TWO CONVERSATIONS IN ONE PROJECT.
// The name used to be the workspace and the pid, which is unique among the
// processes alive at one moment and was therefore enough while a process held
// one conversation per directory. /new twice in one repository now makes two,
// and both would have written the same file: one box saved under the other's
// name, then whichever wrote last winning, then a launch restoring one sentence
// where there had been two.
var draftOrdinals = struct {
	mu sync.Mutex
	n  map[string]int
}{n: map[string]int{}}

// DraftFile names the draft file for A NEW CONVERSATION on one workspace, under
// dir. The name carries a hash of the path rather than the path itself, because
// a directory name can be longer than a file name may be — and the person never
// has to find this file, unlike a session transcript.
//
// IT IS NOT A PURE FUNCTION AND MUST NOT BE CALLED TWICE FOR ONE CONVERSATION:
// each call takes the next ordinal for that workspace, which is what stops two
// conversations of this process colliding. The door calls it exactly once, in
// the same breath as it builds the agent (cmd/aforge's chatv3_process.go).
//
// THE PID STAYS THE LAST TOKEN, which is not a style choice:
// [draftWindowAlive] parses it out of the name to decide whether the window
// that left an orphan is gone, and it reads the token after the final dash.
func DraftFile(dir, workspace string) string {
	if dir == "" {
		return ""
	}
	key := filepath.Clean(workspace)
	draftOrdinals.mu.Lock()
	ordinal := draftOrdinals.n[key]
	draftOrdinals.n[key] = ordinal + 1
	draftOrdinals.mu.Unlock()
	return filepath.Join(dir,
		draftPrefix(workspace)+strconv.Itoa(ordinal)+"-"+strconv.Itoa(draftOwner)+".txt")
}

// draftPrefix is everything in the name before the window: the part two windows
// on one directory share, and the glob an orphan hunt uses.
func draftPrefix(workspace string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(workspace)))
	return "draft-" + hex.EncodeToString(sum[:8]) + "-"
}

// edited is what every draft mutation returns: the two typed overlays follow
// what is in the box, and the debounce is armed.
//
// IT DOES NOT TOUCH THE TRANSCRIPT, and that absence is the point. [app.touch]
// throws away the laid-out screen list ([app.visible]), and typing cannot change
// a single row of it: the box, the tray, the completion overlay, the slash chip,
// the spellout preview and the legend's hint are all CHROME, rebuilt from
// scratch on every frame (view.go's [app.chrome]), and [app.layout] reads none of
// the draft's state. So a keystroke that marked the transcript dirty was buying a
// full relayout of the whole conversation — every entry, every tool cluster,
// every hover pass — to draw exactly the rows it had just drawn, once per
// character on a fast typist's keyboard and once per chunk of a large paste.
//
// THE CALLERS THAT DO CHANGE THE TRANSCRIPT ALREADY SAY SO. Answering a proposal
// (task.go), settling a standing card (standing.go), steering a room
// (room.go, roomorch.go), pulling a parked message back (park.go), dropping a
// picture chip (attach.go) and completing a file (files.go) all touch — or mark
// their own block stale — on the line above their `return a.edited()`, because
// each of them is a change to what is IN the list rather than to what is being
// typed under it. Nothing was ever relying on this call to do it for them.
func (a *app) edited() tea.Cmd {
	if len(a.input.value) == 0 {
		// An empty box is a new draft. Plainness belongs to the sentence that
		// held it and may not be inherited by the next identical slash word.
		a.input.demotedTags = nil
	}
	return tea.Batch(a.syncLists(), a.armDraftKeep())
}

// armDraftKeep asks for the composer to be written down a moment from now, and
// is the ONE debounce this surface has: every recipient's edit arms it, and the
// write is the whole composer — the conversation's words, its caret and
// documents, and every task page's own unsent line (draftkeep.go).
//
// IT IS NOT ONLY FOR KEYSTROKES. An answer to a correction arriving changes what
// a recipient is holding without anybody typing (steersend.go's
// [app.settleSend]), and a change nothing arms is a change the next launch does
// not see.
func (a *app) armDraftKeep() tea.Cmd {
	if a.draftFile == "" || a.draftPending {
		return nil
	}
	a.draftPending = true
	file := a.draftFile
	return tea.Tick(draftDebounce, func(time.Time) tea.Msg { return draftSaveMsg{file: file} })
}

// saveDraft writes the composer as it stands. The write happens in the command
// and not in the loop: it is small, but nothing on this surface waits on a disk.
//
// WHAT IS WRITTEN IS EVERY RECIPIENT'S STATE AND NEVER JUST THE BOX ON SCREEN
// (draftkeep.go's [app.keepDrafts]). The same editor draws a task's page, so a
// debounce that read the editor would put a line meant for a worker into the
// conversation's own file — and the next launch would restore it into the
// conversation as though the person had been writing it for the model.
func (a *app) saveDraft(file string) tea.Cmd {
	if file != "" && file != a.draftFile {
		// Armed by a conversation that is no longer the one on screen. It wrote
		// its own box on the way out, and writing this one under its name would
		// be the switch losing a sentence in each direction.
		return nil
	}
	a.draftPending = false
	return a.keepDrafts()
}

// dropDraft is submit: the sentence went somewhere, so the file goes.
//
// THE RECORD BESIDE IT IS REWRITTEN AND NOT REMOVED (draftkeep.go). Submit spends
// the conversation's own sentence; every task page's unsent line is still there,
// and so is the tray of a message the person has not sent yet.
//
// AND IT GOES THROUGH THE SAME ORDERED DOOR AS EVERY OTHER SAVE. A remove that
// stepped around it would be overtaken by a debounce armed a moment earlier, and
// the sentence the person watched leave would be on disk again ([draftWrites]).
func (a *app) dropDraft() {
	a.draftPending = false
	if a.draftFile == "" {
		return
	}
	a.writeDraftsNow("")
}

// keepMainDraft makes the file agree with MAIN'S BOX — written while there is a
// sentence in it, and removed when there is not ([writeDraft] is the one that
// decides which).
//
// IT IS [app.dropDraft]'s SIBLING AND NOT ITS REPLACEMENT, because the two answer
// different questions. Submit knows the conversation's box is empty — it has just
// been cleared — and a remove is the cheapest true thing to do. The steer guard's
// `m` does not: it spends a line typed at a TASK on the conversation instead
// (room.go's [app.guardSend]), and the conversation's own half-written sentence is
// sitting untouched behind that page. Removing the file there would throw away the
// crash insurance for words nobody sent anywhere.
func (a *app) keepMainDraft() {
	a.draftPending = false
	a.writeDraftsNow(a.mainDraftText())
}

// writeDraftsNow is the whole composer on disk, at once and on this goroutine:
// the record and then the plain export beside it (draftkeep.go's [draftSave]).
//
// IT IS THE SYNCHRONOUS DOOR AND ITS CALLERS ARE THE ONES WITH NOWHERE TO PUT A
// COMMAND — quitting, being taken over, submit, and the steer guard spending a
// page's line on the conversation. Everything else goes through the debounce
// ([app.saveDraft]), because nothing on this surface waits on a disk while a
// person is typing.
//
// THE WORDS ARE THE CALLER'S, because they are not always the box: the door out
// of the program folds every parked message in under the draft (quitarm.go's
// [app.leavingDraft]), and submit passes the empty string.
//
// A FAILURE IS SAID OUT LOUD. This is the write a person's only copy depends on.
func (a *app) writeDraftsNow(text string) {
	if a.draftFile == "" {
		return
	}
	if err := a.draftSaveOf(text).commit(); err != nil {
		a.noteDraftKeepFailed(err)
	}
}

// dropDraftFile removes one conversation's draft, named rather than taken off
// the surface. It is what a CLOSE owes: the file is crash insurance for a
// conversation that can no longer crash, and one left behind is somebody's
// finished sentence waiting to be adopted into the next window that opens on
// that directory ([adoptDraft]).
//
// AND THE RECORD GOES WITH IT, for the same reason and one more: a closed
// conversation's task pages are pages of work that closed with it, so their
// unsent lines have no reader left to be delivered to.
//
// IT IS AN EMPTY SAVE AND NOT A REMOVE, so that it takes its place in the order
// with every other write to this name (draftkeep.go's [draftWrites]): a debounce
// armed a moment before the close must not put the file back afterwards. What is
// in the record for ANOTHER conversation survives it — [draftKeep.empty] counts a
// carried slot, so the record is rewritten rather than deleted when one is there.
func dropDraftFile(path string) {
	if path == "" {
		return
	}
	_ = commitDropped(path)
}

// draftAdopted is the workspaces this process has already hunted an orphan on.
//
// FIRSTNESS CANNOT BE INFERRED FROM THE ORDINAL, which is the trap this closes:
// the first conversation on workspace B may be the third conversation of the
// process, and its ordinal is B's own zero. What adoption is for is a window
// that opened where a dead one left a sentence — the FIRST conversation this
// process puts on a directory — and a conversation opened from home an hour in
// must not paste a stranger's unfinished sentence into its box.
var draftAdopted = struct {
	mu    sync.Mutex
	tried map[string]bool
}{tried: map[string]bool{}}

// draftFirstHere reports whether this process has yet looked for an orphan on
// this workspace, and records that it now has.
//
// The key is the drafts directory AND the workspace, because the pair is what a
// glob is over: one process writes them all under one directory in the live
// door, and keying on both keeps the answer true for any door that does not.
func draftFirstHere(own, workspace string) bool {
	key := filepath.Dir(own) + "\x00" + filepath.Clean(workspace)
	draftAdopted.mu.Lock()
	defer draftAdopted.mu.Unlock()
	if draftAdopted.tried[key] {
		return false
	}
	draftAdopted.tried[key] = true
	return true
}

// restoreDraft puts the composer back at startup.
//
// WHAT IT RESTORES FROM IS THE RECORD (draftkeep.go): every recipient's own
// words, caret, documents and tray, laid out for the recipient they were written
// for. The plain file below is the fallback for a conversation that has no
// record — an older build's leftovers, or an orphan adopted from a dead window on
// this directory — and [app.legacyDraftText] is where it is read.
func (a *app) restoreDraft() {
	if a.draftFile == "" {
		return
	}
	a.layKeptDrafts()
}

// adoptDraft takes over the newest draft on this directory whose window is gone,
// and returns what it said.
//
// AN ORPHAN IS ADOPTED ONCE, BY THE NEXT WINDOW TO OPEN. The file is moved into
// this window's name rather than merely read, so the sentence is still on disk
// if this window dies too, and so a second window opening a moment later adopts
// the NEXT orphan rather than the same one — two windows are two boxes, and one
// sentence cannot be in both.
//
// Older orphans are left exactly where they are. They are somebody's unfinished
// words, they cost a few hundred bytes, and each window that opens rescues one
// more; deleting them to keep the directory tidy would be tidying away the only
// thing this file exists to protect.
//
// IT GLOBS FROM THE WORKSPACE'S OWN PREFIX and not from its own filename. The
// name gained an ordinal when one process could hold two conversations in one
// project, and a glob built by trimming the last token off this file's name
// would then match only drafts whose ORDINAL matches — so conversation 1 would
// never see the orphan a dead single-conversation window left as ordinal 0,
// which is the only case this function exists for.
//
// THE WORDS ARE THE WHOLE OF WHAT IS ADOPTED. The record beside an orphan
// belongs to whatever conversation that window was holding, and this directory
// matching is not that conversation being opened again — so its documents, its
// tray and its task pages stay where they are, for the window that opens it
// (draftkeep.go's [app.reuniteDrafts]).
func adoptDraft(own, workspace string) string {
	directory := filepath.Dir(own)
	if strings.TrimSpace(workspace) == "" {
		// Nothing to glob from. A hunt with no workspace would either sweep the
		// directory or match nothing, and matching nothing is the safe half.
		return ""
	}
	candidates, err := filepath.Glob(filepath.Join(directory, draftPrefix(workspace)+"*.txt"))
	if err != nil {
		return ""
	}

	var (
		newest string
		latest time.Time
	)
	for _, path := range candidates {
		if path == own || draftWindowAlive(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Before(latest) {
			continue
		}
		newest, latest = path, info.ModTime()
	}
	if newest == "" {
		return ""
	}
	text := readDraft(newest)
	if text == "" {
		// An empty file is nobody's sentence, and one left by a window that died
		// between the remove and the write would otherwise sit there forever.
		//
		// ITS RECORD IS LEFT WHERE IT IS. The sentence is empty; the composers
		// beside it are not, and they belong to a conversation that can still be
		// opened again (draftkeep.go's [app.reuniteDrafts] is what finds them).
		_ = os.Remove(newest)
		return ""
	}
	// A FAILED RENAME ONLY MEANS THE NEXT WINDOW WILL OFFER THE SAME SENTENCE
	// AGAIN, which is the harmless half: the words are in this box either way.
	_ = os.Rename(newest, own)
	return text
}

// draftWindowAlive says whether the process a draft is named after is still
// running, and answers YES to anything it cannot tell.
//
// Signal 0 is the question with no side effect: it reports whether the pid could
// be signalled, which is whether it exists. A pid that has been reused by some
// unrelated program reads as alive, and that is the failure this leans towards
// on purpose — the cost is a sentence left on disk for the next window to find,
// and the cost of the other mistake is taking a sentence out from under a window
// the person is typing in.
func draftWindowAlive(path string) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	pid, err := strconv.Atoi(base[strings.LastIndex(base, "-")+1:])
	if err != nil || pid <= 0 {
		return true
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, os.ErrPermission)
}

func readDraft(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Drafts written before the paste fix may carry CR line endings; the
	// editor's rows break on LF, so restore through the same door as paste.
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	// AND A FILE HOLDING NOTHING BUT WHITESPACE IS NOTHING, on the reading side
	// as well as the writing one ([writeDraft] says why). It is stated twice
	// because the two answer different questions: the write stops MAKING these,
	// and this stops the ones already on disk being restored into a box that
	// would then show nothing and behave as though it held a sentence. Every
	// caller treats the empty string as no draft, so [adoptDraft] deletes such a
	// file on its way past and [app.restoreDraft] leaves the box alone.
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

// writeDraft replaces the file, or removes it when the box is empty — an empty
// draft is not a draft, and leaving a zero-byte file behind would mean every
// directory aforge was ever opened in keeps one forever.
//
// EMPTY IS WHAT THE BOX ITSELF CALLS EMPTY, which is whitespace and not only the
// zero-length string ([editor.empty]). A draft of "\n\n" is a draft nobody can
// see: the frame draws the placeholder over it, `enter` trims it away rather
// than sending it, and the door at the foot goes on advertising itself. Keeping
// one on disk meant an invisible draft outliving the window that made it and
// being ADOPTED into the next window on that directory ([adoptDraft]) — a box
// that looked empty in a conversation nobody had typed in yet, with a home door
// drawn over it. It is easy to land in: `ctrl+enter` and `shift+enter` arrive as
// a bare `ctrl+j` on a terminal that cannot spell them, and `ctrl+j` opens a
// line.
// A FAILURE IS RETURNED AND NOT SWALLOWED. This file is the export beside the
// record (draftkeep.go), and what its caller does with a failure is say so: a
// disk that cannot be written is the one fact that makes every promise about a
// kept draft false, and nobody finds out by not being told.
func writeDraft(path, text string) error {
	if strings.TrimSpace(text) == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(text), 0o600)
}
