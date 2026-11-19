package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── the reply check's line, through the door the desktop app uses ───────────
//
// qloop run 26 (engine b3a842964, which carries #2848) reported that the reply
// check never fired: no screenshot and no kept engine file held "its reply
// said more than it checked this turn". Its session journals say otherwise —
// 16 of its 28 turns were sent back by a check on the finished reply, café-site
// t2 among them — and none of those lines reached the canvas chat or any file,
// because codeaf app's pump dropped every one by its words, with the
// completion check's carry-on and the nudges.
//
// This replays café-site t2 through `codeaf app` itself: the real backend and
// the real session, against a model endpoint that answers as run 26's models
// did. The turn makes the two looks it ended on in the run (the same questions,
// answered with the words the run's seer gave), writes the reply it first
// ended on, and the reader answers what run 26's reader answered. Every line
// it uses is read from internal/session/testdata/run26-replies, where the
// session-level replay of the same turn reads it too.

// run26Cafe is café-site t2 as the replay needs it.
type run26Cafe struct {
	looks       []run26Look
	firstReply  string
	readerLines string
	secondReply string
}

type run26Look struct {
	path, question, seen string
}

func loadRun26Cafe(t *testing.T) run26Cafe {
	t.Helper()
	dir := filepath.Join("..", "..", "internal", "session", "testdata", "run26-replies")
	raw, err := os.ReadFile(filepath.Join(dir, "cafe-site-t2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatal(err)
	}
	var cafe run26Cafe
	results := map[string]string{}
	for _, message := range messages {
		if message.Role == "tool" {
			results[message.ToolCallID] = message.Content
		}
	}
	// The looks are the turn's last two, made together just before its reply:
	// the catering page as the canvas drew it, and the same page rendered at
	// phone width.
	for _, message := range messages {
		if len(message.ToolCalls) == 0 || message.ToolCalls[0].Function.Name != "view_image" {
			continue
		}
		cafe.looks = nil
		for _, call := range message.ToolCalls {
			if call.Function.Name != "view_image" {
				continue
			}
			var args struct{ Path, Question string }
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				t.Fatal(err)
			}
			result := results[call.ID]
			colon := strings.Index(result, ": ")
			if !strings.HasPrefix(result, "seen by ") || colon < 0 {
				t.Fatalf("the look %s has no seer's words: %q", call.ID, result)
			}
			// A look at the canvas's own drawing named a file in the run's
			// temporary home; the phone render was named inside the work
			// folder, and keeps its name.
			path := args.Path
			if filepath.IsAbs(path) {
				path = filepath.Join("looks", filepath.Base(path))
			}
			cafe.looks = append(cafe.looks, run26Look{path: path, question: args.Question, seen: result[colon+2:]})
		}
	}
	cafe.firstReply = messages[len(messages)-1].Content
	raw, err = os.ReadFile(filepath.Join(dir, "cafe-site-t2.check.json"))
	if err != nil {
		t.Fatal(err)
	}
	var check struct {
		ReaderAnswer string `json:"reader_answer"`
		SecondReply  string `json:"second_reply"`
	}
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatal(err)
	}
	cafe.readerLines, cafe.secondReply = check.ReaderAnswer, check.SecondReply
	if len(cafe.looks) != 2 || cafe.firstReply == "" || cafe.readerLines == "" || cafe.secondReply == "" {
		t.Fatalf("the café replay is incomplete: %d looks", len(cafe.looks))
	}
	return cafe
}

// run26SeerRow is the catalog row of the model run 26's looks went to.
const run26SeerRow = `{"id":"google/gemini-3.7-flash","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000005","completion":"0.000003"}}`

// run26Model answers as run 26's models did: the turn's model makes the looks,
// then replies, then replies again once it is sent back; the seer answers each
// question with the words it gave in the run; the reader answers with the
// lines the run's reader raised. Anything else (a title, a caption) is "done".
type run26Model struct {
	server *httptest.Server
	reader string
	mu     sync.Mutex
	pages  []string
	asked  atomic.Int64
}

func newRun26Model(t *testing.T, cafe run26Cafe, reader string) *run26Model {
	t.Helper()
	model := &run26Model{reader: reader}
	model.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The catalog names one model that can see, the seer the looks go
		// to; with no catalog the vision fallback cannot vouch for itself and
		// view_image is left off the belt.
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[` + run26SeerRow + `]}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not served in this test", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var request recordedRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		texts := strings.Join(request.texts(), "\n")
		content, calls := "done", ""
		switch {
		case strings.Contains(string(raw), `"image_url"`):
			for _, look := range cafe.looks {
				if strings.Contains(texts, look.question) {
					content = look.seen
				}
			}
		case strings.Contains(texts, "\n\nREPLY:\n"):
			model.asked.Add(1)
			model.mu.Lock()
			model.pages = append(model.pages, texts)
			model.mu.Unlock()
			content = model.reader
		case len(request.Tools) > 0 && strings.Contains(texts, "[carry on] Before your reply goes out"):
			content = cafe.secondReply
		case len(request.Tools) > 0 && strings.Contains(string(raw), `"role":"tool"`):
			content = cafe.firstReply
		case len(request.Tools) > 0:
			var parts []string
			for i, look := range cafe.looks {
				args, _ := json.Marshal(map[string]string{"path": look.path, "question": look.question})
				parts = append(parts, `{"index":`+strconv.Itoa(i)+`,"id":"look-`+strconv.Itoa(i)+`","type":"function","function":{"name":"view_image","arguments":`+strconv.Quote(string(args))+`}}`)
			}
			calls = "[" + strings.Join(parts, ",") + "]"
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			delta, finish := `"content":`+strconv.Quote(content), "stop"
			if calls != "" {
				delta, finish = `"tool_calls":`+calls, "tool_calls"
			}
			_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant",` + delta + `},"finish_reason":"` + finish + `"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":` + strconv.Quote(content) + `},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(model.server.Close)
	return model
}

