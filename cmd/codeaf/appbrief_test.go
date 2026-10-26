package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// startedWeb is the crew of crewBackendOn with web started by the manager's
// start line, and web's agent id.
func startedWeb(t *testing.T, conv *fakeConversation, start string) (*appBackend, string, string) {
	t.Helper()
	backend, _, _, id := crewBackendOn(t, conv)
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for i := range f.Teams {
			for j := range f.Teams[i].Members {
				if f.Teams[i].Members[j].Handle == "web" {
					f.Teams[i].Members[j].Started = true
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := teamstore.AppendTraffic(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, FromKey: appFakeTranscript, To: "web", Text: start}); err != nil {
		t.Fatal(err)
	}
	return backend, id, "b0b0b0b0"
}

// A MEMBER'S BRIEF CHANGES IN PLACE: members.brief.set records who, when, and
// the words before and after, the member is told by one delivered line it reads
// at its next step, nothing is said in its conversation and nothing restarts,
// and a restart reads the same history. A brief never changed has no revisions
// (no edit date is ever invented), and the manager's own seat has no brief here.
func TestAppMemberBriefSetIsDurableAndPostsNothing(t *testing.T) {
	conv := &fakeConversation{}
	const start = "Own the web client."
	backend, id, web := startedWeb(t, conv, start)
	before, err := backend.MemberBrief(id, web)
	if err != nil || before.Text != start || before.From != "lead" || len(before.Revisions) != 0 {
		t.Fatalf("before = %+v, %v", before, err)
	}
	conv.mu.Lock()
	submits, texts := conv.submits, len(conv.texts)
	conv.mu.Unlock()
	// The member exists in crew state alone; admission must not look it up
	// as a conversation or open it just to revise that state.
	backend.locked(func() {
		if a := backend.agents[web]; a != nil && a.live != nil {
			t.Fatal("the brief fixture unexpectedly opened the member")
		}
	})

	got, err := backend.MemberBriefSet(id, web, "Own the web client and skip the e2e suite.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Own the web client and skip the e2e suite." || got.From != "lead" || len(got.Revisions) != 1 {
		t.Fatalf("after = %+v", got)
	}
	backend.locked(func() {
		if a := backend.agents[web]; a != nil && a.live != nil {
			t.Fatal("revising a crew brief opened the member")
		}
	})
	r := got.Revisions[0]
	if r.Before != start || r.By != "you" || r.At.IsZero() || r.Post == "" {
		t.Fatalf("revision = %+v", r)
	}
	// Nothing reached its conversation as a message.
	conv.mu.Lock()
	if conv.submits != submits || len(conv.texts) != texts {
		t.Fatalf("a message was posted: %d submits, %v", conv.submits, conv.texts)
	}
	conv.mu.Unlock()
	// The crew log holds the delivered line, as a brief to web.
	lines, _, err := backend.CrewLog(id, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lines {
		if l.ID == r.Post && l.Kind == teamstore.KindBrief && l.To == "web" && l.From == "you" && l.Agent == web {
			found = true
		}
	}
	if !found {
		t.Fatalf("no brief line %s in %+v", r.Post, lines)
	}
	// A restart reads the same.
	fresh := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { fresh.Close() })
	fresh.profileDir = backend.profileDir
	again, err := fresh.MemberBrief(id, web)
	if err != nil || again.Text != got.Text || len(again.Revisions) != 1 || again.Revisions[0] != r {
		t.Fatalf("after restart = %+v, %v", again, err)
	}
	// The same words again change nothing; the manager has no brief to change.
	if _, err := backend.MemberBriefSet(id, web, got.Text, nil); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("unchanged = %v", err)
	}
	lead := appMemberAgent(teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript})
	if _, err := backend.MemberBriefSet(id, lead, "manage", nil); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("manager = %v", err)
	}
	if none, err := backend.MemberBrief(id, lead); err != nil || none.Text != "" || len(none.Revisions) != 0 {
		t.Fatalf("manager brief = %+v %v", none, err)
	}
}

