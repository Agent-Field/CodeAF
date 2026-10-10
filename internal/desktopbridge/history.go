package desktopbridge

// History: the conversations a person has had, listed, read back and searched.
//
// The bridge's other routes serve ATTACHED conversations, each with an engine
// behind it. History is the opposite case: most of what it lists is closed, so
// it reads the profile's sessions root from disk and attaches nothing. The
// recap beside each row is written by the engine when a turn settles
// (internal/session/recap.go) and is read here from the conversation's
// meta.json; a conversation that has none is still listed, and is still found by
// its title and its words.
//
// SEARCH MAKES NO MODEL CALL. A search runs on every pause in typing, and a
// model call per pause would be a bill nobody agreed to. It ranks what is
// already on disk — titles, recaps and messages — by the words asked for,
// weighted toward recent conversations, and it answers a question directly only
// when a recap SENTENCE that already exists covers the question's terms. An
// answer is never composed here: it is one of the recap's own sentences.

import (
	"encoding/base64"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	historyDefaultLimit = 50
	historyMaxLimit     = 200
	// historyGroupCap is how many hits one search group returns; the counts
	// beside it are totals.
	historyGroupCap = 20
	// historyMessageClip bounds one message's text on the wire.
	historyMessageClip = 20000
	// historyArchiveMax bounds one archive request.
	historyArchiveMax = 500
	// historyCacheBytes bounds the conversation words kept between requests.
	historyCacheBytes = 64 << 20
	// bestCoverage is the share of a question's content terms one recap sentence
	// must cover before it is offered as THE answer.
	bestCoverage = 0.6
	// bestLead is how far the best conversation must outrank the runner-up.
	bestLead = 1.5
)

// History is the door onto the sessions root. The zero value has no root and
// answers every list with nothing.
type History struct {
	// Root is the profile's places root (session.PlacesRoot()).
	Root string

	mu    sync.Mutex
	cache map[string]*historyWords
	bytes int
	// Extra lists conversations the world walk skips. A council chat is an
	// ordinary session that nobody has typed in, kept beside the place graph
	// rather than in a project bucket, so the walk never sees it. Nil adds
	// nothing. UseCouncils sets this.
	Extra func() []session.SessionRow
}

// historyWords is one transcript's words, valid while the file is the size and
// age it was read at.
type historyWords struct {
	size     int64
	modified time.Time
	messages []session.ConversationMessage
	weight   int
}

// UseHistory attaches the conversation history. Without it the routes answer an
// empty list.
func (b *Bridge) UseHistory(history *History) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.history = history
	b.attachCouncilsLocked()
	purgeTrash(time.Now())
}

// ── wire types ──────────────────────────────────────────────────────────────

// HistoryFile is one changed file, as recap and list rows carry it.
type HistoryFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// HistoryItem is one conversation as a list row.
type HistoryItem struct {
	ID           string        `json:"id"`
	SessionFile  string        `json:"sessionFile"`
	Title        string        `json:"title"`
	Line         string        `json:"line,omitempty"`
	At           string        `json:"at"`
	Messages     int           `json:"messages"`
	Tasks        int           `json:"tasks"`
	TasksRunning int           `json:"tasksRunning"`
	Files        []HistoryFile `json:"files"`
	FileCount    int           `json:"fileCount"`
	Decisions    int           `json:"decisions"`
	State        string        `json:"state"`
	Reason       string        `json:"reason,omitempty"`
	Open         bool          `json:"open"`
	Archived     bool          `json:"archived"`
	Workspace    string        `json:"workspace"`
}

// HistoryDecision is one thing a conversation settled.
type HistoryDecision struct {
	Text string `json:"text"`
	By   string `json:"by"`
	How  string `json:"how,omitempty"`
}

// HistoryRecap is a conversation's recap.
type HistoryRecap struct {
	Line      string            `json:"line"`
	Discussed string            `json:"discussed"`
	Decided   []HistoryDecision `json:"decided"`
	Outcome   string            `json:"outcome"`
	Files     []HistoryFile     `json:"files"`
	UpdatedAt string            `json:"updatedAt"`
	Messages  int               `json:"messages"`
}

