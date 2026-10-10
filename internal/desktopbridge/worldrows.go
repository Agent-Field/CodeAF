package desktopbridge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The `world` producer: one row per conversation on this machine, published as
// deltas. A row is the persisted record (session.ReadRows, the same reading the
// history and Places lanes use) with the live state of any attached conversation
// laid over it, because a window that is running a turn knows before its presence
// file does.
//
// IT SCANS ONLY WHILE SOMEBODY LISTENS. The poll starts with the first observer and
// stops with the last, and every pass is two-tiered: a cheap pass that only stats
// each folder's files, then a full read of just the folders whose stamps moved. A
// quiet machine with a hundred conversations costs a few hundred stats every two
// seconds and no parsing at all.

// worldRowsKind is the record type this producer publishes.
const worldRowsKind = "world"

// WorldChatRow is one conversation as the window's rows read it. Unknown renders as
// nothing: an empty title, workspace or time is omitted, never invented.
type WorldChatRow struct {
	ChatID      string `json:"chatId"`
	SessionFile string `json:"sessionFile,omitempty"`
	Title       string `json:"title,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	Running     bool   `json:"running"`
	// NeedsYou is how many questions are open (DESIGN-QUESTIONS Q13): the same
	// number the tray shows. Zero is "nothing waiting".
	NeedsYou     int    `json:"needsYou"`
	Failed       int    `json:"failed"`
	TasksRunning int    `json:"tasksRunning"`
	TasksTotal   int    `json:"tasksTotal"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
	Attached     bool   `json:"attached"`
	Archived     bool   `json:"archived"`
}

// chatRowsDelta is the payload of a `world` record: the rows that changed and the
// ids that are gone, never the whole set.
type chatRowsDelta struct {
	Rows    []WorldChatRow `json:"rows"`
	Removed []string       `json:"removed"`
}

// attachedChat is what a conversation held by a window says about itself now.
type attachedChat struct {
	SessionFile string
	Workspace   string
	Title       string
	Running     bool
	Questions   int
	UpdatedAt   time.Time
}

// folderSig is everything the cheap pass looks at in one conversation folder.
type folderSig struct {
	meta, presence, transcript, tasks stamp
}

type stamp struct {
	mod  time.Time
	size int64
}

type worldRows struct {
	root string
	// publish receives each delta. It runs under the producer's lock so records
	// leave in order; it must not call back into the producer.
	publish   func(kind string, payload any)
	interval  time.Duration
	newTicker func(time.Duration) (<-chan time.Time, func())

	mu        sync.Mutex
	observers int
	stopPoll  chan struct{}
	scanned   bool
	scans     int
	sigs      map[string]folderSig    // by transcript path
	disk      map[string]WorldChatRow // by transcript path, before the attached overlay
	live      map[string]bool         // folders whose presence was fresh at the last read
	attached  map[string]attachedChat
	rows      map[string]WorldChatRow // by chat id, as last published
}

// newWorldRows builds a producer over a places root. A nil publish drops records.
func newWorldRows(root string, publish func(string, any)) *worldRows {
	if publish == nil {
		publish = func(string, any) {}
	}
	return &worldRows{
		root: root, publish: publish, interval: worldInterval,
		newTicker: func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		},
		sigs: map[string]folderSig{}, disk: map[string]WorldChatRow{}, live: map[string]bool{},
		attached: map[string]attachedChat{}, rows: map[string]WorldChatRow{},
	}
}

// Observe counts a reader and starts the poll for the first one. The returned
// function detaches it, and stops the poll with the last.
func (w *worldRows) Observe() func() {
	w.mu.Lock()
	w.observers++
	if w.observers == 1 {
		stop := make(chan struct{})
		w.stopPoll = stop
		go w.poll(stop)
	}
	w.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.observers--
			if w.observers == 0 && w.stopPoll != nil {
				close(w.stopPoll)
				w.stopPoll = nil
			}
		})
	}
}

func (w *worldRows) poll(stop <-chan struct{}) {
	tick, release := w.newTicker(w.interval)
	defer release()
	w.Scan()
	for {
		select {
		case <-stop:
			return
		case <-tick:
			w.Scan()
		}
	}
}

// Full is every row, sorted by chat id. It reads the disk once if nothing has yet,
// then serves the cache the poll (or an attached change) keeps current.
func (w *worldRows) Full() []WorldChatRow {
	w.mu.Lock()
	first := !w.scanned
	w.mu.Unlock()
	if first {
		w.Scan()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]WorldChatRow, 0, len(w.rows))
	for _, row := range w.rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChatID < out[j].ChatID })
	return out
}

// Attached publishes a conversation's own state at once, without waiting for the
// next poll. A zero-valued chat (no session file) withdraws the overlay.
func (w *worldRows) Attached(id string, chat attachedChat, open bool) {
	if id == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if open {
		w.attached[id] = chat
	} else {
		delete(w.attached, id)
	}
	w.mergeLocked()
}

