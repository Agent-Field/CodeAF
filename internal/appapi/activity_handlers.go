package appapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Activity records engine actions. Tab bookkeeping and undo stacks stay in the app.
func (s *Server) searchActivityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/app/v1/agents" && r.URL.Query().Get("q") != "" {
			q := r.URL.Query().Get("q")
			if state := r.URL.Query().Get("state"); state != "" {
				q += " state:" + state
			}
			parsed, err := parseHistoryQuery(q, s.searchNow())
			if err != nil {
				fail(w, 422, "bad_input", err.Error())
				return
			}
			history, err := s.searchHistory(r, parsed)
			if err != nil {
				fail(w, 503, "not_allowed", err.Error())
				return
			}
			write(w, 200, AgentsResponse{Agents: history.Agents})
			return
		}
		object, verb, readPath, ok := tidyAction(r)
		if !ok || s.options.Agents == nil {
			next.ServeHTTP(w, r)
			return
		}
		// Serialize observations with the journal lock across windows and launches.
		// A read-only or full home refuses BEFORE the engine mutation starts.
		file, err := s.activityFile()
		if err != nil {
			fail(w, 503, "not_allowed", "could not open Activity")
			return
		}
		defer file.Close()
		if err = filelock.Lock(file, true, false); err != nil {
			fail(w, 503, "not_allowed", "could not lock Activity")
			return
		}
		defer func() { _ = filelock.Unlock(file) }()
		before := s.tidySnapshot(r, object, readPath)
		reply := &replyWriter{header: make(http.Header)}
		next.ServeHTTP(reply, r)
		if reply.code >= 200 && reply.code < 300 {
			after := s.tidySnapshot(r, object, readPath)
			if before != "" && before != after {
				item := Activity{At: s.searchNow(), Actor: "you", Verb: verb, Object: object, Detail: fmt.Sprintf("%s %s", strings.ToUpper(verb[:1])+verb[1:], object.Kind)}
				raw, _ := json.Marshal(item)
				if _, err = file.Write(append(raw, '\n')); err == nil {
					err = file.Sync()
				}
				if err != nil {
					fail(w, 503, "not_allowed", "the action completed but Activity could not be saved")
					return
				}
				_, _ = s.events.Publish(EventActivity, item)
			}
		}
		sendReply(w, mutationReply{code: reply.code, header: reply.header, body: reply.Bytes()})
	})
}

func tidyAction(r *http.Request) (SourceRef, string, string, bool) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/app/v1/"), "/")
	if len(parts) < 2 {
		return SourceRef{}, "", "", false
	}
	kind := map[string]string{"agents": "agent", "crews": "crew", "rules": "rule", "memory": "memory"}[parts[0]]
	if kind == "" || parts[1] == "" {
		return SourceRef{}, "", "", false
	}
	verb := ""
	if r.Method == "POST" && len(parts) == 3 {
		verbs := map[string]string{"close": "closed", "archive": "archived", "reopen": "reopened", "finish": "finished", "forget": "forgot", "restore": "restored"}
		verb = verbs[parts[2]]
	}
	if r.Method == "DELETE" && kind == "rule" && len(parts) == 2 {
		verb = "removed"
	}
	if r.Method == "PATCH" && kind == "agent" && len(parts) == 2 {
		verb = "changed"
	}
	if verb == "" {
		return SourceRef{}, "", "", false
	}
	readPath := "/app/v1/" + parts[0] + "/" + url.PathEscape(parts[1])
	if kind == "rule" || kind == "memory" {
		readPath = "/app/v1/" + parts[0]
	}
	return SourceRef{Kind: kind, ID: parts[1]}, verb, readPath, true
}
func (s *Server) tidySnapshot(r *http.Request, object SourceRef, path string) string {
	var raw json.RawMessage
	if _, err := s.searchRead(r, path, &raw); err != nil {
		return ""
	}
	switch object.Kind {
	case "agent":
		var a AgentResponse
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		// Only lifecycle and pin transitions are tidy actions; turns are separate events.
		return fmt.Sprintf("%s:%t", a.State, a.Pinned)
	case "crew":
		var c CrewResponse
		if json.Unmarshal(raw, &c) != nil {
			return ""
		}
		return c.State
	case "rule":
		var list RulesResponse
		if json.Unmarshal(raw, &list) != nil {
			return ""
		}
		for _, item := range list.Rules {
			if item.ID == object.ID {
				return "present"
			}
		}
		return "absent"
	case "memory":
		var list MemoryResponse
		if json.Unmarshal(raw, &list) != nil {
			return ""
		}
		for _, item := range list.Items {
			if item.ID == object.ID {
				return "present"
			}
		}
		return "absent"
	}
	return ""
}
func (s *Server) activityFile() (*os.File, error) {
	if s.options.Agents == nil || s.options.Agents.home == "" {
		return nil, errors.New("no engine home")
	}
	dir := filepath.Join(s.options.Agents.home, "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "activity.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
}
func (s *Server) readActivity(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if raw := r.URL.Query().Get("since"); raw != "" {
		var err error
		since, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			since, err = time.Parse("2006-01-02", raw)
		}
		if err != nil {
			fail(w, 422, "bad_input", "since needs an RFC3339 timestamp or UTC date (YYYY-MM-DD)")
			return
		}
	}
	file, err := s.activityFile()
	if err != nil {
		fail(w, 503, "not_allowed", "Activity is unavailable")
		return
	}
	defer file.Close()
	if err = filelock.Lock(file, false, false); err != nil {
		fail(w, 503, "not_allowed", "could not lock Activity")
		return
	}
	defer func() { _ = filelock.Unlock(file) }()
	items := []Activity{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var item Activity
		if json.Unmarshal(bytes.TrimSpace(scanner.Bytes()), &item) != nil {
			fail(w, 503, "not_allowed", "could not read Activity")
			return
		}
		if since.IsZero() || item.At.After(since) {
			items = append(items, item)
		}
	}
	if scanner.Err() != nil {
		fail(w, 503, "not_allowed", "could not read Activity")
		return
	}
	// Reverse insertion order keeps equal timestamps stable and newest actions first.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	write(w, 200, ActivityResponse{Items: items})
}