// THE CHECK'S LINE REACHES THE APP AND IS KEPT. Run through codeaf app, café
// t2's first reply is sent back exactly as in run 26, and the line said while
// it works is an entry of the turn — a quiet notice typed "sent_back", after
// the answer it sent back and before the answer that replaced it — and it is
// still there when a new backend reads the conversation back off the disk,
// which is the record the qloop's harness keeps. Before this change the pump
// dropped it by its words and the turn's entries went straight from one answer
// to the next.
func TestRun26CafeReplyCheckLineReachesTheApp(t *testing.T) {
	cafe := loadRun26Cafe(t)
	unshown := cafe.readerLines[strings.Index(cafe.readerLines, "- UNSHOWN: "):]
	for _, row := range []struct {
		name, reader, line string
	}{
		// What run 26's reader raised on this turn: one contradiction and one
		// promise nothing checked.
		{"as run 26 read it", cafe.readerLines, "its reply did not match what it saw this turn · asking it to correct the reply"},
		// The promise alone: the line the run's report looked for.
		{"the unchecked promise alone", unshown, "its reply said more than it checked this turn · asking it to check or say so"},
	} {
		t.Run(row.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			_, workspace := wakeEnvironment(t, root)
			model := newRun26Model(t, cafe, row.reader)
			t.Setenv("CODEAF_BASE_URL", model.server.URL)
			state := filepath.Join(root, "state")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			settings := `{"tools.approvalMode":"allow"}`
			if err := os.WriteFile(filepath.Join(state, "config.json"), []byte(settings), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, look := range cafe.looks {
				writeRun26Picture(t, filepath.Join(workspace, look.path))
			}

			rec := newAppRecorder()
			backend := newAppBackend(rec, appOpenInProcess)
			closed := false
			defer func() {
				if !closed {
					backend.Close()
				}
			}()
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Send(agent.ID, "add a page for catering, offices can order 24h ahead, min 10 people", ""); err != nil {
				t.Fatal(err)
			}
			rec.waitFor(t, 60*time.Second, appTurnDone)

			if model.asked.Load() != 1 {
				t.Fatalf("the reader was asked %d times; want once", model.asked.Load())
			}
			model.mu.Lock()
			page := model.pages[0]
			model.mu.Unlock()
			if !strings.Contains(page, "Checked all four pages on desktop and at phone width") || !strings.Contains(page, "it stacks very cleanly") {
				t.Fatalf("the reader was not shown the reply and the phone look:\n%s", page)
			}

			var said []appwire.Entry
			rec.mu.Lock()
			for _, data := range rec.data {
				if added, ok := data.(appwire.EntryAdded); ok && added.Entry.Kind != appwire.EntryUser && added.Entry.Kind != appwire.EntryTool && added.Entry.Kind != appwire.EntryReceipt {
					said = append(said, added.Entry)
				}
			}
			rec.mu.Unlock()
			first, line, second := -1, -1, -1
			for i, entry := range said {
				switch {
				case entry.Kind == appwire.EntryAnswer && strings.TrimSpace(entry.Text) == strings.TrimSpace(cafe.firstReply):
					first = i
				case entry.Kind == appwire.EntryNotice && entry.Text == row.line:
					line = i
					if entry.NoticeKind != session.NoticeKindSentBack || !entry.Quiet {
						t.Errorf("the line went out as %+v; want a quiet notice typed %q", entry, session.NoticeKindSentBack)
					}
				case entry.Kind == appwire.EntryAnswer && strings.TrimSpace(entry.Text) == strings.TrimSpace(cafe.secondReply):
					second = i
				}
			}
			if first < 0 || second < 0 {
				t.Fatalf("the app did not get both of the turn's answers: %+v", said)
			}
			if line < 0 {
				t.Fatalf("the check's line %q never reached the app; the turn said %+v", row.line, said)
			}
			if !(first < line && line < second) {
				t.Fatalf("the line is entry %d; want it between the answer it sent back (%d) and the one that replaced it (%d)", line, first, second)
			}

			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
			closed = true
			reopened := newAppBackend(newAppRecorder(), appOpenInProcess)
			defer reopened.Close()
			entries, _, err := reopened.Entries(agent.ID, "", appwire.MaxEntries)
			if err != nil {
				t.Fatal(err)
			}
			kept := false
			for _, entry := range entries {
				kept = kept || (entry.Kind == appwire.EntryNotice && entry.Text == row.line && entry.NoticeKind == session.NoticeKindSentBack)
			}
			if !kept {
				t.Fatalf("the line is not in the reopened conversation: %+v", entries)
			}
		})
	}
}

// writeRun26Picture writes a small real PNG where a look expects one: the
// words about it come from run 26, and the file only has to be a picture.
func writeRun26Picture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
