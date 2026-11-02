package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// configureOwnedChat gives foreground scratch a home without changing the
// borrowed project where the person intentionally reads and edits their files.
func configureOwnedChat(config *Config) error {
	if config.InTask || config.Place.Dir == "" {
		return nil
	}
	meta, err := LoadMeta(config.Place.Dir)
	if err != nil {
		return err
	}
	if err := validateWorkspacePolicy(meta.WorkspacePolicy); err != nil {
		return err
	}
	if !WorkspaceOwnershipEnabled(config.Place) {
		return nil
	}
	if err := os.MkdirAll(config.Place.Dir, 0700); err != nil {
		return err
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return err
	}
	var chat workspaceidentity.Record
	err = withMetaLock(config.Place.Dir, func() error {
		meta, err := LoadMeta(config.Place.Dir)
		if err != nil {
			return err
		}
		if err := validateWorkspacePolicy(meta.WorkspacePolicy); err != nil {
			return err
		}
		if meta.WorkspacePolicy == nil {
			meta.WorkspacePolicy = DefaultWorkspacePolicy()
		}
		if meta.ChatURI != "" {
			chat, err = registry.Resolve(meta.ChatURI)
			if err != nil {
				return fmt.Errorf("resolve conversation identity: %w", err)
			}
			if chat.Role != workspaceidentity.Chat || chat.Path != canonicalPath(config.Place.Dir) {
				return fmt.Errorf("conversation identity is not bound to this chat")
			}
			return saveMetaLocked(config.Place.Dir, meta)
		}
		chat, err = registry.Register(workspaceidentity.Chat, canonicalPath(config.Place.Dir))
		if err != nil {
			return err
		}
		meta.ChatURI = chat.URI
		if meta.ID == "" {
			meta.ID = config.Place.ID()
		}
		if meta.Workspace == "" {
			meta.Workspace = config.Workspace
		}
		return saveMetaLocked(config.Place.Dir, meta)
	})
	if err != nil {
		return err
	}
	config.WorkspaceURI = chat.URI
	scratch := filepath.Join(workspaceRuntimeRoot(chat.URI), "temp")
	var cache string
	cacheOwner := ""
	if !config.Place.Owned && ownershipPlainMode(config.Workspace) != TaskModeReference {
		project, err := registry.Register(workspaceidentity.Project, groundRoot(config.Workspace))
		if err != nil {
			return err
		}
		cache, err = ownedProjectCache(registry, project.URI)
		if err != nil {
			return err
		}
		cacheOwner = project.URI
	} else {
		cache, err = ownedCache(registry, "", home.Join("workspace-caches", "profile", "cache"))
		if err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		path  string
		role  workspaceidentity.Role
		owner string
	}{{scratch, workspaceidentity.Temp, chat.URI}, {cache, workspaceidentity.Cache, cacheOwner}, {config.Place.Logs(), workspaceidentity.Log, chat.URI}} {
		if err := os.MkdirAll(entry.path, 0700); err != nil {
			return err
		}
		if _, err := registry.Register(entry.role, canonicalPath(entry.path), workspaceidentity.Links{OwnerURI: entry.owner}); err != nil {
			return fmt.Errorf("record conversation storage: %w", err)
		}
	}
	config.Seat = executor.WithEnvironment(config.seat(), []string{"TMPDIR=" + scratch, "TMP=" + scratch, "TEMP=" + scratch, "XDG_CACHE_HOME=" + cache, "CODEAF_WORKSPACE_URI=" + chat.URI})
	return nil
}

// AdoptChatWorkspaceIdentity binds sealed conversation identity after an accepted
// takeover. Ordinary session opening only resolves known identities.
func AdoptChatWorkspaceIdentity(place Place) error {
	if !WorkspaceOwnershipEnabled(place) {
		return nil
	}
	meta, err := LoadMeta(place.Dir)
	if err != nil || meta.ChatURI == "" {
		return err
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return err
	}
	_, err = registry.Adopt(workspaceidentity.Record{URI: meta.ChatURI, Role: workspaceidentity.Chat}, canonicalPath(place.Dir), workspaceidentity.Links{})
	if err != nil {
		return fmt.Errorf("adopt conversation identity: %w", err)
	}
	return nil
}
