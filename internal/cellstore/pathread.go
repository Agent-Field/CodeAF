package cellstore

// ── READING ONE FILE OF A CHAT THIS COMPUTER DOES NOT HOLD ──────────────────
//
// A chat another computer holds is a sealed snapshot on the relay: its project
// folder, its .cell/ state and its transcript, all in one object graph. The
// only reader that graph had was a take, and a take wants EVERY object the head
// reaches — the whole project folder, which can be gigabytes — before it shows
// a single line. Showing a person what was said in a chat must not cost them
// the chat's project folder.
//
// So the three verbs below ask the engine for ONE PATH of a head and, for a
// file, ONE BYTE RANGE of it:
//
//   - wantPath names the objects on the way to the path (the snapshot, the
//     trees above it, the file's own index object) and, with a range, only the
//     chunks that overlap it. The engine chunks files by their content, so a
//     transcript that grew by a turn keeps every earlier chunk byte for byte:
//     reading it again downloads its index and its new tail, never its past.
//   - peek says what the store holds of the path without the network.
//   - read writes the bytes of a range out of held chunks and restores nothing.
//
// THE OBJECTS LAND IN THE CHAT'S OWN STORE, the one a take of that chat fetches
// into, through the same import a take uses. A person who reads a chat and then
// continues it here pays for each object once: the take's whole-graph want
// finds the transcript's objects already held and asks only for the rest.
//
// NOTHING HERE LEASES, MATERIALIZES OR NEEDS THE CHAT'S FOLDER TO EXIST. A read
// is a spectator: the chat stays where it is held, nothing is written outside
// codeaf's own store, and the relay and its encryption are exactly as they were.
// DO NOT "SIMPLIFY" A READ INTO A WHOLE-HEAD FETCH OR A MATERIALIZE — that is
// the cost this exists to avoid (docs/design/remote-read/DESIGN.md).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// ErrIncomplete is the engine saying an object on the way to what was asked is
// not in the store yet: the caller wants and imports it, then asks again.
var ErrIncomplete = errors.New("store does not hold every object this read needs")

// ErrNoPath is the engine saying the snapshot has nothing at the path asked: a
// chat sealed before its transcript existed, say. It is an answer, not a fault.
var ErrNoPath = errors.New("the snapshot has nothing at that path")

// PeekChunk is one chunk of a file as the store holds it: the bytes [Start,
// End) of the file, and whether this computer already has them.
type PeekChunk struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Held  bool  `json:"held"`
}

// PeekEntry is one name in a directory a peek looked at.
type PeekEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Len  int64  `json:"len"`
}

// Peek is what the store knows of one path of a head, read without the network:
// a file's length and the chunks it is cut into, or a directory's names. The
// chunk boundaries are what let a reader fetch a page of a transcript and not
// the transcript.
type Peek struct {
	Kind    string      `json:"kind"`
	Len     int64       `json:"len"`
	Chunks  []PeekChunk `json:"chunks,omitempty"`
	Entries []PeekEntry `json:"entries,omitempty"`
}

// wantPathOp is want narrowed to one path and, when Start and End are set,
// one byte range of it. The daemon reads start/end; the command line spells them
// --range S:E.
type wantPathOp struct {
	Head  string `json:"head"`
	Path  string `json:"path"`
	Start *int64 `json:"start,omitempty"`
	End   *int64 `json:"end,omitempty"`
	keyArgs
}

func (wantPathOp) Verb() string { return "want" }

func (o wantPathOp) Args() []string {
	args := []string{"--json", "want", "--head", o.Head, "--path", o.Path}
	if o.Start != nil && o.End != nil {
		args = append(args, "--range", strconv.FormatInt(*o.Start, 10)+":"+strconv.FormatInt(*o.End, 10))
	}
	return args
}

// peekOp asks what the store holds of one path, never the network.
type peekOp struct {
	Head string `json:"head"`
	Path string `json:"path"`
	keyArgs
}

func (peekOp) Verb() string { return "peek" }

func (o peekOp) Args() []string {
	return []string{"--json", "peek", "--head", o.Head, "--path", o.Path}
}

