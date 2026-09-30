package tui3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// A manager seated in All teams has a member's post verb there. Its omitted
// team argument says nothing about which team's numbered message it wrote.
func TestTrafficOmittedTeamPostCannotStealAnOrdinaryTeamsMessage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "retained message"
		if missing {
			name = "missing message"
		}
		t.Run(name, func(t *testing.T) {
			a, harbor, _ := trafficAppWithGlobalManager(t)
			handle, _ := trafficHandle(t, a, harbor, "Refactor")
			id, err := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{
				Kind: teamstore.KindDirective, From: teamstore.FromManager, To: handle, Text: "ordinary team's directive"})
			if err != nil {
				t.Fatal(err)
			}
			trafficReadNow(t, a)
			a.teamActivate("")
			if !missing {
				a.entries = []entry{{kind: entryTool, tool: "team_send", status: toolOK, settled: true,
					detail: toolDetail{Args: `{"team":"harbor","to":"` + handle + `","text":"ordinary team's directive"}`,
						Output: "Sent a directive to @" + handle + " (" + teamstore.ThreadNumber(id) + ")."}}}
			} else {
				a.entries = nil
			}
			a.entries = append(a.entries, entry{kind: entryTool, tool: "team_post", status: toolOK, settled: true,
				detail: toolDetail{Args: `{"to":"manager","text":"global progress"}`,
					Output: `Posted to the manager in "All teams" as ` + teamstore.ThreadNumber(id) + ". It arrives at the start of their next step."}})
			fillEntries(a, 60, "after")
			a.offset, a.stick = 0, false
			_ = railLines(t, a)
			x, y := sideRowOn(t, a, "thread/"+id)
			sideClick(t, a, x, y)
			if missing {
				if !a.stick || !a.traffic.landing.older || a.dockHoverWords() != trafficOlderWords {
					t.Fatalf("global post replaced missing ordinary history: landing=%+v hint=%q", a.traffic.landing, a.dockHoverWords())
				}
			} else if a.traffic.landing.entry != 0 || a.traffic.landing.older {
				t.Fatalf("global post stole the ordinary message: %+v", a.traffic.landing)
			}
		})
	}
}