// HistoryList answers GET /history.
type HistoryList struct {
	Total    int           `json:"total"`
	Matching int           `json:"matching"`
	Items    []HistoryItem `json:"items"`
	Next     string        `json:"next,omitempty"`
}

// HistoryDetail answers GET /history/{id}.
type HistoryDetail struct {
	Item  HistoryItem   `json:"item"`
	Recap *HistoryRecap `json:"recap,omitempty"`
	Stale bool          `json:"stale"`
}

// HistoryMessage is one thing said.
type HistoryMessage struct {
	Index int    `json:"index"`
	Role  string `json:"role"`
	Text  string `json:"text"`
	At    string `json:"at,omitempty"`
}

// HistoryMessages answers GET /history/{id}/messages.
type HistoryMessages struct {
	Total    int              `json:"total"`
	Messages []HistoryMessage `json:"messages"`
}

// HistoryBest is the one conversation that clearly answers a search.
type HistoryBest struct {
	Item         HistoryItem `json:"item"`
	Answer       string      `json:"answer"`
	MessageIndex *int        `json:"messageIndex,omitempty"`
	Terms        []string    `json:"terms"`
}

// DecisionHit, DiscussHit, FileHit and TaskHit are the four search groups.
type DecisionHit struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Context string   `json:"context"`
	At      string   `json:"at"`
	Terms   []string `json:"terms,omitempty"`
}

type DiscussHit struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Snippet      string `json:"snippet"`
	MessageIndex *int   `json:"messageIndex,omitempty"`
	At           string `json:"at"`
}

type FileHit struct {
	Path          string   `json:"path"`
	Conversations int      `json:"conversations"`
	Last          string   `json:"last"`
	IDs           []string `json:"ids"`
}

type TaskHit struct {
	ID                string `json:"id"`
	TaskID            string `json:"taskId"`
	ConversationTitle string `json:"conversationTitle"`
	Title             string `json:"title"`
	Snippet           string `json:"snippet"`
	At                string `json:"at"`
}

// HistoryCounts are the totals behind each search group.
type HistoryCounts struct {
	Decisions int `json:"decisions"`
	Discussed int `json:"discussed"`
	Files     int `json:"files"`
	Tasks     int `json:"tasks"`
}

// HistorySearch answers GET /history/search.
type HistorySearch struct {
	Query     string        `json:"query"`
	Best      *HistoryBest  `json:"best,omitempty"`
	Decisions []DecisionHit `json:"decisions"`
	Discussed []DiscussHit  `json:"discussed"`
	Files     []FileHit     `json:"files"`
	Tasks     []TaskHit     `json:"tasks"`
	Counts    HistoryCounts `json:"counts"`
}

// ── routes ──────────────────────────────────────────────────────────────────

// historyRoutes serves /history[/...]. It reports whether the path was its own.
func (b *Bridge) historyRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if parts[0] != "history" {
		return false
	}
	b.mu.Lock()
	history := b.history
	b.mu.Unlock()
	if history == nil {
		history = &History{}
	}
	attached := b.attachedStates()
	switch {
	case len(parts) == 1:
		if needGet(w, r) {
			write(w, history.list(r, attached))
		}
	case len(parts) == 2 && parts[1] == "search":
		if needGet(w, r) {
			write(w, history.search(r, attached))
		}
	case len(parts) == 2 && parts[1] == "archive":
		if needPost(w, r) {
			history.archive(w, r)
		}
	case len(parts) == 2 && parts[1] == "delete":
		if needPost(w, r) {
			b.deleteConversations(w, r, history)
		}
	case len(parts) == 2 && parts[1] == "restore":
		if needPost(w, r) {
			b.restoreConversations(w, r)
		}
	case len(parts) == 2 && parts[1] == "group-offers":
		if needPost(w, r) {
			b.groupOffers(w, r, history)
		}
	case len(parts) == 2:
		if needGet(w, r) {
			history.detail(w, parts[1], attached)
		}
	case len(parts) == 3 && parts[2] == "messages":
		if needGet(w, r) {
			history.messages(w, r, parts[1])
		}
	default:
		fail(w, 404, "unknown engine action")
	}
	return true
}

// attachedState is what a conversation held by a window says about itself now.
type attachedState struct{ working, needs bool }

