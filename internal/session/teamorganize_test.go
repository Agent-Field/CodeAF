package session

import (
	"context"
	"errors"
	"reflect"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

type organizeProposer struct {
	calls  int
	input  TeamProposalInput
	result TeamProposal
	err    error
}

func (p *organizeProposer) ProposeTeams(_ context.Context, in TeamProposalInput) (TeamProposal, error) {
	p.calls++
	p.input = in
	return p.result, p.err
}

func TestOrganizeModelReservesFolderMembersAndNames(t *testing.T) {
	convs := []OrganizeConversation{{"a", "alpha", "/src/docs"}, {"b", "beta", "/src/docs"}, {"c", "gamma", "/src/one"}, {"d", "delta", "/src/two"}, {"e", "epsilon", "/src/three"}, {"held", "assigned", "/src/one"}}
	teams := []teamstore.Team{{ID: "existing", Name: "release", Members: []teamstore.Member{{Key: "held"}}}}
	usd := 0.0123
	p := &organizeProposer{result: TeamProposal{Model: "namer", CostUSD: &usd, New: []ProposedTeam{
		{Name: "docs", Members: []string{"c", "d"}},
		{Name: "topic", Members: []string{"a", "c", "d", "held", "unknown"}, Reason: "shared topic"},
		{Name: "duplicate", Members: []string{"c", "d", "e"}},
	}, Additions: []ProposedAddition{{TeamID: "existing", Members: []string{"b", "c", "e"}, Reason: "release work"}}}}
	rows, res, err := OrganizeModel(context.Background(), p, convs, teams)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || len(p.input.Conversations) != 5 || len(p.input.Teams) != 1 || p.input.Conversations[0].Folder != "docs" {
		t.Fatalf("ask = %+v; calls %d", p.input, p.calls)
	}
	if len(rows) != 2 || rows[0].Name != "topic" || !reflect.DeepEqual(rows[0].Keys, []string{"c", "d"}) || !reflect.DeepEqual(rows[1].Keys, []string{"e"}) || rows[1].Reason != "release work" {
		t.Fatalf("rows = %+v", rows)
	}
	if res.Model != "namer" || res.CostUSD == nil || *res.CostUSD != usd {
		t.Fatalf("usage = %+v", res)
	}
	folder := OrganizeUnassignedFolders(convs, teams)
	if len(folder) != 1 || !reflect.DeepEqual(folder[0].Keys, []string{"a", "b"}) {
		t.Fatalf("folder changed: %+v", folder)
	}
}

func TestOrganizeModelErrorsAndCancellation(t *testing.T) {
	convs := []OrganizeConversation{{"a", "a", "/a"}, {"b", "b", "/b"}}
	for _, failure := range []error{ErrNoNamingModel, context.DeadlineExceeded, context.Canceled, errors.New("provider failed")} {
		p := &organizeProposer{err: failure}
		_, _, err := OrganizeModel(context.Background(), p, convs, nil)
		if !errors.Is(err, failure) || p.calls != 1 {
			t.Fatalf("%v: %v, calls %d", failure, err, p.calls)
		}
	}
	if _, _, err := OrganizeModel(context.Background(), nil, convs, nil); !errors.Is(err, ErrNoNamingModel) {
		t.Fatal(err)
	}
}

func TestOrganizeFoldersIgnoreRootMembershipAndAssignedAgents(t *testing.T) {
	convs := []OrganizeConversation{{"a", "a", "/docs"}, {"b", "b", "/docs/"}, {"c", "c", "/docs"}}
	teams := []teamstore.Team{{ID: "root", Root: true, Members: []teamstore.Member{{Key: "a"}, {Key: "b"}, {Key: "c"}}}, {ID: "crew", Members: []teamstore.Member{{Key: "c"}}}}
	rows := OrganizeUnassignedFolders(convs, teams)
	if len(rows) != 1 || rows[0].Name != "docs" || rows[0].Folder != "/docs" || !reflect.DeepEqual(rows[0].Keys, []string{"a", "b"}) {
		t.Fatalf("rows %+v", rows)
	}
}
