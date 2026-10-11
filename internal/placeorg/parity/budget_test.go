package parity

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/anchors"
	"github.com/Agent-Field/codeaf/internal/placeorg/audit"
	"github.com/Agent-Field/codeaf/internal/placeorg/community"
	"github.com/Agent-Field/codeaf/internal/placeorg/dedup"
	"github.com/Agent-Field/codeaf/internal/placeorg/kind"
	"github.com/Agent-Field/codeaf/internal/placeorg/knn"
	"github.com/Agent-Field/codeaf/internal/placeorg/link"
	"github.com/Agent-Field/codeaf/internal/placeorg/localembed"
	"github.com/Agent-Field/codeaf/internal/placeorg/nest"
	"github.com/Agent-Field/codeaf/internal/placeorg/park"
	"github.com/Agent-Field/codeaf/internal/placeorg/review"
	"github.com/Agent-Field/codeaf/internal/placeorg/stream"
	"github.com/Agent-Field/codeaf/internal/placeorg/vectors"
)

func TestAGuessAtFiveHundredPlacesStaysUnderTwentyMilliseconds(t *testing.T) {
	nodes := make([]placeorg.BuiltNode, 500)
	homes := map[string]string{}
	for i := range nodes {
		id := fmt.Sprintf("p%03d", i)
		sum := make([]float32, placeorg.PrimaryDims)
		sum[i%placeorg.PrimaryDims] = 1
		nodes[i] = placeorg.BuiltNode{
			ID: id, Name: id, Depth: 1,
			Centroids: map[placeorg.SpaceID]placeorg.Centroid{
				placeorg.SpaceLocal: {Sum: sum, Count: 1},
			},
		}
		chat := fmt.Sprintf("c%03d", i)
		nodes[i].Members = []string{chat}
		homes[chat] = id
	}
	tree, err := placeorg.Assemble(nodes, homes, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := stream.New()
	s.Embed = localembed.NewHashed()
	s.Folders = map[string]string{"/work/kiln": "p000"}
	ctx := context.Background()
	if _, err := s.Guess(ctx, tree, "kiln notes for the week", "/work/kiln", ""); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got, err := s.Guess(ctx, tree, "kiln notes for the week", "/work/kiln", "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if got.Place != "p000" || got.Confidence != 100 {
		t.Fatalf("guess %+v", got)
	}
	if elapsed >= placeorg.GuessBudget {
		t.Fatalf("Guess took %s, budget %s", elapsed, placeorg.GuessBudget)
	}
}

func TestFilingFiveHundredPlacesStaysUnderFiftyMilliseconds(t *testing.T) {
	nodes := make([]placeorg.BuiltNode, 500)
	homes := map[string]string{}
	for i := range nodes {
		id := fmt.Sprintf("f%03d", i)
		sum := make([]float32, placeorg.PrimaryDims)
		sum[0] = float32(i) / 500
		nodes[i] = placeorg.BuiltNode{
			ID: id, Name: id, Depth: 1,
			Centroids: map[placeorg.SpaceID]placeorg.Centroid{
				placeorg.SpacePrimary: {Sum: sum, Count: 1},
			},
		}
		chat := fmt.Sprintf("h%03d", i)
		nodes[i].Members = []string{chat}
		homes[chat] = id
	}
	tree, err := placeorg.Assemble(nodes, homes, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := stream.New()
	vec := make([]float32, placeorg.PrimaryDims)
	vec[0] = 0.2
	arrival := placeorg.Arrival{Chat: placeorg.Chat{ID: "new"}, Vector: vec}
	if _, err := s.File(context.Background(), tree, arrival, nil); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.File(context.Background(), tree, arrival, nil); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= placeorg.FileBudget {
		t.Fatalf("File took %s, budget %s", elapsed, placeorg.FileBudget)
	}
}

func TestTenThousandChatsFitInTwoMinutesAndAHundredAndFiftyMegabytes(t *testing.T) {
	if testing.Short() {
		t.Skip("the scale run takes about a minute")
	}
	state := placeorg.NewState()
	hashed := localembed.NewHashed()
	stages := placeorg.Stages{
		Dedup: dedup.New(1), Kind: kind.New(), Anchors: anchors.Derived{},
		Vectors: vectors.New(t.TempDir()), Neighbours: knn.New(8, 0.35),
		Communities: community.New(), Nest: nest.New(), Audit: audit.Auditor{}, Park: park.New(),
		Name: numbered{}, Review: &review.Stage{}, Link: link.New(state), Stream: stream.New(),
	}
	runner := &placeorg.Runner{Stages: stages, State: state, Primary: hashed, Local: hashed}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak {
					peak = m.HeapAlloc
				}
			}
		}
	}()
	start := time.Now()
	plan, err := runner.Run(context.Background(), scaleLibrary(10_000, 8), placeorg.Options{
		Mode: placeorg.ModeRefresh, Seed: 1, Now: time.Now,
	})
	elapsed := time.Since(start)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	grew := int64(peak) - int64(before.HeapAlloc)
	t.Logf("10k chats: %v, heap grew %d MB, %d ops", elapsed.Round(time.Second), grew>>20, len(plan.Ops))
	if elapsed > 2*time.Minute {
		t.Errorf("took %v, want under 2 minutes", elapsed)
	}
	if grew > 150<<20 {
		t.Errorf("the heap grew %d MB, want under 150", grew>>20)
	}
}

func scaleLibrary(n, topics int) *placeorg.Corpus {
	c := &placeorg.Corpus{Chats: make([]placeorg.Chat, n)}
	for i := range c.Chats {
		topic := i % topics
		var words []string
		for w := 0; w < 12; w++ {
			if w%3 == 0 {
				words = append(words, fmt.Sprintf("noise%d", (i+w)%400))
			} else {
				words = append(words, fmt.Sprintf("topic%dword%d", topic, w%20))
			}
		}
		c.Chats[i] = placeorg.Chat{
			ID: fmt.Sprintf("s%05d", i), Title: fmt.Sprintf("topic%d notes", topic),
			Text: joinWords(words), Replies: 3, Created: time.Unix(int64(1_700_000_000+i*60), 0),
		}
	}
	return c
}

func joinWords(words []string) string {
	out := words[0]
	for _, w := range words[1:] {
		out += " " + w
	}
	return out
}
