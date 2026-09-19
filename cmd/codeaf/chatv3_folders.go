package main

// Logical folders at the v3 door: one wsapi.Service per process, handed to
// the session as Config.Folders and to the surface as tui3.Options.Folders
// through [foldersAdapter]. Store-open failure leaves both nil — unavailable,
// never a fake empty graph. A missing collections.db is a working empty store
// (workspace.Open creates the blank file; schema waits for the first write).

import (
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/wsapi"
)

func collectionsPath() string {
	return home.Join("v3", "collections.db")
}

func openV3FolderService() *wsapi.Service {
	svc, err := wsapi.Open(collectionsPath())
	if err != nil {
		return nil
	}
	svc.SetInventory(folderWorld{})
	return svc
}

func openV3Folders() session.Folders {
	return newSessionFolders(openV3FolderService())
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