// readOp writes bytes [Start, End) of one file to Out from held chunks.
type readOp struct {
	Head  string `json:"head"`
	Path  string `json:"path"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Out   string `json:"out"`
	keyArgs
}

func (readOp) Verb() string { return "read" }

func (o readOp) Args() []string {
	return []string{"--json", "read", "--head", o.Head, "--path", o.Path,
		"--range", strconv.FormatInt(o.Start, 10) + ":" + strconv.FormatInt(o.End, 10), "--out", o.Out}
}

// WantPath names the remote ids the store lacks on the way to path at head,
// and — when ranged — the chunks of the file that overlap [start, end). Empty
// means everything that scope needs is held. It is the narrow half of Want: it
// never walks the project folder.
func (e SyncEngine) WantPath(ctx context.Context, c cell.Cell, head, path string, ranged bool, start, end int64) ([]string, error) {
	op := wantPathOp{Head: head, Path: path, keyArgs: hexKeys(e.Keys)}
	if ranged {
		op.Start, op.End = &start, &end
	}
	var out struct {
		Want []string `json:"want"`
	}
	return out.Want, e.askRead(ctx, c, op, &out)
}

// Peek implements the read seam: what the store holds of path at head.
func (e SyncEngine) Peek(ctx context.Context, c cell.Cell, head, path string) (Peek, error) {
	var out Peek
	return out, e.askRead(ctx, c, peekOp{Head: head, Path: path, keyArgs: hexKeys(e.Keys)}, &out)
}

// ReadRange writes bytes [start, end) of path at head to out from held chunks
// and answers how many it wrote.
func (e SyncEngine) ReadRange(ctx context.Context, c cell.Cell, head, path string, start, end int64, out string) (int64, error) {
	var answer struct {
		Bytes int64 `json:"bytes"`
	}
	op := readOp{Head: head, Path: path, Start: start, End: end, Out: out, keyArgs: hexKeys(e.Keys)}
	return answer.Bytes, e.askRead(ctx, c, op, &answer)
}

// ImportPath is Import for a read: the same verb, the same store and ledger,
// so what a read brings is exactly what a take would have imported, but run
// from the store root (askRead) because the chat's folder does not exist here.
func (e SyncEngine) ImportPath(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	op := importOp{Head: head, Inbox: inbox, Ledger: e.Ledger, keyArgs: hexKeys(e.Keys)}
	var out struct {
		Imported int `json:"imported"`
	}
	return out.Imported, e.askRead(ctx, c, op, &out)
}

// askRead is ask for the read verbs and the read's import, against the chat's own store but
// never its folder. The tree a verb names is only the working directory of a
// spawned engine (none of the three reads it), and the chat's folder does not
// exist on a computer that only reads the chat — creating it to have somewhere
// to stand would be exactly the trace on disk a read must not leave. So the
// engine stands in codeaf's own store root. The engine's refusal is mapped to
// the two errors a reader acts on.
func (e SyncEngine) askRead(ctx context.Context, c cell.Cell, op Op, out any) error {
	t := e.Target(c)
	t.CellDir = ""
	t.Tree = filepath.Dir(t.DataDir)
	if err := os.MkdirAll(t.Tree, 0o700); err != nil {
		return err
	}
	raw, err := e.Transport.Do(ctx, t, op)
	if err != nil {
		return readRefusal(op.Verb(), c, err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return wrapVerb(op.Verb(), c, errors.New("unreadable answer: "+err.Error()))
	}
	return nil
}

// readRefusal maps the engine's JSON error code onto ErrIncomplete or ErrNoPath,
// keeping its words. The engine spells the refusal as one JSON object, whole
// on stderr or as the daemon's err; the codes are matched, not the prose.
func readRefusal(verb string, c cell.Cell, err error) error {
	text := err.Error()
	switch {
	case strings.Contains(text, `"code":"incomplete"`):
		return wrapVerb(verb, c, errors.Join(ErrIncomplete, err))
	case strings.Contains(text, `"code":"not_found"`):
		return wrapVerb(verb, c, errors.Join(ErrNoPath, err))
	}
	return wrapVerb(verb, c, err)
}

func wrapVerb(verb string, c cell.Cell, err error) error {
	return &verbError{verb: verb, cell: c.ID, err: err}
}

type verbError struct {
	verb, cell string
	err        error
}

func (v *verbError) Error() string {
	return "sync " + v.verb + " of cell " + v.cell + ": " + v.err.Error()
}
func (v *verbError) Unwrap() error { return v.err }

// ── WHETHER THE ENGINE CAN READ A PATH ──────────────────────────────────────

// readVerb is the capability the read door needs: an engine whose
// capabilities list names it knows want --path, peek and read.
const readVerb = "read"

// pathReads remembers the answer per engine path, like probed: one spawn per
// process, not one per hello.
var pathReads sync.Map // path -> bool

// ReadsPaths reports whether the engine codeaf seals with can read one path of
// a head. It ASKS THE ENGINE (furrow --json capabilities) rather than trying a
// read and failing, because a capability that cannot work is absent: the app
// is never offered chats.read by an engine that would refuse every call. An
// engine too old to have a capabilities verb exits non-zero, and is absent.
func ReadsPaths() bool {
	bin, err := sealingEngine()
	if err != nil {
		return false
	}
	return ReadsPathsAt(bin)
}

// ReadsPathsAt is ReadsPaths for one engine binary.
func ReadsPathsAt(bin string) bool {
	if known, ok := pathReads.Load(bin); ok {
		return known.(bool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "--json", "capabilities") //codeaf:plumbing capability probe of the sealing engine
	cmd.Stdout = &out
	can := false
	if cmd.Run() == nil {
		var doc struct {
			Verbs []string `json:"verbs"`
		}
		if json.Unmarshal(bytes.TrimSpace(out.Bytes()), &doc) == nil {
			for _, v := range doc.Verbs {
				can = can || v == readVerb
			}
		}
	}
	pathReads.Store(bin, can)
	return can
}
