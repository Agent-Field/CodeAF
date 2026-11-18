package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// goneFromA is the scene the field reported: A's chat worked in a project
// folder, the other computer took it, worked, and let it go, and meanwhile the
// person deleted the project folder on A. A's session record still names the
// folder, as it does in life.
func goneFromA(t *testing.T) *twoHomes {
	t.Helper()
	h := bContinued(t)
	// The rig names the project by a link so B cannot resolve it. Here the
	// folder itself is the path A's record keeps, and it is removed outright.
	m, err := session.LoadMeta(h.cell.Root)
	mustNil(t, err)
	m.Workspace = h.work
	mustNil(t, session.SaveMeta(h.cell.Root, m))
	mustNil(t, os.RemoveAll(h.work))
	return h
}

// TestTakeBackAfterTheProjectFolderWasDeleted is the launch walk's door: the
// folder a chat worked in on this computer is gone, the chat was released by
// the other computer, and taking it back opens it. There are no local files to
// keep, so the chat arrives the way a chat from another computer does, in a
// folder of its own, with every file as the other computer left it, and the
// deleted path is left alone.
func TestTakeBackAfterTheProjectFolderWasDeleted(t *testing.T) {
	h := goneFromA(t)

	back, err := h.continuerA().Take(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatalf("taking back a chat whose folder was deleted here = %v; want it opened", err)
	}
	if back.Taken.Kept != "" {
		t.Fatalf("a folder that is not there kept %s", back.Taken.Kept)
	}
	if back.Wait != nil {
		mustNil(t, back.Wait(context.Background()))
	}
	got := workspaceOf(back.Taken.Cell.Root)
	if want := filepath.Join(back.Taken.Cell.Root, "work"); got != want {
		t.Fatalf("the chat works in %s; want its own folder %s", got, want)
	}
	// Withheld dotenv files come back through the vault, not through the
	// snapshot, so they are checked as the vault's and left out of the byte
	// comparison with what the other computer saved.
	assertEnvInjected(t, got)
	arrived := readTree(t, got)
	delete(arrived, ".env")
	delete(arrived, "web/client/app/.env.production")
	sameTree(t, "the folder the chat arrived in", h.fetchInto(headOf(t, back.Taken.Cell)), arrived)
	if _, err := os.Stat(h.work); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted folder is back (%v); nothing should create it again", err)
	}
}

// TestTakeBackAfterTheProjectFolderWasDeletedKeepsUnsavedWords: the folder is
// gone AND this computer holds a turn that was never shared. The files are not
// at stake, but the words are: the take keeps them as a separate copy of the
// chat, the way it keeps them for a folder that is there, and still opens the
// chat. Sealing a folder that is not there refuses, so the words are sealed
// against the chat's own folder and the deleted path is not made again.
func TestTakeBackAfterTheProjectFolderWasDeletedKeepsUnsavedWords(t *testing.T) {
	h := goneFromA(t)
	ctx := context.Background()
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"keep this turn"}`+"\n")

	back, err := h.continuerA().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatalf("taking back a chat with an unsaved turn and no folder = %v; want it opened", err)
	}
	if back.Wait != nil {
		mustNil(t, back.Wait(ctx))
	}
	if back.Taken.Kept == "" || back.KeptTurns != 1 {
		t.Fatalf("Continued = %+v; want the unsaved turn kept as 1 turn", back)
	}
	rec, err := h.a.Dir.Cell(ctx, back.Taken.Kept)
	if err != nil || rec.Cell.ParentCell != h.cell.ID {
		t.Fatalf("kept branch = %+v (%v); want a child of the chat", rec.Cell, err)
	}
	if _, err := os.Stat(h.work); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted folder is back (%v); nothing should create it again", err)
	}
}

// TestTakeBackIntoAPathThatIsNotAFolderIsRefused: only "does not exist" means
// there is nothing to keep. A file where the project folder was could hide
// edits this computer cannot see, so the take stops and does not open the chat.
func TestTakeBackIntoAPathThatIsNotAFolderIsRefused(t *testing.T) {
	h := goneFromA(t)
	mustNil(t, os.WriteFile(h.work, []byte("not a folder"), 0o600))

	back, err := h.continuerA().Take(context.Background(), h.cell.ID)
	if err == nil {
		t.Fatalf("take = %+v; a path that is not a folder must stop the move", back)
	}
	if got, _ := os.ReadFile(h.work); string(got) != "not a folder" {
		t.Fatalf("the file at the project path was changed: %q", got)
	}
}
