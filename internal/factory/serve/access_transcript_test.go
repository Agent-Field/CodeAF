package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

func TestContractK1TranscriptUsesTheCurrentAccessDecision(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	base := "/v1/resources/chat/" + chat.ID
	path := base + "/transcript"
	o.do(t, "santosh", "GET", path, -1, nil, 200, nil)
	missing := o.raw(t, "abir", "GET", "/v1/resources/chat/ch_unknown/transcript", nil)
	if missing.Code != 404 {
		t.Fatalf("unknown chat: %d %s", missing.Code, missing.Body)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws+"/agent", -1, nil, 200, nil)
	afiID := "afi:" + ws + ":santosh"
	for _, target := range []struct{ who, path string }{
		{"abir", path},
		{"owner", path},
		{"owner", "/v1/resources/session/" + afiID + "/transcript"},
		{"santosh", accessWorkstreamPath(ws) + "/transcript"},
	} {
		got := o.raw(t, target.who, "GET", target.path, nil)
		if got.Code != 404 || got.Body.String() != missing.Body.String() {
			t.Fatalf("%s %s: %d %s, missing: %s", target.who, target.path, got.Code, got.Body, missing.Body)
		}
	}
	row, _, err := o.st.Resource("session", afiID)
	if err != nil {
		t.Fatal(err)
	}
	row.ID = "afi:" + ws + ":abir"
	if err := o.st.PutResource(row); err != nil {
		t.Fatal(err)
	}
	o.do(t, "santosh", "GET", "/v1/resources/session/"+row.ID+"/transcript", -1, nil, 404, nil)

	single := contractChat(t, o, ws, "")
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+single.ID+"/grants", -1, personGrant("abir", "view"), 201, nil)
	o.do(t, "abir", "GET", "/v1/resources/chat/"+single.ID+"/transcript", -1, nil, 200, nil)

	o.do(t, "santosh", "POST", base+"/grants", -1, api.GrantNew{Subject: acl.Subject{Kind: "team", ID: "tm_platform"}, Role: "view"}, 201, nil)
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
	o.do(t, "santosh", "POST", base+"/grants", -1, api.GrantNew{Subject: acl.Subject{Kind: "org", ID: "*"}, Role: "view"}, 201, nil)
	o.do(t, "mia", "GET", path, -1, nil, 200, nil)
}

func TestContractK2SharedTranscriptReadsOwnersLinesAndSince(t *testing.T) {
	o, door := taskOrg(t, "edit")
	var turns []AgentTurn
	o.driverMode(t, spaceRunner(0, &turns))
	ws := filepath.Base(door)
	chat := contractChat(t, o, ws, "")
	o.do(t, "santosh", "POST", door+"/agent/messages", -1, api.AgentMessageNew{Chat: chat.ID, Text: "The first line"}, 202, nil)
	o.settle()
	base := "/v1/resources/chat/" + chat.ID
	o.do(t, "santosh", "POST", base+"/grants", -1, personGrant("abir", "full"), 201, nil)
	var out struct {
		Kind, ID, State string
		Seq             int
		Entries         []api.AgentEntry
	}
	o.do(t, "abir", "GET", base+"/transcript", -1, nil, 200, &out)
	if out.Kind != "chat" || out.ID != chat.ID || out.State != api.AgentIdle || out.Seq != 2 ||
		len(out.Entries) != 2 || out.Entries[0].By != "santosh" || out.Entries[0].Text != "The first line" || out.Entries[1].Kind != api.AgentEntryAgent {
		t.Fatalf("transcript = %+v", out)
	}
	o.do(t, "abir", "GET", base+"/transcript?since=1", -1, nil, 200, &out)
	if out.Seq != 2 || len(out.Entries) != 1 || out.Entries[0].Seq != 2 {
		t.Fatalf("since = %+v", out)
	}
	o.do(t, "abir", "GET", base+"/transcript?since=-1", -1, nil, 400, nil)
	o.do(t, "abir", "POST", door+"/agent/messages", -1, api.AgentMessageNew{Chat: chat.ID, Text: "A message"}, 404, nil)
	o.do(t, "abir", "POST", door+"/agent/stop?chat="+chat.ID, -1, nil, 404, nil)
	o.do(t, "abir", "POST", door+"/agent/cards/1?chat="+chat.ID, -1, api.AgentCardsAdd{}, 404, nil)
	if len(turns) != 1 {
		t.Fatalf("transcript access started another run: %+v", turns)
	}
}

