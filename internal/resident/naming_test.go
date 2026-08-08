package resident

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func oneJob() *store.Subtree {
	return &store.Subtree{Nodes: []store.NodeSpec{{ID: "task-1", Brief: "the whole compiled goal", Stage: 1}}}
}

// A job's display name comes back with the goal it names, from the one call
// that has already read the whole ask. The separate naming pass was a model
// round-trip of its own in front of every job — measured at 222 to 330 prompt
// tokens for a five-token answer — and nothing downstream waits on the label.
func TestACompiledTitleCostsNoSecondCall(t *testing.T) {
	asked := 0
	reconciler := &Reconciler{title: func(context.Context, string) (string, error) {
		asked++
		return "Named the slow way", nil
	}}
	subtree := oneJob()
	reconciler.titleSubtree(context.Background(), subtree, Compiled{
		Goal: "the whole compiled goal", Title: "Interval overlap fix",
	})
	if got := subtree.Nodes[0].Title; got != "Interval overlap fix" {
		t.Fatalf("job title = %q, want the compiler's own name", got)
	}
	if asked != 0 {
		t.Fatalf("the naming pass ran %d times beside a compile that already named the job", asked)
	}
}

// The fallback is the whole reason the pass survives: a compiler that says
// nothing, or a caller with no compiler at all, still gets a named job.
func TestAnUnnamedCompileFallsBackToTheNamingPass(t *testing.T) {
	reconciler := &Reconciler{title: func(context.Context, string) (string, error) {
		return "Named the slow way", nil
	}}
	subtree := oneJob()
	reconciler.titleSubtree(context.Background(), subtree, Compiled{Goal: "the whole compiled goal"})
	if got := subtree.Nodes[0].Title; got != "Named the slow way" {
		t.Fatalf("job title = %q, want the naming pass's answer", got)
	}

	// Neither source answering leaves the title empty, which every rail
	// already reads as "show the first line of the brief".
	failing := &Reconciler{title: func(context.Context, string) (string, error) {
		return "", errors.New("the namer is down")
	}}
	subtree = oneJob()
	failing.titleSubtree(context.Background(), subtree, Compiled{Goal: "the whole compiled goal"})
	if got := subtree.Nodes[0].Title; got != "" {
		t.Fatalf("a failed naming pass invented %q", got)
	}

	bare := &Reconciler{}
	subtree = oneJob()
	bare.titleSubtree(context.Background(), subtree, Compiled{Goal: "the whole compiled goal"})
	if got := subtree.Nodes[0].Title; got != "" {
		t.Fatalf("a reconciler with no namer produced %q", got)
	}
}

// A long name is clipped to what a rail can show, from either source.
func TestALongCompiledTitleIsClippedLikeAnyOther(t *testing.T) {
	reconciler := &Reconciler{}
	subtree := oneJob()
	reconciler.titleSubtree(context.Background(), subtree, Compiled{
		Goal: "the whole compiled goal", Title: strings.Repeat("long name ", 12),
	})
	got := subtree.Nodes[0].Title
	if !strings.HasSuffix(got, "…") || len(got) > 52 {
		t.Fatalf("compiled title reached the rail unclipped at %d bytes: %q", len(got), got)
	}
}