// attachedStates reads the live state of every attached conversation, keyed by
// transcript path, so a row can say `working` for a turn that is running in a
// window whose presence file has not been refreshed yet.
func (b *Bridge) attachedStates() map[string]attachedState {
	b.mu.Lock()
	open := make([]*conversation, 0, len(b.sessions))
	for _, s := range b.sessions {
		open = append(open, s)
	}
	b.mu.Unlock()
	states := make(map[string]attachedState, len(open))
	for _, s := range open {
		file := s.conn.Welcome.SessionFile
		if file == "" {
			continue
		}
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		states[filepath.Clean(file)] = attachedState{working: running, needs: s.conn.Agent != nil && s.conn.Agent.NeedsPerson()}
	}
	return states
}

// row is one conversation with everything a route needs of it.
type historyRow struct {
	row    session.SessionRow
	meta   session.Meta
	recap  *session.ConversationRecap
	attach *attachedState
}

// rows reads every conversation under the root, newest first, then any
// council chat the walk skipped. A blank root still lists those, because
// they do not live in a project bucket.
func (h *History) rows(attached map[string]attachedState) []historyRow {
	var out []historyRow
	if strings.TrimSpace(h.Root) != "" {
		for _, row := range session.ReadWorld(h.Root).Sessions() {
			if row.DeletionPending {
				continue
			}
			meta, _ := session.LoadMeta(row.Dir)
			entry := historyRow{row: row, meta: meta, recap: meta.Recap}
			if state, ok := attached[filepath.Clean(row.Transcript)]; ok {
				entry.attach = &state
			}
			out = append(out, entry)
		}
	}
	if h.Extra != nil {
		seen := map[string]bool{}
		for i := range out {
			seen[out[i].row.ID] = true
		}
		for _, row := range h.Extra() {
			if row.ID == "" || seen[row.ID] || row.DeletionPending || strings.TrimSpace(row.Transcript) == "" {
				continue
			}
			seen[row.ID] = true
			meta, _ := session.LoadMeta(row.Dir)
			entry := historyRow{row: row, meta: meta, recap: meta.Recap}
			if state, ok := attached[filepath.Clean(row.Transcript)]; ok {
				entry.attach = &state
			}
			out = append(out, entry)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].row.At.Equal(out[j].row.At) {
			return out[i].row.At.After(out[j].row.At)
		}
		return out[i].row.ID < out[j].row.ID
	})
	return out
}

// words returns a transcript's words, read again only when the file changed.
func (h *History) words(transcript string) *historyWords {
	info, err := os.Stat(transcript)
	if err != nil {
		return &historyWords{}
	}
	h.mu.Lock()
	if cached := h.cache[transcript]; cached != nil && cached.size == info.Size() && cached.modified.Equal(info.ModTime()) {
		h.mu.Unlock()
		return cached
	}
	h.mu.Unlock()
	words := &historyWords{size: info.Size(), modified: info.ModTime(), messages: session.ReadConversation(transcript)}
	for _, message := range words.messages {
		words.weight += len(message.Text)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil || h.bytes+words.weight > historyCacheBytes {
		h.cache, h.bytes = map[string]*historyWords{}, 0
	}
	if old := h.cache[transcript]; old != nil {
		h.bytes -= old.weight
	}
	h.cache[transcript] = words
	h.bytes += words.weight
	return words
}

func (h *History) title(entry historyRow) string {
	for _, message := range h.words(entry.row.Transcript).messages {
		if message.Role == "user" {
			return clipRunes(oneLine(message.Text), 80)
		}
	}
	return "Untitled conversation"
}

// item draws one conversation as a row. Only the full form reads the
// transcript, for the message count and for the title of a conversation nothing
// has named; the lite form is what a filter needs, and a list of two hundred
// conversations must not read two hundred transcripts to show fifty.
func (h *History) item(entry historyRow, full bool) HistoryItem {
	row := entry.row
	item := HistoryItem{
		ID: row.ID, SessionFile: row.Transcript, Title: strings.TrimSpace(row.Title),
		At:    row.At.UTC().Format(time.RFC3339),
		Files: []HistoryFile{}, State: "idle", Open: row.Open || row.Live || entry.attach != nil,
		Archived: row.Archived, Workspace: row.Workspace,
	}
	if full {
		item.Messages = len(h.words(row.Transcript).messages)
		if item.Title == "" {
			item.Title = h.title(entry)
		}
	}
	for _, task := range row.Tasks.Rows {
		if task.Parent != "" {
			continue
		}
		item.Tasks++
		if row.Runs(task) {
			item.TasksRunning++
		}
	}
	if recap := entry.recap; recap != nil {
		item.Line = recap.Line
		item.Decisions = len(recap.Decided)
		item.FileCount = len(recap.Files)
		for i, file := range recap.Files {
			if i == 3 {
				break
			}
			item.Files = append(item.Files, HistoryFile{Path: file.Path, Added: file.Added, Removed: file.Removed})
		}
	}
	switch {
	case row.NeedsPerson() || (entry.attach != nil && entry.attach.needs):
		item.State, item.Reason = "needs-you", row.Reason()
	case item.TasksRunning > 0 || (row.Live && row.Presence.State == session.PresenceWorking) || (entry.attach != nil && entry.attach.working):
		item.State = "working"
	}
	return item
}

// keeps applies one list filter to a conversation.
func keeps(filter string, entry historyRow, item HistoryItem) bool {
	switch filter {
	case "decisions":
		return item.Decisions > 0
	case "files":
		return item.FileCount > 0
	case "tasks":
		return item.Tasks > 0
	case "open":
		return item.Open
	}
	return true
}

func queryLimit(r *http.Request, fallback, ceiling int) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		return fallback
	}
	return min(limit, ceiling)
}

