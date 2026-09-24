package tui3

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
)

// ── ASKING THE ROUTER FOR TODAY'S LIST, FROM INSIDE /model ─────────────────
//
// The picker's list is resolved before it opens and never fetched on its own
// (models.go), and the catalog behind it is a day old at worst — so a model a
// provider shipped this morning is not in /model until tomorrow. This is the
// one way round that: a key, on the open list, that asks the door for the
// newest list while the list stays usable.
//
// Four properties are the whole design:
//
//   - IT IS ASKED FOR, NEVER TAKEN. Nothing here runs on a clock or on open; the
//     picker still opens in the same frame with what is already known.
//   - THE LIST IS NEVER TAKEN AWAY. A failure keeps the rows exactly as they
//     were, and a success only adds: a model that vanished is not mentioned.
//   - THE PICKER KEEPS ANSWERING. The fetch is a command off the loop; every key
//     still walks, filters and switches while it is out.
//   - A DOOR THAT CANNOT REFRESH HAS NO KEY. With [Options.RefreshModels] nil
//     the chord does nothing and no line on the surface names it — the absence
//     law, which is why every word below is spoken through [picker.offersRefresh].

// refreshModelsKey is the chord, and it is named ONCE: the placeholder, the
// matches line, the key router and the manual's sentence are all this constant.
//
// WHY ctrl+r. It reads as reload, and it is free here: its two other meanings on
// this surface — spell it out over a draft (spellout.go) and reveal a file in
// /files (deliverables.go) — can never fire while the picker is up, because the
// picker owns the keyboard before either of them is read (input.go).
const refreshModelsKey = "ctrl+r"

// The words this gesture says, each once.
const (
	// refreshModelsHint is the placeholder's field for the key.
	refreshModelsHint = refreshModelsKey + " refresh"
	// noModelMatches is the list's one line when the filter matched nothing, and
	// noModelMatchesFetch is that line on a list that can be refreshed: the
	// moment a person cannot find a model is the moment they want the newest
	// list.
	noModelMatches      = "no model matches"
	noModelMatchesFetch = noModelMatches + " · " + refreshModelsKey + " fetches the newest list"
	// modelsFetching stands at the head of the list while the fetch is out. It
	// is ONE PLACE and it is the list's rather than the placeholder's, because
	// the placeholder is gone the moment anything is typed and the list line is
	// there whatever the filter says.
	modelsFetching = "fetching the newest list…"
	// modelsNoteHead leads the note a landed fetch leaves in the conversation,
	// in the picker's voice: `models · 612 · 9 new · a, b, c`.
	modelsNoteHead   = "models"
	modelsNewWord    = "new"
	modelsNothingNew = "nothing new"
	// ModelsFetchFailed leads the note a failed fetch leaves, followed by what
	// the door said went wrong. It is exported for `codeaf models --refresh`,
	// which says the same sentence on stderr rather than a second spelling of it.
	ModelsFetchFailed = "could not fetch the model list"
)

// modelsNewNamed is how many new ids the note names after the count. Three is
// enough to recognise the one somebody was waiting for, and the count says how
// many more there are.
const modelsNewNamed = 3

// errNoModelList is what a fetch that came back with nothing to pick from is
// reported as. Keeping the old list is the law either way; this is the sentence.
var errNoModelList = errors.New("the router sent no list to pick from")

// modelsFetchedMsg is the door's answer, carried back onto the loop with the
// ids the picker was showing when the key was pressed — which is what "new"
// is measured against.
type modelsFetchedMsg struct {
	rows  []Model
	at    time.Time
	err   error
	shown map[string]bool
}

// offersRefresh is whether this list names the key and answers it right now:
// the door has a refresh behind it and none is already out. A second press
// while one is in flight does nothing, so it is not offered either.
func (p *picker) offersRefresh() bool { return p.refresh && !p.fetching }

// headLines is how many lines stand above the rows: one while a fetch is out,
// for [modelsFetching], and one for the table's own head where there is a table
// ([modelTableFit.header]).
//
// IT TAKES A WIDTH BECAUSE ONE OF THE TWO DEPENDS ON ONE. The header is drawn
// only where the columns are, so a narrow frame that falls back to the ranked
// tail spends nothing on a heading for columns it is not drawing — and the
// count here and the lines actually drawn must agree, or the list is laid into
// a block of the wrong size ([overlayItemLines] says the same about a row).
func (p *picker) headLines(width int) int {
	lines := p.tableHead(width)
	if p.fetching {
		lines++
	}
	return lines
}

