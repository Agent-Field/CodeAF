package main

// Logical folders at the v3 door: one wsapi.Service per process, handed to
// the session as Config.Folders and to the surface as tui3.Options.Folders
// through [foldersAdapter]. Store-open failure leaves both nil — unavailable,
// never a fake empty graph. A missing collections.db is a working empty store
// (workspace.Open creates the blank file; schema waits for the first write).

import (
	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsdiscover"
)

func collectionsPath() string {
	return home.Join("v3", "collections.db")
}

func discoveryPath() string {
	return home.Join("v3", "discovery.db")
}

func openV3FolderService() *wsapi.Service {
	svc, _ := openV3FolderServiceWith(nil)
	return svc
}

func openV3Folders() session.Folders {
	return openV3FoldersWith(nil)
}

// openV3FoldersWith binds *workspace.Store through wsapi.Open and a real
// discovery.db through the RoleEmbed adapter. A nil embedder is delayed, never
// a dummy production fallback. A failed discovery Open leaves SearchEvidence
// delayed; it does not take folders down or invent an empty success.
func openV3FoldersWith(embedder embed.Embedder) session.Folders {
	svc, adapter := openV3FolderServiceWith(embedder)
	if svc == nil {
		return nil
	}
	return &sessionFolders{svc: svc, disc: adapter}
}

func openV3FolderServiceWith(embedder embed.Embedder) (*wsapi.Service, *discoveryAdapter) {
	svc, err := wsapi.Open(collectionsPath())
	if err != nil {
		return nil, nil
	}
	svc.SetInventory(folderWorld{})
	adapter := openDiscoveryAdapter(embedder)
	if adapter != nil {
		svc.SetDiscoverer(adapter)
	}
	return svc, adapter
}

func openDiscoveryAdapter(embedder embed.Embedder) *discoveryAdapter {
	store, err := wsdiscover.Open(discoveryPath())
	if err != nil {
		return nil
	}
	return newDiscoveryAdapter(store, embedder)
}

// bindSessionEmbedder hands the launched session's RoleEmbed pin to the
// process-wide discovery adapter. Nil stays the labelled delayed path.
func bindSessionEmbedder(folders session.Folders, embedder embed.Embedder) {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil {
		return
	}
	if wrapped.disc != nil {
		wrapped.disc.SetEmbedder(embedder)
		return
	}
	adapter := openDiscoveryAdapter(embedder)
	if adapter == nil || wrapped.svc == nil {
		if adapter != nil {
			_ = adapter.Close()
		}
		return
	}
	wrapped.disc = adapter
	wrapped.svc.SetDiscoverer(adapter)
}

func attachSurfaceFolders(options *tui3.Options, folders session.Folders) {
	if options == nil {
		return
	}
	options.Folders = surfaceFolders(folders)
}

func surfaceFolders(folders session.Folders) tui3.Folders {
	wrapped, ok := folders.(*sessionFolders)
	if !ok || wrapped == nil || wrapped.svc == nil {
		return nil
	}
	return newFoldersAdapter(wrapped.svc)
}

// folderWorld is the conversation inventory wsapi.SetInventory takes: ids and
// titles from the existing session world.
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
