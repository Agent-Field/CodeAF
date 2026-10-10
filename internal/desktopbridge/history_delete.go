package desktopbridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// historyTrashKeep is how long a deleted conversation waits in the trash.
	// The undo toast lasts 10 seconds; the rest is slack for a slow click.
	historyTrashKeep = 10 * time.Minute
	// trashStampLayout names one delete request's trash folder. It sorts by time
	// and carries no separator the token check would have to allow.
	trashStampLayout = "20060102T150405Z"
	trashManifest    = "manifest.json"
)

// trashRoot is where deleted conversations wait. It is a variable so a test can
// point it at a temporary directory instead of the real profile.
var trashRoot = func() string { return home.Join("trash") }

// undoTokenShape is the only form a restore accepts, so a token can never walk
// out of the trash folder.
var undoTokenShape = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{8}$`)

// trashedConversation remembers where a folder came from, because the trash
// holds folders by id and the project bucket is not part of the id.
type trashedConversation struct {
	ID   string `json:"id"`
	From string `json:"from"`
}

// deleteConversations moves archived, unattached conversations into the trash.
// A rename is atomic on one filesystem, so a conversation is wholly deleted or
// wholly untouched; one that cannot move is simply not counted.
func (b *Bridge) deleteConversations(w http.ResponseWriter, r *http.Request, h *History) {
	var ask struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &ask) {
		return
	}
	if len(ask.IDs) > historyArchiveMax {
		fail(w, 400, "too many conversations in one request")
		return
	}
	attached := b.attachedStates()
	live := map[string]bool{}
	for _, entry := range h.rows(attached) {
		if entry.row.Open || entry.row.Live || entry.attach != nil {
			live[entry.row.ID] = true
		}
	}
	for _, id := range ask.IDs {
		if live[id] {
			fail(w, 409, "a conversation that is open cannot be deleted")
			return
		}
		dir, ok := h.folder(id)
		if !ok {
			continue
		}
		if meta, _ := session.LoadMeta(dir); meta.ID != "" && !meta.Archived {
			fail(w, 409, "only archived conversations can be deleted")
			return
		}
	}
	var salt [4]byte
	_, _ = rand.Read(salt[:])
	token := time.Now().UTC().Format(trashStampLayout) + "-" + hex.EncodeToString(salt[:])
	stamp := filepath.Join(trashRoot(), token)
	if err := os.MkdirAll(stamp, 0o700); err != nil {
		fail(w, 500, "the trash is not writable")
		return
	}
	var moved []trashedConversation
	for _, id := range ask.IDs {
		dir, ok := h.folder(id)
		if !ok {
			continue
		}
		if os.Rename(dir, filepath.Join(stamp, id)) == nil {
			moved = append(moved, trashedConversation{ID: id, From: dir})
		}
	}
	if len(moved) == 0 {
		_ = os.Remove(stamp)
		write(w, map[string]any{"deleted": 0})
		return
	}
	manifest, _ := json.Marshal(moved)
	if err := os.WriteFile(filepath.Join(stamp, trashManifest), manifest, 0o600); err != nil {
		// Without a manifest the move cannot be undone, so put everything back.
		for _, item := range moved {
			_ = os.Rename(filepath.Join(stamp, item.ID), item.From)
		}
		_ = os.Remove(stamp)
		fail(w, 500, "the trash is not writable")
		return
	}
	write(w, map[string]any{"deleted": len(moved), "undoToken": token})
}

// restoreConversations moves a delete request's folders back where they came
// from. A folder whose place was taken in the meantime stays in the trash.
func (b *Bridge) restoreConversations(w http.ResponseWriter, r *http.Request) {
	var ask struct {
		UndoToken string `json:"undoToken"`
	}
	if !decode(w, r, &ask) {
		return
	}
	if !undoTokenShape.MatchString(ask.UndoToken) {
		fail(w, 400, "unknown undo token")
		return
	}
	stamp := filepath.Join(trashRoot(), ask.UndoToken)
	raw, err := os.ReadFile(filepath.Join(stamp, trashManifest))
	var moved []trashedConversation
	if err != nil || json.Unmarshal(raw, &moved) != nil {
		fail(w, 404, "nothing to restore")
		return
	}
	restored := 0
	for _, item := range moved {
		if _, err := os.Stat(item.From); err == nil {
			continue
		}
		if os.MkdirAll(filepath.Dir(item.From), 0o700) != nil {
			continue
		}
		if os.Rename(filepath.Join(stamp, item.ID), item.From) == nil {
			restored++
		}
	}
	if restored == len(moved) {
		_ = os.RemoveAll(stamp)
	}
	write(w, map[string]int{"restored": restored})
}

// purgeTrash removes trash entries older than historyTrashKeep. It runs when a
// history is attached, which is the bridge starting, so the trash never grows
// past one session's deletes.
func purgeTrash(now time.Time) {
	entries, err := os.ReadDir(trashRoot())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !undoTokenShape.MatchString(entry.Name()) {
			continue
		}
		at, err := time.Parse(trashStampLayout, entry.Name()[:len(trashStampLayout)])
		if err == nil && now.Sub(at) > historyTrashKeep {
			_ = os.RemoveAll(filepath.Join(trashRoot(), entry.Name()))
		}
	}
}
