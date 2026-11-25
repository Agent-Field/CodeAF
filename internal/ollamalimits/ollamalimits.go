// Package ollamalimits asks an Ollama server what it will really give a model,
// so the desktop app can say where a model falls short before a turn finds out.
//
// THE OPENAI-SHAPED LISTING CANNOT ANSWER THIS. Ollama's /v1/models names a
// model and nothing else, and the window a local model runs with is decided by
// the server when it loads the model, not by the model: one that can hold
// forty thousand tokens is commonly given four thousand. Only the server's own
// API says which, so this package is the one caller of it, and it reports
// facts only. Nothing here chooses a prompt or a compaction line.
package ollamalimits

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/providerhttp"
)

// SourceID is the model source this package asks. No other source is asked:
// the questions are Ollama's own and mean nothing to another server.
const SourceID = "ollama"

const (
	// Each bounds one question and Whole bounds every question of one list.
	// The server is nearly always on this computer and answers in a few
	// milliseconds; the bounds are for the one that is not there, so that a
	// model list never waits on it for longer than a person would notice.
	Each  = 300 * time.Millisecond
	Whole = 600 * time.Millisecond

	// atOnce is how many models are asked about at the same time. A person may
	// keep dozens of models, and each answer is read from that model's file.
	atOnce = 8

	// answerLimit bounds one answer. A model's licence, template and Modelfile
	// travel in it, so it is far larger than the few fields read here.
	answerLimit = 4 << 20
)

// Probe asks one Ollama server. Client is the test's door; nil is a client of
// its own that follows no redirect off the server's origin. Memory is what
// earlier lists saw ([Memory]); nil remembers nothing.
type Probe struct {
	base   string
	key    string
	Client *http.Client
	Memory *Memory
}

// At is the probe for the server whose OpenAI-shaped address is given, and
// false when that address names no server: the native API sits beside the /v1
// the chat path uses, on the same origin, so the key that reaches one reaches
// the other and goes nowhere else.
func At(address, key string) (Probe, bool) {
	base := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(address), "/"), "/v1")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Probe{}, false
	}
	return Probe{base: base, key: strings.TrimSpace(key)}, true
}

// Memory is what this process has seen of its Ollama servers, by server address
// and model: the window each model was last running with in /api/ps, and each
// model's last reading that a list could finish.
//
// IT IS KEPT IN MEMORY ONLY, FOR THE LIFE OF THE PROCESS. A remembered window
// is what the server gave the model when it last loaded it, and a restart of
// the app is commonly a restart of Ollama too, or comes after its setting was
// changed: a figure carried across that from disk would be one nobody has
// measured since. Forgetting costs only the warning until the model next loads.
type Memory struct {
	mu       sync.Mutex
	windows  map[string]int
	readings map[string]Reading
}

// remembered is the key of one model of one server.
func remembered(base, name string) string { return base + "\x00" + tagged(name) }

// sawRunning remembers the window of every model the server is running now.
func (m *Memory) sawRunning(base string, running map[string]int) {
	if m == nil || len(running) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.windows == nil {
		m.windows = map[string]int{}
	}
	for name, window := range running {
		m.windows[remembered(base, name)] = window
	}
}

// lastWindow is the window the model was last seen running with, or 0.
func (m *Memory) lastWindow(base, name string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.windows[remembered(base, name)]
}

// keep remembers a reading a list finished, and recall answers it again for a
// list that could not ask, marked stale.
func (m *Memory) keep(base, name string, reading Reading) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readings == nil {
		m.readings = map[string]Reading{}
	}
	m.readings[remembered(base, name)] = reading
}

