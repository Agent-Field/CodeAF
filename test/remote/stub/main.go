// Command modelstub is a scripted, deterministic stand-in for the model
// endpoint, so that the two-machine harness in internal/e2e/remote_test.go can
// drive a REAL turn without an API key and without a network.
//
// WHY A STUB AND NOT A REAL MODEL. The thing under test is the wire between two
// filesystems, not the model's judgment. A real model would make every scenario
// probabilistic — a path assertion that fails because the model chose to
// paraphrase instead of quoting is a false red — and it would put a bill and a
// credential in front of a test whose whole point is that anybody can run it.
// So this speaks OpenRouter's OpenAI-compatible dialect exactly as
// internal/provider writes and reads it (client.go's `<base>/chat/completions`,
// sse.go's chunk shape, catalog.go's `<base>/models?output_modalities=all`) and
// answers from a script.
//
// THE SCRIPT IS KEYED OFF THE PERSON'S OWN WORDS. Every scenario submits a
// message carrying one of the markers below, and this server answers the way
// that scenario needs — including calling a tool, so the engine machine does
// real work on its own disk and the reply can be checked against it.
//
//	PROBE-ECHO         one reply, no tools: the handshake and one turn
//	PROBE-PWD          calls `bash` with `pwd`, then quotes the answer back
//	PROBE-READ <path>  calls `read` on the path, then quotes the answer back
//	PROBE-SLOW         a reply spread over seconds, for the link that dies
//
// QUOTING THE TOOL OUTPUT BACK IS THE WHOLE TRICK. `--once` prints the model's
// reply on stdout and the tool's own output nowhere at all, so a scenario that
// wants to assert on what a tool SAW has to have the reply carry it. A real
// model would summarize; this one echoes verbatim, which is what makes an
// assertion about an absolute path possible at all.
//
// STDERR IS THE LOG. Every request is one line, so `docker logs` on the model
// container answers "did the turn ever reach the model, and what did it ask".
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	mux := http.NewServeMux()
	// The two paths internal/provider and internal/catalog actually reach for.
	// They are registered under the /api/v1 prefix because that is the shape of
	// a real base URL (config.DefaultBaseURL is https://openrouter.ai/api/v1),
	// and a harness that pointed at a bare host would be proving the engine
	// works against a URL nobody ever configures.
	mux.HandleFunc("/api/v1/chat/completions", completions)
	mux.HandleFunc("/api/v1/models", models)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// An unrecognised path is worth SAYING rather than swallowing: it is how
		// a harness discovers that some new door in the engine wants an endpoint
		// this stub has never heard of.
		log.Printf("unhandled %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})

	log.Printf("model stub listening on %s", *addr)
	server := &http.Server{
		Addr:    *addr,
		Handler: mux,
		// No write timeout at all: PROBE-SLOW deliberately holds a stream open
		// for longer than any sane default, and a timeout here would end the
		// turn the harness is trying to interrupt by other means.
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ── the model listing ───────────────────────────────────────────────────────

// models is internal/catalog's `/models?output_modalities=all`, holding exactly
// one row. THE PRICES ARE ZERO ON PURPOSE: the session's spend rail reads them,
// and a harness that quietly accumulated dollars against a fake endpoint would
// be teaching the ledger a lie.
func models(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.RequestURI())
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":[{
		"id":"stub/scripted",
		"canonical_slug":"stub/scripted",
		"name":"Scripted stub",
		"context_length":200000,
		"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
		"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
		"supported_parameters":["tools","tool_choice","max_tokens"]
	}]}`))
}

// ── the conversation ────────────────────────────────────────────────────────

// request is the part of the body this stub reads. Everything else on the wire
// — the tools, the reasoning knob, the provider preferences — is deliberately
// ignored: this is a script, not a model, and a field it pretended to honour
// would be a second opinion about a protocol it does not own.
type request struct {
	Model    string    `json:"model"`
	Stream   bool      `json:"stream"`
	Messages []message `json:"messages"`
}

// message holds Content as a raw message because the dialect sends it two ways
// — a plain string, and OpenAI's array of typed parts — and both are ordinary.
type message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
}

// text flattens whichever of the two shapes arrived.
func (m message) text() string {
	if len(m.Content) == 0 {
		return ""
	}
	var plain string
	if json.Unmarshal(m.Content, &plain) == nil {
		return plain
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) != nil {
		return ""
	}
	var out strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Text != "" {
			out.WriteString(part.Text)
		}
	}
	return out.String()
}

// streamID makes every stream's id distinct, which costs nothing and makes a
// log of several turns readable.
var streamID atomic.Int64

func completions(w http.ResponseWriter, r *http.Request) {
	body, err := readRequest(r)
	if err != nil {
		log.Printf("bad request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ask, answered := script(body.Messages)
	log.Printf("POST /chat/completions model=%q stream=%v messages=%d ask=%q phase=%s",
		body.Model, body.Stream, len(body.Messages), shorten(ask, 120), phaseWord(answered))

	reply := compose(ask, answered)
	if !body.Stream {
		// Nothing in aforge asks for a whole completion today, but a stub that
		// only spoke one of the two shapes would fail mysteriously the day
		// something does.
		writeWhole(w, reply)
		return
	}
	writeStream(w, reply)
}

func readRequest(r *http.Request) (request, error) {
	if r.Method != http.MethodPost {
		return request{}, fmt.Errorf("want POST, got %s", r.Method)
	}
	var body request
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return request{}, err
	}
	return body, nil
}

// script reads the conversation for the two facts the script turns on: what the
// person last asked for, and whether a tool has already answered since.
//
// SCANNING BACKWARDS FOR THE LAST USER MESSAGE is what makes the second leg of
// a tool turn work. When the engine comes back with the tool's result, the last
// message on the wire is the tool's, not the person's — so the marker has to be
// found where it was actually said.
func script(messages []message) (ask string, answered string) {
	last := -1
	for at := len(messages) - 1; at >= 0; at-- {
		if strings.EqualFold(messages[at].Role, "user") {
			last = at
			break
		}
	}
	if last < 0 {
		return "", ""
	}
	ask = messages[last].text()
	for _, later := range messages[last+1:] {
		if strings.EqualFold(later.Role, "tool") {
			answered = later.text()
		}
	}
	return ask, answered
}

func phaseWord(answered string) string {
	if answered == "" {
		return "first"
	}
	return "after-tool"
}

// ── what a reply is ─────────────────────────────────────────────────────────

// reply is one scripted answer: either text, or a single tool call.
type reply struct {
	// chunks is the reply text, already split the way it should arrive. More
	// than one chunk with a pause between them is what PROBE-SLOW is.
	chunks []string
	// pause is held between chunks.
	pause time.Duration
	// tool, when named, makes this a tool call instead of an answer.
	tool string
	args string
}

// The markers. They are spelled in the person's own message and nowhere else,
// so a scenario reads as a sentence somebody could actually type.
const (
	markerEcho = "PROBE-ECHO"
	markerPwd  = "PROBE-PWD"
	markerRead = "PROBE-READ"
	markerSlow = "PROBE-SLOW"
)

// The prefixes the harness asserts on. They are constants here and constants in
// the test, because a string that appears in two places drifts.
const (
	sayEcho = "STUB-REPLY: "
	sayPwd  = "ENGINE-PWD: "
	sayRead = "ENGINE-READ: "
	slowTop = "SLOW-BEGIN"
	slowEnd = "SLOW-END"
)

// compose is the script itself.
func compose(ask, answered string) reply {
	switch {
	case strings.Contains(ask, markerPwd):
		if answered == "" {
			// `bash` rather than a path tool, because the question is "which
			// machine's filesystem is this session standing in", and the shell's
			// own answer to that is the least deniable one available.
			return reply{tool: "bash", args: `{"command":"pwd"}`}
		}
		return reply{chunks: []string{sayPwd + strings.TrimSpace(answered)}}

	case strings.Contains(ask, markerRead):
		if answered == "" {
			return reply{tool: "read", args: `{"path":"` + jsonEscape(pathAfter(ask, markerRead)) + `"}`}
		}
		return reply{chunks: []string{sayRead + strings.TrimSpace(answered)}}

	case strings.Contains(ask, markerSlow):
		// Three seconds of silence between the first word and the last is the
		// window the disconnect scenario cuts the link in. A reply that arrived
		// whole in one frame would leave nothing to interrupt.
		return reply{
			chunks: []string{slowTop, " …", " …", " …", " " + slowEnd},
			pause:  3 * time.Second,
		}

	case strings.Contains(ask, markerEcho):
		return reply{chunks: []string{sayEcho + strings.TrimSpace(ask)}}

	default:
		// EVERY OTHER CALL STILL GETS A VALID ANSWER. A session makes calls
		// nobody scripted — a title for the conversation, a compaction — and a
		// stub that refused them would fail turns for reasons the scenario is
		// not about.
		return reply{chunks: []string{sayEcho + shorten(strings.TrimSpace(ask), 200)}}
	}
}

// pathAfter is the word following a marker, which is how PROBE-READ names its
// file. Empty when nothing followed it, which the engine answers as the error
// it is — and that error is itself a thing one scenario asserts on.
func pathAfter(ask, marker string) string {
	at := strings.Index(ask, marker)
	if at < 0 {
		return ""
	}
	rest := strings.TrimSpace(ask[at+len(marker):])
	if rest == "" {
		return ""
	}
	return strings.Fields(rest)[0]
}

func jsonEscape(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil || len(encoded) < 2 {
		return ""
	}
	return string(encoded[1 : len(encoded)-1])
}

// ── writing it on the wire ──────────────────────────────────────────────────

// writeStream is internal/provider's sse.go read backwards: `data: ` lines of
// the chunk shape that file declares, separated by blank lines, ended by
// `[DONE]`. Every chunk is flushed, because a stream buffered to the end is not
// a stream and the slow scenario depends on the difference.
func writeStream(w http.ResponseWriter, answer reply) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the stub needs a flushable writer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	id := fmt.Sprintf("stub-%d", streamID.Add(1))
	send := func(payload string) {
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	send(chunk(id, `{"role":"assistant","content":""}`, ""))
	if answer.tool != "" {
		send(chunk(id, fmt.Sprintf(
			`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":%q,"arguments":%q}}]}`,
			answer.tool, answer.args), ""))
		send(finish(id, "tool_calls"))
		send("[DONE]")
		return
	}
	for at, text := range answer.chunks {
		if at > 0 && answer.pause > 0 {
			time.Sleep(answer.pause)
		}
		send(chunk(id, fmt.Sprintf(`{"content":%q}`, text), ""))
	}
	send(finish(id, "stop"))
	send("[DONE]")
}

