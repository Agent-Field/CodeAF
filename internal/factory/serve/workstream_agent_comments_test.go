package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE AGENT READS A DOCUMENT'S COMMENTS AND ANSWERS THEM IN THEIR THREADS (F2
// slice 4). ws_file_read hands it every thread on the document; ws_file_comment
// answers one, or starts one on words quoted once, as Afi for the person, and
// the document's words never change. What Afi writes stays unread for the
// person it answered for.
func TestServerAgentReadsAndAnswersPassageComments(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	path := "/v1/workstreams/" + webhook.ID

	var thread, page string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		d := turn.Door.(workDoor)
		work := func(tool, args string) map[string]any {
			t.Helper()
			line, err := d.Work(ctx, tool, json.RawMessage(args))
			if err != nil {
				t.Fatalf("%s %s: %v", tool, args, err)
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(line), &out); err != nil {
				t.Fatalf("%s answered %q", tool, line)
			}
			return out
		}
		refused := func(tool, args, want string) {
			t.Helper()
			if line, err := d.Work(ctx, tool, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s %s = %q, %v; want a refusal saying %q", tool, args, line, err, want)
			}
		}
		if thread == "" {
			work("ws_file_create", `{"name":"Launch plan","markdown":"We ship the café build on Friday.\n\nThe café build is the second one."}`)
			return AgentTurnEnd{}, nil
		}
		files := work("ws_files", `{}`)["files"].([]any)
		var doc map[string]any
		for _, f := range files {
			if f.(map[string]any)["name"] == "Launch plan" {
				doc = f.(map[string]any)
			}
		}
		id := doc["id"].(string)
		read := work("ws_file_read", `{"file":"`+id+`"}`)
		threads, _ := read["comments"].([]any)
		if len(threads) != 1 {
			t.Fatalf("ws_file_read handed the comments %v", read["comments"])
		}
		got := threads[0].(map[string]any)
		said := got["comments"].([]any)[0].(map[string]any)
		if got["thread"] != thread || got["quote"] != "Friday" || said["by"] != o.h.personNames()("abir") || said["text"] != "Which Friday?" {
			t.Fatalf("the thread as read: %v", got)
		}
		work("ws_file_comment", `{"file":"`+id+`","thread":"`+thread+`","text":"The 21st."}`)
		refused("ws_file_comment", `{"file":"`+id+`","quote":"café build","text":"Which one?"}`, "more than once")
		refused("ws_file_comment", `{"file":"`+id+`","quote":"Monday","text":"?"}`, "does not say those words")
		refused("ws_file_comment", `{"file":"`+id+`","thread":"pc_nope","text":"?"}`, "no comment thread")
		work("ws_file_comment", `{"file":"`+id+`","quote":"second one","text":"Is this still true?"}`)
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", path+"/agent/messages", -1, api.AgentMessageNew{Text: "Write the plan."}, 202, nil)
	o.settle()

	var ws api.Workstream
	o.do(t, "mia", "GET", path, -1, nil, 200, &ws)
	for _, p := range ws.Pages {
		if p.Title == "Launch plan" {
			page = p.ID
		}
	}
	var made api.PageCommentThread
	o.do(t, "abir", "POST", path+"/pages/"+page+"/comments", -1, api.PageCommentNew{Block: []uint32{0}, BlockText: "We ship the café build on Friday.", Offset: 26, Quote: "Friday", Text: "Which Friday?"}, 201, &made)
	thread = made.ID
	o.do(t, "mia", "POST", path+"/agent/messages", -1, api.AgentMessageNew{Text: "Answer the comments."}, 202, nil)
	o.settle()

	var all api.PageComments
	o.do(t, "mia", "GET", path+"/pages/"+page+"/comments", -1, nil, 200, &all)
	if len(all.Threads) != 2 {
		t.Fatalf("threads %+v", all.Threads)
	}
	answered, started := all.Threads[0], all.Threads[1]
	if n := len(answered.Comments); n != 2 || answered.Comments[1].By.Kind != "afi" || answered.Comments[1].Text != "The 21st." {
		t.Fatalf("the answer %+v", answered.Comments)
	}
	if started.Quote != "second one" || len(started.Block) != 0 || started.BlockText != "second one" || started.Comments[0].By.Kind != "afi" {
		t.Fatalf("the thread Afi started %+v", started)
	}
	if !answered.Unread || !started.Unread {
		t.Fatal("what Afi wrote for mia is still for mia to read")
	}
	var after api.WorkstreamFiles
	o.do(t, "mia", "GET", path+"/files", -1, nil, 200, &after)
	for _, f := range after.Files {
		if f.PageID == page && f.OpenComments != 2 {
			t.Fatalf("the document counts %d open threads", f.OpenComments)
		}
	}
}
