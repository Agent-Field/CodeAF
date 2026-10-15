package main

// ── codeaf app: THE WORK LOG'S DETAIL ──────────────────────────────────────
//
// The app groups, folds, pins and tails a turn's tool rows by its own rules
// (PROTOCOL.md, "Work log detail"); this file supplies the facts of each call
// those rules read: its kind, its clock, the calls started with it, its exit
// code, the earlier run it repeats, the files it changed, the account, task or
// ask it belongs to, what it was asked, and the tail of what it printed.
//
// A CALL'S OUTPUT IS KEPT ON DISK, ONE FILE PER CALL, under the app's own
// folder. The engine sends a shell call's output as it arrives (the app sets
// session.Config.LiveToolOutput) and a quiet tool's output once it ends, and
// both are written to the call's file, which is what tools.output reads: after
// the turn, after the conversation is let go, and after the app is restarted.
// Files older than a week are swept.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

const (
	// appTailLive and appTailEnd are how many lines of a call's output its
	// row carries while it runs and once it has ended.
	appTailLive = 3
	appTailEnd  = 12
	// appToolBeat is how often a long call's row is said again with a fresh
	// tail, and appToolQuiet how long a call runs before it is.
	appToolBeat  = 500 * time.Millisecond
	appToolQuiet = 5 * time.Second
	// appCallsKept is how many calls one agent's log remembers in memory;
	// their output files outlive them.
	appCallsKept = 2000
	// appOutputKept is how long a call's output file is kept.
	appOutputKept = 7 * 24 * time.Hour
	// appOutputPage and appOutputPageMax are tools.output's default and
	// largest page, in lines.
	appOutputPage    = 1000
	appOutputPageMax = 10000
)

// appCall is one tool call as the work log knows it.
type appCall struct {
	id, tool, args, hint, title string
	turn                        uint64
	facts                       appwire.ToolFacts
	ended                       bool
	ok                          *bool
	summary, detail             string
	// path is the call's output file, open in file while output arrives;
	// lines counts its complete lines, partial is the line still being
	// written, and tail the last complete lines.
	path     string
	file     *os.File
	lines    int
	partial  string
	tail     []string
	streamed bool
	followed bool
	// sent is the tail the row last carried.
	sent string
}

// appCallLog is one agent's calls, by id, and the order they began in.
type appCallLog struct {
	mu    sync.Mutex
	dir   string
	calls map[string]*appCall
	order []string
	// turn is the running turn's calls in order, which a retry is found in.
	turn []*appCall
}

func (l *appCallLog) locked(fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fn()
}

// outputRoot is the folder every agent's call output is kept under.
func (b *appBackend) outputRoot() string {
	path := b.state.path
	if path == "" {
		path = appStatePath()
	}
	return filepath.Join(filepath.Dir(path), "output")
}

// callLog is one agent's call log, made the first time it is needed.
func (b *appBackend) callLog(a *appAgent) *appCallLog {
	var log *appCallLog
	b.locked(func() {
		if a.calls == nil {
			a.calls = &appCallLog{dir: filepath.Join(b.outputRoot(), appFileName(a.view.ID)), calls: map[string]*appCall{}}
		}
		log = a.calls
	})
	return log
}

// appFileName is an id as a file name: itself when it is plain, and a hash
// of it when it is not.
func appFileName(id string) string {
	plain := id != ""
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			plain = false
			break
		}
	}
	if plain && len(id) <= 100 {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}

// appToolKind is a call's kind, read off the engine's own family for the
// tool and the account it acts through, never a list kept here.
func appToolKind(tool, service string) string {
	if service != "" {
		return appwire.ToolConnector
	}
	switch session.ActionCategoryForTool(tool) {
	case session.ActionSearch:
		if strings.HasPrefix(tool, "web_") {
			return appwire.ToolWeb
		}
		return appwire.ToolSearch
	case session.ActionRead:
		return appwire.ToolRead
	case session.ActionEdit, session.ActionCreate:
		return appwire.ToolEdit
	case session.ActionRun, session.ActionTest:
		return appwire.ToolRun
	case session.ActionBrowse:
		if tool == "use_service" {
			return appwire.ToolConnector
		}
		return appwire.ToolWeb
	case session.ActionCoordinate:
		return appwire.ToolTask
	}
	return appwire.ToolOther
}

