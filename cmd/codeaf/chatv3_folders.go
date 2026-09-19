package main

// Logical folders at the v3 door: one collections store per process, handed to
// the session as Config.Folders. wsapi.Service is the typed wrapper the
// contract names; this worktree does not have that package yet, so the door
// talks to workspace.Store through the session.Folders interface. Integrate
// replaces [workspaceFolders] with wsapi.Open once service lands.
//
// tui3.Options.Folders is owned by the TUI lane and is not a field on this
// worktree. [attachSurfaceFolders] is the assignment site; it is a no-op until
// that field exists.

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

func openV3Folders() session.Folders {
	return newWorkspaceFolders(home.Join("v3", "collections.db"))
}

func attachSurfaceFolders(options *tui3.Options, folders session.Folders) {
	if options == nil || folders == nil {
		return
	}
	// BIND GAP: assign options.Folders when the TUI lane adds that field.
	// *wsapi.Service (or this wrapper) is what it should hold. First-message
	// order is TUI-owned; wiring only makes AddPlacement available.
}

// workspaceFolders implements session.Folders against today's workspace.Store.
// List of a missing database is empty and does not create the file. A write
// opens it. Move is add-then-remove until workspace.Move exists.
type workspaceFolders struct {
	path  string
	mu    sync.Mutex
	store *workspace.Store
}

func newWorkspaceFolders(path string) *workspaceFolders {
	return &workspaceFolders{path: path}
}

func (f *workspaceFolders) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.store == nil {
		return nil
	}
	err := f.store.Close()
	f.store = nil
	return err
}

func (f *workspaceFolders) open() (*workspace.Store, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.store != nil {
		return f.store, nil
	}
	store, err := workspace.Open(f.path)
	if err != nil {
		return nil, err
	}
	f.store = store
	return store, nil
}

func (f *workspaceFolders) List(ctx context.Context) ([]session.FolderRef, error) {
	if _, err := os.Stat(f.path); errors.Is(err, os.ErrNotExist) {
		return []session.FolderRef{}, nil
	} else if err != nil {
		return nil, err
	}
	store, err := f.open()
	if err != nil {
		return nil, err
	}
	cols, err := store.Collections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]session.FolderRef, 0, len(cols))
	for _, col := range cols {
		out = append(out, session.FolderRef{ID: col.ID, Name: col.Name})
	}
	return out, nil
}

func (f *workspaceFolders) File(ctx context.Context, collectionID, conversationID string) error {
	store, err := f.open()
	if err != nil {
		return err
	}
	return store.Add(ctx, collectionID, workspace.Ref{Kind: workspace.ConversationKind, ID: conversationID})
}

func (f *workspaceFolders) Unfile(ctx context.Context, collectionID, conversationID string) error {
	store, err := f.open()
	if err != nil {
		return err
	}
	return store.Remove(ctx, collectionID, workspace.Ref{Kind: workspace.ConversationKind, ID: conversationID})
}

func (f *workspaceFolders) Move(ctx context.Context, fromID, toID, conversationID string) error {
	store, err := f.open()
	if err != nil {
		return err
	}
	ref := workspace.Ref{Kind: workspace.ConversationKind, ID: conversationID}
	if err := store.Add(ctx, toID, ref); err != nil {
		return err
	}
	return store.Remove(ctx, fromID, ref)
}

// folderWorld is the conversation inventory wsapi.SetInventory will take:
// ids and titles from the existing session world. Wired here so integrate
// can hand it to the service without a second walk.
type folderWorld struct{}

func (folderWorld) ConversationIDs() []string {
	rows := session.ReadWorld(session.PlacesRoot()).Sessions()
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func (folderWorld) Title(id string) string {
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if row.ID == id {
			return row.Title
		}
	}
	return ""
}
