package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func contractChat(t *testing.T, o *wsOrg, ws, agent string) api.AgentChat {
	t.Helper()
	if o.h.config.WorkstreamAgent == nil {
		o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) {
			t.Error("the contract fixture started an unexpected agent turn")
			return AgentTurnEnd{}, nil
		}
	}
	var chat api.AgentChat
	o.do(t, "santosh", "POST", "/v1/workstreams/"+ws+"/chats", -1, api.AgentChatNew{Title: "The draft", Agent: agent}, 201, &chat)
	return chat
}

func contractSpace(t *testing.T, o *wsOrg, who string) string {
	t.Helper()
	var space api.MySpace
	o.do(t, who, "GET", "/v1/myspace", -1, nil, 200, &space)
	return space.Workstream.ID
}

func contractMove(t *testing.T, o *wsOrg, who, chat, to string, status int, out *api.ResourceMoved) {
	t.Helper()
	var reply any
	if out != nil {
		reply = out
	}
	o.do(t, who, "POST", "/v1/resources/chat/"+chat+"/move", -1, api.ResourceMove{To: to}, status, reply)
}

func contractWorkstreamShares(t *testing.T, o *wsOrg, ws, who, role string) {
	t.Helper()
	shares := []factory.Share{{Kind: "person", ID: who, Role: role}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws+"/shares", -1, api.WorkstreamSharesPut{Shares: &shares}, 200, nil)
}

func TestContractI1ChatMovesItsDirectoryAndLegacyReads(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "santosh", api.WorkstreamNew{Name: "Destination"}).ID
	chat := contractChat(t, o, from, "")
	source := o.h.chatDir(from, "santosh", chat.ID)
	destination := o.h.chatDir(to, "santosh", chat.ID)
	keepContractEntry(t, filepath.Join(source, "entries.jsonl"), api.AgentEntry{Seq: 1, By: "santosh", Text: "The original draft"})
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	var prior, after api.WorkstreamAgent
	o.do(t, "santosh", "GET", "/v1/workstreams/"+from+"/agent?chat="+chat.ID, -1, nil, 200, &prior)
	contractMove(t, o, "santosh", chat.ID, to, 200, nil)
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source directory still exists: %v", err)
	}
	if moved, err := os.Stat(destination); err != nil || !os.SameFile(before, moved) {
		t.Fatalf("the directory was not moved: %v", err)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+from+"/agent?chat="+chat.ID, -1, nil, 404, nil)
	o.do(t, "santosh", "GET", "/v1/workstreams/"+to+"/agent?chat="+chat.ID, -1, nil, 200, &after)
	if !reflect.DeepEqual(after.Entries, prior.Entries) || after.Seq != prior.Seq || after.ID != to {
		t.Fatalf("conversation before = %+v, after = %+v", prior, after)
	}
	var src, dest api.AgentChatList
	o.do(t, "santosh", "GET", "/v1/workstreams/"+from+"/chats", -1, nil, 200, &src)
	o.do(t, "santosh", "GET", "/v1/workstreams/"+to+"/chats", -1, nil, 200, &dest)
	if len(src.Chats) != 0 || len(dest.Chats) != 1 || dest.Chats[0].ID != chat.ID {
		t.Fatalf("lists after move: source %+v, destination %+v", src, dest)
	}
}

func TestContractI2OnlyTheOwnerCanMoveToAnEditableDestination(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "abir", api.WorkstreamNew{Name: "View only"}).ID
	hidden := o.create(t, "mia", api.WorkstreamNew{Name: "Hidden"}).ID
	chat := contractChat(t, o, from, "")
	path := "/v1/resources/chat/" + chat.ID
	o.do(t, "abir", "POST", accessWorkstreamPath(to)+"/grants", -1, personGrant("santosh", "view"), 201, nil)
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("abir", "full"), 201, nil)
	contractMove(t, o, "abir", chat.ID, to, 403, nil)
	accessAgentRequest(t, o, "POST", path+"/move", "santosh", "afi", api.ResourceMove{To: contractSpace(t, o, "santosh")}, 403, nil)
	contractMove(t, o, "santosh", chat.ID, to, 403, nil)
	contractMove(t, o, "santosh", chat.ID, hidden, 404, nil)
	contractMove(t, o, "santosh", chat.ID, contractSpace(t, o, "abir"), 404, nil)
	w := o.raw(t, "santosh", "POST", path+"/move", api.ResourceMove{To: from})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "it is already there") {
		t.Fatalf("same parent: %d %s", w.Code, w.Body)
	}
}