func TestContractK2TaskTranscriptUsesTheWorkingConversation(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Research", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	key := taskRunKey{task.WS, task.ID}
	o.h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryPerson, Text: "Research this", By: "santosh"})
	o.h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryAgent, Text: "I found the answer"})
	base := "/v1/resources/session/task:" + task.WS + ":" + task.ID
	var out struct {
		Kind, State string
		Entries     []api.AgentEntry
		Seq         int
	}
	o.do(t, "abir", "GET", base+"/transcript?since=1", -1, nil, 200, &out)
	if out.Kind != "session" || out.State != api.AgentIdle || out.Seq != 2 || len(out.Entries) != 1 || out.Entries[0].Text != "I found the answer" {
		t.Fatalf("task transcript = %+v", out)
	}
	o.do(t, "mia", "GET", base+"/transcript", -1, nil, 404, nil)
}

func TestContractK3TranscriptAccessEndsAfterDeletionExpiryOrMove(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	base := "/v1/resources/chat/" + chat.ID
	path := base + "/transcript"
	grant := personGrant("abir", "view")
	var g acl.Grant
	o.do(t, "santosh", "POST", base+"/grants", -1, grant, 201, &g)
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
	o.do(t, "santosh", "DELETE", base+"/grants/"+g.ID, -1, nil, 204, nil)
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
	expiry := o.now.Add(time.Minute)
	grant.ExpiresAt = &expiry
	o.do(t, "santosh", "POST", base+"/grants", -1, grant, 201, nil)
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
	o.now = expiry
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
	o.do(t, "santosh", "POST", base+"/grants", -1, personGrant("abir", "view"), 201, nil)
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
	contractMove(t, o, "santosh", chat.ID, contractSpace(t, o, "santosh"), 200, nil)
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
}

func TestContractK4OrgDefaultAppliesOnlyToNewChatTranscripts(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	old := contractChat(t, o, ws, "")
	path := func(chat api.AgentChat) string { return "/v1/resources/chat/" + chat.ID + "/transcript" }
	for _, who := range []string{"abir", "mia", "owner"} {
		o.do(t, who, "GET", path(old), -1, nil, 404, nil)
	}
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Defaults["chat"] = acl.KindDefault{Visibility: acl.Org} })
	fresh := contractChat(t, o, ws, "")
	for _, who := range []string{"abir", "mia", "owner"} {
		o.do(t, who, "GET", path(fresh), -1, nil, 200, nil)
		o.do(t, who, "GET", path(old), -1, nil, 404, nil)
	}
}

func TestContractK5TranscriptHasAnAllowlistAndNeverTouchesTheConversation(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	seat := agentSeat{ws, "santosh", chat.ID}
	dir := o.h.agentDir(seat)
	entryPath := filepath.Join(dir, "entries.jsonl")
	keepContractEntry(t, entryPath, api.AgentEntry{Seq: 1, Kind: api.AgentEntryPerson, By: "santosh", Text: "Keep this"})
	beforeMeta, ok := readChatMeta(dir)
	if !ok {
		t.Fatal("missing chat metadata")
	}
	beforeLines, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatal(err)
	}
	_, beforeEvents, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var before, after struct{ Events []json.RawMessage }
	o.do(t, "santosh", "GET", "/v1/audit", -1, nil, 200, &before)
	var fields map[string]json.RawMessage
	o.do(t, "santosh", "GET", "/v1/resources/chat/"+chat.ID+"/transcript", -1, nil, 200, &fields)
	keys := []string{}
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"entries", "id", "kind", "seq", "state"}) {
		t.Fatalf("transcript fields: %v", keys)
	}
	o.do(t, "santosh", "GET", "/v1/audit", -1, nil, 200, &after)
	afterMeta, ok := readChatMeta(dir)
	afterLines, linesErr := os.ReadFile(entryPath)
	_, afterEvents, eventsErr := live.Read(o.st.Root())
	o.h.agentDesk.mu.Lock()
	seatCreated := o.h.agentDesk.agents[seat] != nil
	o.h.agentDesk.mu.Unlock()
	if !ok || linesErr != nil || eventsErr != nil || beforeMeta != afterMeta ||
		string(beforeLines) != string(afterLines) || beforeEvents != afterEvents || !reflect.DeepEqual(before, after) || seatCreated {
		t.Fatal("reading the transcript changed the conversation")
	}
}
