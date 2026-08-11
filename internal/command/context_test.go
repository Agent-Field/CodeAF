package command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
)

// seatCatalog builds a catalog with no network in it: a cache file inside its
// TTL is the one path load() takes before it reaches out, so the rows below are
// exactly what the commander will be asked about.
func seatCatalog(t *testing.T, models []map[string]any) *catalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(map[string]any{
		"fetched_at": time.Now().UTC(),
		"models":     models,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model-catalog.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return catalog.Load(context.Background(), catalog.Options{Dir: dir})
}

// TestAnUnsizedModelHasNoContextWindowRatherThanAZeroOne is the seam's whole
// honesty contract. ctx% is a health signal, and a health signal that guesses
// guesses calm: a window reported as zero-but-known divides into nothing, and a
// window invented from a default reads "plenty of room" for a model nobody
// measured. Absent has to survive the trip to the renderer as absent.
func TestAnUnsizedModelHasNoContextWindowRatherThanAZeroOne(t *testing.T) {
	models := seatCatalog(t, []map[string]any{
		{"id": "vendor/sized", "context_length": 262144},
		{"id": "vendor/unsized"},
	})

	sized := New(Options{Models: models, Prefs: Prefs{ChatModel: "vendor/sized"}})
	tokens, known := sized.ContextWindow("talk")
	if !known || tokens != 262144 {
		t.Fatalf("a sized model = %d tokens, known %v; want the catalog's 262144", tokens, known)
	}

	unsized := New(Options{Models: models, Prefs: Prefs{ChatModel: "vendor/unsized"}})
	if tokens, known := unsized.ContextWindow("talk"); known || tokens != 0 {
		t.Fatalf("a model the catalog cannot size = %d tokens, known %v; want absence", tokens, known)
	}

	unknown := New(Options{Models: models, Prefs: Prefs{ChatModel: "vendor/never-heard-of-it"}})
	if _, known := unknown.ContextWindow("talk"); known {
		t.Fatalf("a slug the catalog does not carry reported a known window")
	}

	// A visitor commander holds no catalog at all, and every capability that
	// would have asked a provider degrades to the recorded preference. There is
	// no recorded preference for how big a window is, so it degrades to silence.
	visitor := New(Options{Prefs: Prefs{ChatModel: "vendor/sized"}})
	if tokens, known := visitor.ContextWindow("talk"); known || tokens != 0 {
		t.Fatalf("a commander with no catalog = %d tokens, known %v; want absence", tokens, known)
	}

	// An empty slot names no model, which is a third way of knowing nothing and
	// must not be answered with the fallback list's first entry.
	empty := New(Options{Models: models})
	if _, known := empty.ContextWindow("voice"); known {
		t.Fatalf("an unchosen slot reported a known window")
	}
}

// TestTheContextWindowFollowsTheSlotsLiveModel locks the half of the figure
// that is not journaled: the numerator is history and cannot move, so a model
// switch has to move the denominator or the gauge keeps measuring the turn
// against the window of a model that is no longer answering.
func TestTheContextWindowFollowsTheSlotsLiveModel(t *testing.T) {
	models := seatCatalog(t, []map[string]any{
		{"id": "vendor/small", "context_length": 8192},
		{"id": "vendor/large", "context_length": 1000000},
	})
	commander := New(Options{Models: models, Prefs: Prefs{ChatModel: "vendor/small"}})
	if tokens, _ := commander.ContextWindow("talk"); tokens != 8192 {
		t.Fatalf("talk window = %d; want the small model's 8192", tokens)
	}
	commander.prefs.ChatModel = "vendor/large"
	if tokens, known := commander.ContextWindow("talk"); !known || tokens != 1000000 {
		t.Fatalf("talk window after a switch = %d, known %v; want the large model's", tokens, known)
	}
}
