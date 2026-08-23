// place.go is the one answer to "where does this conversation keep things".
//
// A v3 session is a FOLDER (docs/CHAT-V3.md, Decision 26): the transcript, the
// working state, the task checkpoint, the node journals, the droppings, the
// worktrees and — for an owned session — the workspace itself all live inside
// one directory, so deleting a session is removing one folder and exporting
// one is zipping one. Every path below is arithmetic on [Place.Dir]; nothing
// here touches the disk except [Meta]'s load and save, because where things
// live and what lives there are one decision made in one file.
//
// The zero Place is the LEGACY layout: every method on it answers "", and a
// caller holding one keeps deriving sidecar paths the flat way. That is what
// lets the new layout land seam-first without a flag day.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The names inside a session folder. They are constants and not configuration:
// a folder a person can read is a folder whose parts always have the same
// names.
const (
	placeTranscript   = "transcript.jsonl"
	placeState        = "state.json"
	placeCard         = "card.json"
	placeTasks        = "tasks.json"
	placeMeta         = "meta.json"
	placeNodeJournals = "tasks"
	placeLogs         = "logs"
	placeTrees        = "trees"
	placeWork         = "work"
	placeArtifacts    = "artifacts"
)

// Place names every location one v3 session may touch on disk.
type Place struct {
	// Dir is the session folder. Empty is the legacy flat layout, and every
	// path method on such a Place answers "".
	Dir string
	// Workspace is the tools root: the borrowed project root, or [Place.Work]
	// when the session owns its workspace. It is recorded here as well as in
	// meta.json because the running process asks constantly and the file is
	// for the next process.
	Workspace string
	// Owned marks a session whose workspace is its own work/ directory —
	// opened outside any project, with nothing borrowed and nothing littered.
	Owned bool
}

// join answers a child of the session folder, or "" on the legacy zero Place.
func (p Place) join(parts ...string) string {
	if strings.TrimSpace(p.Dir) == "" {
		return ""
	}
	return filepath.Join(append([]string{p.Dir}, parts...)...)
}

// ID is the session's id, which is the folder's own name — the same 16-hex id
// the transcript header carries and [Meta.ID] records. It is arithmetic on
// [Place.Dir] like every other method here: the name IS the identity, so a
// caller holding a folder never has to open a file to learn which session it
// is. The legacy zero Place answers "".
func (p Place) ID() string {
	if strings.TrimSpace(p.Dir) == "" {
		return ""
	}
	return filepath.Base(p.Dir)
}

// Transcript is the journal, and the flock that guards the session lives on it.
func (p Place) Transcript() string { return p.join(placeTranscript) }

// State is the BPE working-state file (Decision 22).
func (p Place) State() string { return p.join(placeState) }

// Card is the state card: what the work is FOR and where it stands, maintained
// by the post-turn extractor and rendered into every system prompt (card.go).
// It sits beside state.json rather than inside it because the two are written
// by different hands — the model's own `track`/`commit` tools fill state.json,
// nobody's tool fills this — and a file one of them corrupted would cost the
// other its record.
func (p Place) Card() string { return p.join(placeCard) }

// Tasks is the live graph checkpoint (Decision 19).
func (p Place) Tasks() string { return p.join(placeTasks) }

// MetaPath is the identity file a picker reads without opening the journal.
func (p Place) MetaPath() string { return p.join(placeMeta) }

// NodeJournals is where task nodes and their audits keep their transcripts —
// beside the conversation that commissioned them, not in a parallel tree.
func (p Place) NodeJournals() string { return p.join(placeNodeJournals) }

// Logs holds the droppings — job logs and stubbed tool results. Everything in it is
// re-creatable and carries the sweep's 7-day TTL; nothing in it is a
// deliverable.
func (p Place) Logs() string { return p.join(placeLogs) }

// Artifacts holds deliverables that have no natural home in the workspace —
// a generated image in a borrowed session lands here rather than littering
// the person's repo, and its row in the global index is how it is found.
// An owned session's deliverables land in work/ instead; this directory is
// the borrowed session's answer.
func (p Place) Artifacts() string { return p.join(placeArtifacts) }

// Trees holds the git worktrees, one per running node. Session deletion runs
// git worktree remove/prune against [Meta.Workspace] BEFORE this directory
// goes, or the repository is left holding registrations for paths that are
// gone.
func (p Place) Trees() string { return p.join(placeTrees) }

// Work is the owned session's workspace, and "" for a borrowed one: a
// borrowed session has no work/ at all, which is itself the record of which
// kind it was.
func (p Place) Work() string {
	if !p.Owned {
		return ""
	}
	return p.join(placeWork)
}

