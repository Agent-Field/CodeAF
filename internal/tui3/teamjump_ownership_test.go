package tui3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// A receipt keeps the name used at execution. Replaying a real omitted-team
// post must therefore retain its row jump when that team is renamed later.
func TestTrafficOmittedTeamPostOpensAfterRename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []struct{ Role string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, message := range request.Messages {
			if message.Role == "tool" {
				io.WriteString(w, "data: {\"id\":\"post\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"posted\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				return
			}
		}
		io.WriteString(w, "data: {\"id\":\"post\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"post\",\"type\":\"function\",\"function\":{\"name\":\"team_post\",\"arguments\":\"{\\\"to\\\":\\\"manager\\\",\\\"text\\\":\\\"post rename marker\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	memberFile := filepath.Join(root, "member", "session.jsonl")
	managerFile := filepath.Join(root, "manager", "session.jsonl")
	harbor := teamstore.NewID()
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		off := false
		f.Teams = append(f.Teams, teamstore.Team{ID: harbor, Name: "harbor", Settings: teamstore.Settings{Wake: &off}})
		for _, m := range []teamstore.Member{
			{Key: convKey(managerFile), File: managerFile, Word: "manager", Handle: "boss"},
			{Key: convKey(memberFile), File: memberFile, Word: "member", Handle: "web"},
		} {
			if err := f.AddMember(harbor, m); err != nil {
				return err
			}
		}
		return f.SetManager(harbor, convKey(managerFile))
	}); err != nil {
		t.Fatal(err)
	}
	cfg := session.Config{Workspace: root, ProfileDir: profile, SessionFile: memberFile,
		Model: "vendor/traffic-post", APIKey: "test", BaseURL: server.URL, System: "Post once."}
	member, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = member.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	events, err := member.Submit(ctx, "post to the manager")
	if err != nil {
		t.Fatal(err)
	}
	for e := range events {
		if e.Kind == session.EventError {
			t.Fatal(e.Err)
		}
	}
	if err := member.Close(); err != nil {
		t.Fatal(err)
	}
	member, err = session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(t.Context(), Options{Agent: member, Workspace: root, SessionFile: memberFile, ProfileDir: profile})
	a.dismissWelcome()
	a.width, a.height = 160, 40
	a.teamActivate(harbor)
	trafficReadNow(t, a)
	postAt := -1
	for i, e := range a.entries {
		if e.tool == "team_post" && strings.Contains(e.detail.Output, `in "harbor" as #1`) && !strings.Contains(e.detail.Args, `"team"`) {
			postAt = i
		}
	}
	if postAt < 0 {
		t.Fatal("the real session did not replay its omitted-team post receipt")
	}
	if err := a.teamRename(harbor, "dock"); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	a.sideSetView(sideTraffic)
	a.sideToggleThread(sideThreadKey(harbor, sideGeneral))
	key := railKeyOfReply(t, a, "post rename marker")
	x, y := sideRowOn(t, a, key)
	sideClick(t, a, x, y)
	shown, lifted := landedRow(a, "post rename marker")
	if a.traffic.landing.entry != postAt || a.traffic.landing.older || !shown || !lifted {
		t.Fatalf("rename lost the omitted-team post: shown=%v lifted=%v landing=%+v", shown, lifted, a.traffic.landing)
	}
}

