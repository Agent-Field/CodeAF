package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamSendWakesMemberBeforeItsFirstTurn(t *testing.T) {
	for _, address := range []string{"handle", "everyone"} {
		t.Run(address, func(t *testing.T) {
			fixture := newWakingTeamFixture(t)
			key := convKeyOf(t, fixture.web)
			if err := teams.Update(fixture.profile, func(f *teams.File) error {
				if err := f.RemoveMember(fixture.teamID, key); err != nil {
					return err
				}
				if err := f.RemoveMember(fixture.teamID, convKeyOf(t, fixture.parser)); err != nil {
					return err
				}
				return f.AddMember(fixture.teamID, teams.Member{Key: key, File: fixture.web})
			}); err != nil {
				t.Fatal(err)
			}
			file, err := teams.Load(fixture.profile)
			if err != nil {
				t.Fatal(err)
			}
			team, _ := file.Team(fixture.teamID)
			member, _ := team.Member(key)
			if member.Handle == "" || !strings.Contains(teamHandles(team, team.Manager), "@"+member.Handle) {
				t.Fatalf("fresh member is not addressable: %+v", member)
			}
			manual := func(c *Config) { c.teamWatchManual = true }
			manager := teamAgent(t, fixture, fixture.manager, nil, manual)
			calls := oneAnswer(1)
			fresh := teamAgent(t, fixture, fixture.web, calls, manual)
			if len(fresh.Transcript()) != 0 || calls.requests() != 0 {
				t.Fatal("member already had a turn")
			}
			to := member.Handle
			if address == "everyone" {
				to = address
			}
			sendDirective(t, manager, to)
			fresh.teamWatchTick(fixture.profile, time.Now())
			waitRequests(t, calls, 1)
			if got := userTextIn(calls.request(0)); !strings.Contains(got, "Fix the header.") || !strings.Contains(got, "from manager") {
				t.Fatalf("first turn missed the directive: %s", got)
			}
			waitIdle(t, fresh)
		})
	}
}

func TestTeamSendUndeliverableAddressIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		to    string
		empty bool
		valid string
	}{
		{"unknown handle", "missing", false, "@web, @parser"},
		{"nobody in everyone", "everyone", true, "none yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTeamFixture(t, true)
			if tc.empty {
				if err := teams.Update(fixture.profile, func(f *teams.File) error {
					for _, path := range []string{fixture.web, fixture.parser} {
						if err := f.RemoveMember(fixture.teamID, convKeyOf(t, path)); err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			manager := teamAgent(t, fixture, fixture.manager, nil, nil)
			args, _ := json.Marshal(map[string]string{"to": tc.to, "text": "Do the work", "kind": "directive"})
			said, failed, err := manager.teamSendTool(context.Background(), args)
			if err != nil || !failed || !strings.Contains(said, "Nothing was delivered") || !strings.Contains(said, tc.valid) {
				t.Fatalf("undeliverable send returned %q, failed=%v, err=%v", said, failed, err)
			}
			log, err := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0)
			if err != nil || len(log) != 0 {
				t.Fatalf("undelivered message wrote traffic: %+v, %v", log, err)
			}
		})
	}
}
