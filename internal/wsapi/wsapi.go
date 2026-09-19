// Package wsapi is the typed workspace service the TUI and session tools call.
//
// Storage stays in internal/workspace. This package projects membership into
// folder snapshots, unique conversation counts, and why-here, and it never
// imports session, tui3, provider, run, or wsdiscover. Open binds *workspace.Store
// directly so provenance, atomic Move, WhyHere, and Events are the real store's.
// Wave 2 adds InstructFolder, EffectiveGuidance, SearchEvidence, a typed
// ActionPlan, and SuppressPlacement. Discovery is an injected interface.
// Wave 3 adds CoordinateSelected, ManageFolder, Deliver, InviteToDiscussion,
// CreateDiscussion, and OpenConflictDiscussion. Collaboration is an injected
// interface — this package never imports wscollab. Wave 4 adds IssueGrant, LaunchOrJoin, and the
// inspect/steer/pause/stop/observe work doors. Execution is an injected
// interface — this package never imports wsexec. A missing store door,
// collaborator, or executor leaves those methods absent rather than returning
// a dummy success.
package wsapi

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Inventory is the conversation world injected from cmd/codeaf. Titles are
// whatever the session side already shows; this package does not open journals.
type Inventory interface {
	ConversationIDs() []string
	Title(id string) string
}

// Folder is one collection as the home panel and tools see it.
// ParentIDs empty means the folder hangs off virtual Root.
type Folder struct {
	ID, Name, Purpose, Lifecycle string
	Revision                     int
	ParentIDs                    []string
	MemberCount                  int // unique conversation IDs, not graph paths
}

// Placement is one membership edge plus the other folders that also hold it.
type Placement struct {
	CollectionID string
	Ref          workspace.Ref
	Title        string
	AlsoIn       []string // other collection names
}

// Why is the latest membership event for an edge, active or last.
type Why struct {
	Event workspace.MembershipEvent
}

// RootView is virtual Root: parentless collections and unfiled conversations.
// Root is never a stored collection and never a CLI list entry.
type RootView struct {
	Folders  []Folder
	Unfiled  []Placement
	Revision int // root_state.revision; zero on a blank store before the first write
}

// Service holds a store, an optional inventory, an optional discoverer, an
// optional collaborator, an optional executor, and a clock.
type Service struct {
	store      store
	inv        Inventory
	disc       Discoverer
	collab     Collaborator
	exec       Executor
	now        func() time.Time
	onExisting func()
	chatRev    func(string) string
}

// Open wraps workspace.Open. A corrupt, foreign, or unreadable store is an
// error: the service is not constructed, and callers must not invent an empty
// RootView. The returned service talks to *workspace.Store directly.
func Open(path string) (*Service, error) {
	inner, err := workspace.Open(path)
	if err != nil {
		return nil, err
	}
	return &Service{store: inner, now: time.Now}, nil
}

// Close releases the membership store and, if the injected discoverer
// holds a file, that file too. A missing discoverer is not an error.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	var first error
	if c, ok := s.disc.(interface{ Close() error }); ok {
		first = c.Close()
		s.disc = nil
	}
	if s.store != nil {
		if err := s.store.Close(); err != nil && first == nil {
			first = err
		}
		s.store = nil
	}
	return first
}

// SetInventory replaces the conversation world used for titles and unfiled rows.
func (s *Service) SetInventory(inv Inventory) {
	if s == nil {
		return
	}
	s.inv = inv
}

