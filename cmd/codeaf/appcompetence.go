package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/competence"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// THE COMPETENCE MAP HAS ONE READER, and both doors go through it: the app's
// competence.map and competence.evidence, and `codeaf competence --json`. The
// wire shapes are appwire's; this file only translates the ledger's answer
// into them, so the CLI and the app can never print two different maps.

// competenceReadWindow bounds one read. A ledger is read once per question and
// the bound is generous; a read that takes longer than this is a machine in
// trouble, and saying so beats a window that never draws.
const competenceReadWindow = 10 * time.Second

// competenceReader is the ledger as this process's home holds it, with the
// session's answers to "is this folder still worked in" and "is this source
// still there". It makes no model call and writes nothing.
func competenceReader() competence.Reader {
	return competence.Reader{
		PlaceAdded:  competencePlaces(),
		SourceGone:  session.CheckedSourceGone,
		SourceTitle: session.CheckedSourceTitle,
	}
}

// competencePlaces answers whether codeaf still has a conversation in a
// folder. It looks in that folder's own project bucket — one directory list per
// place, never a walk of every conversation — and finds a conversation folder
// nobody deleted. A folder that IS a conversation's own working folder (under
// the projects root) is worked in while it exists. It creates nothing.
func competencePlaces() func(string) bool {
	root := session.PlacesRoot()
	return func(place string) bool {
		place = filepath.Clean(place)
		if strings.HasPrefix(place, root+string(filepath.Separator)) {
			_, err := os.Stat(place)
			return err == nil
		}
		entries, err := os.ReadDir(filepath.Join(root, encodeWorkspace(place)))
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, encodeWorkspace(place), entry.Name(), teams.ConversationDeletedFile)); os.IsNotExist(err) {
				return true
			}
		}
		return false
	}
}

// readCompetenceMap is competence.map, for any door.
func readCompetenceMap(reader competence.Reader, model, scope string) (appwire.CompetenceMap, error) {
	ctx, done := context.WithTimeout(context.Background(), competenceReadWindow)
	defer done()
	read, err := reader.Map(ctx, model, scope)
	if err != nil {
		return appwire.CompetenceMap{}, competenceError(err)
	}
	return wireCompetenceMap(read), nil
}

// readCompetenceEvidence is competence.evidence, for any door.
func readCompetenceEvidence(reader competence.Reader, p appwire.CompetenceEvidenceParams) (appwire.CompetenceEvidence, error) {
	ctx, done := context.WithTimeout(context.Background(), competenceReadWindow)
	defer done()
	page, err := reader.Evidence(ctx, p.Model, p.Scope, competence.Outcome(p.Outcome), p.Cursor, p.Limit)
	if err != nil {
		return appwire.CompetenceEvidence{}, competenceError(err)
	}
	return wireCompetenceEvidence(page), nil
}

// competenceError is the wire's word for a read that did not answer.
func competenceError(err error) error {
	switch {
	case errors.Is(err, competence.ErrUnknownScope):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	case errors.Is(err, competence.ErrBadCursor):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	case errors.Is(err, competence.ErrHistoryLimit):
		// UNAVAILABLE, NOT PARTIAL: the ledger's own words are the person's.
		return appwire.Errorf(appwire.CodeEngine, "checked-result history exceeds this bounded read; competence unavailable")
	case errors.Is(err, competence.ErrUnreadableHistory):
		return appwire.Errorf(appwire.CodeEngine, "checked-result history has an unreadable row; competence unavailable")
	case errors.Is(err, context.DeadlineExceeded):
		return appwire.Errorf(appwire.CodeEngine, "competence read timed out")
	}
	return err
}

func (b *appBackend) CompetenceMap(p appwire.CompetenceMapParams) (appwire.CompetenceMap, error) {
	return readCompetenceMap(competenceReader(), p.Model, p.Scope)
}

func (b *appBackend) CompetenceEvidence(p appwire.CompetenceEvidenceParams) (appwire.CompetenceEvidence, error) {
	return readCompetenceEvidence(competenceReader(), p)
}

