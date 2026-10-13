package appapi

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type fakeCrewFactoryItems []factory.Item

func (f fakeCrewFactoryItems) List() ([]factory.Item, error) { return f, nil }
func TestCrewFactoryItemStateUsesCanonicalJobStateWithoutChangingWork(t *testing.T) {
	source := factoryCrewItemSource{items: fakeCrewFactoryItems{
		{ID: 1, Repo: "acme/api", Num: 1, URL: "https://github.com/acme/api/issues/1", State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PROpen, URL: "https://github.com/acme/api/pull/2"}},
		{ID: 2, Repo: "acme/api", Num: 3, State: factory.StateShipped},
	}}
	for _, row := range []struct {
		item CrewItem
		done bool
	}{
		{CrewItem{Ref: "api#1"}, false}, {CrewItem{URL: "https://github.com/acme/api/pull/2"}, false}, {CrewItem{Ref: "api#3"}, true}, {CrewItem{Ref: "missing"}, false},
	} {
		done, err := source.CrewItemDone(context.Background(), row.item)
		if err != nil || done != row.done {
			t.Fatalf("item=%+v done=%v err=%v", row.item, done, err)
		}
	}
}
