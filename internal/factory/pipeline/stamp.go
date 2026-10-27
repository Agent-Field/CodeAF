package pipeline

import (
	"reflect"
	"slices"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// StampSteps gives a job that has never run the steps f runs for its kind, so
// the pipeline a repository's file says now is the one a start runs, whichever
// door starts it: an issue is stamped with the default steps when it arrives,
// and the file may name others. Only a job still on those arrival steps is
// stamped. One that ran, one whose steps were changed for it
// ([factory.Item.Adapted]) and one made with steps of its own keep theirs. It
// reports whether it changed the steps.
func StampSteps(it *factory.Item, f File) bool {
	if it.Stream != nil || len(it.Adapted) > 0 || !sameSteps(it.Stages, factory.DefaultRecipe().For(it.Kind)) {
		return false
	}
	want := f.Recipe().For(it.Kind)
	if len(want) == 0 || sameSteps(it.Stages, want) {
		return false
	}
	it.Stages = factory.CopyStages(want)
	return true
}

// sameSteps compares two step lists as the store keeps them, where an empty
// proof list and a missing one are the same.
func sameSteps(a, b []factory.Stage) bool {
	return slices.EqualFunc(a, b, func(x, y factory.Stage) bool {
		px, py := x.Proof, y.Proof
		x.Proof, y.Proof = nil, nil
		return reflect.DeepEqual(x, y) && slices.Equal(px, py)
	})
}
