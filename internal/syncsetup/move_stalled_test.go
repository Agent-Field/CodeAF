package syncsetup

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A move whose bytes stop arriving ends with ErrMoveStalled once it has waited
// the limit, the chat stays where it was, and the move reported how far it got.
func TestAMoveWhoseBytesStopEndsAndLeavesTheChat(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	var stall atomic.Bool
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if stall.Load() && strings.HasPrefix(r.URL.Path, "/v1/store/") && r.URL.Path != "/v1/store/has" {
				<-r.Context().Done() // the relay never answers
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	a := h.openA()
	a.mustSay("a reply that has to travel")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	holder := func() (string, uint64) {
		v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
		mustNil(t, err)
		return v.Cell.Lease.Device, v.Cell.Lease.Fence
	}
	device, fence := holder()
	continuer := h.continuerB()
	continuer.opt.IdleWithin = 300 * time.Millisecond
	var reports atomic.Int32
	continuer.opt.OnMove = func(MoveProgress) { reports.Add(1) }
	stall.Store(true)
	_, err := continuer.Take(t.Context(), h.cell.ID)
	if !errors.Is(err, ErrMoveStalled) {
		t.Fatalf("a stalled move ended with %v, want ErrMoveStalled", err)
	}
	if reports.Load() == 0 {
		t.Fatal("the move never reported how far it got")
	}
	if d, f := holder(); d != device || f != fence {
		t.Fatalf("the chat's lease is %s@%d after a stalled move, want %s@%d", d, f, device, fence)
	}
}

// A chat's record says how many bytes a copy takes, and a move to a computer
// that never held the chat shows that as its total.
func TestAMoveShowsTheChatsSizeAsItsTotal(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	was := moveReportEvery
	moveReportEvery = time.Millisecond
	t.Cleanup(func() { moveReportEvery = was })
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("a reply worth copying")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if v.Cell.Size == 0 {
		t.Fatal("a published chat's record has no size")
	}
	meta, err := session.LoadMeta(h.cell.Root)
	mustNil(t, err)
	folder, err := directory.OpenName(directory.MetadataKey(h.a.Identity.CellKey()), v.Cell.Folder)
	if err != nil || folder != filepath.Base(meta.Workspace) {
		t.Fatalf("the record names folder %q (%v), want %q", folder, err, filepath.Base(meta.Workspace))
	}
	continuer := h.continuerB()
	var total, done atomic.Int64
	continuer.opt.OnMove = func(p MoveProgress) {
		if p.Total > 0 {
			total.Store(p.Total)
		}
		done.Store(p.Done)
	}
	_, err = continuer.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if got := total.Load(); got != int64(v.Cell.Size) {
		t.Fatalf("the move's total was %d, want the record's %d", got, v.Cell.Size)
	}
	if done.Load() == 0 {
		t.Fatal("the move reported no bytes copied")
	}
}

// A chat shared in the background says how many bytes a copy takes, so a
// computer about to move it can warn before a big folder.
func TestABackgroundSharedChatHasASize(t *testing.T) {
	r := newDriveRig(t)
	s := r.a
	c := savedHomeChat(t, r, projectsOf(s), "big folder", 1<<20)
	mustNil(t, s.PublishLocalChats(t.Context(), time.Now, nil))
	v, err := s.Dir.Cell(t.Context(), c.ID)
	mustNil(t, err)
	if v.Cell.Size < 1<<20 {
		t.Fatalf("a background-shared 1 MB chat's record says %d bytes", v.Cell.Size)
	}
}
