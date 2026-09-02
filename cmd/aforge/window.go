package main

import (
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// chatWindow is everything one process owns over a durable graph: the store it
// reads and the conversation it is looking at. It is what `aforge do` opens
// before it builds a brain, and the brain reads these three fields rather than
// resolving any of them a second time.
//
// IT CARRIES ONE PATH, NOT TWO. It used to keep the store's resolved path
// beside the spelling a `--db` flag was given in, because the v2 chat door
// parsed that flag and `~/store.db` and `/home/…/store.db` are different
// strings for one file. That door is gone (#329), the only caller left resolves
// the path before it opens anything, and a second field that always held the
// same value was one more thing to keep in step.
type chatWindow struct {
	path    string
	graph   *store.Store
	session string
}

func openChatWindow(path, requestedSession string) (*chatWindow, error) {
	graph, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	// The session is resolved here rather than by the caller because only the
	// journal knows which conversation this is. Everything downstream reads the
	// resolved id; nothing reads what was asked for again.
	session, err := resolveChatSession(graph, requestedSession)
	if err != nil {
		_ = graph.Close()
		return nil, err
	}
	// One tidying pass per launch, after the room is chosen so the chosen room
	// is never among the ones taken back.
	groomChatRooms(graph, session)
	return &chatWindow{path: path, graph: graph, session: session}, nil
}

func (w *chatWindow) close() {
	if w != nil && w.graph != nil {
		_ = w.graph.Close()
	}
}
