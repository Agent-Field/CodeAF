package resident

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The digest had a branch for every kind of learning except the largest one: a
// whole way of working, distilled from jobs already done, appeared in the
// arrival brief and nowhere in the retrospective line. It reads the journal
// exactly like every other branch — which is why the forging is journaled at
// all — and it says it in the product's own words.
func TestTheRetrospectiveDigestSaysWhenAWayOfWorkingWasLearned(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "room", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	after := reconciler.latestEventSeq()
	if _, err := graph.RecordCraftForged(store.CraftForged{
		Name: "release-notes", Commit: "abc1234", Because: "write the 0.4 release notes",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordCraftForged(store.CraftForged{
		Name: "fetch-pr-context", Commit: "def5678", Refined: true,
	}); err != nil {
		t.Fatal(err)
	}
	reconciler.postRetrospectiveDigest(after)

	digest := ""
	messages, err := graph.Messages("room", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.HasPrefix(message.Body, "· reflected —") {
			digest = message.Body
		}
	}
	if digest == "" {
		t.Fatalf("no digest was posted; messages = %+v", messages)
	}
	if !strings.Contains(digest, "2 new ways of working") {
		t.Fatalf("the digest does not count them: %q", digest)
	}
	for _, detail := range []string{
		"⚒ release-notes · from write the 0.4 release notes",
		"⚒ fetch-pr-context · better than before",
	} {
		if !strings.Contains(digest, detail) {
			t.Fatalf("the digest is missing %q:\n%s", detail, digest)
		}
	}
	// The line a person reads never says craft, workflow or commit.
	for _, jargon := range []string{"craft", "workflow", "commit", "abc1234"} {
		if strings.Contains(strings.ToLower(digest), jargon) {
			t.Fatalf("the digest speaks the machinery's language (%q):\n%s", jargon, digest)
		}
	}
}

// The event is written where the version is: one forging, one journal entry,
// carrying the name, the commit and whether it replaced something that already
// existed. Without it the digest has no interval to read the learning off.
func TestForgingAWayOfWorkingJournalsTheMoment(t *testing.T) {
	graph := openStore(t)
	repo := openCraftRepo(t)
	if _, err := graph.TouchSeen("tui", "forge", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	distill := func(context.Context, string, string, bool) ([]Learned, error) {
		return []Learned{{Craft: &CraftCandidate{Name: "release-notes", YAML: releaseNotesFile}}}, nil
	}
	reconciler := New(graph, nil, nil).
		WithDistiller(distill).
		WithCraftMind(NewCraftMind(repo, repo.Dir(), nil, nil))
	forgeJob(t, graph, reconciler, "notes", "write the release notes since v1.2", false)

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	forged := make([]store.CraftForged, 0, 1)
	for _, event := range events {
		if event.Kind != store.EventCraftForged {
			continue
		}
		var payload store.CraftForged
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		forged = append(forged, payload)
	}
	if len(forged) != 1 {
		t.Fatalf("the journal holds %d forgings, want one", len(forged))
	}
	if forged[0].Name != "release-notes" || forged[0].Commit == "" || forged[0].Refined {
		t.Fatalf("forged = %+v", forged[0])
	}
	if !strings.Contains(forged[0].Because, "write the release notes since v1.2") {
		t.Fatalf("the forging lost the evidence it came from: %+v", forged[0])
	}
}

// Nothing learned is nothing said. A digest that announced an empty interval
// would be the resident talking about itself for no reason.
func TestNoForgingMeansNoDigestLine(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "room", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	after := reconciler.latestEventSeq()
	reconciler.postRetrospectiveDigest(after)

	messages, err := graph.Messages("room", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.HasPrefix(message.Body, "· reflected —") {
			t.Fatalf("a digest was posted about nothing: %q", message.Body)
		}
	}
}
