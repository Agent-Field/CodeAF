package main

import (
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// chatWindow is everything one process owns over a durable graph: the store it
// reads, the conversation it is looking at, and where both live on disk. It is
// what `aforge do` opens before it builds a brain, and the brain reads its
// three fields rather than resolving any of them a second time.
type chatWindow struct {
	path     string
	database string
	dir      string
	graph    *store.Store
	session  string
}

func openChatWindow(path, database, requestedSession string) (*chatWindow, error) {
	graph, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	// The session is resolved here rather than at flag-definition time because
	// only the journal knows which conversation this is. Everything downstream
	// reads the resolved id; nothing reads the flag again.
	session, err := resolveChatSession(graph, requestedSession)
	if err != nil {
		_ = graph.Close()
		return nil, err
	}
	// One tidying pass per launch, after the room is chosen so the chosen room
	// is never among the ones taken back.
	groomChatRooms(graph, session)
	return &chatWindow{
		path: path, database: database, dir: filepath.Dir(path),
		graph: graph, session: session,
	}, nil
}

func (w *chatWindow) close() {
	if w != nil && w.graph != nil {
		_ = w.graph.Close()
	}
}