func TestContractI3OwnerAndVisibilityFollowTheChatRules(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "abir", api.WorkstreamNew{Name: "Destination"}).ID
	o.do(t, "abir", "POST", accessWorkstreamPath(to)+"/grants", -1, personGrant("santosh", "edit"), 201, nil)
	chat := contractChat(t, o, from, "")
	path := "/v1/resources/chat/" + chat.ID
	var grant acl.Grant
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("mia", "view"), 201, &grant)
	var moved api.ResourceMoved
	contractMove(t, o, "santosh", chat.ID, to, 200, &moved)
	if moved.Resource.Parent == nil || moved.Resource.Parent.ID != to || moved.Resource.Owner.ID != "santosh" || len(moved.Dropped) != 0 {
		t.Fatalf("into workstream: %+v", moved)
	}
	o.do(t, "mia", "GET", path, -1, nil, 200, nil)
	space := contractSpace(t, o, "santosh")
	contractMove(t, o, "santosh", chat.ID, space, 200, &moved)
	if moved.Resource.Parent == nil || moved.Resource.Parent.ID != space || moved.Resource.Owner.ID != "santosh" ||
		moved.Resource.Visibility != acl.Private || len(moved.Dropped) != 1 || moved.Dropped[0].ID != grant.ID {
		t.Fatalf("into My space: %+v", moved)
	}
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
}

func TestContractI4ChatMoveSendsOneEventToTheUnionAndRecordsIt(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "santosh", api.WorkstreamNew{Name: "Destination"}).ID
	contractWorkstreamShares(t, o, from, "abir", "view")
	contractWorkstreamShares(t, o, to, "mia", "view")
	chat := contractChat(t, o, from, "")
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+chat.ID+"/grants", -1, personGrant("abir", "view"), 201, nil)
	events := contractResourceEvents(t, o, chat.ID)
	contractMove(t, o, "santosh", chat.ID, to, 200, nil)
	got := contractResourceEvents(t, o, chat.ID)
	// CONTRACT CHANGE (org-follow A4): a chat shared with abir stays private, so the
	// destination's people (mia) and the operator do not hear of it; only the owner
	// and the grantee do. Before, the first grant opened it to the destination too.
	if len(got) != len(events)+1 || !got[len(got)-1].itemAudience.Equal(acl.Audience{
		Persons: []string{"abir", "santosh"},
	}) {
		t.Fatalf("move events: before %+v, after %+v", events, got)
	}
	private := contractChat(t, o, from, "")
	contractMove(t, o, "santosh", private.ID, to, 200, nil)
	got = contractResourceEvents(t, o, private.ID)
	if len(got) != 1 || !got[0].itemAudience.Equal(acl.Audience{Persons: []string{"santosh"}}) {
		t.Fatalf("private move events: %+v", got)
	}
	var audit struct{ Events []record.Event }
	o.do(t, "santosh", "GET", "/v1/audit", -1, nil, 200, &audit)
	if !slices.ContainsFunc(audit.Events, func(e record.Event) bool {
		return e.Verb == "moved" && e.Handle == "santosh" && e.Object == orgObjectResource+"chat/"+chat.ID
	}) {
		t.Fatalf("move missing from record: %+v", audit.Events)
	}
}

func contractResourceEvents(t *testing.T, o *wsOrg, id string) []resourceLive {
	t.Helper()
	all, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var out []resourceLive
	for _, e := range all {
		var row resourceLive
		if e.Kind == eventResourceChanged && json.Unmarshal(e.Data, &row) == nil && row.ID == id {
			out = append(out, row)
		}
	}
	return out
}

