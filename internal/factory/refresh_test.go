package factory_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE SOURCE DOORS ARE ABSENT WITHOUT A SOURCE: a local seam built with no
// refetch draws no `u`, `U` or `g`.
func TestLocalSeamSourceDoorsNeedARefetch(t *testing.T) {
	_, st := openLocal(t)
	bare := factory.LocalSeam(st, time.Now())
	for _, door := range []string{"refresh", "refreshall", "open"} {
		if bare.Has(door) {
			t.Fatalf("door %s is present with no source", door)
		}
	}
	with := factory.LocalSeam(st, time.Now(), factory.WithRefetch(func(context.Context, factory.Item) error { return nil }))
	for _, door := range []string{"refresh", "refreshall", "open"} {
		if !with.Has(door) {
			t.Fatalf("door %s is missing over a source", door)
		}
	}
	if factory.FixtureSeam(time.Now()).Has("open") {
		t.Fatal("the still fixture grew a door")
	}
}

// A dry run is a mark on the context, and only a marked context carries it.
func TestDryRunIsAMarkOnTheContext(t *testing.T) {
	ctx := context.Background()
	if factory.IsDryRun(ctx) || !factory.IsDryRun(factory.DryRun(ctx)) {
		t.Fatal("the dry-run mark is wrong")
	}
}