func cursorOf(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + ":" + id))
}

// afterCursor reports that a row comes AFTER the cursor in list order. A cursor
// that does not parse is no cursor: the list starts from the top.
func afterCursor(cursor string, at time.Time, id string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return true
	}
	stamp, rest, ok := strings.Cut(string(raw), ":")
	nanos, err := strconv.ParseInt(stamp, 10, 64)
	if !ok || err != nil {
		return true
	}
	if mark := at.UnixNano(); mark != nanos {
		return mark < nanos
	}
	return id > rest
}

func (h *History) list(r *http.Request, attached map[string]attachedState) HistoryList {
	filter := r.URL.Query().Get("filter")
	cursor := r.URL.Query().Get("before")
	limit := queryLimit(r, historyDefaultLimit, historyMaxLimit)
	list := HistoryList{Items: []HistoryItem{}}
	all := h.rows(attached)
	list.Total = len(all)
	for _, entry := range all {
		if !keeps(filter, entry, h.item(entry, false)) {
			continue
		}
		list.Matching++
		if cursor != "" && !afterCursor(cursor, entry.row.At, entry.row.ID) {
			continue
		}
		if len(list.Items) == limit {
			list.Next = cursorOf(list.Items[limit-1].atTime(), list.Items[limit-1].ID)
			continue
		}
		list.Items = append(list.Items, h.item(entry, true))
	}
	return list
}

// atTime parses the row's own stamp back, so a cursor is made from exactly what
// the list printed.
func (i HistoryItem) atTime() time.Time {
	at, _ := time.Parse(time.RFC3339, i.At)
	return at
}

func (h *History) find(attached map[string]attachedState, id string) (historyRow, bool) {
	for _, entry := range h.rows(attached) {
		if entry.row.ID == id {
			return entry, true
		}
	}
	return historyRow{}, false
}

func recapView(recap *session.ConversationRecap) *HistoryRecap {
	view := &HistoryRecap{
		Line: recap.Line, Discussed: recap.Discussed, Outcome: recap.Outcome,
		Decided: []HistoryDecision{}, Files: []HistoryFile{},
		UpdatedAt: recap.UpdatedAt.UTC().Format(time.RFC3339), Messages: recap.Messages,
	}
	for _, decision := range recap.Decided {
		view.Decided = append(view.Decided, HistoryDecision{Text: decision.Text, By: decision.By, How: decision.How})
	}
	for _, file := range recap.Files {
		view.Files = append(view.Files, HistoryFile{Path: file.Path, Added: file.Added, Removed: file.Removed})
	}
	return view
}

