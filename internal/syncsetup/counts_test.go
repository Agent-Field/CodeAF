package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// scopesOf lists every scope a home has stats for: the cells it served and the
// vault, which is a scope like any other.
func scopesOf(t *testing.T, home string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(home, "v3", "sync", "stats", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".jsonl"))
	}
	return names
}

// countedAt sums every scope of every home into what the relay should have seen.
func countedAt(t *testing.T, homes ...string) blobstore.Counts {
	t.Helper()
	var sum blobstore.Counts
	for _, home := range homes {
		for _, scope := range scopesOf(t, home) {
			lines, err := cellstats.Read(home, scope)
			if err != nil {
				t.Fatal(err)
			}
			l := cellstats.Total(lines)
			sum.Puts += l.Puts
			sum.Gets += l.Gets
			sum.Has += l.Has
			sum.BytesIn += l.BytesUp
			sum.BytesOut += l.BytesDown
		}
	}
	return sum
}

func relayCounts(t *testing.T, s *Sync) blobstore.Counts {
	t.Helper()
	sign := reqsign.SignFor(deviceSigner{s.Identity, s.Device}, time.Now)
	got, err := blobstore.NewHTTP(s.Relay, sign, nil).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestScopesCountsEveryRelayRequest is the acceptance for cell report: over two
// homes, a publish, a vault push, a takeover fetch and an L12 branch, the sum of
// every scope's stats equals what the relay counted, request for request and
// byte for byte, and each kind of traffic lands under the scope that caused it.
func TestScopesCountsEveryRelayRequest(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	seedTree(t, h.work)
	h.vaultTheEnv()

	a := h.openA()
	a.mustSay("first")
	a.mustSay("second")
	h.durable(h.cell.ID, h.cell)

	h.lapse()
	if _, err := h.continuerB().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	a.mustSay("orphaned") // A does not know it lost the chat; its publish is refused and becomes a branch
	waitFor(t, "A to learn it was superseded", func() bool { _, viewer := a.drive.Viewer(); return viewer })
	if _, ok := branchRowOf(t, h.a, h.cell.ID); !ok {
		t.Fatal("A's refused turns did not become a branch")
	}
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	got, want := countedAt(t, h.homeA, h.b.Home), relayCounts(t, h.a)
	if got != want {
		t.Fatalf("stats over both homes = %+v, relay counted %+v", got, want)
	}
	assertScopeMoved(t, h.homeA, h.cell.ID, "A's publishes", func(l cellstats.Line) bool { return l.Puts > 0 })
	assertScopeMoved(t, h.homeA, cellstats.VaultScope, "A's vault push", func(l cellstats.Line) bool { return l.Puts > 0 })
	assertScopeMoved(t, h.b.Home, h.cell.ID, "B's takeover fetch", func(l cellstats.Line) bool { return l.Gets > 0 && l.BytesDown > 0 })
	assertScopeMoved(t, h.b.Home, cellstats.VaultScope, "B's vault pull", func(l cellstats.Line) bool { return l.Gets > 0 })
}

func assertScopeMoved(t *testing.T, home, scope, what string, moved func(cellstats.Line) bool) {
	t.Helper()
	lines, err := cellstats.Read(home, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !moved(cellstats.Total(lines)) {
		t.Fatalf("scope %q of %s has no record of %s: %+v", scope, filepath.Base(home), what, lines)
	}
}

// TestScopeStatsSettleWritesOnlyWhatMoved pins that a scope that moved nothing
// leaves no file behind, so a machine that never syncs never grows a stats folder.
func TestScopeStatsSettleWritesOnlyWhatMoved(t *testing.T) {
	srv := relay(t)
	s := openSync(t, machine(t, srv.URL))
	s.scope("idle").Settle()
	if _, err := os.Stat(cellstats.Path(s.Home, "idle")); err == nil {
		t.Fatal("a scope that moved nothing wrote a stats file")
	}
}
