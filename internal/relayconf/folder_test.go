//go:build relayurl

package relayconf

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Both relay implementations must advertise and retain the same opaque folder.
func TestChatFolderContract(t *testing.T) {
	acct := newAccount(t)
	a := directory.NewHTTP(baseURL(t), acct.sign("a", wall), httpClient)
	b := directory.NewHTTP(baseURL(t), acct.sign("b", wall), httpClient)
	ctx := t.Context()
	const id = "cccccccccccccccccccccccccccccccc"
	v, err := a.Create(ctx, id, directory.CellInit{Head: "h0", Folder: "sealed-folder"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Cell.Folder != "sealed-folder" {
		t.Fatalf("create folder = %q", v.Cell.Folder)
	}
	missing, err := a.Missing(ctx, id)
	if err != nil || len(missing) != 0 {
		t.Fatalf("service promises missing: %v, %v", missing, err)
	}
	head := "h0"
	for _, folder := range []string{"", "other", ""} {
		next := head + "x"
		v, err = a.Publish(ctx, id, directory.Publish{Fence: v.Cell.Lease.Fence, OldHead: head, Head: next, Folder: folder})
		if err != nil {
			t.Fatal(err)
		}
		head = next
	}
	if err := a.Release(ctx, id, v.Cell.Lease.Fence); err != nil {
		t.Fatal(err)
	}
	got, err := b.Cell(ctx, id)
	if err != nil || got.Cell.Folder != "other" {
		t.Fatalf("second device folder = %+v, %v", got, err)
	}
	list, err := b.List(ctx)
	if err != nil || list.Cells[id].Folder != "other" {
		t.Fatalf("listing folder = %+v, %v", list.Cells[id], err)
	}
}
