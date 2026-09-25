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

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
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
	// noIndex says there is no conversation store behind this window, and so
	// nothing to search at all ([searchNoIndexWord]).
	//
	// THE PLACE MATCHED BY NAME HERE FOR ONE BUILD on 2026-09-22 — the way
	// home's box does — and the owner took that back on 2026-09-23. A place
	// called `search` that quietly searches something narrower than what it
	// says is worse than one that refuses: the refusal is a fact a person can
	// act on, and half a search reads like a whole one that found nothing.
	noIndex bool
	// unfolded is whether every result is drawn rather than the first
	// [searchShown] and a fold line ([searchReading.unfolding]).
	unfolded bool
}

// unfolding is this reading with its fold open or shut, as a copy.
func (r searchReading) unfolding(open bool) searchReading {
	r.unfolded = open
	return r
}

// drawn is how many results the page draws: all of them with the fold open,
// the first [searchShown] with it shut.
func (r searchReading) drawn() int {
	if r.unfolded || len(r.hits) <= searchShown {
		return len(r.hits)
	}
	return searchShown
}

// foldAt is whether body line i is the fold line under the results, which is a
// door both ways: `▸ 38 more` draws the rest where they stand and `▾ 38 fewer`
// puts them back.
func (r searchReading) foldAt(i int) bool {
	if r.query == "" || len(r.hits) <= searchShown {
		return false
	}
	start := 0
	if len(r.facets) > 0 {
		start = 1
	}
	return i == start+r.drawn()
}

// stop is whether body line i is one the cursor may stand on: a result, or the
// fold line.
func (r searchReading) stop(i int) bool {
	_, ok := r.at(i)
	return ok || r.foldAt(i)
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
func (r searchReading) rows(width int, pal palette) []string { return r.paint(width, pal, nil) }

// paint is [searchReading.rows] with the lines the cursor or the pointer is on
// lit (placeprose.go's THE FIVE-LEVEL SCALE); a nil lit lights nothing.
func (r searchReading) paint(width int, pal palette, lit func(line int) bool) []string {
	if width <= 0 {
		return nil
	}
	// EVERY ROW HANGS FROM THE BODY'S OWN COLUMN, which is one cell in — where
	// tasks, standing and spend all hang theirs (placebodies.go's
	// [placeTeachRows]). This page built its rows itself and started at column 1,
	// so walking the tab bar left to right the body stepped sideways on two
	// places out of seven. The lead goes on here, once, and everything below is
	// built into the cell less that leaves.
	room := width - 1
	if room <= 0 {
		return nil
	}
	if r.noIndex {
		// AND A PLACE WITH NO INDEX BEHIND IT SAYS SO. Without this line a machine
		// whose store was never wired answered `nothing on this machine says "x"`,
		// which is a search that never happened reporting a result — the one
		// sentence on this page that could make somebody believe a conversation
		// does not exist.
		return searchHung(placeTeachProse(searchNoIndexWord, width, pal))
	}
	if r.query == "" {
		// NOTHING TYPED IS AN EMPTY PLACE, and it says what arrives here and the
		// one thing that puts it there — the heading and the whisper every empty
		// place draws (placeprose.go's [placeWhisper]).
		return placeWhisperLines(pageSearch, width, pal)
	}
	if len(r.hits) == 0 {
		return searchHung(placeTeachProse(searchNothingSaid(r.query), width, pal))
	}
	var out []string
	if legend := r.legend(room, pal); legend != "" {
		out = append(out, " "+legend)
	}
	for _, hit := range r.hits[:r.drawn()] {
		on := lit != nil && lit(len(out))
		out = append(out, " "+searchRowAt(hit, r.query, room, r.now, pal, on))
	}
	if hidden := len(r.hits) - searchShown; hidden > 0 {
		on := lit != nil && lit(len(out))
		out = append(out, " "+placeFactInk(on, pal)(fit(foldDoor(r.unfolded, hidden, ""), room)))
	}
	return out
}

// searchHung puts the body's own one-cell lead on rows that were built without
// one ([placeTeachRows] does the same for the places that go through it).
func searchHung(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, " "+line)
	}
	return out
}

