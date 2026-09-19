package main

// foldersAdapter maps *wsapi.Service onto tui3.Folders. The two types are not
// interchangeable: service returns wsapi.Folder and takes workspace.Ref, and
// the surface speaks exported TUI DTOs with string ids. *wsapi.Service does
// not satisfy tui3.Folders.
//
// Person-facing TUI verbs stamp OriginPerson here. The session `folders` tool
// has its own seam ([sessionFolders]) and stamps OriginOrganizer at ingress.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
)

var _ tui3.Folders = (*foldersAdapter)(nil)
var _ session.Folders = (*sessionFolders)(nil)
var _ wsapi.Inventory = folderWorld{}

type foldersAdapter struct {
	svc  *wsapi.Service
	jobs *workspace.Store
}

func newFoldersAdapter(svc *wsapi.Service, jobs *workspace.Store) tui3.Folders {
	if svc == nil {
		return nil
	}
	return &foldersAdapter{svc: svc, jobs: jobs}
}

func (a *foldersAdapter) RootSnapshot(ctx context.Context) (tui3.FolderRoot, error) {
	root, err := a.svc.RootSnapshot(ctx)
	if err != nil {
		return tui3.FolderRoot{}, err
	}
	return tui3.FolderRoot{
		Folders:  folderViewsOf(root.Folders),
		Unfiled:  folderPlacementsOf(root.Unfiled),
		Revision: root.Revision,
	}, nil
}

func (a *foldersAdapter) FolderSnapshot(ctx context.Context, id string) (tui3.FolderView, []tui3.FolderPlacement, error) {
	folder, members, err := a.svc.FolderSnapshot(ctx, id)
	if err != nil {
		return tui3.FolderView{}, nil, err
	}
	return folderViewOf(folder), folderPlacementsOf(members), nil
}

func (a *foldersAdapter) CreateFolder(ctx context.Context, name string) (tui3.FolderView, error) {
	folder, err := a.svc.CreateFolder(ctx, name)
	if err != nil {
		return tui3.FolderView{}, err
	}
	return folderViewOf(folder), nil
}

func (a *foldersAdapter) RenameFolder(ctx context.Context, id, name string) error {
	return a.svc.RenameFolder(ctx, id, name)
}

func (a *foldersAdapter) AddPlacement(ctx context.Context, collectionID, refID string) error {
	return a.svc.AddPlacement(ctx, collectionID, conversationRef(refID), personProvenance(""))
}

func (a *foldersAdapter) AddFolderPlacement(ctx context.Context, parentID, childFolderID string) error {
	return a.svc.AddPlacement(ctx, parentID, collectionRef(childFolderID), personProvenance(""))
}

func (a *foldersAdapter) RemovePlacement(ctx context.Context, collectionID, refID string) error {
	ref := conversationRef(refID)
	why, _ := a.svc.WhyHere(ctx, collectionID, ref)
	if err := a.svc.RemovePlacement(ctx, collectionID, ref, personProvenance("")); err != nil {
		return err
	}
	a.suppressOrganizerPlacement(ctx, collectionID, ref, why)
	return nil
}

func (a *foldersAdapter) MovePlacement(ctx context.Context, fromID, toID, refID string) error {
	return a.svc.MovePlacement(ctx, fromID, toID, conversationRef(refID), personProvenance(""))
}

func (a *foldersAdapter) WhyHere(ctx context.Context, collectionID, refID string) (tui3.FolderWhy, error) {
	why, err := a.svc.WhyHere(ctx, collectionID, conversationRef(refID))
	if err != nil {
		return tui3.FolderWhy{}, err
	}
	event := why.Event
	return tui3.FolderWhy{
		Origin:   event.Origin,
		Reason:   event.Reason,
		Actor:    event.Actor,
		Evidence: event.Evidence,
		At:       event.At,
	}, nil
}

// sessionFolders is the tool seam: List/File/Unfile/Move wrapping the same
// service. OriginOrganizer is forced here so a caller of Config.Folders cannot
// claim person; the tool also stamps that origin at ingress.
type sessionFolders struct {
	svc  *wsapi.Service
	jobs *workspace.Store
}

func newSessionFolders(svc *wsapi.Service) session.Folders {
	if svc == nil {
		return nil
	}
	return &sessionFolders{svc: svc}
}

