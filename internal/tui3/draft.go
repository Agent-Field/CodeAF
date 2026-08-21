package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// DraftFile is where THIS WINDOW's draft on one workspace lives under dir. The
// name carries a hash of the path rather than the path itself, because a
// directory name can be longer than a file name may be — and the person never
// has to find this file, unlike a session transcript.
//
// It is stable for the life of the process and different in every other one, so
// a window may write it without ever asking who else is open.
func DraftFile(dir, workspace string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, draftPrefix(workspace)+strconv.Itoa(draftOwner)+".txt")
}

// draftPrefix is everything in the name before the window: the part two windows
// on one directory share, and the glob an orphan hunt uses.
func draftPrefix(workspace string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(workspace)))
	return "draft-" + hex.EncodeToString(sum[:8]) + "-"
}

// edited is what every draft mutation returns: the two typed overlays follow
// what is in the box, the frame is marked, and the debounce is armed.
func (a *app) edited() tea.Cmd {
	lists := a.syncLists()
	a.touch()
	if a.draftFile == "" || a.draftPending {
		return lists
	}
	a.draftPending = true
	file := a.draftFile
	return tea.Batch(lists, tea.Tick(draftDebounce, func(time.Time) tea.Msg { return draftSaveMsg{file: file} }))
}

// saveDraft writes the box as it stands. The write happens in the command and
// not in the loop: it is small, but nothing on this surface waits on a disk.
func (a *app) saveDraft(file string) tea.Cmd {
	if file != "" && file != a.draftFile {
		// Armed by a conversation that is no longer the one on screen. It wrote
		// its own box on the way out, and writing this one under its name would
		// be the switch losing a sentence in each direction.
		return nil
	}
	a.draftPending = false
	if a.draftFile == "" {
		return nil
	}
	path, text := a.draftFile, a.input.String()
	return func() tea.Msg {
		writeDraft(path, text)
		return nil
	}
}

// dropDraft is submit: the sentence went somewhere, so the file goes.
func (a *app) dropDraft() {
	a.draftPending = false
	if a.draftFile == "" {
		return
	}
	_ = os.Remove(a.draftFile)
}

// restoreDraft puts the file back in the box at startup — this window's own if
// it is somehow still there, and otherwise the sentence a dead window left.
func (a *app) restoreDraft() {
	if a.draftFile == "" {
		return
	}
	text := readDraft(a.draftFile)
	if text == "" {
		text = adoptDraft(a.draftFile)
	}
	if text != "" {
		a.input.setText(text)
	}
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
func adoptDraft(own string) string {
	directory := filepath.Dir(own)
	name := strings.TrimSuffix(filepath.Base(own), filepath.Ext(own))
	cut := strings.LastIndex(name, "-")
	if cut < 0 {
		// A name this file did not make. There is no window in it to compare
		// against, and globbing on what is left would sweep the directory.
		return ""
	}
	candidates, err := filepath.Glob(filepath.Join(directory, name[:cut+1]+"*.txt"))
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
		_ = os.Remove(newest)
		return ""
	}
	if err := os.Rename(newest, own); err != nil {
		// The text is in the box either way; the file staying behind only means
		// the next window will offer it again.
		return text
	}
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
	return strings.ReplaceAll(text, "\r", "\n")
}

// writeDraft replaces the file, or removes it when the box is empty — an empty
// draft is not a draft, and leaving a zero-byte file behind would mean every
// directory aforge was ever opened in keeps one forever.
func writeDraft(path, text string) {
	if text == "" {
		_ = os.Remove(path)
		return
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
	}
	// A failed write is dropped in silence, for internal/history's reason: the
	// draft is a convenience, and nothing about it is worth interrupting a
	// person mid-sentence for.
	_ = os.WriteFile(path, []byte(text), 0o600)
}
