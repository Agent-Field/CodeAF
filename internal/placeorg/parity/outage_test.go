package parity

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// deadEmbed is an embedding door that does not answer. The organiser must
// keep going on the local space rather than crash.
type deadEmbed struct{}

func (deadEmbed) Space() placeorg.SpaceID { return placeorg.SpacePrimary }
func (deadEmbed) Dims() int               { return placeorg.PrimaryDims }
func (deadEmbed) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedding door is closed")
}

func TestADeadEmbeddingDoorStaysProvisional(t *testing.T) {
	state := placeorg.NewState()
	stages := placeorg.Stages{
		Dedup: dedup.New(placeorg.DefaultSeed), Kind: kind.New(), Anchors: anchors.Derived{},
		Vectors: vectors.New(t.TempDir()), Neighbours: knn.New(knn.DefaultK, knn.DefaultFloor),
		Communities: community.New(), Nest: nest.New(), Audit: audit.Auditor{}, Park: park.New(),
		Name: numbered{}, Review: &review.Stage{}, Link: link.New(state), Stream: stream.New(),
	}
	runner := &placeorg.Runner{Stages: stages, State: state, Primary: deadEmbed{}, Local: localembed.NewHashed()}
	plan, err := runner.Run(context.Background(), outageLibrary(), placeorg.Options{
		Mode: placeorg.ModeRefresh, Seed: placeorg.DefaultSeed, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("no plan")
	}
	if !plan.Provisional {
		t.Fatal("plan is not provisional")
	}
	if plan.Space != placeorg.SpaceHashed {
		t.Fatalf("space %s", plan.Space)
	}
	passed := 0
	for _, inv := range plan.Invariants {
		if inv.OK {
			passed++
			continue
		}
		t.Errorf("%s: %s", inv.Name, inv.Why)
	}
	if passed != 11 || len(plan.Invariants) != 11 {
		t.Fatalf("invariants %d/%d", passed, len(plan.Invariants))
	}
}

// outageLibrary is three topics of eight chats. The words repeat inside a
// topic and not across topics, so a local space still has structure, and
// none of them are the owner's subjects.
func outageLibrary() *placeorg.Corpus {
	topics := []string{"kiln", "orchard", "harbor"}
	c := &placeorg.Corpus{}
	for ti, topic := range topics {
		for i := 0; i < 8; i++ {
			var words []string
			for w := 0; w < 40; w++ {
				words = append(words, fmt.Sprintf("%s-note-%d", topic, w%6))
			}
			c.Chats = append(c.Chats, placeorg.Chat{
				ID: fmt.Sprintf("o-%d-%d", ti, i), Title: topic + " notes",
				Text: strings.Join(words, " "), Replies: 4,
				Created: time.Unix(int64(1_700_000_000+ti*100+i), 0),
			})
		}
	}
	return c
}
