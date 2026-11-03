package syncsetup

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestIdleRelease_R8_ManualRenameReachesReceiverOrRefuses(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	a, _ := idleContractSession(t, h, c)
	before := idleContractReleased(t, h, c.drive)
	chosen := "Kept after release"
	mustNil(t, a.RenameByPerson(chosen))
	a.SettleWrites()
	// Observe the setter's own save/release. Do not seal or signal Idle here.
	waitFor(t, "manual name and journal to reach a new released fence", func() bool {
		v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
		return err == nil && v.Cell.Lease.Fence > before.Cell.Lease.Fence && v.Cell.Lease.Expires == 0
	})
	got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, nil)
	mustNil(t, err)
	meta, err := session.LoadMeta(got.Taken.Cell.Root)
	mustNil(t, err)
	raw, err := os.ReadFile(transcriptOf(got.Taken.Cell))
	mustNil(t, err)
	if meta.Title != chosen || !strings.Contains(string(raw), chosen) {
		t.Fatalf("received title=%q journal=%s", meta.Title, raw)
	}
	prior, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if err := a.RenameByPerson("refused words"); err == nil {
		t.Fatal("rename in the displaced reader acknowledged")
	}
	after, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	if a.Title() != chosen || !bytes.Equal(prior, after) {
		t.Fatal("refused rename mutated the name or journal")
	}
}