func (f *sessionFolders) Close() error {
	if f == nil {
		return nil
	}
	if f.jobs != nil {
		_ = f.jobs.Close()
		f.jobs = nil
	}
	if f.svc == nil {
		return nil
	}
	return f.svc.Close()
}

func (f *sessionFolders) List(ctx context.Context) ([]session.FolderRef, error) {
	root, err := f.svc.RootSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]session.FolderRef, 0, len(root.Folders))
	seen := map[string]struct{}{}
	for _, folder := range root.Folders {
		if err := f.collectFolder(ctx, folder, seen, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (f *sessionFolders) collectFolder(ctx context.Context, folder wsapi.Folder, seen map[string]struct{}, out *[]session.FolderRef) error {
	if _, ok := seen[folder.ID]; ok {
		return nil
	}
	seen[folder.ID] = struct{}{}
	*out = append(*out, session.FolderRef{ID: folder.ID, Name: folder.Name})
	_, members, err := f.svc.FolderSnapshot(ctx, folder.ID)
	if err != nil {
		return err
	}
	for _, place := range members {
		if place.Ref.Kind != workspace.CollectionKind {
			continue
		}
		child, _, err := f.svc.FolderSnapshot(ctx, place.Ref.ID)
		if err != nil {
			return err
		}
		if err := f.collectFolder(ctx, child, seen, out); err != nil {
			return err
		}
	}
	return nil
}

func (f *sessionFolders) File(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error {
	return f.svc.AddPlacement(ctx, collectionID, ref, organizerProvenance(p))
}

func (f *sessionFolders) Unfile(ctx context.Context, collectionID, conversationID string, p workspace.Provenance) error {
	return f.svc.RemovePlacement(ctx, collectionID, conversationRef(conversationID), organizerProvenance(p))
}

func (f *sessionFolders) Move(ctx context.Context, fromID, toID, conversationID string, p workspace.Provenance) error {
	return f.svc.MovePlacement(ctx, fromID, toID, conversationRef(conversationID), organizerProvenance(p))
}

func conversationRef(id string) workspace.Ref {
	return workspace.Ref{Kind: workspace.ConversationKind, ID: id}
}

func collectionRef(id string) workspace.Ref {
	return workspace.Ref{Kind: workspace.CollectionKind, ID: id}
}

func personProvenance(reason string) workspace.Provenance {
	return workspace.Provenance{Origin: workspace.OriginPerson, Reason: reason, Actor: "person"}
}

func organizerProvenance(p workspace.Provenance) workspace.Provenance {
	p.Origin = workspace.OriginOrganizer
	return p
}

func folderViewOf(folder wsapi.Folder) tui3.FolderView {
	return tui3.FolderView{
		ID:          folder.ID,
		Name:        folder.Name,
		Purpose:     folder.Purpose,
		Lifecycle:   folder.Lifecycle,
		Revision:    folder.Revision,
		ParentIDs:   folder.ParentIDs,
		MemberCount: folder.MemberCount,
	}
}

func folderViewsOf(folders []wsapi.Folder) []tui3.FolderView {
	out := make([]tui3.FolderView, 0, len(folders))
	for _, folder := range folders {
		out = append(out, folderViewOf(folder))
	}
	return out
}

func folderPlacementOf(place wsapi.Placement) tui3.FolderPlacement {
	return tui3.FolderPlacement{
		CollectionID: place.CollectionID,
		RefID:        place.Ref.ID,
		Title:        place.Title,
		Kind:         string(place.Ref.Kind),
		AlsoIn:       place.AlsoIn,
	}
}

func folderPlacementsOf(places []wsapi.Placement) []tui3.FolderPlacement {
	out := make([]tui3.FolderPlacement, 0, len(places))
	for _, place := range places {
		out = append(out, folderPlacementOf(place))
	}
	return out
}

func (a *foldersAdapter) suppressOrganizerPlacement(ctx context.Context, collectionID string, ref workspace.Ref, why wsapi.Why) {
	if a == nil || a.jobs == nil || why.Event.Origin != workspace.OriginOrganizer {
		return
	}
	sum := sha256.Sum256([]byte(why.Event.Evidence))
	_ = a.jobs.Suppress(ctx, collectionID, ref, hex.EncodeToString(sum[:]), personProvenance("removed"))
}