// A BRIEF IS NOT A ROLE: a member carrying a role keeps it word for word when
// its brief changes, the change replaces the start line (not the role's
// words), and nothing is queued into its conversation.
func TestAppMemberBriefSetLeavesTheRoleAlone(t *testing.T) {
	conv := &fakeConversation{}
	const start = "Own the web client."
	backend, id, web := startedWeb(t, conv, start)
	var roleID string
	if err := backend.crewLayer().write(func(f *appCrewLayerFile) error {
		if f.Roles == nil {
			f.Roles = map[string]*appwire.Role{}
		}
		r := &appwire.Role{ID: f.next("role"), Name: "reviewer", Instructions: "Review diffs; never change code."}
		f.Roles[r.ID] = r
		f.member(id, web).Role = r.ID
		roleID = r.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := backend.MemberBriefSet(id, web, "Own the web client; skip e2e.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revisions[0].Before != start {
		t.Fatalf("before = %q, want the start line", got.Revisions[0].Before)
	}
	roles, err := backend.Roles()
	if err != nil || len(roles) != 1 || roles[0].ID != roleID || roles[0].Instructions != "Review diffs; never change code." || len(roles[0].Copies) != 1 || roles[0].Copies[0].Agent != web {
		t.Fatalf("roles = %+v %v", roles, err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if conv.submits != 0 || len(conv.texts) != 0 {
		t.Fatalf("its conversation was written: %v", conv.texts)
	}
}

// TWO EDITORS, ONE BRIEF: a save made from a brief that changed since is a
// conflict and writes nothing; a closed crew's briefs are read-only; a member
// that left is not_found.
func TestAppMemberBriefSetRefusesStaleClosedAndGone(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	none := ""
	first, err := backend.MemberBriefSet(id, web, "first", &none)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.MemberBriefSet(id, web, "second", &none); wireCode(err) != appwire.CodeConflict {
		t.Fatalf("stale = %v", err)
	}
	base := first.Revisions[0].Post
	if _, err := backend.MemberBriefSet(id, web, "second", &base); err != nil {
		t.Fatalf("current base = %v", err)
	}
	if got, _ := backend.MemberBrief(id, web); got.Text != "second" || len(got.Revisions) != 2 {
		t.Fatalf("after = %+v", got)
	}
	if _, err := backend.MemberBriefSet(id, "nobody", "x", nil); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("gone = %v", err)
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams[0].State = teamstore.TeamClosed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.MemberBriefSet(id, web, "third", nil); wireCode(err) != appwire.CodeClosed {
		t.Fatalf("closed = %v", err)
	}
	if got, err := backend.MemberBrief(id, web); err != nil || got.Text != "second" {
		t.Fatalf("a closed crew's brief reads = %+v %v", got, err)
	}
}

// A RECEIPT OPENS ITS OWN LINE: two handoffs with the same words get their own
// ids, and the anchor answers the crew and the window of the very id asked —
// never the one with the same words, the newest, or a guess by time. A line
// the agent neither wrote nor received, a crew it is not in, and an id the log
// does not hold are refused.
func TestAppCrewLogAtIsTheExactLine(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	post := func(text string) string {
		got, err := teamstore.AppendTrafficID(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindNote, From: "web", To: teamstore.FromManager, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := post("LEAF-READY")
	post("between")
	second := post("LEAF-READY")
	post("after")
	for _, want := range []string{first, second} {
		a, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: want, CrewName: "harbor", Around: 1})
		if err != nil || a.Crew != id || a.Closed || len(a.Lines) != 3 || a.Lines[1].ID != want || a.Lines[1].Text != "LEAF-READY" {
			t.Fatalf("anchor %s = %+v %v", want, a, err)
		}
	}
	// "#n" as a person writes it reads the same line.
	if a, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: teamstore.ThreadNumber(second)}); err != nil || !hasLine(a.Lines, second) {
		t.Fatalf("by number = %+v %v", a, err)
	}
	// The start line is the manager's to web: web received it, so it is web's too.
	if _, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: "1"}); err != nil {
		t.Fatalf("a line to web = %v", err)
	}
	for name, ask := range map[string]appwire.LogAnchorAsk{
		"another crew's name": {Agent: web, Post: first, CrewName: "elsewhere"},
		"not a member":        {Agent: "ffffffff", Post: first},
		"not in the log":      {Agent: web, Post: "999"},
	} {
		if _, err := backend.CrewLogAt(ask); wireCode(err) != appwire.CodeNotFound {
			t.Fatalf("%s = %v", name, err)
		}
	}
	if _, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: "LEAF-READY"}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("words for an id = %v", err)
	}
	// A closed crew's line still opens, as history.
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams[0].State = teamstore.TeamClosed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if a, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: second}); err != nil || !a.Closed || !hasLine(a.Lines, second) {
		t.Fatalf("closed = %+v %v", a, err)
	}
}

// A LINE BETWEEN TWO OTHER MEMBERS IS NOT THIS AGENT'S: the anchor refuses it.
func TestAppCrewLogAtRefusesALineThatIsNotTheAgents(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	other, err := teamstore.AppendTrafficID(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: "lead", Text: "to itself"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: other}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("someone else's line = %v", err)
	}
}

func hasLine(lines []appwire.LogLine, id string) bool {
	for _, l := range lines {
		if l.ID == id {
			return true
		}
	}
	return false
}

