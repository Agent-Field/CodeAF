package lane

import (
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// TestAThinkingSpreadSurvivesJournalReplayAndCompactionExactlyOnce holds the
// duration clock's dispersion to the same persistence law as its belief: a
// restart neither forgets its draws nor folds them a second time.
func TestAThinkingSpreadSurvivesJournalReplayAndCompactionExactlyOnce(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	Default().Reset()

	const model, rung = "openai/gpt-5", "high"
	for n := range 40 {
		took := time.Duration(250+n*n*37) * time.Millisecond
		NoteThought(model, rung, took, noon.Add(time.Duration(n)*time.Minute))
	}

	type measure struct {
		spreadStat
		draw float64
	}
	read := func(l *ledger) measure {
		t.Helper()
		draw := l.ThinkDraw(model, rung)
		leaf := thoughtOf(model, rung)[LevelPair]
		l.mu.Lock()
		spread := l.think.Spread[leaf]
		l.mu.Unlock()
		return measure{spreadStat: spread, draw: draw}
	}
	equal := func(stage string, got, want measure) {
		t.Helper()
		if got.N != want.N {
			t.Errorf("%s counted %d thoughts, want %d", stage, got.N, want.N)
		}
		if math.Abs(got.Mean-want.Mean) > 1e-9 || math.Abs(got.M2-want.M2) > 1e-9 {
			t.Errorf("%s kept spread %+v, want %+v", stage, got.spreadStat, want.spreadStat)
		}
		if math.Abs(got.draw-want.draw) > 1e-9 {
			t.Errorf("%s draws %v, want %v", stage, got.draw, want.draw)
		}
	}

	writing := Default().Ledger().(*ledger)
	before := read(writing)
	if before.N != 40 {
		t.Fatalf("forty thoughts made an account of %d: %+v", before.N, before.spreadStat)
	}

	replayed := newLedger()
	replayed.keepIn(newStore())
	equal("journal replay", read(replayed), before)

	Flush()
	Default().SetLedger(nil)
	reloaded := Default().Ledger().(*ledger)
	equal("the compacted snapshot", read(reloaded), before)

	NoteThought(model, rung, 11*time.Second, noon.Add(40*time.Minute))
	after := read(reloaded)
	if after.N != 41 {
		t.Fatalf("one thought after reload made an account of %d, want 41: %+v", after.N, after.spreadStat)
	}
}
