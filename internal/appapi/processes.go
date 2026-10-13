package appapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A process number is local to an agent. The opaque public handle carries both
// identities so Stop can never kill another agent's process with the same number.
func processID(agent string, id int) string {
	return "p_" + base64.RawURLEncoding.EncodeToString([]byte(agent)) + "_" + strconv.Itoa(id)
}
func splitProcessID(id string) (string, int, error) {
	rest, ok := strings.CutPrefix(id, "p_")
	if !ok {
		return "", 0, errAgentMissing
	}
	pos := strings.LastIndexByte(rest, '_')
	if pos < 1 {
		return "", 0, errAgentMissing
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest[:pos])
	if err != nil || len(raw) == 0 {
		return "", 0, errAgentMissing
	}
	n, err := strconv.Atoi(rest[pos+1:])
	if err != nil || n < 1 || processID(string(raw), n) != id {
		return "", 0, errAgentMissing
	}
	return string(raw), n, nil
}

type processRow struct {
	Process Process
	Notice  session.JobNotice
	File    string
}
type processListDoor interface {
	JobNotices() ([]session.JobNotice, error)
}
type processRerunDoor interface {
	RerunProcess(int) (session.JobNotice, error)
}
type processStopDoor interface{ Cancel(string) (string, error) }

func processOf(agent string, n session.JobNotice) Process {
	p := Process{ID: processID(agent, n.ID), AgentID: agent, Name: n.Label(), Command: n.Command, URL: n.URL, State: string(n.State), StartedAt: n.Started}
	if n.Over() {
		at := n.Started.Add(n.Elapsed)
		if !n.Started.IsZero() {
			p.EndedAt = &at
		}
		if n.State == session.JobDone || n.State == session.JobFailed {
			code := n.ExitCode
			p.ExitCode = &code
		}
	}
	return p
}
func processState(s string) bool {
	return s == "running" || s == "done" || s == "failed" || s == "stopped"
}
func matchProcess(p Process, query string) (bool, error) {
	match := true
	for _, token := range strings.Fields(strings.ToLower(query)) {
		key, value, field := strings.Cut(token, ":")
		if !field {
			match = match && strings.Contains(strings.ToLower(p.Name+" "+p.Command), token)
			continue
		}
		switch key {
		case "state":
			if !processState(value) && value != "ended" {
				return false, errors.New("unknown process state")
			}
			match = match && (p.State == value || value == "ended" && p.State != "running")
		case "kind":
			match = match && value == "process"
		case "agent":
			match = match && strings.ToLower(p.AgentID) == value
		case "since":
			at, err := time.Parse(time.RFC3339, strings.ToUpper(value))
			if err != nil {
				at, err = time.Parse("2006-01-02", value)
			}
			if err != nil {
				return false, errors.New("since needs a date or timestamp")
			}
			match = match && !p.StartedAt.Before(at)
		default:
			return false, fmt.Errorf("unknown process filter: %s", key)
		}
	}
	return match, nil
}

func (a *AgentRegistry) processes(ctx context.Context, agent string) ([]processRow, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return nil, agentError(503, "not_allowed", "the engine is stopping")
	}
	rows := []processRow{}
	err := a.disk(func(d *agentDisk) error {
		if agent != "" {
			if _, ok := d.Records[agent]; !ok {
				return errAgentMissing
			}
		}
		for id, record := range d.Records {
			if agent != "" && id != agent {
				continue
			}
			notices, err := session.ReadProcessNotices(record.SessionFile)
			if live := a.live[id]; live != nil {
				if door, ok := live.conn.Engine.(processListDoor); ok {
					notices, err = door.JobNotices()
				}
			} else {
				for i := range notices {
					if notices[i].State == session.JobRunning {
						notices[i].State = session.JobStopped
					}
				}
			}
			if err != nil {
				return err
			}
			if err := session.SweepProcessOutput(record.SessionFile, a.now()); err != nil {
				return err
			}
			for _, n := range notices {
				if n.Kind == session.JobKindTask {
					continue
				}
				p := processOf(id, n)
				if output, err := readProcessOutput(processRow{Process: p, Notice: n, File: record.SessionFile}, processOutputReadBytes, a.now()); err == nil {
					if p.URL == "" {
						p.URL = processURL.FindString(output.Text)
					}
				}
				if file, err := openProcessLog(record.SessionFile, n.LogPath); err == nil {
					info, err := file.Stat()
					file.Close()
					if err == nil {
						p.OutputBytes = info.Size()
					}
				}
				rows = append(rows, processRow{p, n, record.SessionFile})
			}
		}
		return nil
	}, false)
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].Process.StartedAt.Equal(rows[j].Process.StartedAt) {
			return rows[i].Process.StartedAt.After(rows[j].Process.StartedAt)
		}
		return rows[i].Process.ID < rows[j].Process.ID
	})
	return rows, err
}

