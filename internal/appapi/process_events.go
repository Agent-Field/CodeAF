package appapi

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type processObserver struct {
	mu   sync.Mutex
	seen map[int]session.JobNotice
}

func (p *processObserver) publish(hub *EventHub, id string, rows []session.JobNotice) {
	p.mu.Lock()
	defer p.mu.Unlock()
	publishProcessChanges(hub, id, p.seen, rows)
}

// A process shelf outlives turns. Its own bounded poll keeps it independent of
// the task subscription, which the turn lane already owns on a remote client.
// Closing the connection releases the timer on every lifecycle road out.
func (a *AgentRegistry) watchProcesses(id string, conn *AgentConnection) {
	door, ok := conn.Engine.(processListDoor)
	if !ok {
		return
	}
	rows, err := door.JobNotices()
	if err != nil {
		return
	}
	seen := map[int]session.JobNotice{}
	for _, n := range rows {
		seen[n.ID] = n
	}
	observer := &processObserver{seen: seen}
	conn.processes = observer
	done := make(chan struct{})
	var once sync.Once
	closeConnection := conn.Close
	conn.Close = func() {
		once.Do(func() {
			close(done)
			if closeConnection != nil {
				closeConnection()
			}
		})
	}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				rows, err := door.JobNotices()
				if err != nil {
					return
				}
				a.mu.Lock()
				hub := a.events
				a.mu.Unlock()
				observer.publish(hub, id, rows)
			}
		}
	}()
}
func publishProcessChanges(hub *EventHub, id string, seen map[int]session.JobNotice, rows []session.JobNotice) {
	for _, n := range rows {
		if n.Kind == session.JobKindTask {
			continue
		}
		old, known := seen[n.ID]
		seen[n.ID] = n
		if known && old.State == n.State && old.Name == n.Name && old.URL == n.URL {
			continue
		}
		if hub == nil {
			continue
		}
		p := processOf(id, n)
		_, _ = hub.Publish(EventProcessChanged, ProcessChangedEvent{Agent: id, Process: p.ID, State: p.State, ExitCode: p.ExitCode, URL: p.URL})
		if n.Over() && (!known || !old.Over()) {
			_, _ = hub.Publish(EventTurnEntry, map[string]any{"agent": id, "entry": processEndEntry(id, n)})
		}
	}
}
func processEndEntry(agent string, n session.JobNotice) DisplayEntry {
	at := n.Started.Add(n.Elapsed)
	text := n.Label() + " finished · Output"
	if n.State == session.JobFailed {
		text = n.Label() + " failed · exit " + strconv.Itoa(n.ExitCode) + " · Output · Rerun"
	}
	if n.State == session.JobStopped {
		text = n.Label() + " stopped · Output · Rerun"
	}
	return DisplayEntry{ID: fmt.Sprintf("process-end-%s", processID(agent, n.ID)), Kind: "notice", Text: text, At: &at, Link: &SourceRef{Kind: "process", ID: processID(agent, n.ID)}}
}

// End notices are derived from the retained shelf, once per process id. Live
// rows win over saved rows so a snapshot never announces a running process as stopped.
func (a *AgentRegistry) endedProcessEntries(id, file string) []DisplayEntry {
	rows, _ := session.ReadProcessNotices(file)
	live := a.live[id]
	if live != nil {
		if list, ok := live.conn.Engine.(processListDoor); ok {
			rows, _ = list.JobNotices()
		}
	}
	entries := []DisplayEntry{}
	for _, n := range rows {
		if live == nil && n.State == session.JobRunning {
			n.State = session.JobStopped
		}
		if n.Kind != session.JobKindTask && n.Over() {
			entries = append(entries, processEndEntry(id, n))
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].At.Before(*entries[j].At) })
	return entries
}

// Mutations publish their terminal rows before detaching the owner or closing
// the hub. The observer shares the same seen shelf with its timer, so neither
// path can publish a second end notice for the same process.
func (a *AgentRegistry) syncProcessEventsLocked(id string, conn AgentConnection) {
	if conn.processes == nil {
		return
	}
	if door, ok := conn.Engine.(processListDoor); ok {
		if rows, err := door.JobNotices(); err == nil {
			conn.processes.publish(a.events, id, rows)
		}
	}
}

// The engine's owed exit note remains in model context. The app codec replaces
// its display with the same stable notice sent on the process stream, instead
// of drawing both accounts of one ending in a restored chat.
func mergeProcessEndEntries(entries, notices []DisplayEntry) []DisplayEntry {
	for _, notice := range notices {
		if notice.Link == nil {
			continue
		}
		_, id, err := splitProcessID(notice.Link.ID)
		if err != nil {
			continue
		}
		replaced := false
		for i, entry := range entries {
			if entry.ID == notice.ID || entry.Kind == "notice" && entry.Role == "aside" && (strings.HasPrefix(entry.Text, fmt.Sprintf("job %d exited ", id)) || strings.HasPrefix(entry.Text, fmt.Sprintf("job %d was stopped", id))) {
				entries[i] = notice
				replaced = true
				break
			}
		}
		if !replaced {
			entries = append(entries, notice)
		}
	}
	return entries
}