// A real delivered card outlives its Traffic row. Another team's identical
// numbered row cannot claim that card after the original row is evicted.
func TestTrafficEvictedForeignRowCannotClaimARealDelivery(t *testing.T) {
	text := "same-number delivery marker"
	a, harbor, member, id := realTrafficDelivery(t, text, teamstore.KindDirective)
	other, err := a.teamMake("orchard", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	manager := mustTeam(t, a, harbor).Manager
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.SetHandle(other, member, "web"); err != nil {
			return err
		}
		return f.SetManager(other, manager)
	}); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	// A finished-turn event can precede the real directive. Pad this team's
	// log to the directive's exact number, independently of event timing.
	number, err := strconv.Atoi(id)
	if err != nil {
		t.Fatal(err)
	}
	for range number - 1 {
		trafficAppend(t, a, other, teamstore.Entry{Kind: teamstore.KindNote,
			From: teamstore.FromManager, To: teamstore.ToRoom, Text: "before the target"})
	}
	otherID, err := teamstore.AppendTrafficID(a.profileDir, other, teamstore.Entry{
		Kind: teamstore.KindDirective, From: teamstore.FromManager, To: "web", Text: text})
	if err != nil || otherID != id {
		t.Fatalf("fixture lacks colliding team-local numbers: id=%q other=%q err=%v", id, otherID, err)
	}
	trafficReadNow(t, a)
	// Fold a later read through the production cache update. The card above
	// came from a real journal; evicting its row needs no extra model turn.
	after := a.traffic.cursor[harbor]
	last, err := strconv.Atoi(after)
	if err != nil {
		t.Fatal(err)
	}
	var later []teamstore.Entry
	for i := range trafficKeep {
		later = append(later, teamstore.Entry{ID: fmt.Sprintf("%012d", last+i+1), Kind: teamstore.KindNote,
			From: teamstore.FromManager, To: teamstore.ToRoom, Text: "later unrelated message"})
	}
	spend(t, a, a.trafficTake([]trafficGot{{trafficJob: trafficJob{id: harbor, after: after}, entries: later}},
		nil, a.traffic.stamp, a.traffic.edits, false))
	if rows := a.traffic.rows[harbor]; len(rows) != trafficKeep || rows[0].ID <= id {
		t.Fatalf("fixture did not evict the delivered row: retained=%d", len(rows))
	}
	if rows := a.traffic.rows[other]; len(rows) == 0 || rows[len(rows)-1].ID != id {
		t.Fatal("the target team's colliding row is not loaded")
	}
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, false
	managerMember, _ := mustTeam(t, a, harbor).Member(manager)
	managerAgent, err := session.New(session.Config{Workspace: filepath.Dir(a.profileDir), ProfileDir: a.profileDir,
		SessionFile: managerMember.File, Model: "vendor/traffic-delivery", APIKey: "test", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = managerAgent.Close() })
	drain(t, a, a.takeBeside(Conversation{Agent: managerAgent, Workspace: filepath.Dir(a.profileDir),
		SessionFile: managerMember.File, Resumed: true}))
	a.teamActivate(other)
	x, y := sideRowDoor(t, a, "thread/"+id, sideActJump)
	sideClick(t, a, x, y)
	if a.frontTabKey() != member || !a.stick || !a.traffic.landing.older || a.dockHoverWords() != trafficOlderWords {
		t.Fatalf("evicted foreign ownership was guessed: front=%q landing=%+v hint=%q", a.frontTabKey(), a.traffic.landing, a.dockHoverWords())
	}
}

// Team-local handles can differ even when both receipts carry identical
// words and numbers. The newer receipt must never replace the clicked post.
func TestTrafficIdenticalOmittedPostsKeepTheirOwnTeamHandles(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		missing, unloaded, renamed bool
	}{
		{name: "retained post"},
		{name: "missing post", missing: true},
		{name: "unloaded foreign traffic", unloaded: true},
		{name: "missing post and unloaded foreign traffic", missing: true, unloaded: true},
		{name: "ambiguous renamed post", renamed: true},
		{name: "renamed post and unloaded foreign traffic", renamed: true, unloaded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					http.NotFound(w, r)
					return
				}
				var request struct{ Messages []struct{ Role string } }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "tool" {
					io.WriteString(w, "data: {\"id\":\"post\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"posted\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				io.WriteString(w, "data: {\"id\":\"post\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"post\",\"type\":\"function\",\"function\":{\"name\":\"team_post\",\"arguments\":\"{\\\"to\\\":\\\"manager\\\",\\\"text\\\":\\\"ready\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)
			root := t.TempDir()
			profile := filepath.Join(root, "profile")
			memberFile := filepath.Join(root, "member", "session.jsonl")
			managerFile := filepath.Join(root, "manager", "session.jsonl")
			harbor := teamstore.NewID()
			member := teamstore.Member{Key: convKey(memberFile), File: memberFile, Word: "member", Handle: "web"}
			manager := teamstore.Member{Key: convKey(managerFile), File: managerFile, Word: "manager", Handle: "boss"}
			if err := teamstore.Update(profile, func(f *teamstore.File) error {
				off := false
				f.Teams = append(f.Teams, teamstore.Team{ID: harbor, Name: "harbor", Settings: teamstore.Settings{Wake: &off}})
				if err := f.AddMember(harbor, member); err != nil {
					return err
				}
				if err := f.AddMember(harbor, manager); err != nil {
					return err
				}
				return f.SetManager(harbor, manager.Key)
			}); err != nil {
				t.Fatal(err)
			}
			cfg := session.Config{Workspace: root, ProfileDir: profile, SessionFile: memberFile,
				Model: "vendor/traffic-post", APIKey: "test", BaseURL: server.URL, System: "Post once."}
			post := func() {
				t.Helper()
				agent, err := session.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer agent.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				events, err := agent.Submit(ctx, "post to the manager")
				if err != nil {
					t.Fatal(err)
				}
				for e := range events {
					if e.Kind == session.EventError {
						t.Fatal(e.Err)
					}
				}
			}
			post()
			global := ""
			if err := teamstore.Update(profile, func(f *teamstore.File) error {
				if err := f.SetManager(harbor, member.Key); err != nil {
					return err
				}
				global = f.MakeRoot(time.Now())
				if err := f.AddMember(global, manager); err != nil {
					return err
				}
				if err := f.SetManager(global, manager.Key); err != nil {
					return err
				}
				member.Handle = "status"
				return f.AddMember(global, member)
			}); err != nil {
				t.Fatal(err)
			}
			post()
			agent, err := session.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })
			a := newApp(t.Context(), Options{Agent: agent, Workspace: root, SessionFile: memberFile, ProfileDir: profile})
			a.dismissWelcome()
			a.width, a.height = 160, 40
			a.teamActivate(harbor)
			trafficReadNow(t, a)
			postAt, globalAt := -1, -1
			for i, e := range a.entries {
				if e.tool != "team_post" || strings.Contains(e.detail.Args, `"team"`) {
					continue
				}
				if strings.Contains(e.detail.Output, `in "harbor" as #1`) {
					postAt = i
				}
				if strings.Contains(e.detail.Output, `in "All teams" as #1`) {
					globalAt = i
				}
			}
			for _, teamID := range []string{harbor, global} {
				want := "web"
				if teamID == global {
					want = "status"
				}
				found := false
				for _, row := range a.traffic.rows[teamID] {
					if row.Kind == teamstore.KindNote && row.From == want && row.Text == "ready" && teamstore.ThreadNumber(row.ID) == "#1" {
						found = true
					}
				}
				if !found {
					t.Fatalf("real post not loaded for %q under @%s: %+v", teamID, want, a.traffic.rows[teamID])
				}
			}
			if postAt < 0 || globalAt <= postAt {
				t.Fatalf("real journal lacks both ordered omitted-team receipts: harbor=%d All teams=%d", postAt, globalAt)
			}
			if tc.renamed {
				if err := a.teamRename(harbor, "dock"); err != nil {
					t.Fatal(err)
				}
				teamsFlush(t, a)
			}
			if tc.missing {
				a.entries = append(a.entries[:postAt:postAt], a.entries[postAt+1:]...)
			}
			if tc.unloaded {
				delete(a.traffic.rows, global)
				delete(a.traffic.cursor, global)
			}
			fillEntries(a, 60, "after")
			a.offset, a.stick = 0, false
			a.sideSetView(sideTraffic)
			a.sideToggleThread(sideThreadKey(harbor, sideGeneral))
			x, y := sideRowOn(t, a, railKeyOfReply(t, a, "ready"))
			sideClick(t, a, x, y)
			if tc.missing || tc.renamed {
				if !a.stick || !a.traffic.landing.older || a.dockHoverWords() != trafficOlderWords {
					t.Fatalf("foreign or unresolved post replaced the history hint: landing=%+v hint=%q", a.traffic.landing, a.dockHoverWords())
				}
			} else {
				shown, lifted := landedRow(a, "ready")
				if a.traffic.landing.entry != postAt || a.traffic.landing.older || !shown || !lifted {
					t.Fatalf("foreign post stole the clicked row: harbor=%d global=%d shown=%v lifted=%v landing=%+v", postAt, globalAt, shown, lifted, a.traffic.landing)
				}
			}
		})
	}
}

