package main

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

var _ appwire.CrewsOrganizer = (*appBackend)(nil)

// CrewsOrganize reads a snapshot, never the repairing teams loader or watch.
// Naming uses a conversation's existing door, including hosted Teams.Propose;
// no turn, manager or team is created to obtain a suggestion.
func (b *appBackend) CrewsOrganize(ctx context.Context, ask appwire.OrganizeAsk) (*appwire.OrganizeReply, error) {
	if err := appwire.CheckOrganizeAsk(ask); err != nil {
		return nil, err
	}
	file, _, err := teamstore.ReadSnapshot(ctx, b.crewProfile(), appwire.OverviewMaxBytes)
	if err != nil {
		return nil, appEngineError(overviewReadError(err))
	}
	agents, err := b.ListAgents("")
	if err != nil {
		return nil, appEngineError(err)
	}
	convs, teams := appOrganizeInputs(agents, file.Teams)
	out := &appwire.OrganizeReply{New: []appwire.OrganizeNew{}, Additions: []appwire.OrganizeAddition{}}
	if ask.Pass == "folders" {
		return b.organizeFolders(ctx, out, file, convs, teams)
	}
	proposer := b.organizeProposer
	if proposer == nil {
		loose := session.OrganizeUnassigned(convs, teams)
		if len(loose) == 0 {
			return out, ctx.Err()
		}
		var endRead func()
		proposer, endRead, err = b.organizeNamingDoor(loose[0])
		if err != nil {
			return nil, appOrganizeError(err)
		}
		defer endRead()
	}
	rows, res, err := session.OrganizeModel(ctx, proposer, convs, teams)
	if err != nil {
		return nil, appOrganizeError(err)
	}
	out.CostUSD, out.Model = res.CostUSD, res.Model
	for _, p := range rows {
		if p.Team == "" {
			out.New = append(out.New, appwire.OrganizeNew{Name: p.Name, Agents: p.Keys, Why: p.Reason})
		} else {
			out.Additions = append(out.Additions, appwire.OrganizeAddition{Crew: p.Team, Agents: p.Keys, Why: p.Reason})
		}
	}
	return out, ctx.Err()
}

// appOrganizeError preserves the missing-role refusal across a hosted call,
// whose existing error envelope carries a sentence rather than a Go sentinel.
func appOrganizeError(err error) error {
	if errors.Is(err, session.ErrNoNamingModel) || strings.Contains(err.Error(), session.ErrNoNamingModel.Error()) {
		return appwire.Errorf(appwire.CodeNoNamingModel, "no naming model is set")
	}
	return appEngineError(err)
}

// appOrganizeInputs adapts agent ids on a private copy of stored memberships.
func appOrganizeInputs(agents []appwire.Agent, stored []teamstore.Team) ([]session.OrganizeConversation, []teamstore.Team) {
	var convs []session.OrganizeConversation
	for _, a := range agents {
		if a.State == appwire.StateArchived || a.State == appwire.StateClosed {
			continue
		}
		convs = append(convs, session.OrganizeConversation{Key: a.ID, Title: a.Title, Folder: a.Workspace})
	}
	// The store speaks transcript keys; the app speaks opaque agent ids. Translate
	// a private copy so neither the snapshot nor the persisted memberships change.
	teams := append([]teamstore.Team(nil), stored...)
	for i := range teams {
		teams[i].Members = append([]teamstore.Member(nil), teams[i].Members...)
		for j, m := range teams[i].Members {
			teams[i].Members[j].Key = appMemberAgent(m)
		}
	}
	return convs, teams
}

// organizeFolders answers the free pass, using the store's quiet detector.
func (b *appBackend) organizeFolders(ctx context.Context, out *appwire.OrganizeReply, file *teamstore.File, convs []session.OrganizeConversation, teams []teamstore.Team) (*appwire.OrganizeReply, error) {
	loose, open := len(session.OrganizeUnassigned(convs, teams)), 0
	for _, t := range teams {
		if !t.Root && !t.Closed() {
			open++
		}
	}
	out.AgentsUnassigned, out.CrewsOpen = &loose, &open
	quiet, err := teamstore.QuietDetails(b.crewProfile(), file, b.now(), teamstore.QuietAfter)
	if err != nil {
		return nil, appEngineError(err)
	}
	if quiet == nil {
		quiet = []teamstore.QuietCrew{}
	}
	out.Quiet = &quiet
	for _, p := range session.OrganizeUnassignedFolders(convs, teams) {
		out.New = append(out.New, appwire.OrganizeNew{Name: p.Name, Folder: p.Folder, Agents: p.Keys, Why: "from the folder"})
	}
	return out, ctx.Err()
}

// organizeNamingDoor discovers ownership before interpreting the local profile.
// Joining an existing engine never boots a conversation merely to name teams.
func (b *appBackend) organizeNamingDoor(c session.OrganizeConversation) (session.TeamProposer, func(), error) {
	a, live, endRead, err := b.borrowExisting(c.Key)
	if err != nil {
		return nil, nil, err
	}
	if live != nil {
		proposer, _ := live.conn.Conv.(session.TeamProposer)
		return proposer, endRead, nil
	}
	endRead()
	conn, err := appJoinExisting(appOpenAsk{Workspace: c.Folder, Transcript: a.transcript}, nil)
	if err != nil {
		return nil, nil, err
	}
	if conn != nil {
		proposer, _ := conn.Conv.(session.TeamProposer)
		return proposer, conn.Release, nil
	}
	source, err := v3RolesSource(c.Folder, b.crewProfile())
	if err != nil {
		return nil, nil, err
	}
	resolved, err := roles.Resolve(source, roles.RoleTitle, "")
	if err != nil {
		return nil, nil, session.ErrNoNamingModel
	}
	proc, err := openAppEngineProcess()
	if err != nil {
		return nil, nil, err
	}
	settings := proc.Settings
	proposer, err := session.NewTeamProposer(session.TeamProposerConfig{Model: resolved, APIKey: settings.APIKey, BaseURL: settings.BaseURL, Sources: settings.Sources, ProfileDir: settings.ProfileDir, RolesSource: source})
	return proposer, func() {}, err
}
