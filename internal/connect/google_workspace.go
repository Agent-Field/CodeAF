package connect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const googleScope = "https://www.googleapis.com/auth/"

type workspaceGoogle struct {
	google
	source string
	write  bool
}

func (g workspaceGoogle) Service() Service {
	scope := ""
	switch g.source {
	case "gmail":
		scope = "gmail.readonly"
		if g.write {
			scope = "gmail.compose"
		}
	case "calendar":
		scope = "calendar.events.readonly"
		if g.write {
			scope = "calendar.events"
		}
	case "drive":
		scope = "drive.readonly"
		if g.write {
			scope = "drive.file"
		}
	}
	id := "google-workspace-" + g.source
	if scope == "" {
		return Service{}
	}
	if g.write {
		id += "-write"
	}
	return Service{ID: id, Name: "Google Workspace", Auth: AuthBrowser, Scopes: []string{"openid", googleScope + scope}}
}

// The OpenID subject binds independently granted writes to the read account.
func (g workspaceGoogle) identity(ctx context.Context, client *http.Client) (string, error) {
	var identity struct {
		Subject string `json:"sub"`
	}
	err := getJSON(ctx, client, "https://openidconnect.googleapis.com/v1/userinfo", &identity)
	if err != nil || identity.Subject == "" {
		return "", errors.New("Google Workspace: The account could not be identified. Try connecting again.")
	}
	return identity.Subject, nil
}

func (g workspaceGoogle) Account(context.Context, *http.Client) (string, error) {
	return "", nil
}

func (m *Manager) workspace(g workspaceGoogle) (*Manager, error) {
	switch g.source {
	case "gmail", "calendar", "drive":
	default:
		return nil, errors.New("choose Gmail, Calendar, or Drive")
	}
	creds, err := m.credential("google")
	if err != nil {
		return nil, err
	}
	return &Manager{store: m.store, plugs: []Plug{g},
		creds: map[string]ClientCredential{g.Service().ID: creds}}, nil
}

// BeginWorkspaceAuth reuses the browser flow with an independent grant per
// source and purpose, so app consent cannot change the chat's Google powers.
func (m *Manager) BeginWorkspaceAuth(ctx context.Context, source string, write bool) (*Flow, error) {
	g := workspaceGoogle{source: source, write: write}
	manager, err := m.workspace(g)
	if err != nil {
		return nil, err
	}
	return manager.BeginAuth(ctx, g.Service().ID, "")
}

// WorkspaceClient returns the renewed client and its opaque grant generation.
func (m *Manager) WorkspaceClient(ctx context.Context, source string, write bool) (*http.Client, string, string, error) {
	g := workspaceGoogle{source: source, write: write}
	manager, err := m.workspace(g)
	if err != nil {
		return nil, "", "", err
	}
	entry, ok := manager.standing(g)
	if !ok || entry.GrantID == "" || entry.Subject == "" {
		return nil, "", "", fmt.Errorf("connect Google %s for this operation", source)
	}
	if write {
		read := workspaceGoogle{source: source}
		reader, readErr := m.workspace(read)
		if readErr != nil {
			return nil, "", "", readErr
		}
		if base, ok := reader.standing(read); !ok || entry.Subject == "" || base.Subject != entry.Subject {
			return nil, "", "", errors.New("Connect write access with the same Google account as read access.")
		}
	}
	client, err := manager.client(ctx, g, entry)
	return client, entry.GrantID, entry.Subject, err
}

func googleAuthError(err error) error {
	if strings.Contains(err.Error(), "admin_policy_enforced") {
		return errors.New("Google Workspace: Your organization blocked this app. Ask your administrator to approve it or use your organization's OAuth client.")
	}
	if strings.Contains(err.Error(), "org_internal") {
		return errors.New("Google Workspace: This OAuth client is restricted to its organization. Use that organization's account or ask for the right OAuth client.")
	}
	return errors.New("Google Workspace: Google could not complete sign-in. Try connecting again.")
}