// tableHead is the one line the columns' heads take, and none where this frame
// draws no columns. It is separate from [picker.headLines] because the two are
// counted against different ceilings ([picker.height] says why).
func (p *picker) tableHead(width int) int {
	if p.tableFit(width).drawn() {
		return 1
	}
	return 0
}

// emptyLine is the one line a list with no rows draws. While a fetch is out it
// is the fetching line itself, so the list never says two things at once.
func (p *picker) emptyLine() string {
	switch {
	case p.fetching:
		return modelsFetching
	case p.offersRefresh():
		return noModelMatchesFetch
	}
	return noModelMatches
}

// armRefresh tells a freshly opened picker what it may offer. It is called
// after every open, because [picker.start] forgets everything — and a fetch
// that is still out from a picker somebody closed is still out.
func (a *app) armRefresh() {
	a.pick.refresh = a.refreshModels != nil
	a.pick.fetching = a.pick.refresh && a.modelsFetching
}

// fetchModels is the key: it marks the fetch as out and hands the door's call
// to the runtime as a command, so THE PICKER NEVER WAITS ON IT ([Options.Models]
// states that law). It returns nil wherever the key is not offered.
//
// The ceiling is the catalog's own fifteen seconds, applied by the door's
// client (internal/catalog's fetch), so nothing here keeps a second clock that
// could disagree with it.
func (a *app) fetchModels() tea.Cmd {
	if a.refreshModels == nil || !a.pick.offersRefresh() {
		return nil
	}
	a.modelsFetching, a.pick.fetching = true, true
	shown := make(map[string]bool, len(a.pick.all))
	for _, model := range a.pick.all {
		shown[model.ID] = true
	}
	fetch, ctx := a.refreshModels, a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		rows, at, err := fetch(ctx)
		return modelsFetchedMsg{rows: rows, at: at, err: err, shown: shown}
	}
}

// modelsFetched takes the answer. The list an open picker shows is re-ranked
// with the filter kept and the cursor back on the model in use; the note says
// what changed, or why nothing did.
//
// A FAILURE CHANGES NO ROW. The door has already kept its catalog and the
// picker keeps what it was showing, so the only thing a failure adds is its
// sentence — and the key, which is offered again.
func (a *app) modelsFetched(msg modelsFetchedMsg) {
	a.modelsFetching, a.pick.fetching = false, false
	a.touch()
	list := keepModels(msg.rows, chatModel)
	err := msg.err
	if err == nil && (len(list) == 0 || msg.at.IsZero()) {
		// A list with nothing to talk to, or rows nobody fetched, is not a list
		// that may replace the one on screen.
		err = errNoModelList
	}
	if err != nil {
		a.note(ModelsFetchFailed + " · " + strings.Join(strings.Fields(err.Error()), " "))
		return
	}
	// AND A FETCH THAT LANDED REWROTE THE CACHE ON DISK (cmd/codeaf's v3 door),
	// so this is the moment the memo behind the picker's second rung stopped
	// being true. Dropping it here is what makes ctrl+r a fresh list on every
	// road onto it rather than only on the one the fetch came back through
	// (models.go's [app.forgetModelList]).
	//
	// IT IS AFTER THE FAILURE CHECK because a fetch that failed wrote nothing:
	// dropping the memo there would throw away a good reading to punish a bad
	// call, and the next frame would fall to the built-ins.
	a.forgetModelList("", modelcatalog.DefaultBaseURL)
	a.refreshCreditWarnings()
	if a.pick.open && a.pick.refresh {
		a.pick.restock(list)
	}
	note, named := modelsNote(list, msg.shown)
	a.noteFacts(note, named...)
}

// modelsNote is the landed fetch in one line — `models · 612 · 9 new · a, b,
// c`, or `models · 612 · nothing new` — with the new ids as the facts that step
// to ink. New is measured against what the picker was showing, in the list's
// own order, and a model that vanished is never counted: a refresh only adds.
func modelsNote(list []Model, shown map[string]bool) (string, []string) {
	var named []string
	fresh := 0
	for _, model := range list {
		if shown[model.ID] {
			continue
		}
		fresh++
		if len(named) < modelsNewNamed {
			named = append(named, model.ID)
		}
	}
	parts := []string{modelsNoteHead}
	// THE EMPTINESS LAW: a count of zero rows is drawn as nothing.
	if len(list) > 0 {
		parts = append(parts, strconv.Itoa(len(list)))
	}
	if fresh == 0 {
		parts = append(parts, modelsNothingNew)
	} else {
		parts = append(parts, strconv.Itoa(fresh)+" "+modelsNewWord, strings.Join(named, ", "))
	}
	return strings.Join(parts, " · "), named
}