// A named receipt needs the conversation's loaded sender in that team.
// After a rename, each candidate uses its own handle rather than the row's.
func TestTrafficOmittedPostNeedsLoadedTeamSenderOwnership(t *testing.T) {
	for _, mode := range []string{"unloaded post", "different sender", "ambiguous historical name", "unloaded historical candidate"} {
		t.Run(mode, func(t *testing.T) {
			a, harbor, global := trafficAppWithGlobalManager(t)
			front := a.frontTabKey()
			if err := a.teamEdit(func(f *teamstore.File) error {
				return f.SetHandle(global, front, "status")
			}); err != nil {
				t.Fatal(err)
			}
			teamsFlush(t, a)
			sender, ok := mustTeam(t, a, harbor).Member(front)
			if !ok {
				t.Fatal("the source team lost its sender")
			}
			id := "000000000001"
			a.traffic.rows = map[string][]teamstore.Entry{
				harbor: {{ID: id, Kind: teamstore.KindNote, From: sender.Handle, Text: "ready"}},
				global: {{ID: id, Kind: teamstore.KindNote, From: "status", Text: "ready"}},
			}
			a.traffic.cursor = map[string]string{harbor: id, global: id}
			switch mode {
			case "unloaded post":
				delete(a.traffic.rows, harbor)
				delete(a.traffic.cursor, harbor)
			case "different sender":
				a.traffic.rows[harbor][0].From = "someone-else"
			case "ambiguous historical name", "unloaded historical candidate":
				if err := a.teamRename(harbor, "dock"); err != nil {
					t.Fatal(err)
				}
				teamsFlush(t, a)
				if mode == "unloaded historical candidate" {
					delete(a.traffic.rows, global)
					delete(a.traffic.cursor, global)
				}
			}
			a.entries = []entry{{kind: entryTool, tool: "team_post", text: "team_post", status: toolOK, settled: true,
				detail: toolDetail{Args: `{"to":"manager","text":"ready"}`,
					Output: `Posted to the manager in "harbor" as #1. It arrives at the start of their next step.`}}}
			a.offset, a.stick = 0, false
			spend(t, a, a.trafficJumpIn("", id, harbor))
			if !a.stick || !a.traffic.landing.older || a.dockHoverWords() != trafficOlderWords {
				t.Fatalf("unresolved receipt ownership was accepted: landing=%+v hint=%q", a.traffic.landing, a.dockHoverWords())
			}
		})
	}
}
