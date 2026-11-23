package cellstore

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// ── AN EDIT MADE IN PLACE, OUTSIDE A TOOL CALL, IS STILL AN EDIT ────────────
//
// A seal is told which paths the turn's tool calls changed, and an empty list is
// a promise that nothing else changed ([TurnInfo.Changed]), so the engine visits
// only those paths. That promise is widened by the capture index (captureIndex),
// which notices a NEW file or an ATOMIC REPLACEMENT because the folder's own
// modified time moves when a name is added, removed or renamed in it.
//
// An edit made IN PLACE moves nothing but the file's own stat. Appending to
// README.md from a shell leaves its folder's modified time where it was, so a
// chat whose turn ran no tool (or ran one that named other paths) sealed the old
// bytes. On 2026-11-18 a hand test lost exactly that: the Mac's edit was never in
// the chat, the other computer took what the chat held, and when the chat came
// back the stat index (which was written AFTER that seal, from the folder as it
// then stood) vouched for the edited file as if it had been sealed. Nothing was
// kept, because there was nothing unsaved to protect as far as the index knew.
//
// THE FIX IS TO COMPARE THE FILES' OWN STAT RECORDS, ONCE, BEFORE THE SEAL READS
// ANYTHING, and to write the next stat index from that same walk. It costs no
// walk beyond the one every seal already made after itself ([stampWalks]), reads
// no file, and cannot vouch for an edit the seal did not see: a stamp taken
// before the engine reads a file is never newer than the bytes it sealed, so a
// file edited between the two is simply flagged again at the next look, which is
// a slower check and never a lost edit.

// widenByStamps adds to changed every file whose stat record differs from the one
// the last seal left, and the folder of every file that is gone. A caller with
// no promise to widen (changed is nil: unknown, so the engine walks the folder)
// is not asked.
//
// IT ANSWERS nil, WHICH IS "WALK THE WHOLE FOLDER", WHENEVER THE LAST RECORD
// CANNOT BE TRUSTED TO BE THE LAST SEAL'S: it is missing, it names another
// snapshot or folder, or this walk could not finish. Guessing there is how an
// edit is lost; the full plan it costs once is what every first seal of a
// process pays today.
func (e Engine) widenByStamps(c cell.Cell, head *Sealed, tree string, now map[string]statEntry, stamped bool, changed []string) []string {
	if changed == nil {
		return nil
	}
	if head == nil {
		// The first seal of a chat has no earlier seal to differ from, and its
		// capture index has seen nothing either, so it is a full plan already.
		return changed
	}
	if !stamped {
		return nil
	}
	raw, err := os.ReadFile(e.statIndexPath(c))
	if err != nil {
		return nil
	}
	var last statIndex
	if json.Unmarshal(raw, &last) != nil || last.Snapshot != head.Turn.ID || last.Tree != tree {
		return nil
	}
	seen := make(map[string]bool, len(changed))
	out := append([]string(nil), changed...)
	for _, name := range changed {
		seen[name] = true
	}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	var widened []string
	for name, after := range now {
		if name == gitIndexPath {
			// Git's own index is the capture index's to judge: it is a control
			// file whose change asks for a full plan, and the seal itself may
			// rewrite it.
			continue
		}
		if before, ok := last.Entries[name]; !ok || before != after {
			widened = append(widened, name)
		}
	}
	for name := range last.Entries {
		if _, ok := now[name]; !ok && name != gitIndexPath {
			widened = append(widened, path.Dir(name))
		}
	}
	sort.Strings(widened)
	for _, name := range widened {
		add(name)
	}
	return out
}

// gitIndexPath is the one file under .git that the stat index covers
// ([skipStatRel]).
const gitIndexPath = ".git/index"

// recordSealedStamps writes the stat index for a seal from the stamps taken
// BEFORE it, so the record names what the seal could have read and never a file
// edited after. Git's index alone is stamped again now: a seal may refresh it,
// and a record of the stale one would send every later look in a repository down
// the slow road.
func (e Engine) recordSealedStamps(c cell.Cell, snapshot, tree string, stamps map[string]statEntry) {
	if snapshot == "" || tree == "" {
		return
	}
	if info, err := os.Lstat(filepath.Join(tree, ".git", "index")); err == nil && info.Mode().IsRegular() {
		stamps[gitIndexPath] = statEntry{Size: info.Size(), ModNano: info.ModTime().UnixNano(), Mode: uint32(info.Mode())}
	} else {
		delete(stamps, gitIndexPath)
	}
	e.writeStatIndex(c, snapshot, tree, stamps)
}