func (h *History) detail(w http.ResponseWriter, id string, attached map[string]attachedState) {
	entry, ok := h.find(attached, id)
	if !ok {
		fail(w, 404, "no such conversation")
		return
	}
	detail := HistoryDetail{Item: h.item(entry, true)}
	if recap := entry.recap; recap != nil {
		detail.Recap = recapView(recap)
		// STALE IS THE CONVERSATION HAVING MOVED SINCE THE RECAP WAS WRITTEN: the
		// person spoke after it, or the newest words are newer than it.
		detail.Stale = entry.row.At.After(recap.UpdatedAt)
		if messages := h.words(entry.row.Transcript).messages; len(messages) > 0 && messages[len(messages)-1].At.After(recap.UpdatedAt) {
			detail.Stale = true
		}
	}
	write(w, detail)
}

// folder finds a conversation's folder by id without reading the world. The id
// is a folder name and nothing else, so a separator or a dot is refused rather
// than followed, and so is a glob character: the lookup is a Glob, and "*"
// would otherwise name whichever conversation happened to match first.
func (h *History) folder(id string) (string, bool) {
	if strings.TrimSpace(h.Root) == "" || id == "" || strings.ContainsAny(id, `/\.*?[]`) {
		return "", false
	}
	matches, _ := filepath.Glob(filepath.Join(h.Root, "*", id))
	for _, match := range matches {
		if isDirectory(match) {
			return match, true
		}
	}
	return "", false
}

func (h *History) messages(w http.ResponseWriter, r *http.Request, id string) {
	dir, ok := h.folder(id)
	if !ok {
		fail(w, 404, "no such conversation")
		return
	}
	all := h.words(filepath.Join(dir, "transcript.jsonl")).messages
	end := len(all)
	if before, err := strconv.Atoi(r.URL.Query().Get("before")); err == nil && before >= 0 && before < end {
		end = before
	}
	start := max(0, end-queryLimit(r, historyDefaultLimit, historyMaxLimit))
	page := HistoryMessages{Total: len(all), Messages: []HistoryMessage{}}
	for index := start; index < end; index++ {
		message := HistoryMessage{Index: index, Role: all[index].Role, Text: clipBytes(all[index].Text, historyMessageClip)}
		if !all[index].At.IsZero() {
			message.At = all[index].At.UTC().Format(time.RFC3339)
		}
		page.Messages = append(page.Messages, message)
	}
	write(w, page)
}

func (h *History) archive(w http.ResponseWriter, r *http.Request) {
	var ask struct {
		IDs      []string `json:"ids"`
		Archived bool     `json:"archived"`
	}
	if !decode(w, r, &ask) {
		return
	}
	if len(ask.IDs) > historyArchiveMax {
		fail(w, 400, "too many conversations in one request")
		return
	}
	changed := 0
	for _, id := range ask.IDs {
		dir, ok := h.folder(id)
		if !ok {
			continue
		}
		before, _ := session.LoadMeta(dir)
		if before.ID == "" || before.Archived == ask.Archived {
			continue
		}
		if err := session.SetArchived(dir, ask.Archived); err == nil {
			changed++
		}
	}
	write(w, map[string]int{"changed": changed})
}

// ── search ──────────────────────────────────────────────────────────────────

var stopWords = func() map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(`a an and are as at be but by did do does for from had has have how i if in into is it its
		me my of on or our so than that the their then there these they this to was we were what when where which who why will with would you your`) {
		set[word] = true
	}
	return set
}()

// terms are the content words of a text: lower-case, split on anything that is
// not a letter or digit, stop words and single characters dropped, repeats
// removed in order.
func terms(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, word := range words(text) {
		if len(word) < 2 || stopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	return out
}

func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// covers reports that a text's words include the term, exactly or as the front
// of a longer word, so `lex` finds `lexer` and a plural finds its singular.
func covers(tokens []string, term string) bool {
	for _, token := range tokens {
		if token == term || (len(term) >= 3 && strings.HasPrefix(token, term)) || (len(token) >= 3 && strings.HasPrefix(term, token) && len(term)-len(token) <= 2) {
			return true
		}
	}
	return false
}

// coverage is the share of the query's terms a text contains.
func coverage(query []string, text string) (float64, []string) {
	tokens := words(text)
	var hit []string
	for _, term := range query {
		if covers(tokens, term) {
			hit = append(hit, term)
		}
	}
	if len(query) == 0 {
		return 0, nil
	}
	return float64(len(hit)) / float64(len(query)), hit
}

// recency weights a conversation by age: a day-old conversation counts for
// about 1.5 of a stale one's 0.5.
func recency(now, at time.Time) float64 {
	days := math.Max(0, now.Sub(at).Hours()/24)
	return 0.5 + 1/(1+days/45)
}

type scored struct {
	entry   historyRow
	item    HistoryItem
	score   float64
	message int // index of the best matching message, -1 when none
	snippet string
	// messageShare is the share of the query the best message covers.
	messageShare  float64
	sentence      string
	sentenceCover float64
}

// sentences splits a recap paragraph into the sentences an answer may be.
func sentences(text string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == '.' || r == '!' || r == '?' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part+".")
		}
	}
	return out
}

