package tui3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"

	"charm.land/bubbletea/v2"
)

// Known ports to probe for local model servers on loopback (issue #1508).
var localProbePorts = []struct {
	port int
	name string
}{
	{11434, "Ollama  :11434"},
	{1234, "LM Studio  :1234"},
	{8000, "vLLM  :8000"},
	{8080, "llama.cpp  :8080"},
	{8317, "127.0.0.1:8317"},
}

// LocalServerProbe represents a running model server discovered on this machine.
type LocalServerProbe struct {
	Port    int
	Name    string
	Address string
	Models  int
}

var (
	errAuthRequired   = errors.New("authentication required")
	errEmptyModelList = errors.New("lists no models")
	localProbeClient  = &http.Client{Timeout: 600 * time.Millisecond}
)

// probeLoopbackPort probes a single local loopback port for an OpenAI-compatible /v1/models endpoint.
func probeLoopbackPort(ctx context.Context, port int, name string) *LocalServerProbe {
	addr := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	reqURL := addr + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	resp, err := localProbeClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Data   []any `json:"data"`
		Models []any `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}
	count := len(body.Data)
	if count == 0 {
		count = len(body.Models)
	}
	return &LocalServerProbe{
		Port:    port,
		Name:    name,
		Address: addr,
		Models:  count,
	}
}

// ProbeLocalServers probes all usual local ports concurrently with a short timeout.
func ProbeLocalServers(ctx context.Context) []LocalServerProbe {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	var (
		mu      sync.Mutex
		results []LocalServerProbe
		wg      sync.WaitGroup
	)

	for _, target := range localProbePorts {
		wg.Add(1)
		go func(p int, n string) {
			defer wg.Done()
			if probe := probeLoopbackPort(ctx, p, n); probe != nil {
				mu.Lock()
				results = append(results, *probe)
				mu.Unlock()
			}
		}(target.port, target.name)
	}
	wg.Wait()
	return results
}

// ProbeOpenAIEndpoint tests an address live and returns its model count or error.
func ProbeOpenAIEndpoint(ctx context.Context, address, key string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if address == "" {
		return 0, errors.New("empty address")
	}
	reqURL := address + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := localProbeClient.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) && uerr.Err != nil {
			return 0, uerr.Err
		}
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return 0, errAuthRequired
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var body struct {
		Data   []any `json:"data"`
		Models []any `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	count := len(body.Data)
	if count == 0 {
		count = len(body.Models)
	}
	return count, nil
}

// localProbesFoundMsg is the answer to the panel's launch probe: the model
// servers already answering on this machine, or none.
type localProbesFoundMsg struct {
	probes []LocalServerProbe
}

// customAddressCheckedMsg is what the address check came back with. A nil err
// means the address listed models; errAuthRequired means it answered and asked
// for a key, which is a live address all the same.
type customAddressCheckedMsg struct {
	address string
	count   int
	err     error
}

// ── the add-a-provider panel ──────────────────────────────────────────────────
//
// One panel, three doors ([app.openAddProvider]): the /connect panel's add row
// mints a custom draft and then shows what can fill it, /model offers the same
// panel beside its list (ctrl+o), and the settings sheet's Providers tab keeps
// its own row flow. The mint and the connect behind every row are the ONE flow
// (startModelConnect, startCustomAdd, modelEntryAnswer) — the panel is a door,
// never a second implementation.

type addProviderItem struct {
	heading bool
	title   string
	detail  string
	probe   *LocalServerProbe
	// custom marks the typed-address row: the panel's own door onto the mint
	// flow the add row started.
	custom bool
	// sourceID is the vendored provider a row starts (startModelConnect).
	sourceID string
}

type addProviderPanel struct {
	open bool

	items  []addProviderItem
	cursor int

	// loading is whether the local probe is still out.
	loading bool
	// entry is the flow's answer box while the panel hosts it: an address a
	// probe prefilled, then the name and key the mint flow asks for
	// (modelEntryAnswer). Nil while the list is being walked.
	entry *keyEntry
	// err is why the last probe found nothing worth saying. Empty draws nothing.
	err string
}

func (p *addProviderPanel) rebuild(probes []LocalServerProbe, catalog []modelsource.Source) {
	var items []addProviderItem
	if p.loading {
		items = append(items, addProviderItem{heading: true, title: "looking on this machine…"})
	} else if len(probes) > 0 {
		items = append(items, addProviderItem{heading: true, title: "found on this machine"})
		for i := range probes {
			pr := probes[i]
			items = append(items, addProviderItem{
				title:  pr.Name,
				detail: strconv.Itoa(pr.Models) + " models",
				probe:  &probes[i],
			})
		}
	}
	items = append(items, addProviderItem{heading: true, title: "providers"})
	if len(catalog) == 0 {
		catalog = modelsource.Vendored()
	}
	for _, source := range catalog {
		switch {
		case strings.EqualFold(source.ID, modelsource.CustomID), strings.EqualFold(source.ID, modelsource.DefaultID):
			// Custom has its own typed-address row below; openrouter is the
			// default service and connects through its own key, not here.
		case source.ID == "codex":
			items = append(items, addProviderItem{title: source.Name, detail: "browser", sourceID: source.ID})
		case source.KeyOptional:
			items = append(items, addProviderItem{title: source.Name, detail: "address", sourceID: source.ID})
		case len(source.Regions) > 0:
			items = append(items, addProviderItem{title: source.Name, detail: "region · key", sourceID: source.ID})
		default:
			items = append(items, addProviderItem{title: source.Name, detail: "key", sourceID: source.ID})
		}
	}
	items = append(items, addProviderItem{
		title: "any OpenAI-compatible server", detail: "address · key", custom: true,
	})
	p.items = items
	p.cursor = 0
	for i, it := range p.items {
		if !it.heading {
			p.cursor = i
			break
		}
	}
}

