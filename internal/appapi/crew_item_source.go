package appapi

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type crewFactoryItems interface {
	List() ([]factory.Item, error)
}
type factoryCrewItemSource struct{ items crewFactoryItems }

func (source factoryCrewItemSource) CrewItemDone(ctx context.Context, linked CrewItem) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	items, err := source.items.List()
	if err != nil {
		return false, err
	}
	for _, item := range items {
		matches := linked.Ref == factory.JobRef(item) || linked.URL != "" && linked.URL == item.URL
		if item.PullRequest != nil && linked.URL != "" && linked.URL == item.PullRequest.URL {
			matches = true
		}
		if matches {
			return factory.JobState(item) == "done", nil
		}
	}
	return false, nil
}
func (s *Server) crewItemSource(home string) (CrewItemSource, error) {
	if s.options.CrewItems != nil {
		return s.options.CrewItems, nil
	}
	root := filepath.Join(home, "v3", "factory")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	st, err := store.Open(root)
	if err != nil {
		return nil, err
	}
	return factoryCrewItemSource{items: st}, nil
}
