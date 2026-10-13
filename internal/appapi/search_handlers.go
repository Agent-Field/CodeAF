package appapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// These routes read existing resources; searching never starts an engine or calls AI.
var searchContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/search", Response: "SearchResponse"}, handle: (*Server).searchHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/history", Response: "HistoryResponse"}, handle: (*Server).searchHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/activity", Response: "ActivityResponse"}, handle: (*Server).searchHandler},
}

// Resource reads preserve the caller's headers and context, including factory access.
// They use the mounted handlers rather than copying another lane's storage policy.
func (s *Server) searchRead(r *http.Request, path string, value any) (int, error) {
	read := r.Clone(r.Context())
	parsed, err := url.Parse(path)
	if err != nil {
		return 0, err
	}
	read.Method, read.URL, read.Body, read.ContentLength = "GET", parsed, nil, 0
	read.RequestURI, read.Pattern = path, ""
	read.SetPathValue("id", "")
	read.SetPathValue("cid", "")
	captured := &replyWriter{header: make(http.Header)}
	s.mux.ServeHTTP(captured, read)
	if captured.code != 200 {
		return captured.code, fmt.Errorf("resource read failed (%d)", captured.code)
	}
	return 200, json.Unmarshal(captured.Bytes(), value)
}

func (s *Server) searchNow() time.Time {
	if a := s.options.Agents; a != nil {
		return a.now().UTC()
	}
	return time.Now().UTC()
}

