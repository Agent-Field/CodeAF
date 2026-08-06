package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func spliceProject(t *testing.T, s *store.Store) {
	t.Helper()
	err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "goal", Brief: "Write the comparison report", Stage: 2, Needs: []store.Need{
			{NodeID: "part-a", Kind: store.FeedsInto},
			{NodeID: "part-b", Kind: store.FeedsInto},
		}},
		{ID: "part-a", Parent: "goal", Brief: "Fetch city A weather", Stage: 1},
		{ID: "part-b", Parent: "goal", Brief: "Fetch city B weather", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "compare the weather"})
	if err != nil {
		t.Fatalf("splice project: %v", err)
	}
}

func landNode(t *testing.T, s *store.Store, id, summary string) {
	t.Helper()
	claim, won, err := s.Claim(id, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%v err=%v", id, won, err)
	}
	if err := s.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := s.Complete(claim, summary); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

func TestNarratorSpeaksBatchedProgressCasually(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	spliceProject(t, s)

	var seen []Narration
	reconciler := New(s, nil, nil).WithNarrator(
		func(_ context.Context, narration Narration) (string, error) {
			seen = append(seen, narration)
			return "City A is in — city B is close behind.", nil
		})

	ctx := context.Background()
	if err := reconciler.Tick(ctx); err != nil {
		t.Fatalf("initial tick: %v", err)
	}

	landNode(t, s, "part-a", "City A: 21C and clear")
	if err := reconciler.Tick(ctx); err != nil {
		t.Fatalf("tick after first landing: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("narrator fired inside the debounce window: %+v", seen)
	}

	// Age the state past the debounce; the accumulated completion should now
	// be spoken as one line.
	reconciler.progress["goal"].lastPost = time.Now().Add(-2 * narrateDebounce)
	if err := reconciler.Tick(ctx); err != nil {
		t.Fatalf("tick after debounce: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("expected one narration, got %d", len(seen))
	}
	if seen[0].Goal != "compare the weather" || len(seen[0].Finished) != 1 ||
		!strings.Contains(seen[0].Finished[0], "City A: 21C and clear") {
		t.Fatalf("narration context wrong: %+v", seen[0])
	}

	messages, err := s.Messages("s1", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	var narrated *store.Message
	for i := range messages {
		if messages[i].Role == store.RoleAgent {
			narrated = &messages[i]
		}
	}
	if narrated == nil || narrated.Body != "City A is in — city B is close behind." ||
		narrated.NodeID != "goal" {
		t.Fatalf("narration not posted as the agent voice: %+v", messages)
	}
}

func TestNarratorGoesQuietWhenTheAnswerLands(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	spliceProject(t, s)

	fired := 0
	reconciler := New(s, nil, nil).WithNarrator(
		func(_ context.Context, _ Narration) (string, error) {
			fired++
			return "still going", nil
		})

	ctx := context.Background()
	if err := reconciler.Tick(ctx); err != nil {
		t.Fatalf("initial tick: %v", err)
	}
	landNode(t, s, "part-a", "done a")
	landNode(t, s, "part-b", "done b")
	if err := reconciler.Tick(ctx); err != nil {
		t.Fatalf("tick after parts: %v", err)
	}
	landNode(t, s, "goal", "The full comparison: A wins.")

	// Progress accumulated but the deliverable landed before the debounce
	// expired: the final answer owns the ending and the narration is dropped.
	reconciler.progress["goal"].lastPost = time.Now().Add(-2 * narrateDebounce)
	if err := reconciler.Tick(ctx); err != nil {
		t.Fatalf("final tick: %v", err)
	}
	if fired != 0 {
		t.Fatalf("narrator spoke after the answer landed")
	}
	if _, ok := reconciler.progress["goal"]; ok {
		t.Fatalf("terminal job should drop its progress state")
	}
}

func TestContinuityEdgesCarryThePriorResult(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// An earlier job, landed: its summary carries the artifact the next job
	// starts from.
	if err := s.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "podcast", Brief: "Make the podcast", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "make me a podcast"}); err != nil {
		t.Fatalf("splice prior job: %v", err)
	}
	landNode(t, s, "podcast", "Podcast done. Files:\n/abs/path/prehistoric_men_podcast.mp3")

	command, err := s.RequestCommand(store.Command{
		SessionID: "s1", Kind: store.CommandSplice,
		Instruction: "improve the pacing and add background music",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	compile := func(_ context.Context, _, graphContext string) (Compiled, error) {
		if !strings.Contains(graphContext, "asked: make me a podcast") ||
			!strings.Contains(graphContext, "prehistoric_men_podcast.mp3") {
			t.Fatalf("compiler cannot see the prior job: %q", graphContext)
		}
		return Compiled{
			Goal:     "Improve the podcast pacing and add music",
			Scale:    "task",
			BuildsOn: []string{"podcast", "no-such-job"},
		}, nil
	}
	if err := New(s, compile, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	newID := fmt.Sprintf("task-%d", command.Seq)
	digests, err := s.DependencyDigests(newID, store.MaxDigestBytes)
	if err != nil {
		t.Fatalf("digests: %v", err)
	}
	joined := strings.Join(digests, "\n")
	if !strings.Contains(joined, "prehistoric_men_podcast.mp3") {
		t.Fatalf("prior result does not flow to the new job: %q", joined)
	}

	ready, err := s.Ready(10)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	found := false
	for _, node := range ready {
		if node.ID == newID {
			found = true
		}
	}
	if !found {
		t.Fatalf("new job should be ready — its dependency is already done: %+v", ready)
	}
}

func TestLandedJobsFoldOutOfTheActiveView(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	spliceProject(t, s)

	reconciler := New(s, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("initial tick: %v", err)
	}
	landNode(t, s, "part-a", "a done")
	landNode(t, s, "part-b", "b done")
	landNode(t, s, "goal", "Comparison written. Files:\n/tmp/ws/report.md")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick after landing: %v", err)
	}

	root, ok, err := s.Node("goal")
	if err != nil || !ok {
		t.Fatalf("goal node: ok=%v err=%v", ok, err)
	}
	if !root.FoldRoot || root.FoldDigest == "" {
		t.Fatalf("landed job should fold into a digest root: %+v", root)
	}
	if len(root.FoldPointers) != 1 || root.FoldPointers[0] != "/tmp/ws/report.md" {
		t.Fatalf("artifact pointer lost in fold: %+v", root.FoldPointers)
	}

	active, err := s.ActiveNodes()
	if err != nil {
		t.Fatalf("active nodes: %v", err)
	}
	for _, node := range active {
		if node.ID == "part-a" || node.ID == "part-b" {
			t.Fatalf("folded children still in the active view: %+v", active)
		}
	}
}