// A POST UP OPENS ITS OWN LINE IN THE CREW ABOVE. A member of a sub-crew with
// no manager posts to the manager of the crew above; the engine's answer names
// that crew by id and the line it became, so the journal read back (as after a
// restart) opens that very line, never its twin with the same words. The crew
// above is read only for a member of a crew under it, and only its own line
// there; an older answer with no line opens nothing; an unrelated crew, a
// crew gone, a member gone and a room line up there are refused; the name path
// is not widened; a closed crew above still opens as history.
func TestAppCrewLogAtOpensAPostUpByItsCrewAndID(t *testing.T) {
	backend, harbor, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	const lonerKey = "/state/v3/places/proj/c0c0c0c0/transcript.jsonl"
	const loner = "c0c0c0c0"
	dock, elsewhere := teamstore.NewID(), teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: dock, Name: "dock", Made: time.Now()}, teamstore.Team{ID: elsewhere, Name: "harbor", Made: time.Now()})
		if err := f.SetParent(dock, harbor); err != nil {
			return err
		}
		for _, id := range []string{dock, elsewhere} {
			if err := f.AddMember(id, teamstore.Member{Key: lonerKey + id, File: lonerKey, Where: "/w", Word: "loner", Handle: "loner"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	up := func(team, text string) string {
		id, err := teamstore.AppendTrafficID(backend.profileDir, team, teamstore.Entry{Kind: teamstore.KindNote, From: "loner", To: teamstore.ToManager, Member: lonerKey + dock, Text: `(from "dock", a team under yours with no manager of its own) ` + text})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := up(harbor, "LEAF-READY")
	room, err := teamstore.AppendTrafficID(backend.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: "web", To: teamstore.ToRoom, Text: "harbor's own room"})
	if err != nil {
		t.Fatal(err)
	}
	second := up(harbor, "LEAF-READY")
	theirs := up(elsewhere, "LEAF-READY")

	journal := func(answer string) *appwire.Handoff {
		content, _ := json.Marshal(answer)
		record := `{"type":"session","version":1,"id":"n1","cwd":"/w"}
{"type":"message","role":"user","content":"Report up.","timestamp":"2026-10-15T23:06:41Z"}
{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"c1","function":{"name":"team_post","arguments":"{\"to\":\"manager\",\"text\":\"LEAF-READY\"}"}}],"timestamp":"2026-10-15T23:06:42Z"}
{"type":"message","role":"tool","toolCallId":"c1","content":` + string(content) + `,"timestamp":"2026-10-15T23:06:42Z"}
`
		for _, item := range session.ReadTranscriptBytes([]byte(record)).Entries {
			if item.Tool == "team_post" {
				return appHandoff(item.Tool, item.Args, item.Output)
			}
		}
		return nil
	}
	said := func(post string) string {
		return fmt.Sprintf(`Posted to the manager of "harbor" (team %s) as %s, which "dock" answers to while it has no manager of its own. It arrives at the start of its next step.`, harbor, teamstore.ThreadNumber(post))
	}
	for _, want := range []string{first, second} {
		h := journal(said(want))
		if h == nil || !h.Delivered || !h.Up || h.Crew != harbor || h.CrewName != "harbor" || h.FromCrew != "dock" || h.Post != want {
			t.Fatalf("handoff read back = %+v", h)
		}
		a, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: loner, Post: h.Post, CrewName: h.CrewName, Crew: h.Crew, Around: 1})
		if err != nil || a.Crew != harbor || a.Closed || a.Lines[1].ID != want || !strings.HasSuffix(a.Lines[1].Text, "LEAF-READY") {
			t.Fatalf("anchor %s = %+v %v", want, a, err)
		}
	}
	old := journal(`Posted to the manager of "harbor", which "dock" answers to while it has no manager of its own. It arrives at the start of its next step.`)
	if old == nil || !old.Delivered || !old.Up || old.Post != "" || old.Crew != "" {
		t.Fatalf("an older post up = %+v", old)
	}
	for name, ask := range map[string]appwire.LogAnchorAsk{
		"a crew of the same name it is not under": {Agent: loner, Post: theirs, Crew: harbor},
		"harbor's room line, not its own":         {Agent: loner, Post: room, Crew: harbor},
		"a crew gone":                             {Agent: loner, Post: first, Crew: teamstore.NewID()},
		"an agent in neither":                     {Agent: "ffffffff", Post: first, Crew: harbor},
		"the name path is not widened":            {Agent: loner, Post: first, CrewName: "harbor"},
	} {
		if a, err := backend.CrewLogAt(ask); wireCode(err) != appwire.CodeNotFound {
			t.Fatalf("%s = %+v %v", name, a, err)
		}
	}
	// A member of harbor itself still reads harbor's lines to and from it.
	if a, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: web, Post: room, Crew: harbor}); err != nil || !hasLine(a.Lines, room) {
		t.Fatalf("harbor's own member = %+v %v", a, err)
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for i := range f.Teams {
			if f.Teams[i].ID == harbor {
				f.Teams[i].State = teamstore.TeamClosed
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if a, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: loner, Post: second, Crew: harbor}); err != nil || !a.Closed || !hasLine(a.Lines, second) {
		t.Fatalf("a closed crew above = %+v %v", a, err)
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for i := range f.Teams {
			if f.Teams[i].ID == dock {
				f.Teams[i].Members = nil
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.CrewLogAt(appwire.LogAnchorAsk{Agent: loner, Post: second, Crew: harbor}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("a member gone from the crew below = %v", err)
	}
}
