package main

import (
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// v3Organization assembles the same backend for local and hosted conversations.
// Opening a chat does not create a collection database, and memory being off
// does not remove organization. Resolution reads existing owners without waking
// them or taking over their journals.
func v3Organization(ambient *session.Standing) *session.Organization {
	path := home.Join("v3", "collections.db")
	resolver := workspaceview.Resolver{World: session.ReadHome}
	if ambient != nil {
		resolver.Standing = ambient.Store
	}
	return &session.Organization{Path: path, Resolve: resolver.Resolve}
}
