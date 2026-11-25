package ollamalimits

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

const needs = 32_000

// stubServer is an Ollama that answers from two tables: what it is running and
// what it says about each model. A model with no row is one it does not know.
type stubServer struct {
	// ps is the body of /api/ps, and psStatus its status when not 200.
	ps       string
	psStatus int
	// show is the body of /api/show by model name.
	show map[string]string
	// slow holds every answer back for this long.
	slow time.Duration

	mu    sync.Mutex
	asked []string
	auth  []string
}

func (s *stubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The body is read before anything waits: a server notices that its
	// caller gave up only once the request has been read to its end.
	sent, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.mu.Unlock()
	if s.slow > 0 {
		select {
		case <-time.After(s.slow):
		case <-r.Context().Done():
			return
		}
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/ps":
		if s.psStatus != 0 {
			w.WriteHeader(s.psStatus)
			return
		}
		_, _ = w.Write([]byte(s.ps))
	case "POST /api/show":
		var ask struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(sent, &ask)
		s.mu.Lock()
		s.asked = append(s.asked, ask.Model)
		s.mu.Unlock()
		body, known := s.show[ask.Model]
		if !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// limitsFrom asks a stub for the named models the way models.list does, and
// answers the limits of the models that have some.
func limitsFrom(t *testing.T, stub *stubServer, names ...string) map[string][]appwire.ModelLimit {
	t.Helper()
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	probe, ok := At(server.URL+"/v1", "")
	if !ok {
		t.Fatalf("no probe for %s", server.URL)
	}
	probe.Client = server.Client()
	return limitsOf(probe.Read(context.Background(), names, needs))
}

// limitsOf is the limits of the readings that have some.
func limitsOf(readings map[string]Reading) map[string][]appwire.ModelLimit {
	limits := map[string][]appwire.ModelLimit{}
	for name, reading := range readings {
		if len(reading.Limits) > 0 {
			limits[name] = reading.Limits
		}
	}
	return limits
}

const (
	nothingRunning = `{"models":[]}`
	// plainModel is a model that takes tools, can hold 40960 tokens and whose
	// Modelfile sets no window.
	plainModel = `{"capabilities":["completion","tools","thinking"],"parameters":"temperature                    0.6\nstop                           \"<|im_end|>\"","model_info":{"general.architecture":"qwen3","qwen3.context_length":40960}}`
)

// The window the server is running a model with is the one a turn gets, so it
// is reported over whatever the Modelfile asks for.
func TestALoadedWindowIsBelievedOverTheModelfile(t *testing.T) {
	stub := &stubServer{
		ps:   `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":4096}]}`,
		show: map[string]string{"qwen3:8b": `{"capabilities":["completion","tools"],"parameters":"num_ctx                        65536","model_info":{"general.architecture":"qwen3","qwen3.context_length":40960}}`},
	}
	got := limitsFrom(t, stub, "qwen3:8b")
	want := map[string][]appwire.ModelLimit{"qwen3:8b": {{Kind: appwire.LimitSmallWindow, Window: 4096, Needs: needs, Most: 40960, Source: appwire.WindowLoaded}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}
}

// A model loaded with room enough has no window limit, however small its
// Modelfile's own figure is.
func TestALoadedWindowLargeEnoughReportsNothing(t *testing.T) {
	stub := &stubServer{
		ps:   `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":32000}]}`,
		show: map[string]string{"qwen3:8b": `{"capabilities":["completion","tools"],"parameters":"num_ctx 2048"}`},
	}
	if got := limitsFrom(t, stub, "qwen3:8b"); len(got) != 0 {
		t.Fatalf("limits = %+v, want none", got)
	}
}

// The server names a running model with its tag, and a model listed without
// one is the same model.
func TestALoadedModelIsFoundWithoutItsTag(t *testing.T) {
	stub := &stubServer{
		ps:   `{"models":[{"name":"llama3:latest","model":"llama3:latest","context_length":8192}]}`,
		show: map[string]string{"llama3": plainModel},
	}
	got := limitsFrom(t, stub, "llama3")["llama3"]
	if len(got) != 1 || got[0].Source != appwire.WindowLoaded || got[0].Window != 8192 {
		t.Fatalf("limits = %+v, want the loaded window", got)
	}
}

// A model that is not loaded is read by its Modelfile: a small num_ctx is the
// window its next load asks for, and a large one is no limit.
func TestAModelfileWindowIsReportedOnlyWhenSmall(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{
		"small": `{"capabilities":["completion","tools"],"parameters":"temperature 0.6\nnum_ctx                        8192\nstop \"x\"","model_info":{"general.architecture":"llama","llama.context_length":131072}}`,
		"large": `{"capabilities":["completion","tools"],"parameters":"num_ctx                        65536"}`,
	}}
	got := limitsFrom(t, stub, "small", "large")
	want := map[string][]appwire.ModelLimit{"small": {{Kind: appwire.LimitSmallWindow, Window: 8192, Needs: needs, Most: 131072, Source: appwire.WindowModelfile}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}
}

// Not loaded and nothing set: the server decides the window when it loads the
// model, from the machine's memory or a setting the person may have raised, and
// its API does not say what that will be. A model never seen running has no
// window limit, and its reading says the server is not running it.
func TestAnUnsetWindowNeverSeenRunningIsNoLimit(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{"qwen3:8b": plainModel}}
	server := httptest.NewServer(stub)
	defer server.Close()
	probe, _ := At(server.URL+"/v1", "")
	probe.Memory = &Memory{}
	got := probe.Read(context.Background(), []string{"qwen3:8b"}, needs)
	if want := map[string]Reading{"qwen3:8b": {Unloaded: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("readings = %+v, want %+v", got, want)
	}
}

// The window a model was last running with is remembered per server, and once
// the server unloads it the model is reported with that window, said to be the
// last load's. A Modelfile's num_ctx still wins, as it does in the server: it
// overrides the server's own setting. Another server's memory is its own.
func TestALastLoadedWindowIsRememberedPerServer(t *testing.T) {
	stub := &stubServer{
		ps: `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":4096},{"name":"pinned:latest","model":"pinned:latest","context_length":4096}]}`,
		show: map[string]string{
			"qwen3:8b": plainModel,
			"pinned":   `{"capabilities":["tools"],"parameters":"num_ctx 8192"}`,
		},
	}
	server := httptest.NewServer(stub)
	defer server.Close()
	seen := &Memory{}
	probe, _ := At(server.URL+"/v1", "")
	probe.Memory = seen
	loaded := probe.Read(context.Background(), []string{"qwen3:8b", "pinned"}, needs)
	if r := loaded["qwen3:8b"]; r.Unloaded || len(r.Limits) != 1 || r.Limits[0].Source != appwire.WindowLoaded || r.Limits[0].Window != 4096 {
		t.Fatalf("loaded reading = %+v", r)
	}

	stub.ps = nothingRunning
	got := limitsOf(probe.Read(context.Background(), []string{"qwen3:8b", "pinned"}, needs))
	want := map[string][]appwire.ModelLimit{
		"qwen3:8b": {{Kind: appwire.LimitSmallWindow, Window: 4096, Needs: needs, Most: 40960, Source: appwire.WindowLastLoaded}},
		"pinned":   {{Kind: appwire.LimitSmallWindow, Window: 8192, Needs: needs, Source: appwire.WindowModelfile}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}

	// A larger window seen later replaces the remembered one, and a window
	// large enough is no limit once unloaded either.
	stub.ps = `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":32768}]}`
	probe.Read(context.Background(), []string{"qwen3:8b"}, needs)
	stub.ps = nothingRunning
	if got := limitsOf(probe.Read(context.Background(), []string{"qwen3:8b"}, needs)); len(got) != 0 {
		t.Fatalf("limits after a large load = %+v, want none", got)
	}

	other := httptest.NewServer(&stubServer{ps: nothingRunning, show: map[string]string{"qwen3:8b": plainModel}})
	defer other.Close()
	elsewhere, _ := At(other.URL+"/v1", "")
	elsewhere.Memory = seen
	stub.ps = `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":4096}]}`
	probe.Read(context.Background(), []string{"qwen3:8b"}, needs)
	if got := limitsOf(elsewhere.Read(context.Background(), []string{"qwen3:8b"}, needs)); len(got) != 0 {
		t.Fatalf("another server reported this one's window: %+v", got)
	}
}

// A list that runs out of time has learnt nothing about the models it could not
// ask about, so it answers the last finished list's reading, marked stale: the
// warning does not blink away, and whether the server was running the model is
// not forgotten. Without such a list, a late model has nothing.
func TestALateListAnswersTheLastFinishedReading(t *testing.T) {
	stub := &stubServer{ps: `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":4096}]}`, show: map[string]string{"qwen3:8b": plainModel, "chat": `{"capabilities":["completion"]}`}}
	server := httptest.NewServer(stub)
	defer server.Close()
	probe, _ := At(server.URL+"/v1", "")
	probe.Memory = &Memory{}
	first := probe.Read(context.Background(), []string{"qwen3:8b", "chat"}, needs)
	if first["qwen3:8b"].Stale || first["chat"].Stale || !first["chat"].Unloaded {
		t.Fatalf("first readings = %+v", first)
	}

	spent, cancel := context.WithCancel(context.Background())
	cancel()
	asked := len(stub.auth)
	again := probe.Read(spent, []string{"qwen3:8b", "chat", "new"}, needs)
	if len(stub.auth) != asked {
		t.Fatal("a spent list asked the server")
	}
	for _, name := range []string{"qwen3:8b", "chat"} {
		want := first[name]
		want.Stale = true
		if !reflect.DeepEqual(again[name], want) {
			t.Fatalf("%s spent reading = %+v, want %+v", name, again[name], want)
		}
	}
	if r := again["new"]; !r.Stale || r.Limits != nil || r.Unloaded {
		t.Fatalf("a model no list finished = %+v", r)
	}

	stub.slow = 5 * time.Second
	slow := probe.Read(context.Background(), []string{"qwen3:8b"}, needs)
	if r := slow["qwen3:8b"]; !r.Stale || !reflect.DeepEqual(r.Limits, first["qwen3:8b"].Limits) {
		t.Fatalf("a slow server's reading = %+v", r)
	}
}

// A server that runs a model from before it reported windows says the model is
// loaded and not with what, so the model is read as though it were not.
func TestALoadedModelWithNoWindowIsReadByItsModelfile(t *testing.T) {
	stub := &stubServer{
		ps:   `{"models":[{"name":"qwen3:8b","model":"qwen3:8b"}]}`,
		show: map[string]string{"qwen3:8b": `{"parameters":"num_ctx 4096"}`},
	}
	got := limitsFrom(t, stub, "qwen3:8b")["qwen3:8b"]
	if len(got) != 1 || got[0].Source != appwire.WindowModelfile || got[0].Window != 4096 {
		t.Fatalf("limits = %+v, want the Modelfile's window", got)
	}
}

// A model the server only forwards to another machine runs with no window of
// this server's, so the server's default is not reported for it.
func TestAForwardedModelHasNoWindowOfThisServers(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{
		"big:cloud": `{"capabilities":["completion","tools"],"remote_model":"big","remote_host":"https://ollama.com:443"}`,
	}}
	if got := limitsFrom(t, stub, "big:cloud"); len(got) != 0 {
		t.Fatalf("limits = %+v, want none", got)
	}
}

// A capability list without tools is the server saying the model takes none. A
// list with tools, and a server that sends no list, say no such thing.
func TestNoToolsIsReportedOnlyWhenTheServerListsCapabilitiesWithoutIt(t *testing.T) {
	loaded := `{"models":[{"name":"chat","context_length":64000},{"name":"tools","context_length":64000},{"name":"old","context_length":64000},{"name":"empty","context_length":64000}]}`
	stub := &stubServer{ps: loaded, show: map[string]string{
		"chat":  `{"capabilities":["completion","vision"]}`,
		"tools": `{"capabilities":["completion","tools"]}`,
		"old":   `{"parameters":""}`,
		"empty": `{"capabilities":[]}`,
	}}
	got := limitsFrom(t, stub, "chat", "tools", "old", "empty")
	want := map[string][]appwire.ModelLimit{"chat": {{Kind: appwire.LimitNoTools}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}
}

// Both limits are one model's, the window first.
func TestAModelMayHaveBothLimits(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{"tiny": `{"capabilities":["completion"],"parameters":"num_ctx 2048"}`}}
	got := limitsFrom(t, stub, "tiny")["tiny"]
	if len(got) != 2 || got[0].Kind != appwire.LimitSmallWindow || got[1].Kind != appwire.LimitNoTools {
		t.Fatalf("limits = %+v, want a small window then no tools", got)
	}
}

// Without the list of running models nobody knows which window counts, so no
// window is reported; what the model itself says about tools still is.
func TestAFailedRunningListReportsNoWindow(t *testing.T) {
	for _, stub := range []*stubServer{
		{psStatus: http.StatusInternalServerError},
		{ps: `not json`},
		{ps: `{"unrelated":true}`},
	} {
		stub.show = map[string]string{"plain": plainModel, "chat": `{"capabilities":["completion"]}`}
		got := limitsFrom(t, stub, "plain", "chat")
		want := map[string][]appwire.ModelLimit{"chat": {{Kind: appwire.LimitNoTools}}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ps %q/%d: limits = %+v, want %+v", stub.ps, stub.psStatus, got, want)
		}
	}
}

// A model the server will not describe, or describes in a shape nobody knows,
// has no limit: an error is never a warning.
func TestAFailedOrOddAnswerReportsNothingForThatModel(t *testing.T) {
	stub := &stubServer{ps: `{"models":[{"name":"plain:latest","context_length":4096}]}`, show: map[string]string{
		"garbled": `{"capabilities":`,
		"shapes":  `{"capabilities":"tools","parameters":7}`,
		"plain":   plainModel,
	}}
	got := limitsFrom(t, stub, "unknown", "garbled", "shapes", "plain")
	if len(got) != 1 || len(got["plain"]) != 1 {
		t.Fatalf("limits = %+v, want only the model that answered", got)
	}
}

// What a model can hold is filed under its architecture; an answer without
// either still reports the window, with no most.
func TestMostIsAbsentWhenTheServerDoesNotSayIt(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{
		"bare":  `{"capabilities":["tools"],"parameters":"num_ctx 4096","model_info":{"general.architecture":"llama"}}`,
		"words": `{"capabilities":["tools"],"parameters":"num_ctx 4096","model_info":{"general.architecture":"llama","llama.context_length":"many"}}`,
	}}
	got := limitsFrom(t, stub, "bare", "words")
	for _, name := range []string{"bare", "words"} {
		if limits := got[name]; len(limits) != 1 || limits[0].Most != 0 || limits[0].Window != 4096 {
			t.Fatalf("%s: limits = %+v, want the window with no most", name, limits)
		}
	}
}

// A server that does not answer in time costs the list its bound and reports
// nothing, and a list that has no time left asks nothing.
func TestASlowServerReportsNothingWithinTheBound(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{"qwen3:8b": plainModel}, slow: 5 * time.Second}
	began := time.Now()
	got := limitsFrom(t, stub, "qwen3:8b")
	if took := time.Since(began); len(got) != 0 || took > Whole+time.Second {
		t.Fatalf("limits = %+v after %s, want none within %s", got, took, Whole)
	}

	quick := &stubServer{ps: nothingRunning, show: map[string]string{"qwen3:8b": plainModel}}
	server := httptest.NewServer(quick)
	defer server.Close()
	probe, _ := At(server.URL+"/v1", "")
	spent, cancel := context.WithCancel(context.Background())
	cancel()
	if got := limitsOf(probe.Read(spent, []string{"qwen3:8b"}, needs)); len(got) != 0 || len(quick.auth) != 0 {
		t.Fatalf("a spent list asked %d questions and reported %+v", len(quick.auth), got)
	}
}

// Many models are asked about a few at a time and every one is answered.
func TestEveryModelOfALongListIsAnswered(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{}}
	var names []string
	for i := range 40 {
		name := "model-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		names = append(names, name)
		stub.show[name] = plainModel
	}
	server := httptest.NewServer(stub)
	defer server.Close()
	probe, _ := At(server.URL+"/v1", "")
	got := probe.Read(context.Background(), names, needs)
	answered := 0
	for _, reading := range got {
		if !reading.Stale && reading.Unloaded {
			answered++
		}
	}
	if len(got) != len(names) || answered != len(names) {
		t.Fatalf("%d of %d models answered", answered, len(names))
	}
}

