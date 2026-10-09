package session

// THE LIVE PROOF of the places door. It drives internal/placegraph's real
// Recommender through [Agent.AskPlaces] against a REAL model, so the contract
// the recommender checks every answer against is tested against what a model
// actually says, not what a fixture says it says. The raw answer is logged,
// because the failure this exists to catch is an answer that did not fit.
//
// It is OPT-IN and costs a fraction of a cent:
//
//	CODEAF_LIVE_PLACES=1 go test ./internal/session -run LivePlaces -v
//	CODEAF_LIVE_PLACES=1 CODEAF_LIVE_MODEL=deepseek/deepseek-v4.1-flash go test ...
//
// EVERY ROLE IS PINNED TO THE ONE MODEL, and the model that answered is
// asserted, so a fall-through to another rung fails the test instead of
// passing on a model nobody chose.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func livePlacesAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	if os.Getenv("CODEAF_LIVE_PLACES") == "" {
		t.Skip("set CODEAF_LIVE_PLACES=1 to spend real model calls on this")
	}
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("no OPENROUTER_API_KEY")
	}
	model := os.Getenv("CODEAF_LIVE_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4.1-flash"
	}
	agent, err := New(Config{
		Workspace:   t.TempDir(),
		Model:       model,
		APIKey:      key,
		BaseURL:     "https://openrouter.ai/api/v1",
		OneModel:    true,
		RolesSource: func(string) (string, bool) { return model, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.Close() })
	return agent, model
}

// liveAsker is the Asker the desktop bridge builds, with the raw answer and
// the answering model kept for the log and the assertion.
type liveAsker struct {
	agent  *Agent
	mu     sync.Mutex
	models []string
}

func (l *liveAsker) ask(t *testing.T) placegraph.Asker {
	return func(ctx context.Context, req placegraph.ModelRequest) (string, error) {
		answer, err := l.agent.AskPlaces(ctx, req)
		t.Logf("%s asked on %q:\n%s\n→ %q (err %v)", req.Role, answer.Model, req.User, answer.Text, err)
		l.mu.Lock()
		l.models = append(l.models, answer.Model)
		l.mu.Unlock()
		return answer.Text, err
	}
}

func livePlacesRig(t *testing.T) (*placegraph.Store, *placegraph.Recommender, *liveAsker, string) {
	agent, model := livePlacesAgent(t)
	dir := t.TempDir()
	store, err := placegraph.Open(placegraph.Options{Path: filepath.Join(dir, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := placegraph.OpenLedger(filepath.Join(dir, "places-ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	asker := &liveAsker{agent: agent}
	rec := &placegraph.Recommender{Store: store, Ledger: ledger, Ask: asker.ask(t)}
	return store, rec, asker, model
}

func TestLivePlacesFilesAChatIntoTheRightPlace(t *testing.T) {
	store, rec, asker, model := livePlacesRig(t)
	release, _, err := store.CreatePlace(placegraph.NewPlace{Name: "Release pipeline"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreatePlace(placegraph.NewPlace{Name: "Kitchen renovation"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	chat := placegraph.ChatEvidence{ChatID: "c1", Title: "Release code-signing timeout in CI",
		FirstMessage: "Our release pipeline's code-signing step keeps timing out in CI before we publish the release build.", Replies: 1}
	offer, err := rec.FileChat(ctx, chat, []placegraph.ChatEvidence{chat})
	if err != nil {
		t.Fatalf("the real model's answer was refused: %v", err)
	}
	if offer == nil || offer.PlaceID != release.ID || offer.Basis != placegraph.BasisModel {
		t.Fatalf("offered %+v", offer)
	}
	assertOnlyModel(t, asker, model, 1)
}

func TestLivePlacesSuggestsAPlaceForAGroup(t *testing.T) {
	_, rec, asker, model := livePlacesRig(t)
	var library []placegraph.ChatEvidence
	for i, title := range []string{
		"Garden drip irrigation plan emitters clogging",
		"Garden drip irrigation plan watering in July",
		"Garden drip irrigation plan timer morning or evening",
		"Garden drip irrigation plan pressure regulator",
		"Garden drip irrigation plan tubing size",
	} {
		library = append(library, placegraph.ChatEvidence{ChatID: "g" + string(rune('1'+i)), Title: title, Replies: 1})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	open, err := rec.Organize(ctx, library)
	if err != nil {
		t.Fatalf("the real model's answer was refused: %v", err)
	}
	if len(open) != 1 || (open[0].Kind != placegraph.ProposalCreate && open[0].Kind != placegraph.ProposalMove) || len(open[0].ChatIDs) != 5 {
		t.Fatalf("offered %+v", open)
	}
	t.Logf("offered %q (%s, %d%%)", open[0].Name, open[0].Kind, open[0].Confidence)
	assertOnlyModel(t, asker, model, 1)
}

func assertOnlyModel(t *testing.T, asker *liveAsker, model string, calls int) {
	t.Helper()
	asker.mu.Lock()
	defer asker.mu.Unlock()
	if len(asker.models) != calls {
		t.Fatalf("%d model calls, want %d", len(asker.models), calls)
	}
	for _, m := range asker.models {
		if m != model {
			t.Fatalf("answered by %q, not the pinned %q", m, model)
		}
	}
	if _, ok := roles.TierOf(roles.RolePlaceFile); !ok {
		t.Fatal("placefile is not registered")
	}
}