func (s *Server) findProcess(ctx context.Context, id string) (processRow, error) {
	agent, n, err := splitProcessID(id)
	if err != nil {
		return processRow{}, agentError(404, "not_found", "no such process")
	}
	rows, err := s.options.Agents.processes(ctx, agent)
	if err != nil {
		return processRow{}, err
	}
	for _, row := range rows {
		if row.Notice.ID == n {
			return row, nil
		}
	}
	return processRow{}, agentError(404, "not_found", "no such process")
}

func decodeProcessAction(w http.ResponseWriter, r *http.Request) (*ExpectRequest, bool) {
	var body *ExpectRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body == nil {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return nil, false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return nil, false
	}
	return body, true
}
func checkProcessExpect(expect *AgentExpect, state string, version uint64) error {
	if expect != nil && (expect.State != nil && *expect.State != state || expect.Version != nil && *expect.Version != version) {
		return agentError(409, "stale", "the process or agent changed; read it again before acting")
	}
	return nil
}
func (s *Server) mutateProcess(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeProcessAction(w, r)
	if !ok {
		return
	}
	a := s.options.Agents
	if strings.HasSuffix(r.URL.Path, "/stop-all") {
		err := a.stopProcesses(r.Context(), r.PathValue("id"), body.Expect)
		if err != nil {
			agentFailure(w, err)
			return
		}
		write(w, 200, ActionResponse{OK: true})
		return
	}
	row, err := s.findProcess(r.Context(), r.PathValue("pid"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	err = a.disk(func(d *agentDisk) error {
		record, ok := d.Records[row.Process.AgentID]
		if !ok {
			return errAgentMissing
		}
		if a.stopped {
			return agentError(503, "not_allowed", "the engine is stopping")
		}
		if record.Agent.State == "closed" || record.Agent.State == "archived" {
			return agentError(409, "conflict", "reopen this agent before acting on its processes")
		}
		live, err := a.connect(r.Context(), record)
		if err != nil {
			return err
		}
		door, ok := live.conn.Engine.(processListDoor)
		if !ok {
			return agentError(409, "not_allowed", "this engine cannot list processes")
		}
		notices, err := door.JobNotices()
		if err != nil {
			return err
		}
		found := false
		for _, n := range notices {
			if n.ID == row.Notice.ID {
				row.Notice = n
				row.Process = processOf(record.Agent.ID, n)
				found = true
				break
			}
		}
		if !found {
			return agentError(404, "not_found", "no such process")
		}
		if err := checkProcessExpect(body.Expect, row.Process.State, record.Agent.Version); err != nil {
			return err
		}
		if strings.HasSuffix(r.URL.Path, "/rerun") {
			rerun, ok := live.conn.Engine.(processRerunDoor)
			if !ok {
				return agentError(409, "not_allowed", "this engine cannot rerun processes")
			}
			if row.Notice.Kind != session.JobKindCommand || strings.TrimSpace(row.Notice.Command) == "" {
				return agentError(409, "conflict", "only ended shell commands with a saved command can be run again")
			}
			if row.Process.State == "running" {
				return agentError(409, "conflict", "stop this process before running it again")
			}
			_, err = rerun.RerunProcess(row.Notice.ID)
			if err == nil {
				a.syncProcessEventsLocked(record.Agent.ID, live.conn)
			}
			return err
		}
		if row.Process.State != "running" {
			return nil
		}
		stop, ok := live.conn.Engine.(processStopDoor)
		if !ok {
			return agentError(409, "not_allowed", "this engine cannot stop processes")
		}
		_, err = stop.Cancel("job:" + strconv.Itoa(row.Notice.ID))
		if err == nil {
			a.syncProcessEventsLocked(record.Agent.ID, live.conn)
		}
		return err
	}, false)
	if err != nil {
		agentFailure(w, err)
		return
	}
	write(w, 200, ActionResponse{OK: true})
}

func (a *AgentRegistry) stopProcesses(ctx context.Context, id string, expect *AgentExpect) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return agentError(503, "not_allowed", "the engine is stopping")
	}
	return a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		if err := checkProcessExpect(expect, a.view(record).State, record.Agent.Version); err != nil {
			return err
		}
		live, err := a.connect(ctx, record)
		if err != nil {
			return err
		}
		if err := stopEngineProcesses(live.conn.Engine); err != nil {
			return err
		}
		a.syncProcessEventsLocked(id, live.conn)
		return nil
	}, false)
}
func stopEngineProcesses(engine AgentEngine) error {
	list, ok := engine.(processListDoor)
	if !ok {
		return nil
	}
	notices, err := list.JobNotices()
	if err != nil {
		return err
	}
	stop, ok := engine.(processStopDoor)
	for _, n := range notices {
		if n.State != session.JobRunning || n.Kind == session.JobKindTask {
			continue
		}
		if !ok {
			return agentError(409, "not_allowed", "this engine cannot stop processes")
		}
		if _, err := stop.Cancel("job:" + strconv.Itoa(n.ID)); err != nil {
			return err
		}
	}
	return nil
}