func (h *History) search(r *http.Request, attached map[string]attachedState) HistorySearch {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	filter := r.URL.Query().Get("filter")
	result := HistorySearch{Query: q, Decisions: []DecisionHit{}, Discussed: []DiscussHit{}, Files: []FileHit{}, Tasks: []TaskHit{}}
	query := terms(q)
	if len(query) == 0 {
		return result
	}
	now := time.Now()
	var hits []*scored
	fileHits := map[string]*FileHit{}
	var fileOrder []string
	var decisionHits []DecisionHit
	var decisionRank []float64
	var taskHits []TaskHit
	for _, entry := range h.rows(attached) {
		item := h.item(entry, true)
		if !keeps(filter, entry, item) {
			continue
		}
		weight := recency(now, entry.row.At)
		hit := &scored{entry: entry, item: item, message: -1}
		// The title and the recap are the conversation's own account of itself, so
		// a term found there counts for more than the same term somewhere in a
		// long message.
		matched := map[string]bool{}
		note := func(text string, value float64) {
			_, found := coverage(query, text)
			for _, term := range found {
				matched[term] = true
				hit.score += value
			}
		}
		note(item.Title, 3)
		if recap := entry.recap; recap != nil {
			note(recap.Line, 3)
			note(recap.Outcome, 2)
			note(recap.Discussed, 2)
			for _, decision := range recap.Decided {
				note(decision.Text, 3)
				if share, found := coverage(query, decision.Text); len(found) > 0 {
					decisionHits = append(decisionHits, DecisionHit{ID: item.ID, Title: decision.Text, Context: decisionContext(item.Title, decision), At: item.At, Terms: found})
					decisionRank = append(decisionRank, share*weight)
				}
			}
			hit.sentence, hit.sentenceCover = bestSentence(query, recap)
			for _, file := range recap.Files {
				if path := strings.ToLower(file.Path); matchesPath(q, query, path) {
					fileHit := fileHits[file.Path]
					if fileHit == nil {
						fileHit = &FileHit{Path: file.Path, Last: item.At}
						fileHits[file.Path] = fileHit
						fileOrder = append(fileOrder, file.Path)
					}
					fileHit.Conversations++
					if len(fileHit.IDs) < 5 {
						fileHit.IDs = append(fileHit.IDs, item.ID)
					}
					if item.At > fileHit.Last {
						fileHit.Last = item.At
					}
				}
			}
		}
		for _, task := range entry.row.Tasks.Rows {
			if share, _ := coverage(query, task.Title+" "+task.Outcome); share > 0 {
				snippet := strings.TrimSpace(task.Outcome)
				if snippet == "" {
					snippet = task.Title
				}
				taskHits = append(taskHits, TaskHit{ID: item.ID, TaskID: task.ID, ConversationTitle: item.Title, Title: firstNonEmpty(task.Title, task.Label), Snippet: clipRunes(oneLine(snippet), 200), At: item.At})
			}
		}
		for index, message := range h.words(entry.row.Transcript).messages {
			share, found := coverage(query, message.Text)
			if len(found) == 0 {
				continue
			}
			for _, term := range found {
				matched[term] = true
			}
			if hit.message < 0 || share > hit.messageShare {
				hit.message, hit.snippet, hit.messageShare = index, snippetAround(message.Text, found[0]), share
			}
		}
		hit.score += float64(len(matched))
		hit.score += hit.messageShare
		if len(matched) == 0 {
			continue
		}
		hit.score *= weight
		hits = append(hits, hit)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	result.Best = best(query, hits)
	for _, hit := range hits {
		discuss := DiscussHit{ID: hit.item.ID, Title: hit.item.Title, At: hit.item.At}
		switch {
		case hit.message >= 0:
			index := hit.message
			discuss.Snippet, discuss.MessageIndex = hit.snippet, &index
		case hit.entry.recap != nil && hit.entry.recap.Discussed != "":
			discuss.Snippet = clipRunes(hit.entry.recap.Discussed, 200)
		default:
			continue
		}
		result.Counts.Discussed++
		if len(result.Discussed) < historyGroupCap {
			result.Discussed = append(result.Discussed, discuss)
		}
	}
	order := make([]int, len(decisionHits))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return decisionRank[order[a]] > decisionRank[order[b]] })
	result.Counts.Decisions = len(decisionHits)
	for _, i := range order {
		if len(result.Decisions) < historyGroupCap {
			result.Decisions = append(result.Decisions, decisionHits[i])
		}
	}
	sort.SliceStable(fileOrder, func(a, b int) bool {
		return fileHits[fileOrder[a]].Conversations > fileHits[fileOrder[b]].Conversations
	})
	result.Counts.Files = len(fileOrder)
	for _, path := range fileOrder {
		if len(result.Files) < historyGroupCap {
			result.Files = append(result.Files, *fileHits[path])
		}
	}
	result.Counts.Tasks = len(taskHits)
	result.Tasks = append(result.Tasks, taskHits[:min(len(taskHits), historyGroupCap)]...)
	return result
}

