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
// its own that follows no redirect off the server's origin.
type Probe struct {
	base   string
	key    string
	Client *http.Client
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

// shown is what the server says about one model, whether or not it is loaded.
type shown struct {
	// ok says the server answered. Every other field is nothing without it.
	ok bool
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

// Limits answers where each named model falls short, keyed by the name asked
// with; a model nothing is known against is not in the answer. needs is the
// window the app's prompts ask for.
//
// EVERY FAILURE IS SILENCE. A server that is not running, one that answers
// slowly, an answer in a shape this does not know: each reports nothing for
// the models it concerned, because a limit is shown to a person as a warning
// and a warning nobody measured is worse than none.
func (p Probe) Limits(ctx context.Context, names []string, needs int) map[string][]appwire.ModelLimit {
	if len(names) == 0 || ctx.Err() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, Whole)
	defer cancel()
	var (
		running map[string]int
		listed  bool
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
			}
		})
	}
	wait.Wait()
	limits := map[string][]appwire.ModelLimit{}
	for i, name := range names {
		var found []appwire.ModelLimit
		if limit, small := smallWindow(running[tagged(name)], listed, shows[i], needs); small {
			found = append(found, limit)
		}
		// A SERVER THAT SENDS NO LIST HAS NOT SAID THE MODEL LACKS TOOLS. Older
		// servers send none at all, and reading that as "no tools" would warn
		// about every model they hold.
		if len(shows[i].capabilities) > 0 && !slices.Contains(shows[i].capabilities, "tools") {
			found = append(found, appwire.ModelLimit{Kind: appwire.LimitNoTools})
		}
		if len(found) > 0 {
			limits[name] = found
		}
	}
	return limits
}

// smallWindow is the model's window limit and whether it has one. A window the
// server is running now is believed over the Modelfile's, which only says what
// the next load would ask for; and a model that is not loaded and sets nothing
// is reported with no figure, because the server's default applies and its API
// does not say what that is.
func smallWindow(loaded int, listed bool, model shown, needs int) (appwire.ModelLimit, bool) {
	limit := appwire.ModelLimit{Kind: appwire.LimitSmallWindow, Needs: needs, Most: model.most}
	switch {
	case !listed:
		// Whether it is loaded is not known, so neither is which window counts.
		return limit, false
	case loaded > 0:
		limit.Window, limit.Source = loaded, appwire.WindowLoaded
	case !model.ok || model.remote:
		return limit, false
	case model.numCtx > 0:
		limit.Window, limit.Source = model.numCtx, appwire.WindowModelfile
	default:
		limit.Source = appwire.WindowDefault
		return limit, true
	}
	return limit, limit.Window < needs
}

// loaded is the window of every model the server is running now, by name, and
// whether the server answered at all.
func (p Probe) loaded(ctx context.Context) (map[string]int, bool) {
	var answer struct {
		Models *[]struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if !p.ask(ctx, http.MethodGet, "/api/ps", nil, &answer) || answer.Models == nil {
		return nil, false
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
	return running, true
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
	if err != nil || !p.ask(ctx, http.MethodPost, "/api/show", body, &answer) {
		return shown{}
	}
	return shown{
		ok:           true,
		numCtx:       numCtx(answer.Parameters),
		most:         mostOf(answer.ModelInfo),
		capabilities: answer.Capabilities,
		remote:       answer.RemoteHost != "" || answer.RemoteModel != "",
	}
}

// ask sends one question and decodes its answer, and says whether both worked.
func (p Probe) ask(ctx context.Context, method, path string, body []byte, into any) bool {
	ctx, cancel := context.WithTimeout(ctx, Each)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	if err != nil {
		return false
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if p.key != "" {
		request.Header.Set("Authorization", "Bearer "+p.key)
	}
	response, err := providerhttp.Clone(p.Client).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(response.Body, answerLimit)).Decode(into) == nil
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
