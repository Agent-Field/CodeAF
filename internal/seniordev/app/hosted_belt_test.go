//go:build !windows

package app

import (
	"slices"
	"testing"
)

func TestHostedRunDoesNotOfferUnanswerableQuestion(t *testing.T) {
	runtime := newRuntime(t.TempDir(), nil)
	t.Cleanup(runtime.Close)
	if slices.Contains(runtime.registry.IDs(), "question") {
		t.Fatal("hosted run offered question without anyone to answer it")
	}
}
