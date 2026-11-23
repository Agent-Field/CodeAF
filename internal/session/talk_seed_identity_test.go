package session_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSeededCellKeepsItsSessionIdentity(t *testing.T) {
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID}); err != nil {
		t.Fatal(err)
	}
	path := (session.Place{Dir: c.Root}).Transcript()
	if err := session.SeedConversation(path, "", "Manager", "Manage the job."); err != nil {
		t.Fatal(err)
	}
	d, err := session.ReadDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != c.ID {
		t.Fatalf("seeded journal identity %q differs from session folder %q", d.ID, c.ID)
	}
}
