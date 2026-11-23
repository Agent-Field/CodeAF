package syncsetup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLivePreviewGetsTitleAndSavedConversationWithoutTaking(t *testing.T) {
	r := newLiveRig(t)
	chat := liveChat(t, r)
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"What happened?"}`+"\n")
	chat.mustSay("The checks passed.")
	r.durable(r.cell.ID, r.cell)
	ctx := t.Context()

	meta, err := session.LoadMeta(r.cell.Root)
	if err != nil {
		t.Fatal(err)
	}

	for _, title := range []string{"Private title before a move", "Renamed while still on the holder"} {
		meta.Title = title
		mustNil(t, session.SaveMeta(r.cell.Root, meta))
		titles, err := r.b.PeerChatTitles(ctx, r.a.Device.ID(), []string{r.cell.ID})
		if err != nil || titles[r.cell.ID] != title {
			t.Fatalf("paired title = %q (%v), want %q", titles[r.cell.ID], err, title)
		}
		if bytes.Contains(r.tap.bytes(), []byte(title)) {
			t.Fatal("the service saw the plaintext title")
		}
	}

	before, err := r.b.Dir.Cell(ctx, r.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Cell.Title != "" {
		t.Fatal("the directory was given a title")
	}
	var summary session.Summary
	err = r.b.ReadChat(ctx, r.cell.ID, before, func(c cell.Cell) error {
		summary, _ = session.Peek(filepath.Join(c.Root, cell.TranscriptPath))
		if _, err := os.Stat(filepath.Join(c.Root, cell.MetaPath)); !os.IsNotExist(err) {
			t.Fatalf("preview staged other chat state: %v", err)
		}
		return nil
	})
	if err != nil || summary.LastUser != "What happened?" || summary.LastAssistant != "The checks passed." {
		t.Fatalf("preview = %q, %q (%v)", summary.LastUser, summary.LastAssistant, err)
	}
	after, err := r.b.Dir.Cell(ctx, r.cell.ID)
	if err != nil || before.Cell.Lease.Device != after.Cell.Lease.Device || before.Cell.Lease.Fence != after.Cell.Lease.Fence || before.Cell.Head != after.Cell.Head {
		t.Fatalf("preview changed the holder, fence, or saved head: %v", err)
	}
	if n := r.wire.storeWrites(); n != 0 {
		t.Fatalf("the service was sent %d object writes; want none", n)
	}
}