// SetDiscoverer replaces the discovery index used for SearchEvidence and
// IndexProgress. A nil discoverer is delayed, not a fake empty success.
// Replacing a discoverer that owns a file closes the previous one.
func (s *Service) SetDiscoverer(disc Discoverer) {
	if s == nil {
		return
	}
	if s.disc != nil && s.disc != disc {
		if c, ok := s.disc.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	s.disc = disc
}

// SetCollaborator injects the router used by Deliver. Nil means Deliver is
// absent, not a dummy delivered receipt. This package does not import wscollab;
// wiring binds the real router later, the same way Inventory and Discoverer land.
func (s *Service) SetCollaborator(c Collaborator) {
	if s == nil {
		return
	}
	s.collab = c
}

// SetExecutor injects the launch-or-join adapter. Nil means LaunchOrJoin is
// absent, not a dummy completed view. This package does not import wsexec;
// wiring binds the real adapter later, the same way Collaborator lands.
func (s *Service) SetExecutor(e Executor) {
	if s == nil {
		return
	}
	s.exec = e
}

// SetReactiveOptIn is called after Organize existing chats successfully
// enqueues. Wiring writes workspace.reactive=on; tests may leave it nil.
func (s *Service) SetReactiveOptIn(fn func()) {
	if s == nil {
		return
	}
	s.onExisting = fn
}

// SetChatRevision supplies the latest source revision for Organize this chat.
// Nil means the coalesce key is conversationID plus an empty revision.
func (s *Service) SetChatRevision(fn func(string) string) {
	if s == nil {
		return
	}
	s.chatRev = fn
}

// Workspace is the real collections.db handle when Open bound *workspace.Store.
// Tests that inject a fake get nil: job enqueue is then absent, not a second
// SQLite pool on the same file.
func (s *Service) Workspace() *workspace.Store {
	if s == nil {
		return nil
	}
	inner, _ := s.store.(*workspace.Store)
	return inner
}

// CreateFolder makes a logical folder. It is a child of Root until placed.
func (s *Service) CreateFolder(ctx context.Context, name string) (Folder, error) {
	if err := s.ready(ctx); err != nil {
		return Folder{}, err
	}
	created, err := s.store.Create(ctx, name)
	if err != nil {
		return Folder{}, wrapStoreError(err)
	}
	return s.folderFrom(ctx, created)
}

// CreateFolderIn is visible New folder. Empty parentID is Root. Non-empty
// creates and nests in one store transaction so a parent miss cannot leave a
// Root orphan. A fake membership-only store refuses rather than succeeding.
func (s *Service) CreateFolderIn(ctx context.Context, name, parentID string) (Folder, error) {
	if err := s.ready(ctx); err != nil {
		return Folder{}, err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return s.CreateFolder(ctx, name)
	}
	inner, ok := s.store.(*workspace.Store)
	if !ok || inner == nil {
		return Folder{}, errOrganizeUnwired
	}
	created, err := inner.CreateIn(ctx, name, parentID)
	if err != nil {
		return Folder{}, wrapStoreError(err)
	}
	return s.folderFrom(ctx, created)
}

// RenameFolder changes a folder's display name. Membership is untouched.
func (s *Service) RenameFolder(ctx context.Context, id, name string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.Rename(ctx, id, name))
}

// AddPlacement files a ref in a folder. A second call with the same edge is
// success: membership is binary, not counted.
func (s *Service) AddPlacement(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.AddWith(ctx, collectionID, ref, normalize(p)))
}

// RemovePlacement drops one edge. The referenced conversation stays itself.
// AUTHORIZED WORK IS NOT CANCELLED: this is not StopWork (A16 / J29).
func (s *Service) RemovePlacement(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.RemoveWith(ctx, collectionID, ref, normalize(p)))
}

type suppressRemover interface {
	RemoveAndSuppress(ctx context.Context, id string, ref workspace.Ref, evidenceHash string, p workspace.Provenance) error
}

// RemovePlacementSuppressing detaches the edge and records the evidence hash
// in the same store transaction when the hash is set. A suppress error does
// not report a successful remove.
func (s *Service) RemovePlacementSuppressing(ctx context.Context, collectionID string, ref workspace.Ref, evidenceHash string, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	p = normalize(p)
	if remover, ok := s.store.(suppressRemover); ok {
		return wrapStoreError(remover.RemoveAndSuppress(ctx, collectionID, ref, evidenceHash, p))
	}
	if err := s.store.RemoveWith(ctx, collectionID, ref, p); err != nil {
		return wrapStoreError(err)
	}
	if evidenceHash == "" {
		return nil
	}
	return wrapStoreError(s.store.Suppress(ctx, collectionID, ref, evidenceHash, p))
}

// MovePlacement adds the destination and removes the source in the store's
// one writer transaction. Other placements of the same ref stay.
func (s *Service) MovePlacement(ctx context.Context, fromID, toID string, ref workspace.Ref, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.Move(ctx, fromID, toID, ref, normalize(p)))
}

// WhyHere is the latest event for that active or last edge.
func (s *Service) WhyHere(ctx context.Context, collectionID string, ref workspace.Ref) (Why, error) {
	if err := s.ready(ctx); err != nil {
		return Why{}, err
	}
	event, err := s.store.WhyHere(ctx, collectionID, ref)
	if err != nil {
		return Why{}, wrapStoreError(err)
	}
	return Why{Event: event}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s == nil || s.store == nil {
		return wrapStoreError(workspace.ErrInvalid)
	}
	return ctx.Err()
}

func normalize(p workspace.Provenance) workspace.Provenance {
	if p.Origin == "" {
		p.Origin = workspace.OriginPerson
	}
	return p
}
