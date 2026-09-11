package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// GoverningReader is intentionally unable to create, edit, arm, or run work.
// ApplicableScope remains the standing owner's single applicability decision.
type GoverningReader interface {
	ApplicableScope(string, string, map[string]int) ([]standing.Item, error)
}

// Governing keeps the originating scope when execution changes directories.
// OrganizationRef supplies work identity separately; folder navigation grants
// no governing scope without an explicit placement in the organization owner.
type Governing struct {
	Reader    GoverningReader
	Workspace string
	SessionID string
	Owners    []workspace.Ref
}

// governingLocked snapshots the read capability before delegation. Callers hold
// a.mu so the owning conversation cannot change midway through this snapshot.
func (a *Agent) governingLocked() *Governing {
	var g Governing
	if a.config.Governing != nil {
		g = *a.config.Governing
		g.Owners = append([]workspace.Ref(nil), g.Owners...)
	} else {
		g.Workspace, g.SessionID = a.standingPlaceLocked()
		if a.config.Standing != nil && a.config.Standing.Store != nil {
			g.Reader = a.config.Standing.Store
		}
	}
	if owner := a.organizationSourceLocked(); owner.Kind != "" {
		found := false
		for _, existing := range g.Owners {
			if existing == owner {
				found = true
				break
			}
		}
		if !found {
			g.Owners = append(g.Owners, owner)
		}
	}
	if g.Reader == nil && a.config.Organization == nil {
		return nil
	}
	return &g
}

// governingItemsLocked resolves only explicit work placements and their chosen
// ancestors. A missing organization database is a valid unorganized workspace;
// an unreadable existing database is a failure, never evidence of no rules.
func (a *Agent) governingItemsLocked() ([]standing.Item, error) {
	g := a.governingLocked()
	if g == nil || g.Reader == nil {
		return nil, nil
	}
	collections := map[string]int{}
	if o := a.config.Organization; o != nil {
		store, err := o.open(false)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if store != nil {
			defer store.Close()
			for _, ref := range a.organizationTargetsLocked() {
				places, err := store.GoverningCollections(context.Background(), ref)
				if err != nil {
					return nil, err
				}
				nearestDepths(collections, places)
			}
		}
	}
	a.governingCollections = collections
	return g.Reader.ApplicableScope(g.Workspace, g.SessionID, collections)
}

// nearestDepths folds governing folders into the depth map the standing owner
// reads, keeping each folder's nearest path when several reach it.
func nearestDepths(collections map[string]int, places []workspace.GoverningCollection) {
	for _, place := range places {
		if depth, exists := collections[place.ID]; !exists || place.Depth < depth {
			collections[place.ID] = place.Depth
		}
	}
}

// GoverningRules is the ONE reading of which of the orders that apply to a
// piece of work are rules over it: the holds that carry words. A turn's
// <standing> block and a firing's report check read it here, the card that
// proposes ongoing work quotes it before the yes, and `aforge standing show`
// prints it — four readers of one question, so they cannot come to disagree
// about what governs.
func GoverningRules(items []standing.Item) []standing.Item {
	var rules []standing.Item
	for _, item := range items {
		if item.When.Kind == standing.WhenHold && strings.TrimSpace(item.Prompt()) != "" {
			rules = append(rules, item)
		}
	}
	return rules
}

// workOrganizationRefLocked keeps repair/checking runs attached to the task
// being evaluated even when those runs have no writable task controller.
func (a *Agent) workOrganizationRefLocked(id uint64) workspace.Ref {
	root := a.config.rootSession
	if root == "" {
		root = a.sessionID()
	}
	if root == "" || root == "unfiled" {
		return workspace.Ref{}
	}
	return workspace.Ref{Kind: workspace.TaskKind, ID: fmt.Sprint(id), SessionID: root}
}