func (m *Memory) recall(base, name string) Reading {
	if m == nil {
		return Reading{Stale: true}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reading := m.readings[remembered(base, name)]
	reading.Stale = true
	return reading
}

// Reading is what one list learnt about one model.
type Reading struct {
	// Limits is where the model falls short, empty when nothing is known
	// against it.
	Limits []appwire.ModelLimit
	// Unloaded says the server answered that it is not running the model.
	Unloaded bool
	// Stale says this list ran out of time before the server could answer
	// about the model, so Limits and Unloaded are those of the last list that
	// could (nothing, when none could).
	Stale bool
}

// outcome is how one question went. Late is the list's time running out
// before the answer came, which says nothing about the model; refused is an
// answer, however unusable.
type outcome int

const (
	answered outcome = iota
	refused
	late
)

// shown is what the server says about one model, whether or not it is loaded.
type shown struct {
	// asked is how the question went. Every other field is nothing unless it
	// was answered.
	asked outcome
	// numCtx is the window the model's Modelfile sets, or 0 when it sets none.
	numCtx int
	// most is the most the model itself can hold, or 0 when the answer has none.
	most int
	// capabilities is the server's own list, empty from a server too old to
	// send one.
	capabilities []string
	// remote says the server only forwards this model to another machine, so
	// no window of this server's applies to it.
	remote bool
}

// Read answers, for each named model by the name asked with, where it falls
// short and whether the server is running it. needs is the window the app's
// prompts ask for.
//
// EVERY FAILURE IS SILENCE. A server that is not running, one that refuses, an
// answer in a shape this does not know: each reports nothing for the models it
// concerned, because a limit is shown to a person as a warning and a warning
// nobody measured is worse than none. A list that runs out of time is the one
// exception: it has learnt nothing, so it answers what the last list that
// finished learnt (Reading.Stale) rather than take a warning away for a moment.
func (p Probe) Read(ctx context.Context, names []string, needs int) map[string]Reading {
	if len(names) == 0 {
		return nil
	}
	readings := make(map[string]Reading, len(names))
	if ctx.Err() != nil {
		for _, name := range names {
			readings[name] = p.Memory.recall(p.base, name)
		}
		return readings
	}
	ctx, cancel := context.WithTimeout(ctx, Whole)
	defer cancel()
	var (
		running map[string]int
		listed  outcome
		wait    sync.WaitGroup
	)
	shows := make([]shown, len(names))
	gate := make(chan struct{}, atOnce)
	wait.Add(1 + len(names))
	guard.Go("ollamalimits/loaded", func() {
		defer wait.Done()
		running, listed = p.loaded(ctx)
	})
	for i, name := range names {
		guard.Go("ollamalimits/show", func() {
			defer wait.Done()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
				shows[i] = p.show(ctx, name)
			case <-ctx.Done():
				shows[i] = shown{asked: late}
			}
		})
	}
	wait.Wait()
	p.Memory.sawRunning(p.base, running)
	for i, name := range names {
		if listed == late || shows[i].asked == late {
			readings[name] = p.Memory.recall(p.base, name)
			continue
		}
		var reading Reading
		if listed == answered {
			loaded := running[tagged(name)]
			reading.Unloaded = loaded == 0
			if limit, small := smallWindow(loaded, p.Memory.lastWindow(p.base, name), shows[i], needs); small {
				reading.Limits = append(reading.Limits, limit)
			}
		}
		// A SERVER THAT SENDS NO LIST HAS NOT SAID THE MODEL LACKS TOOLS. Older
		// servers send none at all, and reading that as "no tools" would warn
		// about every model they hold.
		if len(shows[i].capabilities) > 0 && !slices.Contains(shows[i].capabilities, "tools") {
			reading.Limits = append(reading.Limits, appwire.ModelLimit{Kind: appwire.LimitNoTools})
		}
		p.Memory.keep(p.base, name, reading)
		readings[name] = reading
	}
	return readings
}

