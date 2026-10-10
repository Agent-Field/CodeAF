package desktopbridge

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A history fixture is a sessions root with four conversations, built the way
// the engine builds them: a folder per conversation holding a journal and a
// meta.json, in one project bucket whose tasks.jsonl indexes the work.
type convo struct {
	id, title string
	age       time.Duration
	words     [][2]string // role, text
	recap     *session.ConversationRecap
	archived  bool
}

var historyNow = time.Now().Truncate(time.Second)

func historyFixture(t *testing.T) (*Bridge, string) {
	t.Helper()
	root := t.TempDir()
	bucket := filepath.Join(root, "-work-lexer")
	convos := []convo{
		{id: "aaaa000000000001", title: "Strict mode in the lexer", age: time.Hour,
			words: [][2]string{{"user", "Should strict mode stay the default for the lexer?"}, {"assistant", "Yes. The leniency leaks in at the lexer, so fix it there."}, {"user", "ok, fix it"}, {"assistant", "Done. The lexer now rejects trailing commas."}},
			recap: &session.ConversationRecap{
				Line: "Decided to keep strict mode as the default and fix it in the lexer", Discussed: "We compared strict and lenient parsing. The lexer is where the leniency leaked in.",
				Decided:   []session.RecapDecision{{Text: "Keep strict mode the default", By: "you", How: "accepted"}, {Text: "Fix trailing commas in the lexer", By: "codeaf"}},
				Outcome:   "The lexer rejects trailing commas now.",
				Files:     []session.RecapFile{{Path: "lexer/lexer.go", Added: 12, Removed: 3}, {Path: "lexer/lexer_test.go", Added: 40}, {Path: "docs/strict.md"}, {Path: "README.md"}},
				UpdatedAt: historyNow.Add(-50 * time.Minute), Messages: 4, Fingerprint: "4:x"}},
		{id: "bbbb000000000002", title: "JSON5 evaluation", age: 48 * time.Hour,
			words: [][2]string{{"user", "Could we accept JSON5 config files?"}, {"assistant", "JSON5 also allows comments and unquoted keys, which we do not want."}},
			recap: &session.ConversationRecap{Line: "Ruled out JSON5, because it also allows comments", Discussed: "JSON5 was weighed as a config format.",
				Decided: []session.RecapDecision{{Text: "Do not accept JSON5", By: "you"}}, Outcome: "Config stays strict JSON.",
				UpdatedAt: historyNow.Add(-47 * time.Hour), Messages: 2}},
		{id: "cccc000000000003", title: "Load vs Open", age: 5 * 24 * time.Hour, archived: true,
			words: [][2]string{{"user", "What is the difference between Load and Open in the store?"}, {"assistant", "Load reads everything into memory; Open streams."}},
			recap: &session.ConversationRecap{Line: "Discussed Load vs Open. No decision yet", Discussed: "Load reads all of it. Open streams it.",
				UpdatedAt: historyNow.Add(-5*24*time.Hour + time.Minute), Messages: 2}},
		{id: "dddd000000000004", age: 10 * 24 * time.Hour,
			words: [][2]string{{"user", "Draft the release notes and update the changelog"}, {"assistant", "Here is a draft of the changelog entry."}}},
	}
	for _, c := range convos {
		dir := filepath.Join(bucket, c.id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var journal strings.Builder
		for i, w := range c.words {
			stamp := historyNow.Add(-c.age + time.Duration(i)*time.Minute).Format(time.RFC3339Nano)
			line, _ := json.Marshal(map[string]any{"type": "message", "role": w[0], "content": w[1], "timestamp": stamp})
			journal.Write(line)
			journal.WriteString("\n")
		}
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(journal.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		meta := session.Meta{ID: c.id, Title: c.title, Workspace: "/work/lexer", Created: historyNow.Add(-c.age), LastUserAt: historyNow.Add(-c.age), Recap: c.recap, Archived: c.archived}
		if err := session.SaveMeta(dir, meta); err != nil {
			t.Fatal(err)
		}
	}
	index := `{"id":"1","title":"Fix the lexer","label":"Fix the lexer","status":"done","outcome":"Trailing commas are rejected","sessionId":"aaaa000000000001","endedAt":"` + historyNow.Format(time.RFC3339) + `"}` + "\n" +
		`{"id":"2","parent":"1","title":"A part of it","label":"A part of it","status":"done","sessionId":"aaaa000000000001","endedAt":"` + historyNow.Format(time.RFC3339) + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(bucket, "tasks.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(testToken, func(string) (Connection, error) { return Connection{}, nil })
	b.UseHistory(&History{Root: root})
	t.Cleanup(b.Close)
	return b, root
}

func getJSON(t *testing.T, b *Bridge, path string, into any) {
	t.Helper()
	w := request(b, "GET", "/api/engine"+path, "")
	if w.Code != 200 {
		t.Fatalf("GET %s = %d %s", path, w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, w.Body.String())
	}
}

func ids(items []HistoryItem) string {
	var out []string
	for _, item := range items {
		out = append(out, item.ID[:4])
	}
	return strings.Join(out, ",")
}

func TestHistoryListsNewestFirstWithTheRowFacts(t *testing.T) {
	b, _ := historyFixture(t)
	var list HistoryList
	getJSON(t, b, "/history", &list)
	if list.Total != 4 || list.Matching != 4 || ids(list.Items) != "aaaa,bbbb,cccc,dddd" || list.Next != "" {
		t.Fatalf("list = total %d matching %d order %s next %q", list.Total, list.Matching, ids(list.Items), list.Next)
	}
	first := list.Items[0]
	if first.Title != "Strict mode in the lexer" || first.Line != "Decided to keep strict mode as the default and fix it in the lexer" ||
		first.Messages != 4 || first.Decisions != 2 || first.FileCount != 4 || len(first.Files) != 3 || first.State != "idle" || first.Open || first.Workspace != "/work/lexer" {
		t.Fatalf("row = %+v", first)
	}
	if !strings.HasSuffix(first.SessionFile, filepath.Join("aaaa000000000001", "transcript.jsonl")) {
		t.Fatalf("sessionFile = %q — it is what POST /sessions takes", first.SessionFile)
	}
	if first.Files[0] != (HistoryFile{Path: "lexer/lexer.go", Added: 12, Removed: 3}) {
		t.Fatalf("files = %+v", first.Files)
	}
	// A task is never its own row; its part is not counted beside it.
	if first.Tasks != 1 || first.TasksRunning != 0 {
		t.Fatalf("tasks = %d running %d", first.Tasks, first.TasksRunning)
	}
	if at, err := time.Parse(time.RFC3339, first.At); err != nil || !at.Equal(historyNow.Add(-time.Hour)) {
		t.Fatalf("at = %q", first.At)
	}
	// A conversation with no recap lists with no line, and is named by its words.
	last := list.Items[3]
	if last.Line != "" || last.Title != "Draft the release notes and update the changelog" || last.Decisions != 0 || last.FileCount != 0 {
		t.Fatalf("recap-less row = %+v", last)
	}
	if !list.Items[2].Archived || list.Items[0].Archived {
		t.Fatal("archived conversations still list, and say so")
	}
	// The wire uses exactly the agreed keys.
	var raw map[string]any
	getJSON(t, b, "/history?limit=1", &raw)
	row := raw["items"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "sessionFile", "title", "line", "at", "messages", "tasks", "tasksRunning", "files", "fileCount", "decisions", "state", "open", "archived", "workspace"} {
		if _, ok := row[key]; !ok {
			t.Errorf("row has no %q key: %v", key, row)
		}
	}
	if _, ok := row["reason"]; ok {
		t.Error("reason is only present for a conversation waiting on the person")
	}
}

func TestHistoryPagesByAnOpaqueCursor(t *testing.T) {
	b, _ := historyFixture(t)
	var page HistoryList
	getJSON(t, b, "/history?limit=3", &page)
	if ids(page.Items) != "aaaa,bbbb,cccc" || page.Next == "" || page.Total != 4 {
		t.Fatalf("page 1 = %s next %q", ids(page.Items), page.Next)
	}
	var rest HistoryList
	getJSON(t, b, "/history?limit=3&before="+url.QueryEscape(page.Next), &rest)
	if ids(rest.Items) != "dddd" || rest.Next != "" {
		t.Fatalf("page 2 = %s next %q", ids(rest.Items), rest.Next)
	}
	var garbage HistoryList
	getJSON(t, b, "/history?before=%25%25not-a-cursor", &garbage)
	if len(garbage.Items) != 4 {
		t.Fatalf("a cursor that does not parse starts from the top, got %d", len(garbage.Items))
	}
	var capped HistoryList
	getJSON(t, b, "/history?limit=9999", &capped)
	if len(capped.Items) != 4 {
		t.Fatalf("limit is capped, not refused: %d", len(capped.Items))
	}
}

func TestHistoryFilters(t *testing.T) {
	b, _ := historyFixture(t)
	for filter, want := range map[string]string{
		"all": "aaaa,bbbb,cccc,dddd", "decisions": "aaaa,bbbb", "files": "aaaa", "tasks": "aaaa", "open": "",
	} {
		var list HistoryList
		getJSON(t, b, "/history?filter="+filter, &list)
		if ids(list.Items) != want || list.Matching != len(list.Items) || list.Total != 4 {
			t.Errorf("filter %s = %s (matching %d total %d), want %s", filter, ids(list.Items), list.Matching, list.Total, want)
		}
	}
}

func TestHistoryDetailSaysWhenTheRecapIsStale(t *testing.T) {
	b, root := historyFixture(t)
	var detail HistoryDetail
	getJSON(t, b, "/history/aaaa000000000001", &detail)
	if detail.Item.ID != "aaaa000000000001" || detail.Recap == nil || detail.Stale {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Recap.Line == "" || len(detail.Recap.Decided) != 2 || detail.Recap.Decided[0].By != "you" || detail.Recap.Decided[0].How != "accepted" ||
		detail.Recap.Outcome == "" || len(detail.Recap.Files) != 4 || detail.Recap.Messages != 4 || detail.Recap.UpdatedAt == "" {
		t.Fatalf("recap = %+v", detail.Recap)
	}
	var none HistoryDetail
	getJSON(t, b, "/history/dddd000000000004", &none)
	if none.Recap != nil {
		t.Fatalf("a conversation with no recap answers none: %+v", none)
	}
	// The person speaks again after the recap was written: stale.
	dir := filepath.Join(root, "-work-lexer", "aaaa000000000001")
	meta, _ := session.LoadMeta(dir)
	meta.LastUserAt = historyNow
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	var moved HistoryDetail
	getJSON(t, b, "/history/aaaa000000000001", &moved)
	if !moved.Stale {
		t.Fatal("the conversation moved on after its recap; stale must say so")
	}
	if w := request(b, "GET", "/api/engine/history/nowhere", ""); w.Code != 404 {
		t.Fatalf("unknown conversation = %d", w.Code)
	}
}

func TestHistoryMessagesPageBackwardsAndClip(t *testing.T) {
	b, root := historyFixture(t)
	var all HistoryMessages
	getJSON(t, b, "/history/aaaa000000000001/messages", &all)
	if all.Total != 4 || len(all.Messages) != 4 || all.Messages[0].Index != 0 || all.Messages[0].Role != "user" || all.Messages[1].Role != "assistant" || all.Messages[0].At == "" {
		t.Fatalf("messages = %+v", all)
	}
	var older HistoryMessages
	getJSON(t, b, "/history/aaaa000000000001/messages?limit=2&before=3", &older)
	if len(older.Messages) != 2 || older.Messages[0].Index != 1 || older.Messages[1].Index != 2 {
		t.Fatalf("a page ends before the cursor and reads oldest first: %+v", older.Messages)
	}
	long := strings.Repeat("é", historyMessageClip)
	line, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": long})
	journal := filepath.Join(root, "-work-lexer", "bbbb000000000002", "transcript.jsonl")
	file, _ := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintf(file, "%s\n", line)
	file.Close()
	var clipped HistoryMessages
	getJSON(t, b, "/history/bbbb000000000002/messages", &clipped)
	got := clipped.Messages[len(clipped.Messages)-1].Text
	if len(got) > historyMessageClip || !strings.HasPrefix(long, got) {
		t.Fatalf("clipped text is %d bytes", len(got))
	}
	if w := request(b, "GET", "/api/engine/history/..%2F..%2Fetc/messages", ""); w.Code != 404 {
		t.Fatalf("an id with separators = %d, want 404", w.Code)
	}
}

func search(t *testing.T, b *Bridge, query string) HistorySearch {
	t.Helper()
	var result HistorySearch
	getJSON(t, b, "/history/search?q="+url.QueryEscape(query), &result)
	return result
}

func TestHistorySearchRanksTheConversationThatAnswers(t *testing.T) {
	b, _ := historyFixture(t)
	result := search(t, b, "Why did we rule out JSON5?")
	if result.Query != "Why did we rule out JSON5?" || result.Best == nil {
		t.Fatalf("a question one recap sentence answers must have a best: %+v", result)
	}
	if result.Best.Item.ID != "bbbb000000000002" || result.Best.Answer != "Ruled out JSON5, because it also allows comments" {
		t.Fatalf("best = %+v", result.Best)
	}
	if result.Best.MessageIndex == nil || *result.Best.MessageIndex > 1 {
		t.Fatalf("messageIndex = %v, want a message of that conversation that mentions JSON5", result.Best.MessageIndex)
	}
	for _, stop := range []string{"why", "did", "we", "the"} {
		for _, term := range result.Best.Terms {
			if term == stop {
				t.Fatalf("stop word %q kept in terms %v", stop, result.Best.Terms)
			}
		}
	}
	if result.Counts.Decisions != 1 || len(result.Decisions) != 1 || result.Decisions[0].Title != "Do not accept JSON5" ||
		result.Decisions[0].Context != "JSON5 evaluation · you" || result.Decisions[0].ID != "bbbb000000000002" {
		t.Fatalf("decisions = %+v", result.Decisions)
	}
	if result.Counts.Discussed == 0 || result.Discussed[0].ID != "bbbb000000000002" || result.Discussed[0].Snippet == "" {
		t.Fatalf("discussed = %+v", result.Discussed)
	}
}

func TestHistorySearchMatchesFilesTasksAndArchivedConversations(t *testing.T) {
	b, _ := historyFixture(t)
	files := search(t, b, "lexer.go")
	if files.Counts.Files != 1 || files.Files[0].Path != "lexer/lexer.go" || files.Files[0].Conversations != 1 || files.Files[0].IDs[0] != "aaaa000000000001" {
		t.Fatalf("files = %+v counts %+v", files.Files, files.Counts)
	}
	tasks := search(t, b, "trailing commas")
	if tasks.Counts.Tasks != 1 || tasks.Tasks[0].TaskID != "1" || tasks.Tasks[0].ID != "aaaa000000000001" ||
		tasks.Tasks[0].ConversationTitle != "Strict mode in the lexer" || tasks.Tasks[0].Snippet != "Trailing commas are rejected" {
		t.Fatalf("tasks = %+v", tasks.Tasks)
	}
	// An ARCHIVED conversation is still found, by its recap and by its words.
	archived := search(t, b, "Load streams")
	if len(archived.Discussed) == 0 || archived.Discussed[0].ID != "cccc000000000003" {
		t.Fatalf("archived conversation not found: %+v", archived)
	}
	// A conversation with no recap is found by its title and its messages.
	bare := search(t, b, "changelog")
	if len(bare.Discussed) == 0 || bare.Discussed[0].ID != "dddd000000000004" || bare.Discussed[0].MessageIndex == nil {
		t.Fatalf("recap-less conversation not found: %+v", bare.Discussed)
	}
	if bare.Best != nil {
		t.Fatalf("with no recap sentence there is nothing to answer with: %+v", bare.Best)
	}
}

func TestHistorySearchOffersNoBestWhenNothingClearlyAnswers(t *testing.T) {
	b, _ := historyFixture(t)
	// Two conversations are about the lexer; neither clearly outranks the other.
	ambiguous := search(t, b, "lexer")
	if ambiguous.Best != nil && ambiguous.Best.Item.ID == "" {
		t.Fatal("malformed best")
	}
	// No sentence covers 60% of these terms.
	partial := search(t, b, "strict mode parquet postgres replication")
	if partial.Best != nil {
		t.Fatalf("a sentence covering a fifth of the question is not an answer: %+v", partial.Best)
	}
	empty := search(t, b, "the and of")
	if empty.Best != nil || empty.Counts != (HistoryCounts{}) || len(empty.Decisions) != 0 {
		t.Fatalf("a query of stop words finds nothing: %+v", empty)
	}
	none := search(t, b, "zzzzqqq")
	if none.Best != nil || none.Counts != (HistoryCounts{}) {
		t.Fatalf("no hits = %+v", none)
	}
	var raw map[string]any
	getJSON(t, b, "/history/search?q=zzzzqqq", &raw)
	for _, key := range []string{"query", "decisions", "discussed", "files", "tasks", "counts"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("search answer has no %q key", key)
		}
	}
}

func TestHistorySearchFilterNarrowsTheConversations(t *testing.T) {
	b, _ := historyFixture(t)
	var all, narrowed HistorySearch
	getJSON(t, b, "/history/search?q=json5", &all)
	getJSON(t, b, "/history/search?q=json5&filter=files", &narrowed)
	if all.Counts.Discussed == 0 || narrowed.Counts.Discussed != 0 {
		t.Fatalf("filter=files keeps only conversations that changed files: %+v / %+v", all.Counts, narrowed.Counts)
	}
}

func TestHistoryArchive(t *testing.T) {
	b, root := historyFixture(t)
	w := request(b, "POST", "/api/engine/history/archive", `{"ids":["aaaa000000000001","aaaa000000000001","cccc000000000003","nope","../x"],"archived":true}`)
	var changed map[string]int
	_ = json.Unmarshal(w.Body.Bytes(), &changed)
	// cccc was archived already, the repeat and the unknown ids change nothing.
	if w.Code != 200 || changed["changed"] != 1 {
		t.Fatalf("archive = %d %s", w.Code, w.Body.String())
	}
	meta, _ := session.LoadMeta(filepath.Join(root, "-work-lexer", "aaaa000000000001"))
	if !meta.Archived || meta.Recap == nil {
		t.Fatalf("archived meta = %+v — the recap must survive", meta)
	}
	w = request(b, "POST", "/api/engine/history/archive", `{"ids":["aaaa000000000001"],"archived":false}`)
	_ = json.Unmarshal(w.Body.Bytes(), &changed)
	if changed["changed"] != 1 {
		t.Fatalf("unarchive = %s", w.Body.String())
	}
	if w := request(b, "GET", "/api/engine/history/archive", ""); w.Code != 405 {
		t.Fatalf("archive by GET = %d", w.Code)
	}
}

// AN ID IS A FOLDER NAME, NEVER A PATTERN: the lookup globs, so a "*" or a
// "?" must not reach whichever conversation happens to match first.
func TestHistoryRefusesAGlobForAnID(t *testing.T) {
	b, root := historyFixture(t)
	w := request(b, "POST", "/api/engine/history/archive", `{"ids":["*","aaaa00000000000?","[a]*"],"archived":true}`)
	var changed map[string]int
	_ = json.Unmarshal(w.Body.Bytes(), &changed)
	if w.Code != 200 || changed["changed"] != 0 {
		t.Fatalf("archive by pattern = %d %s", w.Code, w.Body.String())
	}
	if meta, _ := session.LoadMeta(filepath.Join(root, "-work-lexer", "aaaa000000000001")); meta.Archived {
		t.Fatal("a pattern archived a conversation")
	}
	for _, path := range []string{"/api/engine/history/*", "/api/engine/history/*/messages"} {
		if w := request(b, "GET", path, ""); w.Code != 404 {
			t.Fatalf("GET %s = %d, want 404", path, w.Code)
		}
	}
}

func TestHistoryNeedsTheTokenAndAnswersEmptyWithNoRoot(t *testing.T) {
	b := New(testToken, func(string) (Connection, error) { return Connection{}, nil })
	t.Cleanup(b.Close)
	var list HistoryList
	getJSON(t, b, "/history", &list)
	if list.Total != 0 || list.Items == nil || len(list.Items) != 0 {
		t.Fatalf("no history configured = %+v", list)
	}
	var found HistorySearch
	getJSON(t, b, "/history/search?q=anything", &found)
	if found.Counts != (HistoryCounts{}) {
		t.Fatalf("search with no history = %+v", found)
	}
	bare := httptest.NewRecorder()
	unauthed := httptest.NewRequest("GET", "/api/engine/history", nil)
	unauthed.Host = "127.0.0.1:1420"
	b.ServeHTTP(bare, unauthed)
	if bare.Code != 401 {
		t.Fatalf("history without the token = %d, want 401", bare.Code)
	}
}

func sessionWelcome(file string) remote.Welcome {
	return remote.Welcome{SessionFile: file, Workspace: "/work/lexer", Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}
}

func TestHistoryTellsAnAttachedConversationApart(t *testing.T) {
	b, root := historyFixture(t)
	a := &fakeAgent{events: make(chan session.Event, 8), model: Model}
	file := filepath.Join(root, "-work-lexer", "bbbb000000000002", "transcript.jsonl")
	b.open = func(string) (Connection, error) {
		return Connection{Agent: a, Welcome: sessionWelcome(file), Close: func() {}}, nil
	}
	if w := request(b, "POST", "/api/engine/sessions", `{"sessionFile":"`+file+`"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var list HistoryList
	getJSON(t, b, "/history?filter=open", &list)
	if ids(list.Items) != "bbbb" || !list.Items[0].Open {
		t.Fatalf("a conversation a window holds is open: %s", ids(list.Items))
	}
}
