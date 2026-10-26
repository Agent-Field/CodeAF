package furrow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// peek reads what was last asked and never asks: with nothing remembered it says
// so, and a furrow that would answer is not launched by looking.
func TestPeekNeverAsks(t *testing.T) {
	installFake(t, "ok")
	root := t.TempDir()
	if _, _, known := peek(root); known {
		t.Fatal("peek knew a folder nobody had asked about")
	}
	if tools, _ := PeekTools(root); len(tools) != 0 {
		t.Fatalf("PeekTools on an unasked folder = %d tools; want none", len(tools))
	}
	Detect(context.Background(), root)
	presence, fresh, known := peek(root)
	if !known || !fresh || !presence.Available() {
		t.Fatalf("peek after Detect = %+v fresh=%v known=%v; want the fresh available answer", presence, fresh, known)
	}
	tools, fresh := PeekTools(root)
	if len(tools) != len(Names()) || !fresh {
		t.Fatalf("PeekTools on an attached folder = %d tools fresh=%v; want %d fresh", len(tools), fresh, len(Names()))
	}
}

func TestPeekToolsOnAnUnattachedFolderIsNothing(t *testing.T) {
	installFake(t, "unattached")
	root := t.TempDir()
	Detect(context.Background(), root)
	if tools, fresh := PeekTools(root); len(tools) != 0 || !fresh {
		t.Fatalf("PeekTools on an unwatched folder = %d tools fresh=%v; want none from a fresh answer", len(tools), fresh)
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
	if _, _, known := peek(root); known {
		t.Fatal("a cancelled detection was cached as furrow's answer")
	}
}

// A furrow that fails or hangs on its own IS an answer, and it is remembered for
// the TTL: however many times the question is asked, furrow is launched for it
// once. Launches are counted; nothing is timed.
func TestAFailingOrHangingFurrowIsAskedOncePerTTL(t *testing.T) {
	for name, status := range map[string]string{
		"failing": "echo boom >&2; exit 1",
		"hanging": "sleep 30",
	} {
		t.Run(name, func(t *testing.T) {
			carryNothing(t)
			dir := t.TempDir()
			log := filepath.Join(dir, "launches")
			script := "#!/bin/sh\necho \"$*\" >> " + log + "\nif [ \"$1\" = \"--version\" ]; then echo furrow 0.1.0; exit 0; fi\n" + status + "\n"
			if err := os.WriteFile(filepath.Join(dir, Binary), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
			t.Setenv(BinaryEnvVar, "")
			Forget()
			t.Cleanup(Forget)
			restore := detectTimeout
			detectTimeout = 200 * time.Millisecond
			t.Cleanup(func() { detectTimeout = restore })

			root := t.TempDir()
			for i := 0; i < 8; i++ {
				if Detect(context.Background(), root).Available() {
					t.Fatal("a furrow that cannot answer reported available")
				}
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(raw), "\n"); got != 2 {
				t.Fatalf("furrow was launched %d times for eight questions; want 2 (--version, status)", got)
			}
		})
	}
}