// appToolInput is what a call was asked: its command, query or path, and for
// a command the folder it runs in and how long it may run.
func appToolInput(kind, tool, args, hint, workspace string) *appwire.ToolInput {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(args), &fields)
	text := ""
	if raw, ok := fields["command"]; ok {
		_ = json.Unmarshal(raw, &text)
	}
	if text == "" {
		text = tui3.ToolTarget(tool, args, hint)
	}
	input := &appwire.ToolInput{Text: redact.Secrets(text)}
	if kind == appwire.ToolRun {
		input.Cwd = workspace
		seconds := bare.BashCeilingSeconds
		if raw, ok := fields["timeout"]; ok {
			var given float64
			if json.Unmarshal(raw, &given) == nil && given > 0 {
				seconds = int(given)
			}
		}
		input.TimeoutS = &seconds
	}
	if input.Text == "" && input.Cwd == "" {
		return nil
	}
	return input
}

// newTurnCalls starts a turn's own list of calls, which retries are found in.
func (b *appBackend) newTurnCalls(a *appAgent) {
	log := b.callLog(a)
	log.locked(func() { log.turn = nil })
}

// callBegan records a call that began and answers its row.
func (b *appBackend) callBegan(a *appAgent, agent string, turn uint64, event session.Event, title string) appwire.TurnTool {
	log := b.callLog(a)
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	var row appwire.TurnTool
	log.locked(func() {
		c := log.calls[event.CallID]
		if c == nil || event.CallID == "" {
			c = &appCall{id: event.CallID}
			if event.CallID != "" {
				log.calls[event.CallID] = c
				log.order = append(log.order, event.CallID)
				log.forgetLocked()
			}
			at := b.now()
			c.facts.StartedAt = &at
			log.turn = append(log.turn, c)
		}
		c.tool, c.args, c.hint, c.title, c.turn = event.Tool, event.Args, event.Hint, title, turn
		kind := appToolKind(event.Tool, event.Service)
		if c.facts.Ask != "" {
			kind = appwire.ToolAsksFirst
		}
		c.facts.Kind, c.facts.Batch = kind, event.Batch
		c.facts.Input = appToolInput(kind, event.Tool, event.Args, event.Hint, workspace)
		switch kind {
		case appwire.ToolConnector:
			c.facts.Connector = event.ServiceName
			if c.facts.Connector == "" {
				c.facts.Connector = event.Service
			}
			if c.facts.Connector == "" {
				c.facts.Connector = redact.Secrets(strings.TrimSpace(event.Hint))
			}
		case appwire.ToolRun:
			c.facts.RetriedOf = log.retriedLocked(c)
		}
		if path, adds, dels, ok := tui3.ToolFileChange(event.Tool, event.Args); ok {
			c.facts.Files = []appwire.FileChange{{Path: path, Add: adds, Del: dels}}
		}
		row = b.rowLocked(agent, c)
	})
	return row
}

// retriedLocked is the latest earlier call of this turn that ran the same
// command as c, or "". Callers hold l.mu.
func (l *appCallLog) retriedLocked(c *appCall) string {
	if c.facts.Input == nil || strings.TrimSpace(c.facts.Input.Text) == "" {
		return ""
	}
	for i := len(l.turn) - 1; i >= 0; i-- {
		prior := l.turn[i]
		if prior == c || prior.facts.Kind != appwire.ToolRun || prior.facts.Input == nil {
			continue
		}
		if prior.facts.Input.Text == c.facts.Input.Text {
			return prior.id
		}
	}
	return ""
}