func decisionContext(title string, decision session.RecapDecision) string {
	who := strings.TrimSpace(decision.By + " " + decision.How)
	if who == "" {
		return title
	}
	return title + " · " + who
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// matchesPath reports a changed file the query is asking about. A query that
// looks like a path (it has a dot or a slash in it) is a substring of the path;
// anything else asks for files whose path words include every term.
func matchesPath(raw string, query []string, path string) bool {
	if strings.ContainsAny(raw, "./") {
		return strings.Contains(path, strings.ToLower(raw))
	}
	tokens := words(path)
	for _, term := range query {
		if !covers(tokens, term) {
			return false
		}
	}
	return true
}

// bestSentence is the recap sentence that covers the most query terms. The
// candidates are the recap's own sentences and decisions, verbatim.
func bestSentence(query []string, recap *session.ConversationRecap) (string, float64) {
	var candidates []string
	candidates = append(candidates, recap.Line, recap.Outcome)
	candidates = append(candidates, sentences(recap.Discussed)...)
	for _, decision := range recap.Decided {
		candidates = append(candidates, decision.Text)
	}
	bestText, bestShare := "", 0.0
	for _, candidate := range candidates {
		if share, _ := coverage(query, candidate); strings.TrimSpace(candidate) != "" && share > bestShare {
			bestText, bestShare = candidate, share
		}
	}
	return bestText, bestShare
}

// best offers one conversation as THE answer when it clearly is: a recap
// sentence covers enough of the question, and no other conversation is close.
func best(query []string, hits []*scored) *HistoryBest {
	var pick *scored
	for _, hit := range hits {
		if hit.sentence != "" && hit.sentenceCover >= bestCoverage {
			pick = hit
			break
		}
	}
	if pick == nil {
		return nil
	}
	for _, hit := range hits {
		if hit != pick && pick.score < hit.score*bestLead {
			return nil
		}
	}
	answer := &HistoryBest{Item: pick.item, Answer: pick.sentence, Terms: query}
	if pick.message >= 0 {
		index := pick.message
		answer.MessageIndex = &index
	}
	return answer
}

// snippetAround is the sentence-sized stretch of a message around a term.
func snippetAround(text, term string) string {
	text = oneLine(text)
	at := strings.Index(strings.ToLower(text), term)
	if at < 0 {
		return clipRunes(text, 200)
	}
	start := max(0, at-80)
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	snippet := clipRunes(text[start:], 200)
	if start > 0 {
		snippet = "…" + snippet
	}
	return snippet
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// clipRunes cuts to n runes, marking the cut.
func clipRunes(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n-1]) + "…"
}

// clipBytes cuts to n bytes on a rune boundary.
func clipBytes(text string, n int) string {
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
