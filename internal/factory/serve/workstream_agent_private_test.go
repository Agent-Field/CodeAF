package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AFI'S PANE IS PRIVATE TO EACH PERSON (L1 F2), over the wire: santosh and
// abir, each signed in, talk to one organization server on a real loopback
// listener. Each reads only their own turns with the workstream's agent;
// both see the tasks it made, because tasks are shared.
func TestTwoPeopleEachHaveTheirOwnAfi(t *testing.T) {
	o := newWSOrg(t)
	srv := httptest.NewServer(o.h)
	t.Cleanup(srv.Close)
	call := func(who, method, path string, body any, out any) int {
		t.Helper()
		var buf io.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			buf = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, srv.URL+path, buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+o.tokens[who])
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil && res.StatusCode < 300 {
			if err := json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode
	}
	var ws api.Workstream
	if got := call("santosh", "POST", "/v1/workstreams", api.WorkstreamNew{Name: "Pantry"}, &ws); got != 201 {
		t.Fatalf("create = %d", got)
	}
	edit := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	if got := call("santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", api.WorkstreamSharesPut{Shares: &edit, Rev: &ws.Rev}, nil); got != 200 {
		t.Fatalf("share = %d", got)
	}
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		d := turn.Door.(workDoor)
		args, _ := json.Marshal(map[string]any{"title": "For " + turn.Asker, "request": turn.Text, "goal": "Ship it right", "deliverable": "A document in Files", "acceptance": []string{"Covers the change", "Plain words"}})
		if _, err := d.Work(ctx, "ws_task_create", args); err != nil {
			t.Errorf("%s's task: %v", turn.Asker, err)
		}
		turn.Step(api.AgentEntryAgent, "Made your task, "+turn.Asker+".", "", false)
		return AgentTurnEnd{}, nil
	})
	path := "/v1/workstreams/" + ws.ID + "/agent"
	if got := call("santosh", "POST", path+"/messages", api.AgentMessageNew{Text: "salary bands are secret"}, nil); got != 202 {
		t.Fatalf("santosh's message = %d", got)
	}
	o.settle()
	if got := call("abir", "POST", path+"/messages", api.AgentMessageNew{Text: "revise the spec between us"}, nil); got != 202 {
		t.Fatalf("abir's message = %d", got)
	}
	o.settle()

	for who, other := range map[string]string{"santosh": "abir", "abir": "santosh"} {
		var got api.WorkstreamAgent
		if code := call(who, "GET", path, nil, &got); code != 200 {
			t.Fatalf("%s reads = %d", who, code)
		}
		raw, _ := json.Marshal(got.Entries)
		if !got.Private || len(got.Entries) != 2 || got.Entries[0].By != who || strings.Contains(string(raw), other) {
			t.Fatalf("%s reads %s", who, raw)
		}
		var tasks api.WorkstreamTasks
		call(who, "GET", "/v1/workstreams/"+ws.ID+"/tasks", nil, &tasks)
		if len(tasks.Tasks) != 2 {
			t.Fatalf("%s sees %d of Afi's tasks", who, len(tasks.Tasks))
		}
		for _, task := range tasks.Tasks {
			if task.DoneBy.Kind != "afi" || task.Owner.ID != strings.TrimPrefix(task.Title, "For ") {
				t.Fatalf("%s sees the task %+v", who, task)
			}
		}
	}
	// The operator, who only views, reads nobody's conversation.
	var op api.WorkstreamAgent
	call("owner", "GET", path, nil, &op)
	if len(op.Entries) != 0 {
		t.Fatalf("the operator reads %+v", op.Entries)
	}
}

// A WORKSTREAM THAT HAD ONE SHARED CONVERSATION hands each person only their
// own turns from it, once; nobody else's line is copied.
func TestTheSharedConversationIsSplitByPerson(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) { return AgentTurnEnd{}, nil })
	shared := filepath.Join(o.h.sharedAgentDir(webhook.ID), "entries.jsonl")
	var lines []string
	for i, e := range []api.AgentEntry{
		{Kind: "person", By: "mia", Text: "mia's secret", Turn: "t1"},
		{Kind: "agent", By: "mia", Text: "answer to mia", Turn: "t1"},
		{Kind: "person", By: "abir", Text: "abir's ask", Turn: "t2"},
		{Kind: "agent", By: "abir", Text: "answer to abir", Turn: "t2"},
	} {
		e.Seq = i + 1
		raw, _ := json.Marshal(e)
		lines = append(lines, string(raw))
	}
	if err := os.MkdirAll(filepath.Dir(shared), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got api.WorkstreamAgent
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &got)
	raw, _ := json.Marshal(got.Entries)
	if len(got.Entries) != 2 || got.Seq != 2 || strings.Contains(string(raw), "mia") || got.Entries[1].Text != "answer to abir" {
		t.Fatalf("abir's conversation %s", raw)
	}
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &got)
	if len(got.Entries) != 2 || got.Entries[0].Text != "mia's secret" {
		t.Fatalf("mia's conversation %+v", got.Entries)
	}
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "next"}, 202, &got)
	o.settle()
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent?since=2", -1, nil, 200, &got)
	if len(got.Entries) != 1 || got.Entries[0].Seq != 3 || got.Entries[0].Text != "next" {
		t.Fatalf("mia's next line %+v (seq %s)", got.Entries, strconv.Itoa(got.Seq))
	}
}
