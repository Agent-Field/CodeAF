package session

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// Contract 4.1 and 4.2: Every raise names a larger cap and keeps positive sub-cent spend.
func TestTeamCapPacketKeepsPreciseFigures(t *testing.T) {
	for _, tc := range []struct {
		cap, raise         float64
		capWord, raiseWord string
	}{
		{0.00001, 0.00002, "$0.00001", "$0.00002"},
		{0.0004, 0.0008, "$0.0004", "$0.0008"},
		{0.001, 0.002, "$0.001", "$0.002"},
		{0.004, 0.008, "$0.004", "$0.008"},
		{0.005, 0.01, "$0.005", "$0.01"},
		{0.009, 0.018, "$0.009", "$0.018"},
		{0.01, 0.02, "$0.01", "$0.02"},
		{0.015, 0.03, "$0.015", "$0.03"},
		{1, 2, "$1", "$2"},
		{5, 10, "$5", "$10"},
		{7.5, 15, "$7.50", "$15"},
		{1234.5, 2469, "$1,234.50", "$2,469"},
	} {
		p := capPacket(teams.Team{ID: "aaaaaaaaaaaa", Name: "harbor"}, "2026-09-25", tc.cap, tc.cap)
		if p.Cap == nil || p.Cap.RaiseTo <= tc.cap || p.Cap.RaiseTo != tc.raise || p.Cap.SpentUSD <= 0 {
			t.Errorf("cap %.8f: facts %+v", tc.cap, p.Cap)
			continue
		}
		if want := "harbor reached its " + tc.capWord + " cap today"; p.Question != want {
			t.Errorf("cap %.8f: question %q, want %q", tc.cap, p.Question, want)
		}
		if want := "Raise to " + tc.raiseWord; p.Options[0].Label != want {
			t.Errorf("cap %.8f: label %q, want %q", tc.cap, p.Options[0].Label, want)
		}
		if want := "harbor and its sub-teams go on until " + tc.raiseWord + " today"; p.Options[0].Consequence != want {
			t.Errorf("cap %.8f: consequence %q, want %q", tc.cap, p.Options[0].Consequence, want)
		}
	}
}

// Contract 4.2 and 4.3: A decided sub-cent raise admits new work until its new ceiling.
func TestTeamSubCentRaiseAdmitsWorkUntilNewCeiling(t *testing.T) {
	fixture := newTeamFixture(t, true)
	cap := 0.001
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.SetSettings(fixture.teamID, func(s *teams.Settings) { s.CapUSDDay = &cap })
	}); err != nil {
		t.Fatal(err)
	}
	spend := &capSpend{usd: cap, stamp: "one"}
	stubCapSpend(t, spend)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	roles := manager.teamRoles()
	if held := manager.teamCapHold(fixture.profile, roles); !strings.Contains(held, "$0.001 cap") || !strings.Contains(held, "spent $0.001") {
		t.Fatalf("at the first ceiling: %q", held)
	}
	p := onlyPacket(t, fixture.profile, teams.Person)
	if _, err := teams.Decide(fixture.profile, p.ID, teams.Person, teams.OptionRaiseCap, ""); err != nil {
		t.Fatal(err)
	}
	spend.usd, spend.stamp = 0.0015, "two"
	if held := manager.teamCapHold(fixture.profile, roles); held != "" {
		t.Fatalf("below the raised ceiling: %q", held)
	}
	spend.usd, spend.stamp = 0.002, "three"
	if held := manager.teamCapHold(fixture.profile, roles); !strings.Contains(held, "$0.002 cap") {
		t.Fatalf("at the raised ceiling: %q", held)
	}
}
