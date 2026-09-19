package wsapi

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// RootSnapshot is parentless collections plus inventory conversations that have
// no membership. ROOT IS VIRTUAL: nothing here is stored as a collection row.
func (s *Service) RootSnapshot(ctx context.Context) (RootView, error) {
	if err := s.ready(ctx); err != nil {
		return RootView{}, err
	}
	collections, err := s.store.Collections(ctx)
	if err != nil {
		return RootView{}, wrapStoreError(err)
	}
	folders, err := s.parentlessFolders(ctx, collections)
	if err != nil {
		return RootView{}, err
	}
	unfiled, err := s.unfiledPlacements(ctx)
	if err != nil {
		return RootView{}, err
	}
	revision, err := rootRevision(ctx, s.store)
	if err != nil {
		return RootView{}, err
	}
	return RootView{Folders: folders, Unfiled: unfiled, Revision: revision}, nil
}

// FolderSnapshot is one folder and its direct placements, with AlsoIn filled
// from every other collection that holds the same ref.
func (s *Service) FolderSnapshot(ctx context.Context, id string) (Folder, []Placement, error) {
	if err := s.ready(ctx); err != nil {
		return Folder{}, nil, err
	}
	collection, err := s.lookup(ctx, id)
	if err != nil {
		return Folder{}, nil, err
	}
	folder, err := s.folderFrom(ctx, collection)
	if err != nil {
		return Folder{}, nil, err
	}
	members, err := s.store.Members(ctx, id)
	if err != nil {
		return Folder{}, nil, wrapStoreError(err)
	}
	placements, err := s.placements(ctx, id, members)
	if err != nil {
		return Folder{}, nil, err
	}
	return folder, placements, nil
}

// PlacementsOf lists every folder that currently holds ref.
func (s *Service) PlacementsOf(ctx context.Context, ref workspace.Ref) ([]Folder, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	parents, err := s.store.CollectionsFor(ctx, ref)
	if err != nil {
		return nil, wrapStoreError(err)
	}
	folders := make([]Folder, 0, len(parents))
	for _, parent := range parents {
		folder, err := s.folderFrom(ctx, parent)
		if err != nil {
			return nil, err
		}
		folders = append(folders, folder)
	}
	return folders, nil
}

func (s *Service) parentlessFolders(ctx context.Context, collections []workspace.Collection) ([]Folder, error) {
	folders := make([]Folder, 0)
	for _, collection := range collections {
		parents, err := s.store.CollectionsFor(ctx, collectionRef(collection.ID))
		if err != nil {
			return nil, wrapStoreError(err)
		}
		if len(parents) > 0 {
			continue
		}
		folder, err := s.folderFrom(ctx, collection)
		if err != nil {
			return nil, err
		}
		folders = append(folders, folder)
	}
	return folders, nil
}

func (s *Service) unfiledPlacements(ctx context.Context) ([]Placement, error) {
	unfiled := make([]Placement, 0)
	if s.inv == nil {
		return unfiled, nil
	}
	for _, id := range s.inv.ConversationIDs() {
		ref := workspace.Ref{Kind: workspace.ConversationKind, ID: id}
		parents, err := s.store.CollectionsFor(ctx, ref)
		if err != nil {
			return nil, wrapStoreError(err)
		}
		if len(parents) > 0 {
			continue
		}
		unfiled = append(unfiled, Placement{Ref: ref, Title: s.inv.Title(id)})
	}
	return unfiled, nil
}

func (s *Service) folderFrom(ctx context.Context, collection workspace.Collection) (Folder, error) {
	parents, err := s.store.CollectionsFor(ctx, collectionRef(collection.ID))
	if err != nil {
		return Folder{}, wrapStoreError(err)
	}
	parentIDs := make([]string, 0, len(parents))
	for _, parent := range parents {
		parentIDs = append(parentIDs, parent.ID)
	}
	count, err := s.memberCount(ctx, collection.ID)
	if err != nil {
		return Folder{}, err
	}
	lifecycle := collection.Lifecycle
	if lifecycle == "" {
		lifecycle = workspace.LifecycleActive
	}
	return Folder{
		ID:          collection.ID,
		Name:        collection.Name,
		Purpose:     collection.Purpose,
		Lifecycle:   lifecycle,
		Revision:    collection.Revision,
		ParentIDs:   parentIDs,
		MemberCount: count,
	}, nil
}

func (s *Service) memberCount(ctx context.Context, id string) (int, error) {
	ids := map[string]struct{}{}
	if err := s.collectConversations(ctx, id, map[string]struct{}{}, ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// collectConversations walks descendants once. MEMBER COUNTS ARE UNIQUE
// CONVERSATION IDS, NOT GRAPH PATHS: a chat reachable through two nested
// folders is still one member.
func (s *Service) collectConversations(ctx context.Context, id string, walked, ids map[string]struct{}) error {
	if _, seen := walked[id]; seen {
		return nil
	}
	walked[id] = struct{}{}
	members, err := s.store.Members(ctx, id)
	if err != nil {
		return wrapStoreError(err)
	}
	for _, ref := range members {
		switch ref.Kind {
		case workspace.ConversationKind:
			ids[ref.ID] = struct{}{}
		case workspace.CollectionKind:
			if err := s.collectConversations(ctx, ref.ID, walked, ids); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) placements(ctx context.Context, collectionID string, members []workspace.Ref) ([]Placement, error) {
	out := make([]Placement, 0, len(members))
	for _, ref := range members {
		also, err := s.alsoIn(ctx, collectionID, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, Placement{
			CollectionID: collectionID,
			Ref:          ref,
			Title:        s.titleOf(ctx, ref),
			AlsoIn:       also,
		})
	}
	return out, nil
}

func (s *Service) alsoIn(ctx context.Context, collectionID string, ref workspace.Ref) ([]string, error) {
	parents, err := s.store.CollectionsFor(ctx, ref)
	if err != nil {
		return nil, wrapStoreError(err)
	}
	names := make([]string, 0)
	for _, parent := range parents {
		if parent.ID == collectionID {
			continue
		}
		names = append(names, parent.Name)
	}
	return names, nil
}

func (s *Service) titleOf(ctx context.Context, ref workspace.Ref) string {
	if ref.Kind == workspace.CollectionKind {
		collection, err := s.lookup(ctx, ref.ID)
		if err == nil {
			return collection.Name
		}
		return ""
	}
	if ref.Kind == workspace.ConversationKind && s.inv != nil {
		return s.inv.Title(ref.ID)
	}
	return ""
}

func (s *Service) lookup(ctx context.Context, id string) (workspace.Collection, error) {
	collections, err := s.store.Collections(ctx)
	if err != nil {
		return workspace.Collection{}, wrapStoreError(err)
	}
	for _, collection := range collections {
		if collection.ID == id {
			return collection, nil
		}
	}
	return workspace.Collection{}, wrapStoreError(workspace.ErrNotFound)
}

func collectionRef(id string) workspace.Ref {
	return workspace.Ref{Kind: workspace.CollectionKind, ID: id}
}

func conversationRef(id string) workspace.Ref {
	return workspace.Ref{Kind: workspace.ConversationKind, ID: id}
}
