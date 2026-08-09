// Durable persistence — port of src/plandb/persist.ts.
//
// Append-only JSONL journal of RESULTING ROWS + snapshot compaction. See the
// TS original's header comment for the full design rationale; the contract
// preserved here:
//   - journal rows, never operations (id stability across restarts)
//   - record kinds t/p/c/d/x/r/s, unknown kinds ignored (forward-compatible)
//   - last-writer-wins fold by id; restored order = FIRST-appearance order
//     (JS Map.set keeps the original position on update)
//   - corruption tolerance: unparseable lines are skipped, never thrown
//   - fail-open: the first write error disables the journal for the run
//   - compact every COMPACT_EVERY appends and on every OpenPlanDB
//   - kill switch CODEAF_PLANDB_PERSIST=0 (exact-string comparison)
package plandb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const compactEvery = 500

// persistDisabled mirrors persistDisabled(): any value other than exactly
// "0" leaves persistence ON.
func persistDisabled() bool {
	return os.Getenv("CODEAF_PLANDB_PERSIST") == "0"
}

// PlanDBPath mirrors planDBPath(): PLANDB_DB env override, else
// <workspace>/.plandb.db.
func PlanDBPath(workspace string) string {
	if env := os.Getenv("PLANDB_DB"); env != "" {
		return env
	}
	return filepath.Join(workspace, ".plandb.db")
}

// journal record shapes. Field order matters: the TS side writes object
// literals {k, v} / {k, f, t, d} / {k, f, t} and encoding/json emits
// declaration order.
type recValue struct {
	K string          `json:"k"`
	V json.RawMessage `json:"v"`
}

type recDep struct {
	K string  `json:"k"`
	F TaskID  `json:"f"`
	T TaskID  `json:"t"`
	D DepKind `json:"d"`
}

type recDepRemove struct {
	K string `json:"k"`
	F TaskID `json:"f"`
	T TaskID `json:"t"`
}

type recReset struct {
	K string `json:"k"`
}

type recSnapshot struct {
	K string   `json:"k"`
	V Snapshot `json:"v"`
}

// decoded shape for the fold — permissive so partial/foreign records don't
// abort the load.
type journalLine struct {
	K string          `json:"k"`
	V json.RawMessage `json:"v"`
	F string          `json:"f"`
	T string          `json:"t"`
	D *DepKind        `json:"d"`
}

type foldState struct {
	projects *jscompat.OrderedMap[string, *Project]
	tasks    *jscompat.OrderedMap[string, *Task]
	contexts *jscompat.OrderedMap[string, *ContextEntry]
	deps     *jscompat.OrderedMap[string, Dependency]
}

func emptyFold() *foldState {
	return &foldState{
		projects: jscompat.NewOrderedMap[string, *Project](),
		tasks:    jscompat.NewOrderedMap[string, *Task](),
		contexts: jscompat.NewOrderedMap[string, *ContextEntry](),
		deps:     jscompat.NewOrderedMap[string, Dependency](),
	}
}

func depKey(from, to TaskID) string {
	return from + " " + to
}

// normalizeRaw maps the decoded literal `null` back to a nil RawMessage. The
// live store represents "no value" as nil (which marshals to null); without
// this, a restored row would hold RawMessage("null") — JSON-identical but not
// structurally equal, unlike TS where null is one value in both states.
func normalizeRaw(m json.RawMessage) json.RawMessage {
	if string(m) == "null" {
		return nil
	}
	return m
}

func normalizeTask(t *Task) {
	t.Result = normalizeRaw(t.Result)
	t.Metadata = normalizeRaw(t.Metadata)
}

func normalizeProject(p *Project) {
	p.Metadata = normalizeRaw(p.Metadata)
}

// applyRecord mirrors applyRecord: fold one record, ignore unknown kinds.
func applyRecord(state *foldState, rec journalLine) {
	switch rec.K {
	case "t":
		var t Task
		if json.Unmarshal(rec.V, &t) == nil && t.ID != "" {
			normalizeTask(&t)
			state.tasks.Set(t.ID, &t)
		}
	case "p":
		var p Project
		if json.Unmarshal(rec.V, &p) == nil && p.ID != "" {
			normalizeProject(&p)
			state.projects.Set(p.ID, &p)
		}
	case "c":
		var c ContextEntry
		if json.Unmarshal(rec.V, &c) == nil && c.ID != "" {
			state.contexts.Set(c.ID, &c)
		}
	case "d":
		if rec.F != "" && rec.T != "" {
			kind := DepFeedsInto
			if rec.D != nil {
				kind = *rec.D
			}
			state.deps.Set(depKey(rec.F, rec.T), Dependency{FromTask: rec.F, ToTask: rec.T, Kind: kind})
		}
	case "x":
		if rec.F != "" && rec.T != "" {
			state.deps.Delete(depKey(rec.F, rec.T))
		}
	case "r":
		state.projects.Clear()
		state.tasks.Clear()
		state.contexts.Clear()
		state.deps.Clear()
	case "s":
		state.projects.Clear()
		state.tasks.Clear()
		state.contexts.Clear()
		state.deps.Clear()
		var v Snapshot
		if json.Unmarshal(rec.V, &v) != nil {
			return
		}
		for _, p := range v.Projects {
			if p != nil && p.ID != "" {
				normalizeProject(p)
				state.projects.Set(p.ID, p)
			}
		}
		for _, t := range v.Tasks {
			if t != nil && t.ID != "" {
				normalizeTask(t)
				state.tasks.Set(t.ID, t)
			}
		}
		for _, c := range v.Contexts {
			if c != nil && c.ID != "" {
				state.contexts.Set(c.ID, c)
			}
		}
		for _, d := range v.Dependencies {
			if d.FromTask != "" && d.ToTask != "" {
				state.deps.Set(depKey(d.FromTask, d.ToTask), d)
			}
		}
	}
}

