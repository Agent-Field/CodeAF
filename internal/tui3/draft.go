package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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

// draftDebounce is how long the box has to be still before the draft is
// written. A file write per keystroke would be a syscall per character to save
// a sentence nobody has finished; three hundred milliseconds is the pause
// between words.
const draftDebounce = 300 * time.Millisecond

// draftSaveMsg is the debounce firing.
type draftSaveMsg struct{}

// DraftFile is where one workspace's draft lives under dir. The name carries a
// hash of the path rather than the path itself, because a directory name can be
// longer than a file name may be — and the person never has to find this file,
// unlike a session transcript.
func DraftFile(dir, workspace string) string {
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(filepath.Clean(workspace)))
	return filepath.Join(dir, "draft-"+hex.EncodeToString(sum[:8])+".txt")
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
	return tea.Batch(lists, tea.Tick(draftDebounce, func(time.Time) tea.Msg { return draftSaveMsg{} }))
}

// saveDraft writes the box as it stands. The write happens in the command and
// not in the loop: it is small, but nothing on this surface waits on a disk.
func (a *app) saveDraft() tea.Cmd {
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

// restoreDraft puts the file back in the box at startup.
func (a *app) restoreDraft() {
	if a.draftFile == "" {
		return
	}
	if text := readDraft(a.draftFile); text != "" {
		a.input.setText(text)
	}
}

func readDraft(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
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
