package desktopbridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAttentionOmitsAQuestionThePlaceDecided(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	graph, err := placegraph.Open(placegraph.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	place, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := graph.AddChat("chat", place.ID, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	snap, err := graph.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	ledger := session.DecideLedgerDir(path)
	if session.LedgerDecided(ledger, "chat", "consent", "7", []string{place.ID}) {
		t.Fatal("a place with no ledger counted as decided")
	}
	file, err := decide.LedgerFile(ledger, place.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file + ".lock"); !os.IsNotExist(err) {
		t.Fatal("looking up a missing ledger created its lock")
	}

	store, err := decide.Open(ledger, place.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(decide.Decision{
		ID: "taken", PlaceID: place.ID, By: place.ID, At: time.Now(),
		Command:     "go test",
		QuestionRef: decide.QuestionRef{Session: "chat", Kind: "consent", ID: "7"},
	}); err != nil {
		t.Fatal(err)
	}

	row := asking("chat", "needs your ok to run bash")
	row.Presence.Question.Full = &session.Question{ID: 7, Kind: session.QuestionConsent, Head: "needs your ok to run bash"}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
	if _, items := projectWorld(world, ledger, snap); len(items) != 0 {
		t.Fatalf("a decided question stayed in needs you: %+v", items)
	}

	row.Presence.Question.Full.ID = 8
	world.Projects[0].Sessions[0] = row
	if _, items := projectWorld(world, ledger, snap); len(items) != 1 {
		t.Fatal("a different question was hidden with the decided one")
	}

	row.Presence.Question.Full.ID = 7
	row.Presence.Question.Full.Pick = &session.Pick{Key: "1", Percent: 97, Reason: "You allowed go test here 6 times. Costly."}
	world.Projects[0].Sessions[0] = row
	if _, items := projectWorld(world, "", snap); len(items) != 1 {
		t.Fatal("confidence alone hid the question")
	}
}
