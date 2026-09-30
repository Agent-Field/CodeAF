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
