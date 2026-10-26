package furrow

import (
	"context"
	"testing"
)

// Peek reads what was last asked and never asks: with nothing remembered it
// says so, and a furrow that would answer is not launched by looking.
func TestPeekNeverAsks(t *testing.T) {
	installFake(t, "ok")
	root := t.TempDir()
	if _, _, known := Peek(root); known {
		t.Fatal("Peek knew a folder nobody had asked about")
	}
	if tools, _, known := PeekTools(root); known || len(tools) != 0 {
		t.Fatalf("PeekTools on an unasked folder = %d tools, known=%v; want none", len(tools), known)
	}
	Detect(context.Background(), root)
	presence, fresh, known := Peek(root)
	if !known || !fresh || !presence.Available() {
		t.Fatalf("Peek after Detect = %+v fresh=%v known=%v; want the fresh available answer", presence, fresh, known)
	}
	tools, _, _ := PeekTools(root)
	if len(tools) != len(Names()) {
		t.Fatalf("PeekTools on an attached folder = %d tools; want %d", len(tools), len(Names()))
	}
}

func TestPeekToolsOnAnUnattachedFolderIsNothing(t *testing.T) {
	installFake(t, "unattached")
	root := t.TempDir()
	Detect(context.Background(), root)
	if tools, _, known := PeekTools(root); !known || len(tools) != 0 {
		t.Fatalf("PeekTools on an unwatched folder = %d tools, known=%v; want none from a known answer", len(tools), known)
	}
}

// A question cut short is not an answer: it must not be remembered as "not
// attached" for the next session on the folder.
func TestACancelledDetectionIsNotRemembered(t *testing.T) {
	installFake(t, "ok")
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Detect(ctx, root)
	if _, _, known := Peek(root); known {
		t.Fatal("a cancelled detection was cached as furrow's answer")
	}
}
