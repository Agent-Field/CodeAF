package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A reused handle does not carry the former membership's brief across the
// new join boundary, even when both memberships were created moments apart.
func TestAReusedHandleDoesNotReplayAnotherMembershipsStart(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sameKey   bool
		withStart bool
	}{
		{name: "replacement conversation"},
		{name: "same conversation rejoins", sameKey: true},
		{name: "replacement has its own start", withStart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTeamFixture(t, true)
			key := convKeyOf(t, f.web)
			if err := teams.Update(f.profile, func(file *teams.File) error { return file.RemoveMember(f.teamID, key) }); err != nil {
				t.Fatal(err)
			}
			const stale = "Rewrite the old worker's billing subsystem."
			oldID, err := teams.AppendTrafficID(f.profile, f.teamID, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "worker", Text: stale})
			if err != nil {
				t.Fatal(err)
			}
			if err := teams.Update(f.profile, func(file *teams.File) error {
				return file.AddMember(f.teamID, teams.Member{Key: key, File: f.web, Handle: "worker", JoinedAt: time.Now(), StartID: oldID})
			}); err != nil {
				t.Fatal(err)
			}
			if err := teams.Update(f.profile, func(file *teams.File) error { return file.RemoveMember(f.teamID, key) }); err != nil {
				t.Fatal(err)
			}
			journal := f.web
			if !tc.sameKey {
				dir := filepath.Join(filepath.Dir(filepath.Dir(f.web)), "replacement")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				journal = filepath.Join(dir, placeTranscript)
				if err := os.WriteFile(journal, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			const fresh = "Write the replacement worker's migration guide."
			startID := ""
			if tc.withStart {
				startID, err = teams.AppendTrafficID(f.profile, f.teamID, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "worker", Text: fresh})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := teams.Update(f.profile, func(file *teams.File) error {
				return file.AddMember(f.teamID, teams.Member{Key: convKeyOf(t, journal), File: journal, Handle: "worker", JoinedAt: time.Now(), StartID: startID})
			}); err != nil {
				t.Fatal(err)
			}
			c := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }}}
			a := teamAgent(t, f, journal, c, nil)
			if got := len(a.teamStarts(f.profile)) > 0; got != tc.withStart {
				t.Fatalf("membership startup = %v, want %v", got, tc.withStart)
			}
			collect(t, mustSubmit(t, a, "What is my current assignment?"))
			text := userTextIn(c.request(0))
			if strings.Contains(text, stale) {
				t.Fatalf("new membership received the former member's start: %s", text)
			}
			if tc.withStart && !briefIn(text, fresh) {
				t.Fatalf("new membership did not receive its own start: %s", text)
			}
			if news := a.teamBoundary(); strings.Contains(news, fresh) || strings.Contains(news, stale) {
				t.Fatalf("a brief was delivered twice: %s", news)
			}
		})
	}
}
