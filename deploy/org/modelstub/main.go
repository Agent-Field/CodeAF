// modelstub is a keyless, schema-driven stand-in for an OpenRouter-compatible
// model endpoint, so a SWE-AF run can be driven end to end with no key and no
// spend by default. An operator can set STUB_COST_USD and STUB_REQUIRE_KEY
// to exercise measured spend and authentication.
//
// Protocol it speaks (the dialect internal/provider writes):
//
//	GET  /api/v1/models            one row, stub/scripted
//	POST /api/v1/chat/completions  SSE stream
//
// What it does with a turn:
//
//   - A prompt that carries the SDK harness's "CRITICAL OUTPUT REQUIREMENTS"
//     block (a file to Write, with the JSON Schema inline or in a file) gets one
//     `write` tool call whose content is the SMALLEST valid document for that
//     schema (schema-driven: first enum value, one array element, "x" strings),
//     then a closing text turn once the tool answered.
//
//   - Anything else (a steer note, a title, a compaction) gets a short text
//     answer that quotes the person's last words, so a steer note is visible in
//     the run's transcript.
//
//   - A user message that starts with `bash:` gets ONE `bash` tool call carrying
//     the rest as its command, then, once the tool answered, the text
//     "STUB ran: " followed by the tool's output (scripted.go). That is how the
//     hosted-session smoke test makes a session run a shell command.
//
// STUB_DELAY (default 4s) is held before each answer so a run lasts long enough
// to be watched and steered by hand.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	delay      = 4 * time.Second
	costUSD    float64
	requireKey string
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18890", "listen address")
	flag.Parse()
	if v := os.Getenv("STUB_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			delay = d
		}
	}
	if cost, err := strconv.ParseFloat(os.Getenv("STUB_COST_USD"), 64); err == nil && cost > 0 && !math.IsInf(cost, 0) {
		costUSD = cost
	}
	requireKey = os.Getenv("STUB_REQUIRE_KEY")
	log.Printf("modelstub on %s delay=%s", *addr, delay)
	log.Fatal((&http.Server{Addr: *addr, Handler: routes(), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/chat/completions", completions)
	mux.HandleFunc("/api/v1/models", models)
	mux.HandleFunc("/_calls", serveCalls)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("unhandled %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	return mux
}

func models(w http.ResponseWriter, r *http.Request) {
	if _, ok := checkStubKey(w, r, ""); !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":[{"id":"stub/scripted","canonical_slug":"stub/scripted","name":"Scripted stub","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},"supported_parameters":["tools","tool_choice","max_tokens"]}]}`))
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (m message) text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(m.Content, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

var turn atomic.Int64

const callLimit = 500

type stubCall struct {
	At     time.Time `json:"at"`
	Key    string    `json:"key"`
	Model  string    `json:"model"`
	USD    float64   `json:"usd"`
	Status int       `json:"status"`
}

var calls struct {
	sync.Mutex
	rows []stubCall
}

func rememberCall(call stubCall) {
	calls.Lock()
	defer calls.Unlock()
	call.At = time.Now().UTC()
	if len(calls.rows) == callLimit {
		copy(calls.rows, calls.rows[1:])
		calls.rows = calls.rows[:callLimit-1]
	}
	calls.rows = append(calls.rows, call)
}

func checkStubKey(w http.ResponseWriter, r *http.Request, model string) (stubCall, bool) {
	auth := r.Header.Get("Authorization")
	key, bearer := strings.CutPrefix(auth, "Bearer ")
	if !bearer {
		key = ""
	}
	call := stubCall{Key: key, Model: model}
	if requireKey == "" || auth == "Bearer "+requireKey {
		return call, true
	}
	call.Status = http.StatusUnauthorized
	rememberCall(call)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(call.Status)
	_, _ = w.Write([]byte(`{"error":{"message":"invalid key","code":401}}`))
	return call, false
}

func serveCalls(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		calls.Lock()
		rows := append([]stubCall{}, calls.rows...)
		calls.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Calls []stubCall `json:"calls"`
		}{Calls: rows})
	case http.MethodDelete:
		calls.Lock()
		calls.rows = nil
		calls.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

var (
	reOut    = regexp.MustCompile(`create this file: (\S+)`)
	reSchema = regexp.MustCompile(`Read the JSON Schema at: (\S+)`)
)

func completions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}
	err := json.NewDecoder(r.Body).Decode(&body)
	call, ok := checkStubKey(w, r, body.Model)
	if !ok {
		return
	}
	if err != nil {
		call.Status = http.StatusBadRequest
		rememberCall(call)
		http.Error(w, err.Error(), 400)
		return
	}
	call.USD, call.Status = costUSD, http.StatusOK
	rememberCall(call)
	n := turn.Add(1)
	// A person's `bash: <command>` message is answered by running that command
	// through the session's own bash tool and quoting what came back, so a
	// hosted session can be driven end to end, keylessly, with real shell output
	// (scripted.go). It is checked first and it never waits: it is a script.
	if handled := scriptedBash(w, body.Messages, n); handled {
		return
	}
	// The original prompt is the first user message carrying the output block.
	promptAt := -1
	for i, m := range body.Messages {
		if strings.EqualFold(m.Role, "user") && strings.Contains(m.text(), "CRITICAL OUTPUT REQUIREMENTS") {
			promptAt = i
			break
		}
	}
	last := ""
	for i := len(body.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(body.Messages[i].Role, "user") {
			last = body.Messages[i].text()
			break
		}
	}
	tools := 0
	if promptAt >= 0 {
		for _, m := range body.Messages[promptAt+1:] {
			if strings.EqualFold(m.Role, "tool") {
				tools++
			}
		}
	}
	time.Sleep(delay)
	if promptAt >= 0 && tools == 0 {
		p := body.Messages[promptAt].text()
		out := ""
		if m := reOut.FindStringSubmatch(p); m != nil {
			out = m[1]
		}
		doc, err := documentFor(p)
		if err != nil || out == "" {
			log.Printf("turn %d: cannot build a document (%v), path=%q", n, err, out)
			stream(w, "", "", "", "I could not read the schema.")
			return
		}
		args, _ := json.Marshal(map[string]string{"path": out, "content": doc})
		log.Printf("turn %d: write %s (%d bytes)", n, out, len(doc))
		stream(w, fmt.Sprintf("call_%d", n), "write", string(args), "Writing the output file.")
		return
	}
	note := strings.TrimSpace(last)
	if len(note) > 160 {
		note = note[:160]
	}
	log.Printf("turn %d: text (tools=%d) last=%q", n, tools, note)
	if promptAt >= 0 && tools > 0 {
		stream(w, "", "", "", "Done. The output file is written.")
		return
	}
	stream(w, "", "", "", "STUB noted: "+note)
}

func documentFor(prompt string) (string, error) {
	var raw string
	if m := reSchema.FindStringSubmatch(prompt); m != nil {
		b, err := os.ReadFile(m[1])
		if err != nil {
			return "", err
		}
		raw = string(b)
	} else if i := strings.Index(prompt, "Required JSON Schema:"); i >= 0 {
		raw = prompt[i+len("Required JSON Schema:"):]
		if j := strings.Index(raw, "Write ONLY valid JSON"); j >= 0 {
			raw = raw[:j]
		}
	} else {
		return "", fmt.Errorf("no schema in the prompt")
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &schema); err != nil {
		return "", err
	}
	doc, _ := json.Marshal(gen(schema, schema, "", 0))
	return string(doc), nil
}

var emptyKeys = []string{"question", "clarif", "depend", "blocked", "blocks", "parallel", "predecessor", "prerequisite"}

func gen(root, s map[string]any, key string, depth int) any {
	if depth > 12 || key == "ask_user_form" {
		return nil
	}
	if ref, ok := s["$ref"].(string); ok {
		name := ref[strings.LastIndex(ref, "/")+1:]
		for _, bag := range []string{"$defs", "definitions"} {
			if defs, ok := root[bag].(map[string]any); ok {
				if t, ok := defs[name].(map[string]any); ok {
					return gen(root, t, key, depth+1)
				}
			}
		}
		return nil
	}
	if c, ok := s["const"]; ok {
		return c
	}
	if e, ok := s["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}
	for _, k := range []string{"anyOf", "oneOf", "allOf"} {
		if br, ok := s[k].([]any); ok && len(br) > 0 {
			for _, b := range br {
				if m, ok := b.(map[string]any); ok && m["type"] != "null" {
					return gen(root, m, key, depth+1)
				}
			}
		}
	}
	typ, _ := s["type"].(string)
	if tl, ok := s["type"].([]any); ok {
		for _, t := range tl {
			if ts, _ := t.(string); ts != "null" {
				typ = ts
				break
			}
		}
	}
	if typ == "" {
		if _, ok := s["properties"]; ok {
			typ = "object"
		}
	}
	switch typ {
	case "object":
		out := map[string]any{}
		props, _ := s["properties"].(map[string]any)
		for k, v := range props {
			if m, ok := v.(map[string]any); ok {
				out[k] = gen(root, m, k, depth+1)
			}
		}
		return out
	case "array":
		for _, e := range emptyKeys {
			if strings.Contains(strings.ToLower(key), e) {
				return []any{}
			}
		}
		items, _ := s["items"].(map[string]any)
		n := 1
		if mi, ok := s["minItems"].(float64); ok && int(mi) > n {
			n = int(mi)
		}
		out := []any{}
		for i := 0; i < n && items != nil; i++ {
			out = append(out, gen(root, items, key, depth+1))
		}
		return out
	case "integer":
		if m, ok := s["minimum"].(float64); ok {
			return int(m)
		}
		return 1
	case "number":
		if m, ok := s["minimum"].(float64); ok {
			return m
		}
		return 1.0
	case "boolean":
		return false
	case "string":
		if f, _ := s["format"].(string); f == "date-time" {
			return time.Now().UTC().Format(time.RFC3339)
		}
		str := "x"
		if ml, ok := s["minLength"].(float64); ok {
			for len(str) < int(ml) {
				str += "x"
			}
		}
		return str
	}
	return nil
}

func chunk(id, delta, reason string) string {
	if reason == "" {
		reason = "null"
	} else {
		reason = fmt.Sprintf("%q", reason)
	}
	return fmt.Sprintf(`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":"stub/scripted","provider":"stub","choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`, id, time.Now().Unix(), delta, reason)
}

func stream(w http.ResponseWriter, callID, tool, args, text string) {
	fl := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	id := fmt.Sprintf("stub-%d", turn.Load())
	send := func(p string) { fmt.Fprintf(w, "data: %s\n\n", p); fl.Flush() }
	send(chunk(id, `{"role":"assistant","content":""}`, ""))
	if text != "" {
		send(chunk(id, fmt.Sprintf(`{"content":%q}`, text), ""))
	}
	reason := "stop"
	if tool != "" {
		send(chunk(id, fmt.Sprintf(`{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}`, callID, tool, args), ""))
		reason = "tool_calls"
	}
	in, out := 1, 1
	if costUSD != 0 {
		in, out = 11, 7
	}
	send(fmt.Sprintf(`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":"stub/scripted","provider":"stub","choices":[{"index":0,"delta":{},"finish_reason":%q}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"cost":%g}}`, id, time.Now().Unix(), reason, in, out, in+out, costUSD))
	send("[DONE]")
}
