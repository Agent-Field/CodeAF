package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestFollowPRKeepsItsStepLabel(t *testing.T) {
	for _, label := range []string{"Follow the pull request", ""} {
		it := factory.Item{Stages: []factory.Stage{{Name: factory.FollowPRStep, Label: label}}}
		for range 2 {
			factory.EnsureFollowPR(&it)
			if len(it.Stages) != 1 || it.Stages[0].Name != factory.FollowPRStep || it.Stages[0].Label != label {
				t.Fatalf("following the PR changed its step label or id: %+v", it.Stages)
			}
		}
	}
}