// forgetLocked lets the oldest calls go once the log is full; their files
// stay. Callers hold l.mu.
func (l *appCallLog) forgetLocked() {
	for len(l.order) > appCallsKept {
		oldest := l.calls[l.order[0]]
		if oldest != nil && !oldest.ended {
			return
		}
		delete(l.calls, l.order[0])
		l.order = l.order[1:]
	}
}

// rowLocked is a call's row as it stands. Callers hold the log's lock.
func (b *appBackend) rowLocked(agent string, c *appCall) appwire.TurnTool {
	phase := appwire.PhaseBegin
	if c.ended {
		phase = appwire.PhaseEnd
	}
	row := appwire.TurnTool{Agent: agent, Turn: c.turn, Call: c.id, Name: c.tool, Title: c.title, Phase: phase, OK: c.ok, Summary: c.summary, Detail: c.detail}
	row.ToolFacts = c.factsLocked()
	return row
}

// factsLocked is a call's facts with its tail as it stands: three lines while
// it runs, twelve once it has ended.
func (c *appCall) factsLocked() appwire.ToolFacts {
	facts := c.facts
	if c.facts.Files != nil {
		facts.Files = append([]appwire.FileChange(nil), c.facts.Files...)
	}
	tail := c.tail
	if c.partial != "" {
		tail = append(append([]string(nil), tail...), c.partial)
	}
	keep := appTailEnd
	if !c.ended {
		keep = appTailLive
	}
	if len(tail) > keep {
		tail = tail[len(tail)-keep:]
	}
	if len(tail) > 0 {
		facts.Tail = append([]string(nil), tail...)
	}
	if c.lines > 0 || c.partial != "" {
		lines := c.lines
		if c.partial != "" {
			lines++
		}
		facts.Lines = &lines
	}
	return facts
}

// callOutput writes what a call printed to its file and its tail, and says
// the new lines to a follower.
func (b *appBackend) callOutput(a *appAgent, agent string, live *appLive, event session.Event) {
	if event.CallID == "" || event.Text == "" {
		return
	}
	log := b.callLog(a)
	var said []string
	followed := false
	log.locked(func() {
		c := log.calls[event.CallID]
		if c == nil || c.ended {
			return
		}
		c.streamed = true
		said = log.writeLocked(c, redact.Secrets(event.Text))
		followed = c.followed
	})
	if followed && len(said) > 0 {
		live.box.Event(appwire.EventToolOutput, appwire.ToolOutputEvent{Agent: agent, Call: event.CallID, Lines: said})
	}
}

