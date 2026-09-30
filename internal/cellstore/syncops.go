package cellstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Names the engine reads its two secrets from when it is spawned.
const (
	cellKeyEnv = "FURROW_CELL_KEY"
	dedupEnv   = "FURROW_DEDUP_SECRET"
)

// SyncKeys are the identity's two secrets. They are handed to each verb and
// never stored: not on argv, not on disk.
type SyncKeys struct{ CellKey, Dedup []byte }

// LedgerName names the published ledger of one (sync URL, identity) pair, so
// pointing at a new or wiped store starts an empty ledger.
func LedgerName(syncURL, identity string) string {
	sum := sha256.Sum256([]byte(syncURL + "\n" + identity))
	return hex.EncodeToString(sum[:8])
}

// keyArgs is the pair of secrets as they travel: hex, in the daemon's JSON
// arguments or in the spawned engine's environment. Embedding it gives an Op
// both spellings at once.
type keyArgs struct {
	CellKey string `json:"cell_key"`
	Dedup   string `json:"dedup"`
}

func hexKeys(k SyncKeys) keyArgs {
	return keyArgs{CellKey: hex.EncodeToString(k.CellKey), Dedup: hex.EncodeToString(k.Dedup)}
}

// Env implements EnvOp.
func (k keyArgs) Env() []string {
	return []string{cellKeyEnv + "=" + k.CellKey, dedupEnv + "=" + k.Dedup}
}

// exportOp seals what a ledger has not yet recorded into frames in Outbox.
type exportOp struct {
	Head     string `json:"head"`
	Outbox   string `json:"outbox"`
	Ledger   string `json:"ledger"`
	MaxFrame int    `json:"max_frame,omitempty"` // omitted when 0, so the engine picks its default: a literal 0 would give every object its own frame
	keyArgs
}

func (exportOp) Verb() string { return "export" }

func (o exportOp) Args() []string {
	args := []string{"--json", "export", "--head", o.Head, "--outbox", o.Outbox, "--ledger", o.Ledger}
	if o.MaxFrame > 0 {
		args = append(args, "--max-frame", strconv.Itoa(o.MaxFrame))
	}
	return args
}

// publishedOp records uploaded frames in the ledger and deletes them.
type publishedOp struct {
	Ledger string   `json:"ledger"`
	Frames []string `json:"frames"`
}

func (publishedOp) Verb() string { return "published" }

func (o publishedOp) Args() []string {
	return append([]string{"--json", "published", "--ledger", o.Ledger}, o.Frames...)
}

// wantOp asks which remote ids complete a head locally.
type wantOp struct {
	Head string `json:"head"`
	keyArgs
}

func (wantOp) Verb() string { return "want" }

func (o wantOp) Args() []string { return []string{"--json", "want", "--head", o.Head} }

// importOp opens the objects fetched into Inbox and stores them.
type importOp struct {
	Head  string `json:"head"`
	Inbox string `json:"inbox"`
	// Ledger is the store the objects were fetched from: what it handed over
	// is recorded as published there, so the next export never sends it back.
	Ledger string `json:"ledger"`
	keyArgs
}

func (importOp) Verb() string { return "import" }

func (o importOp) Args() []string {
	return []string{"--json", "import", "--head", o.Head, "--inbox", o.Inbox, "--ledger", o.Ledger}
}

// materializeOp restores a head into the target tree.
type materializeOp struct {
	Head string `json:"head"`
}

func (materializeOp) Verb() string { return "materialize" }

// composing marks the verb that reads the tree's composed .cell/ entry: the
// other four work on the store alone and the engine refuses a --cell-dir there.
type composing interface{ composesCellDir() }

func (materializeOp) composesCellDir() {}

func (o materializeOp) Args() []string { return []string{"--json", "materialize", "--head", o.Head} }

// FrameFile is one frame the engine wrote to the outbox.
type FrameFile struct {
	Path    string `json:"path"`
	Objects int    `json:"objects"`
	Bytes   int64  `json:"bytes"`
}

// Export is what one export produced. Its shape is the sync engine seam's
// (cellsync.Engine), which cellstore may not import: the sync package names
// these types as aliases so that SyncEngine satisfies its interface.
type Export struct {
	Frames  []FrameFile `json:"frames"`
	HeadRID string      `json:"head_rid"`
	Objects int         `json:"objects"`
	Bytes   int64       `json:"bytes"`
}

