package tui3

// THE SEARCH PLACE DRAWS A READING, NEVER THE CONVERSATION STORE.
//
// The store read can wait on SQLite, so callers run it as a Bubble Tea command
// after the quiet interval below. Resizes, cursor moves, and paint frames only
// reshape [searchReading], which is an immutable answer made from supplied
// facts and a supplied clock.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

const (
	searchShown         = 12
	searchFetch         = 50
	searchDebounceEvery = 150 * time.Millisecond
)

type searchHit struct {
	sessionID  string
	transcript string
	dir        string
	title      string
	snippet    string
	when       time.Time
	project    string
	live       bool
	more       int
}

type searchFacet struct {
	word  string
	count int
}

type searchReading struct {
	query  string
	hits   []searchHit
	facets []searchFacet
	now    time.Time
}

// readSearch joins the store's remembered turns to the already-read world.
// A conversation absent from that snapshot remains a valid result because the
// message itself is durable even when its project metadata is unavailable.
func readSearch(query string, found []store.ConversationHit, world session.World, now time.Time) searchReading {
	r := searchReading{query: strings.TrimSpace(query), now: now}
	rows := make(map[string]session.SessionRow)
	for _, row := range world.Sessions() {
		rows[row.ID] = row
	}
	projects := make(map[string]int)
	bySession := make(map[string]int)
	for _, found := range found {
		id := strings.TrimSpace(found.SessionID)
		if at, ok := bySession[id]; ok && id != "" {
			r.hits[at].more++
			continue
		}
		hit := searchHit{sessionID: id, title: searchOneLine(found.Title), snippet: searchOneLine(found.Body), when: found.Time}
		if row, ok := rows[id]; ok {
			hit.transcript = row.Transcript
			hit.dir = row.Dir
			hit.project = strings.TrimSpace(row.Project)
			hit.live = row.Live
			if hit.title == "" {
				hit.title = searchOneLine(row.Title)
			}
			if hit.project != "" {
				projects[hit.project]++
			}
		}
		if hit.title == "" {
			hit.title = hit.snippet
		}
		bySession[id] = len(r.hits)
		r.hits = append(r.hits, hit)
	}
	for word, count := range projects {
		r.facets = append(r.facets, searchFacet{word: strings.ToLower(word), count: count})
	}
	sort.Slice(r.facets, func(i, j int) bool {
		if r.facets[i].count != r.facets[j].count {
			return r.facets[i].count > r.facets[j].count
		}
		return r.facets[i].word < r.facets[j].word
	})
	return r
}

// searchOneLine turns journal prose into a row without changing its words.
// Newlines and repeated spacing are layout, not part of a matching turn's
// quotation on this page.
func searchOneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// rows keeps one physical line per conversation. The project is the first
// optional fact to leave a narrow frame; the quoted turn leaves next, while
// the title and its door remain.
func (r searchReading) rows(width int, pal palette) []string {
	if width <= 0 {
		return nil
	}
	if r.query == "" {
		out := searchTeach(pal)
		for i := range out {
			out[i] = fit(out[i], width)
		}
		return out
	}
	if len(r.hits) == 0 {
		return []string{pal.dim(fit(fmt.Sprintf("nothing on this machine says %q", r.query), width))}
	}
	var out []string
	if legend := r.legend(width, pal); legend != "" {
		out = append(out, legend)
	}
	shown := len(r.hits)
	if shown > searchShown {
		shown = searchShown
	}
	for _, hit := range r.hits[:shown] {
		out = append(out, searchRowAt(hit, r.query, width, r.now, pal))
	}
	if more := len(r.hits) - shown; more > 0 {
		out = append(out, pal.dim(fit(fmt.Sprintf("%s %d more", tokens.GlyphCollapsed, more), width)))
	}
	return out
}

func (r searchReading) legend(width int, pal palette) string {
	parts := make([]string, 0, len(r.facets))
	for _, facet := range r.facets {
		parts = append(parts, fmt.Sprintf("%s %d", facet.word, facet.count))
	}
	return pal.dim(fit(strings.Join(parts, " · "), width))
}