func (p *addProviderPanel) move(delta int) {
	if len(p.items) == 0 {
		return
	}
	next := p.cursor + delta
	for next >= 0 && next < len(p.items) {
		if !p.items[next].heading {
			p.cursor = next
			return
		}
		next += delta
	}
}

func (p *addProviderPanel) current() (addProviderItem, bool) {
	if p.cursor >= 0 && p.cursor < len(p.items) && !p.items[p.cursor].heading {
		return p.items[p.cursor], true
	}
	return addProviderItem{}, false
}

func (p *addProviderPanel) close() {
	*p = addProviderPanel{}
}

// height is how many rows the panel wants from the frame's overlay budget.
func (a *app) addPanelKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.addPanel
	if p.entry != nil {
		switch msg.String() {
		case "esc":
			p.entry = nil
			p.err = ""
			a.touch()
		case "enter":
			entry := p.entry
			p.entry = nil
			return a.modelEntryAnswer(entry)
		default:
			p.entry.typeInto(msg)
			a.touch()
		}
		return nil
	}
	switch msg.String() {
	case "esc":
		p.close()
		a.touch()
	case "up", "ctrl+p":
		p.move(-1)
		a.touch()
	case "down", "ctrl+n":
		p.move(1)
		a.touch()
	case "enter":
		item, ok := p.current()
		if !ok {
			return nil
		}
		p.close()
		a.touch()
		switch {
		case item.custom:
			return a.startCustomAdd(false)
		case item.sourceID != "":
			source, found := a.modelSource(item.sourceID)
			if !found {
				return nil
			}
			return a.startModelConnect(modelConnectionStatus(source, false), false)
		case item.probe != nil:
			// A LIVE LOCAL SERVER MINTS A CUSTOM DRAFT WITH ITS ADDRESS
			// PREFILLED: the answer box the mint flow raises comes up already
			// carrying the address that answered, so the only thing left to
			// type is the key it may want.
			cmd := a.startCustomAdd(false)
			if entry := a.connPanel.entry; entry != nil {
				entry.box.setText(item.probe.Address)
			}
			return cmd
		}
	}
	return nil
}

func (p *addProviderPanel) height(width int) int {
	if !p.open {
		return 0
	}
	want := len(p.items)
	if p.entry != nil {
		want += 3
	}
	if p.err != "" {
		want++
	}
	if want < 1 {
		return 1
	}
	return want
}

// draw is the panel as rows. Headings dim, the cursor's row accent, and the
// flow's answer box hangs under the list while it is open — the panel is a
// short list, so both fit where a taller list would have to choose.
func (p *addProviderPanel) draw(width, n int, pal palette, hover int) []string {
	if !p.open || n < 1 {
		return nil
	}
	rows := make([]string, 0, n)
	// THE BOX DRAWS LAST AND THE LIST GIVES IT ROOM: the question being answered
	// is why the panel is up, and a list that scrolled the box off would be a
	// list that swallowed a keystroke.
	box := []string(nil)
	if p.entry != nil {
		box, _, _ = keyBoxLines(p.entry, pal, width, 2, 3)
	}
	list := n - len(box)
	if p.err != "" {
		list--
	}
	for at := 0; at < len(p.items) && len(rows) < list; at++ {
		item := p.items[at]
		line := ""
		switch {
		case item.heading:
			line = pal.dim(fit("  "+item.title, width))
		case at == p.cursor:
			line = pal.accent("  › ") + pal.ink(fit(item.title+"  "+item.detail, width-4))
		default:
			line = pal.dim(fit("    "+item.title+"  "+item.detail, width))
		}
		rows = append(rows, line)
	}
	for _, line := range box {
		if len(rows) >= n {
			break
		}
		rows = append(rows, line)
	}
	if p.err != "" && len(rows) < n {
		rows = append(rows, pal.dim(fit("  "+p.err, width)))
	}
	return rows
}

// ── the app's door onto the panel ────────────────────────────────────────────
//
// addProviderRowWord rides [app.modelList] as a row; this is what enter on it
// opens. The panel is a door and not a second implementation: every row it
// activates ends in the mint flow (startModelConnect, startCustomAdd).

// localServersProbedMsg is the probe's landing: the servers the loopback walk
// found, ready to be drawn as rows.
type localServersProbedMsg struct {
	probes []LocalServerProbe
}

// probeLocalServersCmd walks the known local ports once, off the loop.
func probeLocalServersCmd() tea.Cmd {
	return func() tea.Msg {
		return localServersProbedMsg{probes: ProbeLocalServers(context.Background())}
	}
}

// openAddProvider raises the panel. From the settings sheet the tab keeps its
// own add row ([startCustomAdd]); from everywhere else the panel is the door:
// it opens, walks the machine for live servers, and rebuilds on their landing.
func (a *app) openAddProvider(inSheet bool) tea.Cmd {
	if inSheet {
		return a.startCustomAdd(true)
	}
	p := &a.addPanel
	p.open = true
	p.loading = true
	p.rebuild(nil, nil)
	a.touch()
	// THE FRAME AND THE WALK GO OUT TOGETHER: the panel is up this frame, and
	// the probe's landing rebuilds it with what the machine answered.
	return tea.Batch(a.frameTick(), probeLocalServersCmd())
}