func chunk(id, delta, reason string) string {
	if reason == "" {
		reason = "null"
	} else {
		reason = fmt.Sprintf("%q", reason)
	}
	return fmt.Sprintf(
		`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":"stub/scripted","provider":"stub","choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`,
		id, time.Now().Unix(), delta, reason)
}

// finish carries the usage block too, because internal/provider always asks for
// usage accounting ({"usage":{"include":true}}) and a stream that never sent it
// would leave the session's ledger with no reading at all. The cost is stated
// as zero rather than omitted: unknown and free are different claims, and this
// endpoint really is free.
func finish(id, reason string) string {
	return fmt.Sprintf(
		`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":"stub/scripted","provider":"stub","choices":[{"index":0,"delta":{},"finish_reason":%q}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`,
		id, time.Now().Unix(), reason)
}

// writeWhole is the non-streaming shape, for completeness.
func writeWhole(w http.ResponseWriter, answer reply) {
	w.Header().Set("Content-Type", "application/json")
	if answer.tool != "" {
		fmt.Fprintf(w, `{"id":"stub","object":"chat.completion","model":"stub/scripted","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`,
			answer.tool, answer.args)
		return
	}
	fmt.Fprintf(w, `{"id":"stub","object":"chat.completion","model":"stub/scripted","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`,
		strings.Join(answer.chunks, ""))
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