// SyncEngine is the Go half of the sync engine: five verbs over a Transport,
// each on the cell's own device-local store.
type SyncEngine struct {
	Transport Transport
	Keys      SyncKeys
	// Ledger is LedgerName(sync URL, identity id).
	Ledger string
	// Target says where a cell's store and tree are.
	Target func(c cell.Cell) Target
	// Inbox is the device-local scratch directory a cell's fetched objects land in.
	Inbox func(c cell.Cell) string
	// MaxFrame bounds a frame's size; 0 takes the engine's default.
	MaxFrame int
	// Outbox is where a cell's frames are written; nil means a directory
	// beside the cell's store.
	Outbox func(c cell.Cell) string
}

// Export implements the sync seam.
func (e SyncEngine) Export(ctx context.Context, c cell.Cell, head string) (Export, error) {
	var out Export
	op := exportOp{Head: head, Outbox: e.outbox(c), Ledger: e.Ledger, MaxFrame: e.MaxFrame, keyArgs: hexKeys(e.Keys)}
	return out, e.ask(ctx, c, op, &out)
}

// Published implements the sync seam.
func (e SyncEngine) Published(ctx context.Context, c cell.Cell, frames []string) error {
	return e.ask(ctx, c, publishedOp{Ledger: e.Ledger, Frames: frames}, nil)
}

// Want implements the sync seam.
func (e SyncEngine) Want(ctx context.Context, c cell.Cell, head string) ([]string, error) {
	var out struct {
		Want []string `json:"want"`
	}
	return out.Want, e.ask(ctx, c, wantOp{Head: head, keyArgs: hexKeys(e.Keys)}, &out)
}

// Import implements the sync seam.
func (e SyncEngine) Import(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	var out struct {
		Imported int `json:"imported"`
	}
	return out.Imported, e.ask(ctx, c, importOp{Head: head, Inbox: inbox, Ledger: e.Ledger, keyArgs: hexKeys(e.Keys)}, &out)
}

// Materialize implements the sync seam.
func (e SyncEngine) Materialize(ctx context.Context, c cell.Cell, head string) error {
	if err := e.ask(ctx, c, materializeOp{Head: head}, nil); err != nil {
		return err
	}
	return noteAdopted(c, head)
}

// targetFor is the cell's target as op needs it: only a composing verb is
// told where the composed .cell/ directory is.
func (e SyncEngine) targetFor(c cell.Cell, op Op) Target {
	t := e.Target(c)
	if _, ok := op.(composing); !ok {
		t.CellDir = ""
	}
	return t
}

// outbox is the cell's frame directory: the caller's choice, else a sibling
// of the store so frames stay on the store's filesystem.
func (e SyncEngine) outbox(c cell.Cell) string {
	if e.Outbox != nil {
		return e.Outbox(c)
	}
	return e.Target(c).DataDir + ".outbox"
}

// ask runs one verb and decodes its ok object into out (nil ignores it). Every
// error names the verb and the cell, because a person is told which chat failed.
func (e SyncEngine) ask(ctx context.Context, c cell.Cell, op Op, out any) error {
	raw, err := e.Transport.Do(ctx, e.targetFor(c, op), op)
	if err != nil {
		return fmt.Errorf("sync %s of cell %s: %w", op.Verb(), c.ID, err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("sync %s of cell %s: unreadable answer: %w", op.Verb(), c.ID, err)
	}
	return nil
}

// Sync is the sync engine over this store: the same transport and the same
// per-cell directories the seals use, so an export reads exactly what a seal
// wrote. The inbox is device-local scratch beside the cell's store.
func (e Engine) Sync(keys SyncKeys, ledger string) SyncEngine {
	return SyncEngine{
		Transport: e.transport(),
		Keys:      keys,
		Ledger:    ledger,
		Target: func(c cell.Cell) Target {
			return Target{Tree: e.tree(c), DataDir: e.LocalDir(c), CellDir: e.cellDir(c)}
		},
		Inbox: func(c cell.Cell) string { return e.LocalDir(c) + ".inbox" },
	}
}
