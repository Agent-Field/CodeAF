package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func appWorkstreamSourceItem(item workstream.SourceItem) appwire.WorkstreamSourceItem {
	out := appwire.WorkstreamSourceItem{ID: item.ID, WorkstreamID: item.WorkstreamID, Kind: item.Kind, Title: item.Title, Path: workstreamOptional(item.Path), Repo: workstreamOptional(item.Repo), Number: workstreamPositive(item.Number), URL: workstreamOptional(item.URL), ReadByJobs: item.ReadByJobs, Proposed: item.Proposed, Why: workstreamOptional(item.Why), AddedBy: item.AddedBy, AddedAt: item.AddedAt}
	if item.Ref != nil {
		out.Ref = &appwire.WorkstreamKeptRef{Kind: item.Ref.Kind, ID: item.Ref.ID, ChatID: item.Ref.ChatID, Factory: item.Ref.Factory}
	}
	if item.Gone {
		out.Gone = &item.Gone
	}
	return out
}

func (b *appBackend) WorkstreamSourceList(p appwire.WorkstreamSourceListParams) (appwire.WorkstreamSourceListResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamSourceListResult, workstream.Change, error) {
		items, err := s.Sources(context.Background(), p.WorkstreamID)
		out := appwire.WorkstreamSourceListResult{Sources: []appwire.WorkstreamSourceItem{}}
		for _, item := range items {
			out.Sources = append(out.Sources, appWorkstreamSourceItem(item))
		}
		return out, workstream.Change{}, err
	})
}

func (b *appBackend) WorkstreamSourceAdd(p appwire.WorkstreamSourceAddParams) (appwire.WorkstreamSourceAddResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamSourceAddResult, workstream.Change, error) {
		item, already, change, err := s.AddSource(context.Background(), p.WorkstreamID, p.SourceAdd())
		return appwire.WorkstreamSourceAddResult{Source: appWorkstreamSourceItem(item), Already: already}, change, err
	})
}

func (b *appBackend) WorkstreamSourceUpdate(p appwire.WorkstreamSourceUpdateParams) (appwire.WorkstreamSourceUpdateResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamSourceUpdateResult, workstream.Change, error) {
		item, change, err := s.UpdateSource(context.Background(), p.ID, workstream.SourcePatch{Title: p.Title, ReadByJobs: p.ReadByJobs, Keep: p.Keep})
		return appwire.WorkstreamSourceUpdateResult{Source: appWorkstreamSourceItem(item)}, change, err
	})
}

func (b *appBackend) WorkstreamSourceRemove(p appwire.WorkstreamSourceRemoveParams) (appwire.WorkstreamSourceRemoveResult, error) {
	return workstreamDo(b, p, func(s *workstream.Store) (appwire.WorkstreamSourceRemoveResult, workstream.Change, error) {
		item, change, err := s.RemoveSource(context.Background(), p.ID)
		return appwire.WorkstreamSourceRemoveResult{Source: appWorkstreamSourceItem(item)}, change, err
	})
}
