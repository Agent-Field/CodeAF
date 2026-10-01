package inventory

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// Probe asks the machine about a process group. It is an interface so a test
// can say which groups are alive without starting any.
type Probe interface {
	Alive(pgid int) bool
	Ports(pgid int) []int
}

// hostProbe asks this machine's own process table.
type hostProbe struct{}

func (hostProbe) Alive(pgid int) bool  { return processgroup.Alive(pgid) }
func (hostProbe) Ports(pgid int) []int { return executor.GroupPorts(pgid) }

// Live is the commands a session started that may still be running. It is fed
// from two places that see different processes: the job registry, which starts
// background jobs, and the executor's own account of a call that left a process
// behind. It holds no verdict: whether one is alive is asked when the record is
// written, because a process that was killed from outside has told nobody.
type Live struct {
	mu    sync.Mutex
	now   func() time.Time
	items map[string]liveItem
}

type liveItem struct {
	command string
	dir     string
	pgid    int
	since   int64
}

// NewLive makes an empty set that stamps each command with the clock's time.
func NewLive(now func() time.Time) *Live {
	if now == nil {
		now = time.Now
	}
	return &Live{now: now, items: map[string]liveItem{}}
}

var _ executor.Lifecycle = (*Live)(nil)

func jobKey(id int) string     { return fmt.Sprintf("job:%d", id) }
func groupKey(pgid int) string { return fmt.Sprintf("group:%d", pgid) }

// put stores the command under key. A process group is one thing however many
// sites saw it (a foreground call that was then made a job), so an earlier item
// for the same group is replaced rather than listed twice.
func (l *Live) put(key string, item liveItem) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for other, seen := range l.items {
		if seen.pgid == item.pgid {
			delete(l.items, other)
		}
	}
	item.since = l.now().UnixMilli()
	l.items[key] = item
}

// Started implements executor.Lifecycle.
func (l *Live) Started(j executor.Job) {
	l.put(jobKey(j.ID), liveItem{command: j.Command, dir: j.Dir, pgid: j.PGID})
}

// Ended implements executor.Lifecycle: a job that ended, or was killed, leaves.
func (l *Live) Ended(id int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.items, jobKey(id))
}

// Left records a process group a call left running when it returned.
func (l *Live) Left(s executor.Service, dir string) {
	l.put(groupKey(s.PGID), liveItem{command: strings.Join(s.Argv, " "), dir: dir, pgid: s.PGID})
}

// Alive is the commands whose process group is running now, oldest first, with
// the ports each listens on and their command lines cleaned of secrets. A group
// the probe finds gone is dropped for good.
func (l *Live) Alive(probe Probe) []Running {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Running
	for key, item := range l.items {
		if !probe.Alive(item.pgid) {
			delete(l.items, key)
			continue
		}
		out = append(out, Running{Command: Cleaned(item.command), Cwd: Cwd(item.dir), Ports: probe.Ports(item.pgid), Since: item.since})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Since != out[b].Since {
			return out[a].Since < out[b].Since
		}
		return out[a].Command < out[b].Command
	})
	return out
}

// Cleaned is a command line as it may be recorded: on one line, secrets
// replaced, and cut to the bound at a character boundary. A secret never enters
// the record (L3). It is one line because a script's line breaks would otherwise
// reach the card a person reads, which shows the first line and loses the rest.
func Cleaned(command string) string {
	line := keys.Clean(oneLine(command))
	if len(line) <= maxCommand {
		return line
	}
	cut := maxCommand
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut]
}

// oneLine joins the lines of a script with "; ", dropping blank ones.
func oneLine(command string) string {
	var lines []string
	for _, line := range strings.Split(command, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}

// Cwd is a folder as the record spells it: cell-relative, with "." for the root.
func Cwd(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}
