package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/charmbracelet/x/ansi"
)

const setupProviderHeading = "choose a model provider"
const setupProviderSentence = "connect the provider you want to use. you can add more later with /connect."

// The connection attempt carries cancellation through address probes, sign-in,
// and model discovery. Results from a screen the person left are never adopted.
type setupProviderAttempt struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type setupProviderHit struct{ x, y, width, at int }
type setupProviderRow struct{ id, name string }

// All provider doors use the same supported catalog and display order.
func providerCatalog(catalog []modelsource.Source) []modelsource.Source {
	if len(catalog) == 0 {
		catalog = modelsource.Vendored()
	}
	byID := map[string]modelsource.Source{modelsource.DefaultID: modelsource.DefaultSource("")}
	for _, source := range catalog {
		byID[source.ID] = source
	}
	var rows []modelsource.Source
	seen := map[string]bool{}
	for _, id := range []string{modelsource.DefaultID, "ollama", "codex", "deepseek"} {
		if source, ok := byID[id]; ok {
			rows = append(rows, source)
			seen[id] = true
		}
	}
	for _, source := range catalog {
		if !seen[source.ID] {
			rows = append(rows, source)
			seen[source.ID] = true
		}
	}
	return rows
}

func (a *app) setupProviderRows() []setupProviderRow {
	var rows []setupProviderRow
	for _, source := range providerCatalog(a.modelCatalog) {
		rows = append(rows, setupProviderRow{source.ID, source.Name})
	}
	return rows
}

func (a *app) setupProviderKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.setup
	rows := a.setupProviderRows()
	switch msg.String() {
	case "esc":
		return a.endSetup(true)
	case "up", "ctrl+p":
		s.providerAt = moveCursor(s.providerAt, -1, len(rows))
	case "down", "ctrl+n", "tab":
		s.providerAt = moveCursor(s.providerAt, 1, len(rows))
	case "home", "pgup":
		s.providerAt = 0
	case "end", "pgdown":
		s.providerAt = len(rows) - 1
	case "enter":
		if len(rows) > 0 {
			return a.selectSetupProvider(rows[clampIndex(s.providerAt, len(rows))].id)
		}
	}
	a.touch()
	return nil
}

func (a *app) selectSetupProvider(id string) tea.Cmd {
	s := &a.setup
	a.cancelSetupProvider()
	s.provider, s.text, s.refusal = id, "", ""
	s.providerHits = nil
	a.touch()
	if id != modelsource.DefaultID && id != "codex" {
		return a.startSetupProvider()
	}
	return nil
}

func (a *app) startSetupProvider() tea.Cmd {
	a.cancelSetupProvider()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	a.setup.providerAttempt = &setupProviderAttempt{ctx: ctx, cancel: cancel}
	if len(a.modelCatalog) == 0 {
		a.modelCatalog = modelsource.Vendored()
	}
	source, ok := a.modelSource(a.setup.provider)
	if !ok {
		a.setup.refusal = "that provider is unavailable here"
		return nil
	}
	return a.startModelConnect(modelConnectionStatus(source, false), false)
}

func (a *app) cancelSetupProvider() {
	if a.setup.providerAttempt != nil {
		a.setup.providerAttempt.cancel()
		a.setup.providerAttempt = nil
		if a.codexFlow != nil {
			a.codexFlow.Cancel()
			a.codexFlow = nil
		}
		a.cancelModelEntry(a.connPanel.entry)
		a.modelDraft = nil
		a.connPanel.entry = nil
	}
	a.setup.providerBusy, a.setup.providerLink = false, ""
}

func (a *app) backSetupProvider() tea.Cmd {
	if a.setup.connection {
		return a.endSetup(true)
	}
	a.cancelSetupAuth()
	a.cancelSetupProvider()
	a.setup.provider, a.setup.text, a.setup.refusal = "", "", ""
	a.setup.providerHits = nil
	a.touch()
	return nil
}