// writeLocked appends text to a call's file and tail and answers the lines it
// completed. Callers hold l.mu.
func (l *appCallLog) writeLocked(c *appCall, text string) []string {
	if c.file == nil && c.path == "" {
		c.path = filepath.Join(l.dir, appFileName(c.id)+".log")
		if err := os.MkdirAll(l.dir, 0o700); err == nil {
			c.file, _ = os.OpenFile(c.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		}
	}
	if c.file != nil {
		_, _ = io.WriteString(c.file, text)
	}
	parts := strings.Split(c.partial+text, "\n")
	c.partial = parts[len(parts)-1]
	done := parts[:len(parts)-1]
	for i := range done {
		done[i] = strings.TrimSuffix(done[i], "\r")
	}
	c.lines += len(done)
	c.tail = append(c.tail, done...)
	if len(c.tail) > appTailEnd {
		c.tail = append([]string(nil), c.tail[len(c.tail)-appTailEnd:]...)
	}
	return done
}

// callFinished keeps when a call's own work ended, which the engine says
// before the batch around it does.
func (b *appBackend) callFinished(a *appAgent, event session.Event) {
	log := b.callLog(a)
	log.locked(func() {
		if c := log.calls[event.CallID]; c != nil && c.facts.StartedAt != nil && event.Took > 0 {
			ended := c.facts.StartedAt.Add(event.Took)
			c.facts.EndedAt = &ended
		}
	})
}

// callEnded records a call's end and answers its row.
func (b *appBackend) callEnded(a *appAgent, agent string, turn uint64, event session.Event, title, summary, detail string, ok bool) appwire.TurnTool {
	log := b.callLog(a)
	var row appwire.TurnTool
	log.locked(func() {
		c := log.calls[event.CallID]
		if c == nil {
			c = &appCall{id: event.CallID, tool: event.Tool, args: event.Args, turn: turn}
			c.facts.Kind = appToolKind(event.Tool, "")
			if event.CallID != "" {
				log.calls[event.CallID] = c
				log.order = append(log.order, event.CallID)
			}
		}
		if c.title == "" {
			c.title = title
		}
		c.summary, c.detail, c.ok = summary, detail, &ok
		if c.facts.EndedAt == nil {
			at := b.now()
			c.facts.EndedAt = &at
		}
		if c.facts.Kind == appwire.ToolRun {
			code := 0
			switch {
			case event.Exit != nil:
				code = *event.Exit
				c.facts.ExitCode = &code
			case ok:
				c.facts.ExitCode = &code
			}
		}
		if !c.streamed && strings.TrimSpace(event.Output) != "" {
			log.writeLocked(c, redact.Secrets(event.Output))
		}
		if c.partial != "" && c.file == nil {
			c.lines++
			c.tail = append(c.tail, c.partial)
			c.partial = ""
		}
		if c.file != nil {
			_ = c.file.Close()
			c.file = nil
		}
		c.ended = true
		row = b.rowLocked(agent, c)
	})
	return row
}

// callAsked marks the call an approval is about: the log pauses there.
func (b *appBackend) callAsked(a *appAgent, agent string, live *appLive, call, ask string) {
	if call == "" {
		return
	}
	log := b.callLog(a)
	var row appwire.TurnTool
	known := false
	log.locked(func() {
		c := log.calls[call]
		if c == nil {
			return
		}
		c.facts.Kind, c.facts.Ask, known = appwire.ToolAsksFirst, ask, true
		row = b.rowLocked(agent, c)
	})
	if known {
		live.box.Event(appwire.EventTurnTool, row)
	}
}

// callTask names the task a running task call made, once.
func (b *appBackend) callTask(a *appAgent, task string) {
	log := b.callLog(a)
	log.locked(func() {
		for _, c := range log.turn {
			if !c.ended && c.facts.Kind == appwire.ToolTask && c.facts.Task == "" {
				c.facts.Task = task
				return
			}
		}
	})
}

// tickTools says a long call's row again with a fresh tail, at most every
// half second, until the turn ends.
func (b *appBackend) tickTools(a *appAgent, agent string, live *appLive) func() {
	done := make(chan struct{})
	var once sync.Once
	guard.Go("app/tool-tail", func() {
		tick := time.NewTicker(appToolBeat)
		defer tick.Stop()
		log := b.callLog(a)
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			var rows []appwire.TurnTool
			now := b.now()
			log.locked(func() {
				for _, c := range log.turn {
					if c.ended || c.facts.StartedAt == nil || now.Sub(*c.facts.StartedAt) < b.toolQuiet {
						continue
					}
					row := b.rowLocked(agent, c)
					said := strings.Join(row.Tail, "\n")
					if said == c.sent {
						continue
					}
					c.sent = said
					rows = append(rows, row)
				}
			})
			for _, row := range rows {
				live.box.Event(appwire.EventTurnTool, row)
			}
		}
	})
	return func() { once.Do(func() { close(done) }) }
}

// FollowTool says a call's new lines as tool.output events while followed.
func (b *appBackend) FollowTool(agent, call string, follow bool) error {
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	log := b.callLog(a)
	log.locked(func() {
		if c := log.calls[call]; c != nil {
			c.followed = follow
		}
	})
	return nil
}