func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/app/v1/activity" {
		s.readActivity(w, r)
		return
	}
	query, err := parseHistoryQuery(r.URL.Query().Get("q"), s.searchNow())
	if err != nil {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	kinds, err := searchKinds(r.URL.Query().Get("kinds"))
	if err != nil {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	includeClosed := false
	if v := r.URL.Query().Get("include_closed"); v != "" {
		if v != "true" && v != "false" {
			fail(w, 422, "bad_input", "include_closed needs true or false")
			return
		}
		includeClosed = v == "true"
	}
	agents, crews, err := s.searchResources(r)
	if err != nil {
		fail(w, 503, "not_allowed", err.Error())
		return
	}
	history, err := s.filterSearchHistory(query, agents, crews)
	if err != nil {
		fail(w, 503, "not_allowed", err.Error())
		return
	}
	if r.URL.Path == "/app/v1/history" {
		write(w, 200, history)
		return
	}
	result := SearchResponse{Groups: []SearchGroup{}}
	if kinds["agents_crews"] {
		rows := []SearchRow{}
		for _, a := range history.Agents {
			if !kinds["agent"] || (!includeClosed && searchClosed(a.State)) {
				continue
			}
			at := a.CreatedAt
			if a.LastMessageAt != nil {
				at = *a.LastMessageAt
			}
			rows = append(rows, SearchRow{Kind: "agent", ID: a.ID, Title: a.Name, State: a.State, Source: &SourceRef{Kind: "agent", ID: a.ID}, LastUsedAt: &at})
		}
		for _, c := range history.Crews {
			if !kinds["crew"] || (!includeClosed && searchClosed(c.State)) {
				continue
			}
			rows = append(rows, SearchRow{Kind: "crew", ID: c.ID, Title: c.Name, State: c.State, Source: &SourceRef{Kind: "crew", ID: c.ID}, LastUsedAt: crewSearchTime(c, agents.Agents)})
		}
		result.Groups = append(result.Groups, rankedSearchGroup("agents_crews", rows, query.text()))
	}
	if kinds["jobs"] {
		rows := []SearchRow{}
		var queue factoryapi.Queue
		status, err := s.searchRead(r, "/v1/queue", &queue)
		if err != nil && status != 404 && status != 501 {
			fail(w, 503, "not_allowed", "the solo factory search is unavailable")
			return
		}
		for _, j := range queue.Jobs {
			at := j.CreatedAt
			if j.DoneAt.After(at) {
				at = j.DoneAt
			}
			var spent *Money
			if j.Cost > 0 {
				spent = &Money{USD: fmt.Sprintf("%.2f", j.Cost)}
			}
			if !query.match(historyItem{kind: "job", id: j.JobID, name: j.Title, state: j.State, at: at, spent: spent}) {
				continue
			}
			rows = append(rows, SearchRow{Kind: "job", ID: j.JobID, Title: j.Title, State: j.State, Source: &SourceRef{Kind: "job", ID: j.JobID, Factory: queue.Factory.Address}, LastUsedAt: &at})
		}
		result.Groups = append(result.Groups, rankedSearchGroup("jobs", rows, query.text()))
	}
	if kinds["files"] {
		rows, seen := []SearchRow{}, map[string]bool{}
		// File finding stays behind the owning resource's safe workspace interface.
		// Context chips are useful even while the file lane is still reserved.
		for _, a := range agents.Agents {
			if !includeClosed && searchClosed(a.State) {
				continue
			}
			files := []FileStat{}
			for _, chip := range a.Context {
				files = append(files, FileStat{Path: chip.Path, Kind: chip.Kind, Exists: chip.Exists})
			}
			var found FileFindResponse
			status, err := s.searchRead(r, "/app/v1/agents/"+url.PathEscape(a.ID)+"/files/find?q="+url.QueryEscape(query.text()), &found)
			if err != nil && status != 501 {
				fail(w, 503, "not_allowed", "file search is unavailable")
				return
			}
			files = append(files, found.Files...)
			for _, file := range files {
				if !file.Exists || seen[file.Path] || !query.match(historyItem{kind: "file", name: file.Path, at: agentSearchTime(a)}) {
					continue
				}
				seen[file.Path] = true
				rows = append(rows, SearchRow{Kind: "file", ID: file.Path, Title: filepath.Base(file.Path), Path: file.Path, Source: &SourceRef{Kind: "agent", ID: a.ID}, LastUsedAt: a.LastMessageAt})
			}
		}
		result.Groups = append(result.Groups, rankedSearchGroup("files", rows, query.text()))
	}
	write(w, 200, result)
}

func searchClosed(state string) bool { return state == "closed" || state == "archived" }
func agentSearchTime(a Agent) time.Time {
	if a.LastMessageAt != nil {
		return *a.LastMessageAt
	}
	return a.CreatedAt
}
func crewSearchTime(c Crew, agents []Agent) *time.Time {
	var at time.Time
	for _, a := range agents {
		if (a.CrewID == c.ID || a.ID == c.ManagerID) && agentSearchTime(a).After(at) {
			at = agentSearchTime(a)
		}
	}
	if c.ClosedAt != nil && c.ClosedAt.After(at) {
		at = *c.ClosedAt
	}
	if c.ArchivedAt != nil && c.ArchivedAt.After(at) {
		at = *c.ArchivedAt
	}
	if at.IsZero() {
		return nil
	}
	return &at
}
func searchKinds(raw string) (map[string]bool, error) {
	kinds := map[string]bool{}
	if raw == "" {
		raw = "agents_crews,jobs,files"
	}
	for _, kind := range strings.Split(raw, ",") {
		switch kind {
		case "agents_crews":
			kinds["agents_crews"], kinds["agent"], kinds["crew"] = true, true, true
		case "agents", "agent":
			kinds["agents_crews"], kinds["agent"] = true, true
		case "crews", "crew":
			kinds["agents_crews"], kinds["crew"] = true, true
		case "jobs", "job":
			kinds["jobs"] = true
		case "files", "file":
			kinds["files"] = true
		default:
			return nil, fmt.Errorf("unknown search kind: %s", kind)
		}
	}
	return kinds, nil
}
func rankedSearchGroup(kind string, rows []SearchRow, query string) SearchGroup {
	sort.Slice(rows, func(i, j int) bool {
		x, y := searchRank(rows[i].Title, query), searchRank(rows[j].Title, query)
		if x != y {
			return x < y
		}
		a, b := rows[i].LastUsedAt, rows[j].LastUsedAt
		if a != nil && b != nil && !a.Equal(*b) {
			return a.After(*b)
		}
		if (a == nil) != (b == nil) {
			return a != nil
		}
		return rows[i].Kind+":"+rows[i].ID < rows[j].Kind+":"+rows[j].ID
	})
	if len(rows) > 4 {
		rows = rows[:4]
	}
	return SearchGroup{Kind: kind, Rows: rows}
}
func searchRank(title, query string) int {
	title, query = strings.ToLower(title), strings.ToLower(query)
	if strings.HasPrefix(title, query) {
		return 0
	}
	for _, word := range strings.FieldsFunc(title, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if strings.HasPrefix(word, query) {
			return 1
		}
	}
	return 2
}

func (s *Server) searchResources(r *http.Request) (AgentsResponse, CrewsResponse, error) {
	var agents AgentsResponse
	var crews CrewsResponse
	if _, err := s.searchRead(r, "/app/v1/agents", &agents); err != nil {
		return agents, crews, fmt.Errorf("agent history is unavailable")
	}
	if _, err := s.searchRead(r, "/app/v1/crews", &crews); err != nil {
		return agents, crews, fmt.Errorf("crew history is unavailable")
	}
	return agents, crews, nil
}
func (s *Server) searchHistory(r *http.Request, q historyQuery) (HistoryResponse, error) {
	agents, crews, err := s.searchResources(r)
	if err != nil {
		return HistoryResponse{}, err
	}
	return s.filterSearchHistory(q, agents, crews)
}
func (s *Server) filterSearchHistory(q historyQuery, agents AgentsResponse, crews CrewsResponse) (HistoryResponse, error) {
	result := HistoryResponse{Agents: []Agent{}, Crews: []Crew{}}
	names := map[string]string{}
	for _, c := range crews.Crews {
		names[c.ID] = c.Name
	}
	words := map[string]string{}
	spent := map[string]*Money{}
	needsSavedSpend := q.needsCost()
	for _, a := range agents.Agents {
		if searchClosed(a.State) && a.Spent == nil {
			needsSavedSpend = true
		}
	}
	if q.needsSaid() || needsSavedSpend {
		registry := s.options.Agents
		if registry == nil {
			return result, fmt.Errorf("agent history is unavailable")
		}
		registry.mu.Lock()
		err := registry.disk(func(d *agentDisk) error {
			for _, a := range agents.Agents {
				if rec, ok := d.Records[a.ID]; ok {
					if q.needsSaid() {
						for _, entry := range registry.turnBaseEntries(rec) {
							if entry.Kind == "message" && (entry.Role == "user" || entry.Role == "assistant") {
								words[a.ID] += "\n" + entry.Text
							}
						}
					}
					if needsSavedSpend {
						meta, err := session.LoadMeta(filepath.Dir(rec.SessionFile))
						if err == nil && meta.SpentUSD > 0 {
							spent[a.ID] = &Money{USD: fmt.Sprintf("%.2f", meta.SpentUSD)}
						}
					}
				}
			}
			return nil
		}, false)
		registry.mu.Unlock()
		if err != nil {
			return result, fmt.Errorf("transcript search is unavailable")
		}
	}
	for _, a := range agents.Agents {
		if a.Spent == nil {
			a.Spent = spent[a.ID]
		}
		item := historyItem{kind: "agent", id: a.ID, name: a.Name, state: a.State, model: a.Model, crew: a.CrewID, crewName: names[a.CrewID], pinned: a.Pinned, at: agentSearchTime(a), spent: a.Spent, said: words[a.ID]}
		if q.match(item) {
			result.Agents = append(result.Agents, a)
		}
	}
	for _, c := range crews.Crews {
		item := historyItem{kind: "crew", id: c.ID, name: c.Name + " " + c.Goal, state: c.State, crew: c.ID, crewName: c.Name, spent: c.Spent}
		if at := crewSearchTime(c, agents.Agents); at != nil {
			item.at = *at
		}
		for _, a := range agents.Agents {
			if a.ID == c.ManagerID {
				item.model = a.Model
			}
			if a.CrewID == c.ID || a.ID == c.ManagerID {
				item.said += "\n" + words[a.ID]
			}
		}
		if c.Report != nil {
			item.said += "\n" + c.Report.Markdown
		}
		if q.match(item) {
			result.Crews = append(result.Crews, c)
		}
	}
	sortAgents(result.Agents)
	sort.Slice(result.Crews, func(i, j int) bool {
		a, b := crewSearchTime(result.Crews[i], agents.Agents), crewSearchTime(result.Crews[j], agents.Agents)
		if a != nil && b != nil && !a.Equal(*b) {
			return a.After(*b)
		}
		if (a == nil) != (b == nil) {
			return a != nil
		}
		return result.Crews[i].ID < result.Crews[j].ID
	})
	return result, nil
}