// A server that is not there reports nothing.
func TestAnAbsentServerReportsNothing(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL + "/v1"
	server.Close()
	probe, ok := At(address, "")
	if !ok {
		t.Fatal("no probe for a closed server's address")
	}
	if got := limitsOf(probe.Read(context.Background(), []string{"qwen3:8b"}, needs)); len(got) != 0 {
		t.Fatalf("limits = %+v, want none", got)
	}
}

// The native API is beside the /v1 the chat path uses. An address that names
// no server has no probe, and the key travels only when there is one.
func TestTheProbeIsBesideTheChatAddress(t *testing.T) {
	for address, base := range map[string]string{
		"http://localhost:11434/v1":  "http://localhost:11434",
		"http://localhost:11434/v1/": "http://localhost:11434",
		"https://box.example/ollama": "https://box.example/ollama",
		" http://10.0.0.5:11434/v1 ": "http://10.0.0.5:11434",
	} {
		if probe, ok := At(address, ""); !ok || probe.base != base {
			t.Fatalf("At(%q) = %q %v, want %q", address, probe.base, ok, base)
		}
	}
	for _, address := range []string{"", "localhost:11434", "ftp://localhost/v1", "http:///v1"} {
		if _, ok := At(address, ""); ok {
			t.Fatalf("At(%q) made a probe", address)
		}
	}

	stub := &stubServer{ps: nothingRunning, show: map[string]string{"qwen3:8b": plainModel}}
	server := httptest.NewServer(stub)
	defer server.Close()
	probe, _ := At(server.URL+"/v1", " secret ")
	probe.Read(context.Background(), []string{"qwen3:8b"}, needs)
	if len(stub.auth) != 2 || stub.auth[0] != "Bearer secret" || stub.auth[1] != "Bearer secret" {
		t.Fatalf("authorization sent = %q", stub.auth)
	}
	stub.auth = nil
	probe, _ = At(server.URL+"/v1", "")
	probe.Read(context.Background(), []string{"qwen3:8b"}, needs)
	if len(stub.auth) != 2 || stub.auth[0] != "" || stub.auth[1] != "" {
		t.Fatalf("a keyless probe sent authorization %q", stub.auth)
	}
}

// The wire's field names are the contract the desktop app reads.
func TestTheLimitsTravelUnderTheirWireNames(t *testing.T) {
	stub := &stubServer{ps: nothingRunning, show: map[string]string{"tiny": `{"capabilities":["completion"],"parameters":"num_ctx 2048","model_info":{"general.architecture":"llama","llama.context_length":8192}}`}}
	data, err := json.Marshal(limitsFrom(t, stub, "tiny")["tiny"])
	want := `[{"kind":"small_window","window":2048,"needs":32000,"most":8192,"source":"modelfile"},{"kind":"no_tools"}]`
	if err != nil || string(data) != want {
		t.Fatalf("wire = %s %v, want %s", data, err, want)
	}
}