// ToolOutput is one call's whole output by line, from its file, or from the
// conversation's own record for a call this app never watched.
func (b *appBackend) ToolOutput(agent, call string, from, limit int) ([]string, int, int, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, 0, 0, err
	}
	if limit <= 0 {
		limit = appOutputPage
	}
	limit = min(limit, appOutputPageMax)
	log := b.callLog(a)
	path := filepath.Join(log.dir, appFileName(call)+".log")
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		lines, total := appLinesOf(file, from, limit)
		return lines, from + len(lines), total, nil
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	if transcript != "" {
		record := session.ReadTranscript(transcript)
		for _, items := range [][]session.DisplayEntry{record.Earlier, record.Entries} {
			for _, item := range items {
				if item.Role == "tool" && item.CallID == call {
					lines, total := appLinesOf(strings.NewReader(redact.Secrets(item.Output)), from, limit)
					return lines, from + len(lines), total, nil
				}
			}
		}
	}
	return nil, 0, 0, appwire.Errorf(appwire.CodeNotFound, "there is no output for call %s", call)
}

// appReadLines reads lines [from, from+limit) of r and counts them all.
func appLinesOf(r io.Reader, from, limit int) ([]string, int) {
	reader := bufio.NewReader(r)
	var lines []string
	total := 0
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if total >= from && len(lines) < limit {
				lines = append(lines, strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
			}
			total++
		}
		if err != nil {
			break
		}
	}
	return lines, total
}

// appEntryFacts is a tool entry's facts read back: what this process saw of
// the call, or what its record and its file still say.
func (b *appBackend) appEntryFacts(a *appAgent, item session.DisplayEntry) appwire.ToolFacts {
	log := b.callLog(a)
	var facts appwire.ToolFacts
	known := false
	log.locked(func() {
		if c := log.calls[item.CallID]; c != nil && item.CallID != "" {
			facts, known = c.factsLocked(), true
		}
	})
	if known {
		return facts
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	facts.Kind = appToolKind(item.Tool, "")
	facts.Input = appToolInput(facts.Kind, item.Tool, item.Args, item.Hint, workspace)
	if path, adds, dels, ok := tui3.ToolFileChange(item.Tool, item.Args); ok {
		facts.Files = []appwire.FileChange{{Path: path, Add: adds, Del: dels}}
	}
	if facts.Kind == appwire.ToolRun && item.Answered {
		code := 0
		if exit := appExitOf(item.Output); exit != nil {
			code = *exit
		}
		facts.ExitCode = &code
	}
	output := redact.Secrets(item.Output)
	if file, err := os.Open(filepath.Join(log.dir, appFileName(item.CallID)+".log")); err == nil && item.CallID != "" {
		data, _ := io.ReadAll(io.LimitReader(file, 64<<20))
		_ = file.Close()
		output = string(data)
	}
	if output != "" {
		lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
		count := len(lines)
		facts.Lines = &count
		facts.Tail = lines[max(0, len(lines)-appTailEnd):]
	}
	return facts
}

// appExitRe is the status line a shell result ends with when its command
// exited with a code other than zero.
var appExitRe = regexp.MustCompile(`Command exited with code (-?\d+)`)

// appExitOf is the exit code a shell result's status line names, or nil.
func appExitOf(output string) *int {
	all := appExitRe.FindAllStringSubmatch(output, -1)
	if len(all) == 0 {
		return nil
	}
	code, err := strconv.Atoi(all[len(all)-1][1])
	if err != nil {
		return nil
	}
	return &code
}

// sweepOutputs removes call output files older than a week, once, after the
// app has said hello.
func (b *appBackend) sweepOutputs() {
	root := b.outputRoot()
	cutoff := b.now().Add(-appOutputKept)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".log") {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
		return nil
	})
}