func (a *app) setupProviderCurrent(attempt *setupProviderAttempt) bool {
	return a.setup.open && a.setup.step() == setupKey && a.setup.providerAttempt == attempt && attempt.ctx.Err() == nil
}

func (a *app) setupServiceKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.setup
	switch msg.String() {
	case "esc":
		if s.providerBusy {
			a.cancelSetupProvider()
			s.refusal = "cancelled · enter tries again"
			a.touch()
			return nil
		}
		return a.endSetup(true)
	case "enter":
		if !s.providerBusy && a.connPanel.entry == nil {
			s.refusal = ""
			return a.startSetupProvider()
		}
	}
	if s.providerBusy {
		return nil
	}
	s.refusal = ""
	if a.connPanel.entry != nil {
		return a.connectEntryKey(msg)
	}
	return nil
}

func (a *app) adoptSetupProviderResult(msg modelConnectResultMsg) tea.Cmd {
	if !a.setupProviderCurrent(msg.setupAttempt) {
		return nil
	}
	a.setup.providerBusy, a.setup.providerLink = false, ""
	// The ordinary connection adoption owns the model switch and profile reload.
	if msg.err != nil {
		if msg.browser {
			a.codexFlow = nil
			a.setup.refusal = msg.word
		} else {
			a.setup.refusal = setupSaid(msg.err, "could not connect · check the provider and try again")
		}
		a.touch()
		return nil
	}
	a.adoptModelConnectResult(msg)
	if msg.err == nil && msg.outcome.Kind == modelsource.OutcomeConnected {
		a.cancelSetupProvider()
		return a.advanceSetup()
	}
	if msg.browser {
		a.setup.refusal = msg.word
	}
	a.touch()
	return nil
}

