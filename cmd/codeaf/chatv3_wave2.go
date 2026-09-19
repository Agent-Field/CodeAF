package main

// Wave 2 door adapters: folder guidance and hybrid search ride the same
// wsapi.Service the folders tool already opened. Nil folders leave both
// seams absent rather than a dummy that claims the workspace was checked.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/wsapi"
)

func v3HybridOf(folders session.Folders) session.HybridSearcher {
	svc := folderServiceOf(folders)
	if svc == nil {
		return nil
	}
	return folderHybrid{svc: svc}
}

func v3GuidanceOf(folders session.Folders) session.GuidanceSource {
	svc := folderServiceOf(folders)
	if svc == nil {
		return nil
	}
	return folderGuidance{svc: svc}
}

func folderServiceOf(folders session.Folders) *wsapi.Service {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil {
		return nil
	}
	return wrapped.svc
}

type folderHybrid struct{ svc *wsapi.Service }

func (h folderHybrid) HybridMessages(ctx context.Context, query, sessionID, exclude string, limit int) ([]session.SearchCandidate, error) {
	if h.svc == nil {
		return nil, nil
	}
	hits, err := h.svc.SearchEvidence(ctx, wsapi.SearchQuery{
		Query: query, SessionID: sessionID, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]session.SearchCandidate, 0, len(hits))
	for _, hit := range hits {
		if exclude != "" && hit.SessionID == exclude {
			continue
		}
		out = append(out, session.SearchCandidate{
			Hit: store.MessageHit{
				SessionID: hit.SessionID,
				Body:      hit.Passage,
			},
			ScoreKind: hit.ScoreKind,
			Degraded:  hit.Degraded,
		})
	}
	return out, nil
}

type folderGuidance struct{ svc *wsapi.Service }

func (g folderGuidance) Effective(ctx context.Context, conversationID string) (session.EffectiveGuidance, error) {
	if g.svc == nil {
		return session.EffectiveGuidance{}, nil
	}
	loaded, err := g.svc.EffectiveGuidance(ctx, conversationID)
	if err != nil {
		return session.EffectiveGuidance{}, err
	}
	items := make([]session.GuidanceItem, 0, len(loaded.Items))
	for _, item := range loaded.Items {
		items = append(items, session.GuidanceItem{
			ScopeID: item.ScopeID, Name: item.Name, Text: item.Text,
			Origin: item.Origin, Actor: item.Actor, SourceRef: item.SourceRef,
			Revision: item.Revision,
		})
	}
	revs := make([]session.GuidanceRev, 0, len(loaded.Snapshot.Guidance))
	for _, row := range loaded.Snapshot.Guidance {
		revs = append(revs, session.GuidanceRev{ScopeID: row.ScopeID, Revision: row.Revision})
	}
	return session.EffectiveGuidance{
		Items: items,
		Snapshot: session.GuidanceSnapshot{
			RootRevision: loaded.Snapshot.RootRevision,
			Guidance:     revs,
			Conflict:     loaded.Conflict,
		},
		Conflict: loaded.Conflict,
	}, nil
}