// realTrafficDelivery asks a real session to read a stored directive, then
// reopens its journal so the card is the production delivery and replay shape.
func realTrafficDelivery(t *testing.T, text, kind string) (*app, string, string, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"delivery\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"received\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	memberFile := filepath.Join(root, "member", "session.jsonl")
	cfg := session.Config{Workspace: root, ProfileDir: profile, SessionFile: memberFile,
		Model: "vendor/traffic-delivery", APIKey: "test", BaseURL: server.URL, System: "Reply received."}
	member, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = member.Close() })
	managerFile := filepath.Join(root, "manager", "session.jsonl")
	managerConfig := cfg
	managerConfig.SessionFile = managerFile
	manager, err := session.New(managerConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	a := newApp(t.Context(), Options{Agent: manager, Workspace: root, SessionFile: managerFile, ProfileDir: profile})
	a.dismissWelcome()
	a.width, a.height = 160, 40
	memberKey := convKey(memberFile)
	harbor := teamstore.NewID()
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		off := false
		f.Teams = append(f.Teams, teamstore.Team{ID: harbor, Name: "harbor", Settings: teamstore.Settings{Wake: &off}})
		for _, m := range []teamstore.Member{
			{Key: convKey(managerFile), File: managerFile, Word: "manager", Handle: "boss"},
			{Key: memberKey, File: memberFile, Word: "member", Handle: "web"},
		} {
			if err := f.AddMember(harbor, m); err != nil {
				return err
			}
		}
		return f.SetManager(harbor, convKey(managerFile))
	}); err != nil {
		t.Fatal(err)
	}
	turn := func(words string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		events, err := member.Submit(ctx, words)
		if err != nil {
			t.Fatal(err)
		}
		for e := range events {
			if e.Kind == session.EventError {
				t.Fatalf("real delivery turn: %v", e.Err)
			}
		}
	}
	turn("initialise the cursor")
	id, err := teamstore.AppendTrafficID(profile, harbor, teamstore.Entry{
		Kind: kind, From: teamstore.FromManager, To: "web", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	turn("read the directive")
	if err := member.Close(); err != nil {
		t.Fatal(err)
	}
	member, err = session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range member.Transcript() {
		for _, line := range e.Team {
			if line.Thread == id {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("real session did not deliver and replay the directive")
	}
	a.teamActivate(harbor)
	trafficReadNow(t, a)
	drain(t, a, a.attachConversation(Conversation{Agent: member, Workspace: root, SessionFile: memberFile, Resumed: true}, nil))
	return a, harbor, memberKey, id
}

func TestTrafficRenamedTeamOpensAClippedRealDelivery(t *testing.T) {
	text := "long directive marker\n" + strings.Repeat("配送の行\n", 1100)
	a, harbor, member, id := realTrafficDelivery(t, text, teamstore.KindDirective)
	clipped := false
	for _, e := range a.entries {
		for _, line := range e.team {
			if line.Thread == id && line.Text != strings.TrimSpace(text) && strings.HasSuffix(line.Text, "…") {
				clipped = true
			}
		}
	}
	if !clipped {
		t.Fatal("fixture did not exercise a clipped real delivery")
	}
	if err := a.teamRename(harbor, "dock"); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	spend(t, a, a.trafficJumpIn(member, id, harbor))
	shown, lifted := landedRow(a, "long directive marker")
	if !shown || !lifted || a.traffic.landing.older {
		t.Fatalf("rename lost clipped real delivery: shown=%v lifted=%v landing=%+v", shown, lifted, a.traffic.landing)
	}
}

func TestTrafficRenamedTeamRetainsDeliveryWhenItsOldNameIsReused(t *testing.T) {
	a, harbor, member, id := realTrafficDelivery(t, "historical delivery marker", teamstore.KindDirective)
	if err := a.teamRename(harbor, "dock"); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	other, err := a.teamMake("harbor", a.tabList())
	if err != nil || other == harbor {
		t.Fatalf("reusing the free name: team=%q err=%v", other, err)
	}
	teamsFlush(t, a)
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	spend(t, a, a.trafficJumpIn(member, id, harbor))
	shown, lifted := landedRow(a, "historical delivery marker")
	if !shown || !lifted || a.traffic.landing.older {
		t.Fatalf("name reuse lost historical delivery: shown=%v lifted=%v landing=%+v", shown, lifted, a.traffic.landing)
	}
	// The new team's same-number row must not claim the historical card just
	// because the card still carries its name. Identical retained identities
	// cannot establish which team originally delivered it either.
	a.traffic.rows[other] = []teamstore.Entry{{ID: id, Kind: teamstore.KindDirective,
		From: teamstore.FromManager, To: "web", Text: "new team's different message"}}
	spend(t, a, a.trafficJumpIn(member, id, other))
	if !a.traffic.landing.older || a.dockHoverWords() != trafficOlderWords {
		t.Fatalf("new owner of the name claimed the historical card: %+v", a.traffic.landing)
	}
	a.traffic.rows[other][0].Text = "historical delivery marker"
	spend(t, a, a.trafficJumpIn(member, id, harbor))
	if !a.traffic.landing.older || a.dockHoverWords() != trafficOlderWords {
		t.Fatalf("ambiguous historical identity was guessed: %+v", a.traffic.landing)
	}
}

// A new member's brief is allowed more words than an ordinary message. Its
// historical identity must use the brief's actual delivery limit after rename.
func TestTrafficRenamedTeamOpensAClippedRealBrief(t *testing.T) {
	text := "long brief marker\n" + strings.Repeat("配送の行\n", 3000)
	a, harbor, member, id := realTrafficDelivery(t, text, teamstore.KindStart)
	clipped := false
	for _, e := range a.entries {
		for _, line := range e.team {
			if line.Thread == id && line.Kind == teamstore.KindStart && line.Text != strings.TrimSpace(text) && strings.HasSuffix(line.Text, "…") {
				clipped = true
			}
		}
	}
	if !clipped {
		t.Fatal("fixture did not exercise a clipped real brief")
	}
	if err := a.teamRename(harbor, "dock"); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	spend(t, a, a.trafficJumpIn(member, id, harbor))
	shown, lifted := landedRow(a, "long brief marker")
	if !shown || !lifted || a.traffic.landing.older {
		t.Fatalf("rename lost clipped real brief: shown=%v lifted=%v landing=%+v", shown, lifted, a.traffic.landing)
	}
}