// The pointer reads only the rows actually drawn, including a short window.
func (a *app) setupProviderPress(x, y int) tea.Cmd {
	for _, hit := range a.setup.providerHits {
		if y != hit.y || x < hit.x || x >= hit.x+hit.width {
			continue
		}
		if hit.at == -1 {
			return a.backSetupProvider()
		}
		if hit.at == -2 {
			return a.setupServiceKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		if a.setup.provider == "" {
			a.setup.providerAt = hit.at
			return a.selectSetupProvider(a.setupProviderRows()[hit.at].id)
		}
		if entry := a.connPanel.entry; entry != nil && entry.choosing() {
			entry.at = hit.at
			return a.setupServiceKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	}
	return nil
}

func (a *app) setupProvidersFrame(width, height int) ([]string, int, int) {
	inner := max(1, min(width-4, welcomeUnitWidth))
	rows := a.setupProviderRows()
	a.setup.providerAt = clampIndex(a.setup.providerAt, len(rows))
	heading := setupProviderHeading
	body := a.setupProviderHead(height)
	body = append(body, a.pal.dim("setting up"), "", a.pal.ink(heading))
	if height >= 18 {
		for _, line := range wrap(setupProviderSentence, inner) {
			body = append(body, a.pal.dim(line))
		}
	}
	body = append(body, "")
	hits := []setupProviderHit{}
	for at := range rows {
		mark, ink := "  ", a.pal.dim
		if at == a.setup.providerAt {
			mark, ink = setupLead, a.pal.ink
		}
		hits = append(hits, setupProviderHit{y: len(body), at: at})
		body = append(body, ink(mark+rows[at].name))
	}
	footer := "↑/↓ choose · enter selects · " + setupSkipKeysWord
	if inner < 58 {
		footer = "enter chooses · " + setupSkipKeysWord
	}
	// THE PROVIDERS COME BEFORE THE HINT. Keep the footer only when it cannot
	// displace a provider; the shared block already lets the heading yield.
	if height > len(rows) {
		if height >= len(rows)+2 {
			body = append(body, "")
		}
		body = append(body, a.pal.dim(footer))
	}
	return a.setupProviderBlock(body, hits, width, height, -1, 0)
}

func (a *app) setupServiceFrame(width, height int) ([]string, int, int) {
	s := &a.setup
	inner := max(1, min(width-4, welcomeUnitWidth))
	source, _ := a.modelSource(s.provider)
	name := strings.ToLower(source.Name)
	body := a.setupProviderHead(height)
	body = append(body, a.pal.dim(setupTitle(s)), "", a.pal.ink("connect "+name))
	hits := []setupProviderHit{}
	caret, caretX := -1, 0
	if s.providerBusy {
		word := "connecting · checking the provider"
		if s.provider == "codex" {
			word = "finish signing in in your browser"
		}
		body = append(body, a.pal.dim(word))
		if s.providerLink != "" {
			body = append(body, a.pal.dim(linkify(fit(signInLinkWord, inner), s.providerLink)))
		}
	} else if entry := a.connPanel.entry; entry != nil {
		body = append(body, a.pal.dim("your "+entry.blank), "")
		if entry.choosing() {
			for at, choice := range entry.choices {
				mark := "  "
				if at == entry.at {
					mark = setupLead
				}
				hits = append(hits, setupProviderHit{y: len(body), at: at})
				body = append(body, a.pal.ink(mark+choice.Name))
			}
		} else {
			shown := entry.box.String()
			if entry.secret {
				shown = maskTyped(shown)
			}
			caret, caretX = len(body), ansi.StringWidth(setupLead+shown)
			body = append(body, a.pal.accent(setupLead)+a.pal.ink(shown))
		}
	} else if s.provider == "codex" {
		for _, line := range wrap("sign in once in your browser with your ChatGPT plan.", inner) {
			body = append(body, a.pal.dim(line))
		}
	} else {
		hits = append(hits, setupProviderHit{y: len(body), at: -2})
		body = append(body, a.pal.ink("enter tries again"))
	}
	if s.refusal != "" {
		for _, line := range wrap(s.refusal, inner) {
			body = append(body, a.pal.accent(line))
		}
	}
	body = append(body, "")
	hits = append(hits, setupProviderHit{y: len(body), at: -1})
	body = append(body, a.pal.dim("Back · alt+left"))
	footer := "enter continues · " + setupSkipKeysWord
	if s.provider == "codex" && !s.providerBusy {
		footer = setupBrowserConnectKeysWord + " · " + setupSkipKeysWord
		hits = append(hits, setupProviderHit{y: len(body), at: -2})
	}
	if s.providerBusy {
		footer = "esc cancels · alt+left back"
		if s.providerLink != "" {
			footer = "ctrl+y copies link · esc cancels"
		}
	}
	body = append(body, a.pal.dim(footer))
	return a.setupProviderBlock(body, hits, width, height, caret, caretX)
}

// A provider form shares the welcome measure. When height runs out, the head
// gives way before the answer and navigation at the foot.
func (a *app) setupProviderBlock(body []string, hits []setupProviderHit, width, height, caret, caretX int) ([]string, int, int) {
	lead := max(0, (width-max(1, min(width-4, welcomeUnitWidth)))/2)
	dropped := max(0, len(body)-height)
	body = body[dropped:]
	top := max(0, welcomeAbove(len(body), height-len(body)))
	a.setup.providerHits = nil
	for _, hit := range hits {
		if hit.y < dropped {
			continue
		}
		hit.x, hit.y, hit.width = lead, top+hit.y-dropped, max(1, width-lead*2)
		a.setup.providerHits = append(a.setup.providerHits, hit)
	}
	lines := make([]string, top)
	for _, line := range body {
		lines = append(lines, strings.Repeat(" ", lead)+ansi.Truncate(line, max(1, width-lead), ""))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	a.caret = caret >= dropped
	return lines, lead + caretX, top + caret - dropped
}

// The wordmark keeps the welcome screen's measure and yields on short windows.
func (a *app) setupProviderHead(height int) []string {
	if height < 24 {
		return nil
	}
	body := []string{}
	for _, row := range wordmarkRows(a.pal.ascii) {
		body = append(body, a.pal.muted(row))
	}
	return append(body, "")
}