// smallWindow is the model's window limit and whether it has one, from the
// window the server runs it with now (loaded) and the one it last ran it with
// (last), either 0 when there is none. Whether the server answered /api/ps at
// all is the caller's: without that nobody knows which window counts.
//
// A window running now is believed over the Modelfile's, which only says what
// the next load would ask for; the Modelfile's over the last load's, because
// it overrides the server's own setting; and a model with none of the three
// has no window limit. The server decides its default when it loads a model,
// from the machine's memory or a setting the person may have raised, and no
// API says what it is, so a warning without a figure would be a guess.
func smallWindow(loaded, last int, model shown, needs int) (appwire.ModelLimit, bool) {
	limit := appwire.ModelLimit{Kind: appwire.LimitSmallWindow, Needs: needs, Most: model.most}
	switch {
	case loaded > 0:
		limit.Window, limit.Source = loaded, appwire.WindowLoaded
	case model.asked != answered || model.remote:
		return limit, false
	case model.numCtx > 0:
		limit.Window, limit.Source = model.numCtx, appwire.WindowModelfile
	case last > 0:
		limit.Window, limit.Source = last, appwire.WindowLastLoaded
	default:
		return limit, false
	}
	return limit, limit.Window < needs
}

// loaded is the window of every model the server is running now, by name, and
// how the question went.
func (p Probe) loaded(ctx context.Context) (map[string]int, outcome) {
	var answer struct {
		Models *[]struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if asked := p.ask(ctx, http.MethodGet, "/api/ps", nil, &answer); asked != answered {
		return nil, asked
	}
	if answer.Models == nil {
		return nil, refused
	}
	running := map[string]int{}
	for _, model := range *answer.Models {
		// A server from before the field was added sends no window; such a
		// model is then read by its Modelfile as though it were not loaded.
		if model.ContextLength <= 0 {
			continue
		}
		running[tagged(model.Name)] = model.ContextLength
		running[tagged(model.Model)] = model.ContextLength
	}
	return running, answered
}

// show is what the server says about one model.
func (p Probe) show(ctx context.Context, name string) shown {
	var answer struct {
		Parameters   string                     `json:"parameters"`
		ModelInfo    map[string]json.RawMessage `json:"model_info"`
		Capabilities []string                   `json:"capabilities"`
		RemoteHost   string                     `json:"remote_host"`
		RemoteModel  string                     `json:"remote_model"`
	}
	body, err := json.Marshal(map[string]string{"model": name})
	if err != nil {
		return shown{asked: refused}
	}
	if asked := p.ask(ctx, http.MethodPost, "/api/show", body, &answer); asked != answered {
		return shown{asked: asked}
	}
	return shown{
		asked:        answered,
		numCtx:       numCtx(answer.Parameters),
		most:         mostOf(answer.ModelInfo),
		capabilities: answer.Capabilities,
		remote:       answer.RemoteHost != "" || answer.RemoteModel != "",
	}
}

// ask sends one question and decodes its answer, and says how that went: any
// failure once this question's time or the list's has run out is late.
func (p Probe) ask(ctx context.Context, method, path string, body []byte, into any) outcome {
	ctx, cancel := context.WithTimeout(ctx, Each)
	defer cancel()
	failed := func() outcome {
		if ctx.Err() != nil {
			return late
		}
		return refused
	}
	request, err := http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	if err != nil {
		return refused
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if p.key != "" {
		request.Header.Set("Authorization", "Bearer "+p.key)
	}
	response, err := providerhttp.Clone(p.Client).Do(request)
	if err != nil {
		return failed()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return refused
	}
	if json.NewDecoder(io.LimitReader(response.Body, answerLimit)).Decode(into) != nil {
		return failed()
	}
	return answered
}

// numCtx reads the window out of a model's parameters, which the server sends
// as lines of a name and a value, and 0 when no line sets one.
func numCtx(parameters string) int {
	for _, line := range strings.Split(parameters, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "num_ctx" {
			continue
		}
		if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// mostOf is the most the model can hold. The server files it under the model's
// own architecture ("qwen3.context_length"), so the architecture is read first.
func mostOf(info map[string]json.RawMessage) int {
	var architecture string
	if json.Unmarshal(info["general.architecture"], &architecture) != nil || architecture == "" {
		return 0
	}
	var most float64
	if json.Unmarshal(info[architecture+".context_length"], &most) != nil || most < 1 || most > 1<<40 {
		return 0
	}
	return int(most)
}

// tagged is a model's name with the tag the server assumes when none is
// written, so "llama3" and "llama3:latest" are the one model they are.
func tagged(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		return name
	}
	return name + ":latest"
}
