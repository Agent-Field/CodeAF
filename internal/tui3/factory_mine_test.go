package tui3

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestFactoryMineUsesLinkedGitHubLoginAndRefreshesRows(t *testing.T) {
	login := " @ALICE "
	var linkErr error
	snap := factory.Snapshot{Items: []factory.Item{
		{ID: 1, Kind: factory.KindIssue, State: factory.StateNew, Author: "alice"},
		{ID: 2, Kind: factory.KindIssue, State: factory.StateNew, Assignee: "bob"},
		{ID: 3, Kind: factory.KindIssue, State: factory.StateNew, Taker: "alice", Starter: "bob"},
		{ID: 4, Kind: factory.KindIssue, State: factory.StateNew, Starter: "bob"},
		{ID: 5, Kind: factory.KindIssue, State: factory.StateNew, Author: "santosh"},
	}}
	a := placeApp(t)
	a.width, a.height = 120, 30
	a.factory = factoryTestSeam(t, factory.Seam{
		Load: func() (factory.Snapshot, error) { return snap, nil },
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: login, Via: factory.ViaToken}, linkErr
		},
	})
	drive(t, a, runCmd(a.showPage(pageFactory))...)
	drive(t, a, key("/"), key("m"), key("i"), key("n"), key("e"), key("enter"))
	if a.fp.query != "mine" {
		t.Fatalf("filter box kept %q, want mine", a.fp.query)
	}
	assertWalk := func(want []int) {
		t.Helper()
		got := slices.Clone(a.factoryWalkNow())
		slices.Sort(got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mine for %q walks %v, want %v", login, got, want)
		}
	}
	assertWalk([]int{0, 2})
	login = "bob"
	drive(t, a, runCmd(a.readGitHubLink())...)
	assertWalk([]int{1, 3})
	if text := factoryFrameText(a); !strings.Contains(text, "#2") || strings.Contains(text, "#1") {
		t.Fatalf("bob's frame did not update the cached mine rows:\n%s", text)
	}
	for _, tc := range []struct {
		name  string
		login string
		err   error
	}{
		{"unknown", "", nil},
		{"failed identity read", "alice", errors.New("github not reachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			login, linkErr = tc.login, tc.err
			drive(t, a, runCmd(a.readGitHubLink())...)
			assertWalk(nil)
			if text := factoryFrameText(a); !strings.Contains(text, "mine needs your GitHub login") {
				t.Fatalf("unknown viewer's filter hint did not explain why mine matches nothing:\n%s", text)
			}
		})
	}
}

func TestFactoryMineWithoutGitHubDoorMatchesNothing(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.query = "mine"
	if got := a.factoryWalkNow(); len(got) != 0 {
		t.Fatalf("mine without a GitHub door walks %v", got)
	}
	if text := factoryFrameText(a); !strings.Contains(text, "mine needs your GitHub login") {
		t.Fatalf("filter hint without a GitHub door did not explain the missing login:\n%s", text)
	}
}