// readState mirrors readState: never fails — a missing/unreadable file yields
// empty state, and any unparseable line is skipped.
func readState(dbPath string) Snapshot {
	state := emptyFold()
	raw := ""
	if b, err := os.ReadFile(dbPath); err == nil {
		raw = string(b)
	}
	for _, line := range strings.Split(raw, "\n") {
		if len(line) == 0 {
			continue
		}
		var rec journalLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// Corrupt/torn line — skip it and keep folding the rest.
			continue
		}
		applyRecord(state, rec)
	}
	return Snapshot{
		Projects:     state.projects.Values(),
		Tasks:        state.tasks.Values(),
		Contexts:     state.contexts.Values(),
		Dependencies: state.deps.Values(),
	}
}

// FileJournal is the concrete write-through journal. All writes are
// fail-open: the first IO error disables the journal for the remainder of
// the run. Its methods are invoked while the store lock is held (see
// Journal), which is why compaction uses the store's unexported
// snapshotLocked — re-entering the public API would deadlock, and the TS
// original's synchronous mid-mutation snapshot is exactly this.
type FileJournal struct {
	dbPath              string
	store               *PlanDB
	appendsSinceCompact int
	disabled            bool
}

func NewFileJournal(dbPath string, store *PlanDB) *FileJournal {
	return &FileJournal{dbPath: dbPath, store: store}
}

func (j *FileJournal) append(rec any) {
	if j.disabled {
		return
	}
	line, err := jscompat.Stringify(rec)
	if err == nil {
		err = appendFile(j.dbPath, append(line, '\n'))
	}
	if err != nil {
		j.disabled = true
		fmt.Fprintf(os.Stderr,
			"[plandb-persist] journal write failed on %s; persistence disabled for this run (in-memory state unaffected): %v\n",
			j.dbPath, err)
		return
	}
	j.appendsSinceCompact++
	if j.appendsSinceCompact >= compactEvery {
		j.compactLocked()
	}
}

func appendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func (j *FileJournal) PutTask(task *Task) {
	j.append(recValue{K: "t", V: mustRaw(task)})
}

func (j *FileJournal) PutProject(project *Project) {
	j.append(recValue{K: "p", V: mustRaw(project)})
}

func (j *FileJournal) PutContext(entry *ContextEntry) {
	j.append(recValue{K: "c", V: mustRaw(entry)})
}

func (j *FileJournal) AddDep(from, to TaskID, kind DepKind) {
	j.append(recDep{K: "d", F: from, T: to, D: kind})
}

func (j *FileJournal) RemoveDep(from, to TaskID) {
	j.append(recDepRemove{K: "x", F: from, T: to})
}

func (j *FileJournal) Reset() {
	if j.disabled {
		return
	}
	if err := os.WriteFile(j.dbPath, []byte{}, 0o644); err != nil {
		j.disabled = true
		fmt.Fprintf(os.Stderr, "[plandb-persist] journal reset failed on %s: %v\n", j.dbPath, err)
		return
	}
	j.appendsSinceCompact = 0
}

func mustRaw(v any) json.RawMessage {
	b, err := jscompat.Stringify(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

// compactLocked rewrites the whole file to a single snapshot record via a
// temp file + rename. Best effort: on failure the append-only log stays in
// place. Caller must hold the store lock.
func (j *FileJournal) compactLocked() {
	if j.disabled {
		return
	}
	snapshot := j.store.snapshotLocked()
	line, err := jscompat.Stringify(recSnapshot{K: "s", V: snapshot})
	if err != nil {
		return
	}
	tmp := j.dbPath + ".tmp"
	if err := os.WriteFile(tmp, append(line, '\n'), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, j.dbPath); err != nil {
		return
	}
	j.appendsSinceCompact = 0
}

// Compact is the externally-callable variant (used by OpenPlanDB, outside
// any store mutation): it takes the store lock itself.
func (j *FileJournal) Compact() {
	j.store.mu.Lock()
	defer j.store.mu.Unlock()
	j.compactLocked()
}

// OpenPlanDB mirrors openPlanDB(): load durable state into the process-global
// singleton and attach a write-through journal. Idempotent per process; no-op
// under CODEAF_PLANDB_PERSIST=0. Task ids load verbatim — never regenerated.
func OpenPlanDB(dbPath string) {
	if persistDisabled() {
		return
	}
	store := GetPlanDB()
	if store.HasJournal() {
		return // already opened this process — idempotent
	}

	state := readState(dbPath)
	store.Restore(state)

	if dir := filepath.Dir(dbPath); dir != "" {
		if _, err := os.Stat(dir); err != nil {
			// If we can't ensure the directory, the journal's fail-open path
			// handles it.
			_ = os.MkdirAll(dir, 0o755)
		}
	}

	journal := NewFileJournal(dbPath, store)
	store.AttachJournal(journal)
	// Compact on open: collapse whatever we just replayed into one snapshot
	// line, bounding the file size across restarts.
	journal.Compact()
}