// openProcessLog confines the engine-listed path to the conversation, including
// symlinks. The query never supplies a path and cannot select another file.
func openProcessLog(sessionFile, path string) (*os.File, error) {
	if sessionFile == "" || path == "" {
		return nil, os.ErrNotExist
	}
	root := filepath.Dir(sessionFile)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, errors.New("that output is outside this agent")
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	file, err := handle.Open(rel)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("process output must be a regular file")
	}
	return file, nil
}

// Refreshing one live output stream does not walk the world or sweep its logs.
// The engine owns the terminal state; the reader only needs this one shelf.
func (s *Server) refreshProcess(row processRow) (processRow, error) {
	a := s.options.Agents
	a.mu.Lock()
	live := a.live[row.Process.AgentID]
	stopped := a.stopped
	a.mu.Unlock()
	if stopped {
		return row, errors.New("the engine is stopping")
	}
	var notices []session.JobNotice
	var err error
	if live != nil {
		if door, ok := live.conn.Engine.(processListDoor); ok {
			notices, err = door.JobNotices()
		}
	} else {
		notices, err = session.ReadProcessNotices(row.File)
	}
	if err != nil {
		return row, err
	}
	for _, n := range notices {
		if n.ID == row.Notice.ID {
			if live == nil && n.State == session.JobRunning {
				n.State = session.JobStopped
			}
			row.Notice = n
			row.Process = processOf(row.Process.AgentID, n)
			return row, nil
		}
	}
	return row, errors.New("no such process")
}

// StopAllProcesses is the power-event door for the factory lane. Agents and
// tasks stay attached; only their running processes stop when the machine sleeps.
func (a *AgentRegistry) StopAllProcesses() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, live := range a.live {
		if err := stopEngineProcesses(live.conn.Engine); err != nil {
			return err
		}
		a.syncProcessEventsLocked(id, live.conn)
	}
	return nil
}