func TestContractI5ChatMoveWaitsAndRechecksAgentAndSpend(t *testing.T) {
	o := newWSOrg(t)
	started := make(chan AgentTurn, 2)
	finish := make(chan struct{})
	o.driverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		started <- turn
		select {
		case <-ctx.Done():
			return AgentTurnEnd{}, ctx.Err()
		case <-finish:
			turn.Step(api.AgentEntryAgent, "Done.", "", false)
			return AgentTurnEnd{USD: 0.25}, nil
		}
	})
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "santosh", api.WorkstreamNew{Name: "Destination"}).ID
	archived := o.create(t, "santosh", api.WorkstreamNew{Name: "Archived"}).ID
	agent := o.agent(t, "santosh", "Critic", "Find a weak claim.")
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+from+"/agents/"+agent.ID, -1, nil, 200, nil)
	o.do(t, "santosh", "DELETE", "/v1/workstreams/"+archived, -1, nil, 204, nil)
	chat := contractChat(t, o, from, agent.ID)
	body := api.AgentMessageNew{Text: "Read the draft", Chat: chat.ID}
	path := "/v1/resources/chat/" + chat.ID
	o.do(t, "santosh", "POST", "/v1/workstreams/"+from+"/agent/messages", -1, body, 202, nil)
	if first := <-started; first.Workstream != from {
		t.Fatalf("first turn: %+v", first)
	}
	w := o.raw(t, "santosh", "POST", path+"/move", api.ResourceMove{To: to})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "stop it or wait for it to finish, then move it") {
		t.Fatalf("busy: %d %s", w.Code, w.Body)
	}
	close(finish)
	o.settle()
	w = o.raw(t, "santosh", "POST", path+"/move", api.ResourceMove{To: archived})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "This workstream is archived.") {
		t.Fatalf("archived: %d %s", w.Code, w.Body)
	}
	var moved api.ResourceMoved
	contractMove(t, o, "santosh", chat.ID, to, 200, &moved)
	var wire struct {
		Usable *bool  `json:"agent_usable"`
		Words  string `json:"agent_words"`
	}
	raw, _ := json.Marshal(moved)
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Usable == nil || *wire.Usable || wire.Words == "" {
		t.Fatalf("unusable agent: %s (%v)", raw, err)
	}
	door := "/v1/workstreams/" + to + "/agent"
	w = o.raw(t, "santosh", "POST", door+"/messages", body)
	if w.Code != 409 || !strings.Contains(w.Body.String(), wire.Words) {
		t.Fatalf("agent refusal: %d %s, want %q", w.Code, w.Body, wire.Words)
	}
	o.do(t, "santosh", "GET", door+"?chat="+chat.ID, -1, nil, 200, nil)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+to+"/agents/"+agent.ID, -1, nil, 200, nil)
	o.do(t, "santosh", "POST", door+"/messages", -1, body, 202, nil)
	if second := <-started; second.Workstream != to || second.Agent == nil || second.Agent.ID != agent.ID {
		t.Fatalf("destination turn: %+v", second)
	}
	o.settle()
	spent, err := o.st.WorkstreamSpend()
	if err != nil || spent[from] != 0.25 || spent[to] != 0.25 {
		t.Fatalf("spend = %v, %v", spent, err)
	}
}

func TestContractI6ChatMoveFieldsAreAdditive(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "santosh", api.WorkstreamNew{Name: "Destination"}).ID
	chat := contractChat(t, o, from, "")
	w := o.raw(t, "santosh", "POST", "/v1/resources/chat/"+chat.ID+"/move", api.ResourceMove{To: to})
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || w.Code != 200 || string(fields["agent_usable"]) != "true" ||
		fields["agent_words"] != nil || fields["resource"] == nil || string(fields["dropped"]) != "[]" {
		t.Fatalf("move response: %d %s, %v", w.Code, w.Body, err)
	}
	raw, err := json.Marshal(api.ResourceMoved{Dropped: []api.GrantTuple{}})
	if err != nil || strings.Contains(string(raw), "agent_") {
		t.Fatalf("canvas move shape changed: %s, %v", raw, err)
	}
}

func TestContractI7SpacesWorkstreamsAndSessionsStayPut(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Workstream"}).ID
	space := contractSpace(t, o, "santosh")
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws+"/agent", -1, nil, 200, nil)
	for _, res := range []string{"space/" + space, "workstream/" + ws, "session/afi:" + ws + ":santosh"} {
		w := o.raw(t, "santosh", "POST", "/v1/resources/"+res+"/move", api.ResourceMove{To: ws})
		if w.Code != 409 || !strings.Contains(w.Body.String(), "error") {
			t.Fatalf("%s: %d %s", res, w.Code, w.Body)
		}
	}
}

