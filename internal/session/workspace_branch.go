package session

import (
	"fmt"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// ForkChatWorkspaceIdentity establishes a new independent owner at the fork's
// own folder. It never rebinds the source chat's identity or imports its runs.
func ForkChatWorkspaceIdentity(place Place) error {
	if !WorkspaceOwnershipEnabled(place) {
		return nil
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return err
	}
	chat, err := registry.Register(workspaceidentity.Chat, canonicalPath(place.Dir))
	if err != nil {
		return err
	}
	return withMetaLock(place.Dir, func() error {
		meta, err := LoadMeta(place.Dir)
		if err != nil {
			return err
		}
		if meta.ID != "" && meta.ID != place.ID() {
			return fmt.Errorf("independent conversation metadata belongs to another chat")
		}
		meta.ID, meta.ChatURI = place.ID(), chat.URI
		meta.Owned = true
		meta.Workspace = filepath.Join(place.Dir, "work")
		// Forking is the one authorized identity split. Ordinary SaveMeta must
		// continue refusing replacement of an existing chat identity.
		truth := layoutOf(place.Dir).metaTruth(place.Dir)
		if truth == "" {
			return fmt.Errorf("independent conversation requires a sealed cell")
		}
		if err := writeTruth(place.Dir, truth, meta); err != nil {
			return err
		}
		return writeJSONAtomic(place.MetaPath(), meta.derived())
	})
}