func wireCompetenceMap(read competence.Map) appwire.CompetenceMap {
	out := appwire.CompetenceMap{
		Model: read.Model, Scope: read.Scope,
		Models:  make([]appwire.CompetenceModel, 0, len(read.Models)),
		Filters: make([]appwire.CompetenceFilter, 0, len(read.Filters)),
		Groups:  make([]appwire.CompetenceGroup, 0, len(read.Groups)),
		Totals:  appwire.CompetenceTotals{Scopes: read.Totals.Scopes, Results: read.Totals.Results},
		Policy: appwire.CompetencePolicy{
			StaleAfterDays: read.Policy.StaleAfterDays, StrongAtLeast: read.Policy.StrongAtLeast,
			WeakAtMost: read.Policy.WeakAtMost, MinResults: read.Policy.MinResults,
		},
		ReadAt: read.ReadAt, Truncated: read.Truncated,
		Omitted: appwire.CompetenceOmitted{Scopes: read.Omitted.Scopes, Filters: read.Omitted.Filters, Models: read.Omitted.Models},
	}
	for _, model := range read.Models {
		out.Models = append(out.Models, appwire.CompetenceModel{ID: model.ID, Results: model.Results, LastAt: model.LastAt})
	}
	for _, filter := range read.Filters {
		out.Filters = append(out.Filters, appwire.CompetenceFilter{
			ID: filter.ID, Kind: filter.Kind, Path: filter.Path,
			PlaceKind: filter.PlaceKind, Removed: filter.Removed, Words: filter.Words,
		})
	}
	for _, group := range read.Groups {
		scopes := make([]appwire.CompetenceScope, 0, len(group.Scopes))
		for _, scope := range group.Scopes {
			wire := appwire.CompetenceScope{
				ID: scope.ID,
				Place: appwire.CompetencePlace{
					ID: scope.Place.ID, Path: scope.Place.Path,
					PlaceKind: scope.Place.Kind, Removed: scope.Place.Removed,
				},
				Work:    appwire.CompetenceWork{ID: scope.Work.ID, Words: scope.Work.Words},
				Class:   string(scope.Class),
				Results: scope.Results, Passed: scope.Passed, Failed: scope.Failed, Undone: scope.Undone,
				LastAt: scope.LastAt,
			}
			if !scope.Since.IsZero() {
				since := scope.Since
				wire.Since = &since
			}
			scopes = append(scopes, wire)
		}
		out.Groups = append(out.Groups, appwire.CompetenceGroup{Class: string(group.Class), Scopes: scopes})
	}
	return out
}

func wireCompetenceEvidence(page competence.EvidencePage) appwire.CompetenceEvidence {
	out := appwire.CompetenceEvidence{
		Model: page.Model, Scope: page.Scope, NextCursor: page.NextCursor,
		Counts: appwire.CompetenceCounts{All: page.Counts.All, Passed: page.Counts.Passed, Failed: page.Counts.Failed, Undone: page.Counts.Undone},
		Items:  make([]appwire.CompetenceItem, 0, len(page.Items)),
	}
	for _, item := range page.Items {
		wire := appwire.CompetenceItem{
			ID: item.ID, Title: item.Title, At: item.At, Outcome: string(item.Outcome),
			Source: appwire.CompetenceSource{
				Kind: item.Source.Kind, Agent: item.Source.Agent, Turn: item.Source.Turn, Title: item.SourceTitle,
				ID: item.Source.Agent,
			},
			SourceMissing: item.SourceMissing,
			Checks:        make([]appwire.CompetenceCheck, 0, len(item.Checks)),
		}
		// A TASK IS NAMED BY ITS WIRE ID, the one tasks.get and task.changed carry.
		if item.Source.Kind == competence.SourceTask {
			if number, err := strconv.ParseUint(item.Source.Task, 10, 64); err == nil {
				wire.Source.ID = appTaskID(item.Source.Agent, number)
			}
		}
		for _, check := range item.Checks {
			wire.Checks = append(wire.Checks, appwire.CompetenceCheck{Command: check.Command, Outcome: string(check.Outcome)})
		}
		if item.Check != nil {
			wire.Check = &appwire.CompetenceCheck{Command: item.Check.Command, Outcome: string(item.Check.Outcome)}
		}
		if !item.UndoneAt.IsZero() {
			undone := item.UndoneAt
			wire.UndoneAt = &undone
		}
		out.Items = append(out.Items, wire)
	}
	return out
}