// Meta is one session's identity, written where a picker can read it without
// parsing a journal. It is a citation, not a copy: everything in it is
// recoverable from the transcript, and a session whose meta.json is missing
// or corrupt is a session with a blank row, never a session that will not
// open.
type Meta struct {
	// ID is the session's id — the same 16-hex id the transcript header
	// carries, and the folder's name.
	ID string `json:"id"`
	// Title is what a picker row says. Empty until something names the
	// session; an empty title marks a session the launch groom may reuse.
	Title string `json:"title,omitempty"`
	// Workspace is the REAL workspace path — the resolved git root for a
	// borrowed session, the work/ directory for an owned one. The encoded
	// bucket directory above the session folder is derived from it and is
	// NOT an identity; this field is.
	Workspace string `json:"workspace"`
	// LaunchDir is where the person actually stood when the session opened —
	// the repo subdirectory, or the temp dir whose presence marks the session
	// as sweepable litter.
	LaunchDir string `json:"launchDir,omitempty"`
	// Owned marks a session that owns its workspace (work/).
	Owned bool `json:"owned,omitempty"`
	// Model is the conversation's model at last save, for the picker row.
	Model string `json:"model,omitempty"`
	// Created is when the session was minted.
	Created time.Time `json:"created"`
	// LastUserAt is when the PERSON last said something. Resume order is on
	// this and deliberately not on file mtime: a background write touching a
	// file is not a person returning to a conversation
	// (internal/store/session_rooms.go holds the original of this law).
	LastUserAt time.Time `json:"lastUserAt,omitempty"`
	// SpentUSD and Tokens are WHAT THE TALKING HAS COST — this conversation's
	// own running total, every turn and every auxiliary call beside it, as
	// [Usage] holds it while the session is open.
	//
	// THEY ARE HERE SO THAT A READER CAN ANSWER WITHOUT OPENING THE JOURNAL.
	// The transcript's usage lines are the record and stay the record; a
	// session's price is recoverable from them and nothing else. But a surface
	// asking about every conversation on the machine (world.go) reads a folder,
	// a meta.json and a presence file per session and must not grow a transcript
	// scan per row — so the running total is stamped here at the end of every
	// turn (placemeta.go's [Agent.stampSpend]) and read from here.
	//
	// Zero is "nobody has said", exactly as an absent Title is, and every
	// surface draws nothing for it rather than $0.00 (the emptiness law).
	// Tokens is input plus output as ONE sum, which is the spelling
	// [TaskIndexEntry.Tokens] already uses for the same fact about a task.
	SpentUSD float64 `json:"spentUsd,omitempty"`
	Tokens   int     `json:"tokens,omitempty"`
	// Archived marks a conversation somebody PUT AWAY from home's resting
	// list: it leaves its project's block and gathers under home's one folded
	// archive line, reachable there and still found by search. It is the
	// person's own act (home's `e`) and its own undoing — nothing automatic
	// ever sets or clears it, and nothing else about the session changes.
	Archived bool `json:"archived,omitempty"`
}

// LoadMeta reads a session folder's identity. A missing file, an unparsable
// file, or a file with no id answers a zero Meta and no error — see [Meta] —
// and only an I/O failure that is not absence is worth reporting.
func LoadMeta(dir string) (Meta, error) {
	raw, err := os.ReadFile(filepath.Join(dir, placeMeta))
	if errors.Is(err, fs.ErrNotExist) {
		return Meta{}, nil
	}
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if json.Unmarshal(raw, &meta) != nil || strings.TrimSpace(meta.ID) == "" {
		return Meta{}, nil
	}
	// A TITLE THAT IS THE NAMER'S OWN INSTRUCTION IS NOT A TITLE ([healedTitle],
	// title.go), and this file is where that name landed: [Agent.stampTitle]
	// wrote it over the placeholder the person's own opening words were.
	//
	// So the bad name is dropped on the way IN — nothing on disk is rewritten,
	// because meta.json is a citation and not the record — and the placeholder
	// is read back out of the journal to stand in its place, exactly as it did
	// before the name arrived. Only a folder that actually holds one of these
	// pays for that read ([openingPlaceholder]), and the next thing said in the
	// session stamps the words back onto the file ([Agent.stampUserLocked]).
	if strings.TrimSpace(meta.Title) != "" && healedTitle(meta.Title) == "" {
		meta.Title = openingPlaceholder(dir)
	}
	return meta, nil
}

// SaveMeta writes the identity whole, temp-and-rename, never partially: a
// picker that reads a half-written meta.json would draw a phantom row.
// SetArchived marks or unmarks one conversation as put away, through the same
// meta file every other fact about the session rides. A folder with no
// conversation in it is refused rather than given a meta that claims one.
func SetArchived(dir string, archived bool) error {
	meta, err := LoadMeta(dir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(meta.ID) == "" {
		return fmt.Errorf("no conversation at %s", dir)
	}
	if meta.Archived == archived {
		return nil
	}
	meta.Archived = archived
	return SaveMeta(dir, meta)
}

func SaveMeta(dir string, meta Meta) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("save session meta: no session directory")
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("save session meta: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("save session meta: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".meta-*.json")
	if err != nil {
		return fmt.Errorf("save session meta: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("save session meta: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("save session meta: %w", err)
	}
	if err := os.Rename(name, filepath.Join(dir, placeMeta)); err != nil {
		os.Remove(name)
		return fmt.Errorf("save session meta: %w", err)
	}
	return nil
}
