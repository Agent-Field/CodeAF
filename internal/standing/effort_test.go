package standing

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// The rung one item's firings think at is written down with the item and read
// back with it — which is the whole difference between "this check is worth
// thinking about" being a setting and being a thing somebody said once.
func TestOneItemsRungIsSavedAndReadBack(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	made, err := store.Create(reminder("tell me when the build goes red", now.Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if made.Does.Effort != "" {
		t.Fatalf("a fresh item carries %q; empty is nobody said, and the role's own floor decides",
			made.Does.Effort)
	}

	if err := store.SetStandingEffort(made.ID, effort.XHigh); err != nil {
		t.Fatalf("SetStandingEffort: %v", err)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if back.Does.Effort != "xhigh" {
		t.Fatalf("the item reads back at %q, want xhigh", back.Does.Effort)
	}

	// CLEARING IT IS A CHOICE TOO, and it puts the item back on the floor rather
	// than leaving it stuck at whatever it was last dialled to.
	if err := store.SetStandingEffort(made.ID, effort.None); err != nil {
		t.Fatalf("clearing the rung: %v", err)
	}
	if back, _ = store.Get(made.ID); back.Does.Effort != "" {
		t.Fatalf("after clearing, the item reads %q, want nothing", back.Does.Effort)
	}

	// A word that is not a rung is refused at the door, and an item nobody has
	// is not found — both before anything touches the document.
	if err := store.SetStandingEffort(made.ID, effort.Rung("deepest")); err == nil {
		t.Fatal("a word that is not a rung was written onto an item")
	}
	if err := store.SetStandingEffort("0123456789abcdef", effort.High); err != ErrNotFound {
		t.Fatalf("setting a rung on an item nobody has gave %v, want ErrNotFound", err)
	}
}