func searchRowAt(hit searchHit, query string, width int, now time.Time, pal palette) string {
	lead := pal.dim(tokens.GlyphPromptChat + " ")
	age := sinceAt(hit.when, now)
	project := hit.project
	if width < 80 {
		project = ""
	}
	tail := searchTail(project, age, pal)
	room := width - ansi.StringWidth(lead) - ansi.StringWidth(tail)
	if tail != "" {
		room--
	}
	if room < 0 {
		room = 0
	}
	title := fit(hit.title, room)
	snippet := hit.snippet
	more := ""
	if hit.more > 0 {
		more = fmt.Sprintf(" · %d more in this chat", hit.more)
	}
	plainMiddle := ""
	if snippet != "" {
		plainMiddle = " · " + snippet
	}
	plainMiddle += more
	remaining := room - ansi.StringWidth(title)
	if ansi.StringWidth(plainMiddle) > remaining {
		plainMiddle = fit(plainMiddle, remaining)
	}
	if remaining < 6 {
		plainMiddle = ""
	}
	left := lead + pal.ink(title) + searchMarked(plainMiddle, query, pal)
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(tail)
	if tail != "" && pad < 1 {
		tail = ""
		pad = width - ansi.StringWidth(left)
	}
	if pad < 0 {
		pad = 0
	}
	return fit(left+strings.Repeat(" ", pad)+tail, width)
}

func searchTail(project, age string, pal palette) string {
	words := make([]string, 0, 2)
	if project != "" {
		words = append(words, project)
	}
	if age != "" {
		words = append(words, age)
	}
	return pal.dim(strings.Join(words, " · "))
}

func searchMarked(text, query string, pal palette) string {
	terms := strings.Fields(query)
	if text == "" || len(terms) == 0 {
		return pal.dim(text)
	}
	sort.Slice(terms, func(i, j int) bool { return len(terms[i]) > len(terms[j]) })
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, regexp.QuoteMeta(term))
	}
	re := regexp.MustCompile("(?i)" + strings.Join(quoted, "|"))
	var out strings.Builder
	last := 0
	for _, loc := range re.FindAllStringIndex(text, -1) {
		out.WriteString(pal.dim(text[last:loc[0]]))
		out.WriteString(pal.bold(text[loc[0]:loc[1]]))
		last = loc[1]
	}
	out.WriteString(pal.dim(text[last:]))
	return out.String()
}

// at maps only visible conversation rows. The facet legend and folded count
// are context, not doors.
func (r searchReading) at(i int) (searchHit, bool) {
	if i < 0 || r.query == "" || len(r.hits) == 0 {
		return searchHit{}, false
	}
	start := 0
	if len(r.facets) > 0 {
		start = 1
	}
	at := i - start
	if at < 0 || at >= len(r.hits) || at >= searchShown {
		return searchHit{}, false
	}
	return r.hits[at], true
}

func searchTeach(pal palette) []string {
	return []string{
		pal.dim("search reads every message in every conversation on this machine."),
		pal.dim("typing here searches; typing on home starts something."),
		pal.dim("enter opens the conversation at the matching turn."),
	}
}

type searchStore interface {
	SearchConversations(terms string, limit int) ([]store.ConversationHit, error)
}

type searchAsk struct {
	query string
	gen   int
}

type searchDoneMsg struct {
	ask  searchAsk
	hits []store.ConversationHit
	err  error
}

func searchCmd(s searchStore, ask searchAsk) tea.Cmd {
	return func() tea.Msg {
		hits, err := s.SearchConversations(ask.query, searchFetch)
		return searchDoneMsg{ask: ask, hits: hits, err: err}
	}
}

type searchTickMsg struct{ gen int }

func searchDebounce(gen int) tea.Cmd {
	return tea.Tick(searchDebounceEvery, func(time.Time) tea.Msg { return searchTickMsg{gen: gen} })
}
