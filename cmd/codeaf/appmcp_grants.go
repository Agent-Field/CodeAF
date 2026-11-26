package main

import (
	"context"
	"maps"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

type appAgentPermission struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Allowed bool   `json:"allowed"`
}

type appAgentGrantRow struct {
	Agent   appAgentIdentity     `json:"agent"`
	Actions []appAgentPermission `json:"actions"`
	Data    []appAgentPermission `json:"data"`
}

func (b *appBackend) appAgentGrantProjection(app string, def store.Definition, own appAgentIdentity) ([]appAgentGrantRow, error) {
	catalog, err := b.readAppContentCatalog(context.Background(), appwire.AppContentScope{App: app, Version: def.Meta.Version})
	if err != nil {
		return nil, err
	}
	db, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	agents, err := db.MCPAgents(app)
	if err != nil {
		return nil, appDataError(err)
	}
	identities := []appAgentIdentity{own}
	for _, agent := range agents {
		identities = append(identities, appAgentIdentity{ID: agent.ID, Name: agent.Name, Harness: agent.Harness})
	}
	marksAvailable := appMCPMarksAvailable()
	result := make([]appAgentGrantRow, 0, len(identities))
	for _, identity := range identities {
		row := appAgentGrantRow{Agent: identity, Actions: []appAgentPermission{}, Data: []appAgentPermission{}}
		permission := func(group, id, label string) appAgentPermission {
			return appAgentPermission{ID: id, Label: label, Allowed: b.appAgentAllowed(app, data.AgentPrincipal{ID: identity.ID}, group, id)}
		}
		for _, view := range slices.Sorted(maps.Keys(catalog.Views)) {
			row.Data = append(row.Data, permission("data", view, view))
		}
		for _, action := range catalog.Actions {
			row.Actions = append(row.Actions, permission("actions", action.ID, action.Label))
		}
		if identity.ID == own.ID {
			row.Actions = append(row.Actions, permission("actions", "propose_change", "Propose changes"))
		} else {
			for _, screen := range def.Screens {
				if b.builds.look.ready.Load() {
					row.Data = append(row.Data, permission("data", "look/"+screen, "Look at "+screen))
				}
				if marksAvailable {
					row.Actions = append(row.Actions, permission("actions", "mark/"+screen, "Suggest a change to "+screen))
				}
			}
		}
		result = append(result, row)
	}
	return result, nil
}