// searchNothingSaid is the whole answer to a search that found nothing: the
// emptiness, and then what to do about it.
//
// IT USED TO BE THE EMPTINESS ALONE — one dim line in a forty-three row body,
// honest and finished. A person cannot tell a typo from a machine that has not
// indexed anything from a phrase that was never said, and an empty place says
// what to do next, in a verb (the law tasks and spend already keep).
func searchNothingSaid(query string) string {
	return fmt.Sprintf("nothing on this machine says %q · try fewer words, or a name", query)
}

// searchNoIndexWord is the page over a surface with no conversation store
// wired: A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, and this is the
// sentence that says which of the two silences this one is.
const searchNoIndexWord = "there is no index of this machine's conversations behind this window, so nothing can be searched from here."

func (r searchReading) legend(width int, pal palette) string {
	parts := make([]string, 0, len(r.facets))
	for _, facet := range r.facets {
		parts = append(parts, fmt.Sprintf("%s %d", facet.word, facet.count))
	}
	return pal.dim(fit(strings.Join(parts, " · "), width))
}

func searchRowAt(hit searchHit, query string, width int, now time.Time, pal palette, lit bool) string {
	// THE LEAD IS AIR, as it is on every row of every place. It was the chat
	// prompt's `›` on every row — the one shape this surface spends on the
	// cursor — over a list whose every row is a conversation anyway.
	lead := searchLead
	facts := placeFactInk(lit, pal)
	age := sinceAt(hit.when, now)
	project := hit.project
	if width < 80 {
		project = ""
	}
	tail := searchTail(project, age, facts)
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
	left := lead + placeSubject(title, lit, pal) + searchMarked(plainMiddle, query, facts, pal)
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

// searchLead is the two cells in front of every result, after the body's one
// ([placeLead]). A result wears no glyph, so its title starts in the column
// every other place's subject starts in — after the mark the other places stand
// on the edge — and a heading or a fold still hangs from the edge itself.
const searchLead = "  "

func searchTail(project, age string, ink func(string) string) string {
	words := make([]string, 0, 2)
	if project != "" {
		words = append(words, project)
	}
	if age != "" {
		words = append(words, age)
	}
	return ink(strings.Join(words, " · "))
}

// searchMarked is a result's quoted turn with the words a person searched for
// picked out. A MATCH IS A DATUM, and it steps up one role the way every datum
// in a quiet line does (THE PAYLOAD RULE) — never bold, which is the band's own
// mark on a place and nowhere else.
func searchMarked(text, query string, rest func(string) string, pal palette) string {
	terms := strings.Fields(query)
	if text == "" || len(terms) == 0 {
		return rest(text)
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
		out.WriteString(rest(text[last:loc[0]]))
		out.WriteString(pal.data(text[loc[0]:loc[1]]))
		last = loc[1]
	}
	out.WriteString(rest(text[last:]))
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
	if at < 0 || at >= r.drawn() {
		return searchHit{}, false
	}
	return r.hits[at], true
}

// SearchStore is the exact durable seam the search place needs: ONE call, which
// is one full-text query over every message on this machine and a left join for
// the thread's name. Keeping it to one method is what keeps the place's promise
// that a search costs one round trip rather than one per row.
type SearchStore interface {
	SearchConversations(terms string, limit int) ([]store.ConversationHit, error)
}

// searchStore is the lowercase spelling this file was written against, kept as
// an alias for [MemoryStore]'s reason: the seam is named in the package's own
// register inside the package, and exported so a door can wire it.
type searchStore = SearchStore

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

// searchTickAccepted keeps an old quiet interval from starting work for text
// that the person has already replaced.
func searchTickAccepted(current searchAsk, msg searchTickMsg) bool {
	return current.gen == msg.gen
}

// searchDoneAccepted keeps a slower old store read from replacing the results
// for the words currently in the box.
func searchDoneAccepted(current searchAsk, msg searchDoneMsg) bool {
	return current.gen == msg.ask.gen
}

func searchDebounce(gen int) tea.Cmd {
	return surfaceTick(searchDebounceEvery, func(time.Time) tea.Msg { return searchTickMsg{gen: gen} })
}