// Scan runs one two-tier pass and publishes what moved.
func (w *worldRows) Scan() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scans++
	w.scanned = true
	seen := map[string]bool{}
	var changed []string
	buckets, _ := os.ReadDir(w.root)
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		bucketDir := filepath.Join(w.root, bucket.Name())
		tasks := statOf(filepath.Join(bucketDir, "tasks.jsonl"))
		folders, _ := os.ReadDir(bucketDir)
		for _, folder := range folders {
			if !folder.IsDir() {
				continue
			}
			dir := filepath.Join(bucketDir, folder.Name())
			transcript := filepath.Join(dir, "transcript.jsonl")
			sig := folderSig{
				meta: statOf(filepath.Join(dir, "meta.json")), presence: statOf(filepath.Join(dir, "presence.json")),
				transcript: statOf(transcript), tasks: tasks,
			}
			if sig.transcript == (stamp{}) || sig.meta == (stamp{}) {
				continue
			}
			seen[transcript] = true
			// A live claim goes stale by the clock alone, so a folder that was live is
			// read again even when nothing was written.
			if old, ok := w.sigs[transcript]; !ok || old != sig || w.live[transcript] {
				changed = append(changed, transcript)
			}
			w.sigs[transcript] = sig
		}
	}
	for path := range w.sigs {
		if !seen[path] {
			delete(w.sigs, path)
			delete(w.disk, path)
			delete(w.live, path)
		}
	}
	if len(changed) > 0 {
		reads := session.ReadRows(changed)
		indexes := map[string][]session.TaskIndexEntry{}
		for _, path := range changed {
			row, ok := reads[filepath.Clean(path)]
			if !ok {
				delete(w.disk, path)
				delete(w.live, path)
				continue
			}
			index := filepath.Join(filepath.Dir(filepath.Dir(path)), "tasks.jsonl")
			entries, have := indexes[index]
			if !have {
				entries = session.ReadTaskIndex(index)
				indexes[index] = entries
			}
			w.disk[path] = diskRow(row, entries)
			w.live[path] = row.Live
		}
	}
	w.mergeLocked()
}

// diskRow is the persisted reading of one conversation. A pending-question count
// is not on disk, so a live conversation stopped on a question counts as one.
func diskRow(row session.SessionRow, index []session.TaskIndexEntry) WorldChatRow {
	out := WorldChatRow{
		ChatID: row.ID, SessionFile: row.Transcript, Title: row.Title, Workspace: row.Workspace,
		Running: row.Live && row.Presence.State == session.PresenceWorking, Archived: row.Archived,
	}
	if row.NeedsPerson() {
		out.NeedsYou = 1
	}
	if !row.At.IsZero() {
		out.UpdatedAt = row.At.UTC().Format(time.RFC3339)
	}
	for _, entry := range index {
		if strings.TrimSpace(entry.SessionID) != row.ID {
			continue
		}
		out.TasksTotal++
		if row.Runs(entry) {
			out.TasksRunning++
		}
	}
	return out
}

// mergeLocked lays the attached state over the disk rows and publishes the delta
// against what was last published.
func (w *worldRows) mergeLocked() {
	next := make(map[string]WorldChatRow, len(w.disk)+len(w.attached))
	for _, row := range w.disk {
		next[row.ChatID] = row
	}
	for id, chat := range w.attached {
		row, ok := next[id]
		if !ok {
			// A conversation the disk has not recorded yet is still a conversation.
			row = WorldChatRow{ChatID: id, SessionFile: chat.SessionFile, Workspace: chat.Workspace}
		}
		row.Attached = true
		row.Running = chat.Running
		row.NeedsYou = chat.Questions
		if chat.Title != "" {
			row.Title = chat.Title
		}
		if !chat.UpdatedAt.IsZero() {
			row.UpdatedAt = chat.UpdatedAt.UTC().Format(time.RFC3339)
		}
		next[id] = row
	}
	delta := chatRowsDelta{Rows: []WorldChatRow{}, Removed: []string{}}
	for id, row := range next {
		if old, ok := w.rows[id]; !ok || old != row {
			delta.Rows = append(delta.Rows, row)
		}
	}
	for id := range w.rows {
		if _, ok := next[id]; !ok {
			delta.Removed = append(delta.Removed, id)
		}
	}
	w.rows = next
	if len(delta.Rows) == 0 && len(delta.Removed) == 0 {
		return
	}
	sort.Slice(delta.Rows, func(i, j int) bool { return delta.Rows[i].ChatID < delta.Rows[j].ChatID })
	sort.Strings(delta.Removed)
	w.publish(worldRowsKind, delta)
}

func statOf(path string) stamp {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{mod: info.ModTime(), size: info.Size()}
}

// worldChanged is what a conversation calls from publish when its running flag,
// pending questions or title may have moved. It is a no-op until a producer is
// set with UseWorldRows, so the call site costs nothing before integration.
func (b *Bridge) worldChanged(s *conversation) {
	b.mu.Lock()
	rows := b.worldRows
	b.mu.Unlock()
	if rows == nil {
		return
	}
	file := s.conn.Welcome.SessionFile
	id := filepath.Base(filepath.Dir(file))
	if file == "" || id == "." {
		return
	}
	s.mu.Lock()
	running, stamp := s.running, s.updatedAt
	s.mu.Unlock()
	chat := attachedChat{SessionFile: file, Workspace: s.conn.Welcome.Workspace, Running: running, UpdatedAt: stamp}
	if a := s.conn.Agent; a != nil {
		chat.Title = a.Title()
		if door, ok := a.(interface{ OpenQuestions() []session.Question }); ok {
			chat.Questions = len(door.OpenQuestions())
		} else if a.NeedsPerson() {
			chat.Questions = 1
		}
	}
	rows.Attached(id, chat, true)
}

// UseWorldRows sets the rows producer worldChanged feeds.
func (b *Bridge) UseWorldRows(w *worldRows) {
	b.mu.Lock()
	b.worldRows = w
	b.mu.Unlock()
}