func TestContractI8CollisionRollbackStaleDoorAndRestart(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := contractSpace(t, o, "santosh")
	chat := contractChat(t, o, from, "")
	path := "/v1/resources/chat/" + chat.ID
	source := o.h.chatDir(from, "santosh", chat.ID)
	dest := o.h.chatDir(to, "santosh", chat.ID)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	contractMove(t, o, "santosh", chat.ID, to, 409, nil)
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	// A file in place of the destination parent makes the directory move fail.
	if err := os.Remove(filepath.Dir(dest)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(dest), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	contractMove(t, o, "santosh", chat.ID, to, 500, nil)
	var res api.OrgResource
	o.do(t, "santosh", "GET", path, -1, nil, 200, &res)
	if res.Parent == nil || res.Parent.ID != from || !existsOnDisk(source) {
		t.Fatalf("failed move did not keep source: %+v", res)
	}
	if err := os.Remove(filepath.Dir(dest)); err != nil {
		t.Fatal(err)
	}

	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("mia", "view"), 201, nil)
	row, _, err := o.st.Resource("chat", chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.Parent = acl.Resource{Kind: "space", ID: to}
	row.Visibility = acl.Private
	if err := o.st.PutResource(row); err != nil {
		t.Fatal(err)
	}
	restartAccessOrg(t, o)
	if existsOnDisk(source) || !existsOnDisk(dest) {
		t.Fatal("restart left the folder at the source")
	}
	if grants, err := o.st.GrantsOn("chat", chat.ID); err != nil || len(grants) != 0 {
		t.Fatalf("restart left chat grants: %v, %v", grants, err)
	}
	old, _, err := o.st.Workstream(from)
	if err != nil {
		t.Fatal(err)
	}
	err = o.h.startAgentTurn(old, o.h.viewerOf("santosh"), "santosh", chat.ID, "A stale message", "", false)
	if !errors.Is(err, errChatNotHere) || existsOnDisk(source) {
		t.Fatalf("stale message: %v", err)
	}
	if events := contractResourceEvents(t, o, chat.ID); len(events) != 1 {
		t.Fatalf("an uncommitted move emitted an event: %+v", events)
	}
}

// I9, as changed by org-follow A9 (Abir 2026-11-24): a chat moved into a shared
// workstream keeps its privacy, so the workstream's people are not told of it
// until it is shared; once shared they see its metadata only.
func TestContractI9SharedWorkstreamChatListKeepsOnlyMetadata(t *testing.T) {
	o := newWSOrg(t)
	from := contractSpace(t, o, "santosh")
	to := o.create(t, "santosh", api.WorkstreamNew{Name: "Destination"}).ID
	contractWorkstreamShares(t, o, to, "abir", "edit")
	chat := contractChat(t, o, from, "")
	contractMove(t, o, "santosh", chat.ID, to, 200, nil)
	var rows api.AgentChatList
	door := "/v1/workstreams/" + to
	o.do(t, "abir", "GET", door+"/chats", -1, nil, http.StatusOK, &rows)
	if len(rows.Others) != 0 {
		t.Fatalf("a moved private chat is listed: %+v", rows)
	}
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+chat.ID+"/grants", -1, personGrant("abir", "view"), 201, nil)
	o.do(t, "abir", "GET", door+"/chats", -1, nil, http.StatusOK, &rows)
	if len(rows.Others) != 1 || rows.Others[0].ID != chat.ID || rows.Others[0].Title != chat.Title ||
		rows.Others[0].Owner != "santosh" || rows.Others[0].State == "" || rows.Others[0].LastActivity == "" {
		t.Fatalf("shared metadata: %+v", rows)
	}
	o.do(t, "abir", "GET", door+"/agent?chat="+chat.ID, -1, nil, 404, nil)
	contractMove(t, o, "santosh", chat.ID, from, 200, nil)
	o.do(t, "abir", "GET", door+"/chats", -1, nil, 200, &rows)
	if len(rows.Others) != 0 {
		t.Fatalf("My space chat still listed: %+v", rows)
	}
}

func TestContractI8IdleSeatStillRequiresRunCompletion(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "santosh", api.WorkstreamNew{Name: "Destination"}).ID
	chat := contractChat(t, o, from, "")
	done := make(chan struct{})
	seat := agentSeat{from, "santosh", chat.ID}
	o.h.agentDesk.mu.Lock()
	a := o.h.agentOfLocked(seat)
	a.done = done
	o.h.agentDesk.mu.Unlock()
	contractMove(t, o, "santosh", chat.ID, to, 409, nil)
	close(done)
	contractMove(t, o, "santosh", chat.ID, to, 200, nil)
}

func TestContractI8RestartFinishesPrivateCleanupAfterTheRename(t *testing.T) {
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := contractSpace(t, o, "santosh")
	chat := contractChat(t, o, from, "")
	path := "/v1/resources/chat/" + chat.ID
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("mia", "view"), 201, nil)
	row, found, err := o.st.Resource("chat", chat.ID)
	if err != nil || !found {
		t.Fatalf("chat missing from registry: %v", err)
	}
	row.Parent = acl.Resource{Kind: "space", ID: to}
	row.Visibility = acl.Private
	o.h.Close()
	if err := o.st.WithResourceMove(func(move store.ResourceMove) error {
		_, err := move.MoveResource(row, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fromDir := o.h.chatDir(from, "santosh", chat.ID)
	toDir := o.h.chatDir(to, "santosh", chat.ID)
	if err := os.MkdirAll(filepath.Dir(toDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fromDir, toDir); err != nil {
		t.Fatal(err)
	}
	restartAccessOrg(t, o)
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
	if grants, err := o.st.GrantsOn("chat", chat.ID); err != nil || len(grants) != 0 {
		t.Fatalf("unfinished cleanup: %+v, %v", grants, err)
	}

	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("abir", "view"), 201, nil)
	restartAccessOrg(t, o)
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
}
