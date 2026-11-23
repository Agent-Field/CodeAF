package cellstore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// LocalWorkFast performs the cheap in-process portion of LocalWork without
// spawning a furrow subprocess (F2). When known is true, dirty is the final
// answer; when known is false (for example, stat-index.json is missing or
// stale), the full check must run before the workspace is swapped in.
func (e Engine) LocalWorkFast(ctx context.Context, c cell.Cell) (dirty bool, known bool, err error) {
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil {
		return false, true, err
	}
	// Completed WAL calls remain local work even when their capture fails.
	if err := Captured(ctx, c); err != nil && len(recovery.Completed) == 0 {
		return false, true, err
	}
	if len(recovery.Completed) > 0 {
		log.Printf("continuation needs a choice: %d completed calls have not been saved", len(recovery.Completed))
		return true, true, nil
	}
	base, err := sharedBoundary(c)
	if err != nil {
		return false, true, err
	}
	if base == "" {
		return true, true, nil
	}
	head, err := Head(c)
	if err != nil || head == nil {
		return head == nil, true, err
	}
	if work, err := transcriptWork(c, head.Receipt.Transcript.End, sizeOf(rel(c, cell.TranscriptPath))); err != nil || work {
		return work, true, err
	}
	if head.Turn.ID != base {
		work, err := unsharedCalls(c, base)
		if err != nil || work {
			return work, true, err
		}
		return false, false, nil
	}
	if e.checkMaterializedStat(c, base) {
		return false, true, nil
	}
	return false, false, nil
}

// LocalWork says whether continuing a remote chat needs a local-work choice.
// Startup can seal unchanged files with fresh bookkeeping, which is a local
// save but not something a person needs to keep as a branch. Compare
// against the shared boundary as well as the current head, so an editor's work
// captured by that startup save is never mistaken for mere bookkeeping.
func (e Engine) LocalWork(ctx context.Context, c cell.Cell) (bool, error) {
	dirty, known, err := e.LocalWorkFast(ctx, c)
	if err != nil || known {
		return dirty, err
	}
	base, err := sharedBoundary(c)
	if err != nil {
		return false, err
	}
	head, err := Head(c)
	if err != nil || head == nil {
		return head == nil, err
	}
	if e.FolderGone(c) {
		// A FOLDER THAT IS NOT THERE HOLDS NO LOCAL FILES, SO ONLY THE
		// CONVERSATION CAN BE WORK. The screen and the plan below both read the
		// folder, and the engine answers a missing one with an error, which a
		// launch used to show as a chat that "could not be put in this folder"
		// on every start until the person made the folder again by hand.
		log.Printf("continuation: the chat's folder %s is gone, so there are no local files to keep; checking only the conversation", e.tree(c))
		same, err := e.sameTranscriptPrefix(ctx, c, base)
		return !same, err
	}
	if err := e.screenCapture(ctx, c); err != nil {
		return false, err
	}
	// The shared snapshot belongs to this chat. A workspace's latest head can
	// belong to another chat that opened in the same working folder.
	out, err := e.plan(ctx, c, base)
	if err != nil {
		return false, err
	}
	work, err := e.workInPlan(ctx, c, base, out)
	if err != nil || work {
		return work, err
	}
	same, err := e.sameTranscriptPrefix(ctx, c, base)
	if err == nil && same && head.Turn.ID == base {
		e.recordMaterializedStat(c, base)
	}
	return !same, err
}

// FolderGone says the folder the chat's files live in is not on this computer:
// the path does not exist, or it is a link whose target does not. ONLY
// "DOES NOT EXIST" COUNTS. A path that exists but is not a folder, or one this
// computer may not look at, is a refusal the check still reports, because
// reading it as "nothing to keep" could throw away edits that are there.
func (e Engine) FolderGone(c cell.Cell) bool {
	w := e.workspace(c)
	if w == "" {
		return false
	}
	_, err := os.Stat(w)
	return errors.Is(err, fs.ErrNotExist)
}

// workInPlan asks Git about semantic index entries only when the engine found
// nothing beyond its directory and index. Other changed paths stay a choice.
func (e Engine) workInPlan(ctx context.Context, c cell.Cell, base string, raw []byte) (bool, error) {
	work, indexOnly, _, err := localPlanChanges(raw)
	if err != nil || !indexOnly {
		return work, err
	}
	same, err := e.sameIndex(ctx, base, e.target(c))
	return !same, err
}

// sharedBoundary retains the existing older-installation adoption fallback.
func sharedBoundary(c cell.Cell) (string, error) {
	raw, err := os.ReadFile(rel(c, publishedPath))
	if errors.Is(err, os.ErrNotExist) {
		if adopted, ok := readAdoption(c); ok {
			return adopted.Head, nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var note struct {
		Head string `json:"head"`
	}
	err = json.Unmarshal(raw, &note)
	return note.Head, err
}

// unsharedCalls preserves effects outside the workspace, even when a command
// changed no files. An unfamiliar boundary remains a choice, never a discard.
func unsharedCalls(c cell.Cell, base string) (bool, error) {
	turns, err := Turns(c)
	if err != nil {
		return false, err
	}
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		if turn.ID == base {
			return false, nil
		}
		receipt, err := readReceipt(c, turn.Receipt)
		if err != nil {
			return false, err
		}
		if len(receipt.Calls) > 0 {
			return true, nil
		}
		if work, err := transcriptWork(c, receipt.Transcript.Start, receipt.Transcript.End); err != nil || work {
			return work, err
		}
		if turn.Parent == base {
			return false, nil
		}
	}
	return true, nil
}

// transcriptWork distinguishes a conversation change from the timing and
// auxiliary accounting appended after a reply's shared seal. Unknown records
// and a shortened journal remain work, so a failed reading never loses edits.
func transcriptWork(c cell.Cell, start, end int64) (bool, error) {
	if start == end {
		return false, nil
	}
	if end < start {
		return true, nil
	}
	file, err := os.Open(rel(c, cell.TranscriptPath))
	if err != nil {
		return false, err
	}
	defer file.Close()
	scan := bufio.NewScanner(io.NewSectionReader(file, start, end-start))
	scan.Buffer(make([]byte, 4096), 8<<20)
	for scan.Scan() {
		var record struct {
			Type  string `json:"type"`
			Error struct {
				Role string `json:"role"`
			} `json:"error"`
			Failure struct {
				Role string `json:"role"`
			} `json:"failure"`
		}
		if json.Unmarshal(scan.Bytes(), &record) != nil {
			return true, nil
		}
		switch {
		case record.Type == "pace":
		case record.Type == "usage":
		case record.Type == "call", record.Type == "title":
		case record.Type == "error" && auxiliaryRole(record.Error.Role):
		case record.Type == "failure" && auxiliaryRole(record.Failure.Role):
		default:
			return true, nil
		}
	}
	return false, scan.Err()
}

func auxiliaryRole(role string) bool {
	return role == "title" || role == "memory" || role == "reflex"
}

// localPlanWork ignores only reproducible capture bookkeeping. Conversation
// truth, unknown state and all workspace files still count as local work.
func localPlanWork(raw []byte) (bool, bool, error) {
	work, indexOnly, _, err := localPlanChanges(raw)
	return work, indexOnly, err
}

func localPlanChanges(raw []byte) (work bool, indexOnly bool, transcriptChanged bool, err error) {
	var plan struct {
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return false, false, false, err
	}
	for _, change := range plan.Changes {
		name := path.Clean(change.Path)
		if name == cell.StateDir || strings.HasPrefix(name, ReceiptsDir+"/") || name == ReceiptsDir || strings.HasPrefix(name, cell.EnvPath+"/") || name == cell.EnvPath {
			continue
		}
		switch name {
		case TurnsPath, publishedPath, adoptedPath, cell.CopyOfferedPath, cell.CopyReadyPath:
			continue
		case cell.TranscriptPath:
			// The shared prefix is checked before a changed journal is discarded.
			transcriptChanged = true
			continue
		case cell.StateDir + "/memories.jsonl":
			continue
		}
		if name == ".git" || name == ".git/index" {
			indexOnly = true
			continue
		}
		log.Printf("continuation needs a choice for changed path: %s", name)
		return true, false, transcriptChanged, nil
	}
	return indexOnly, indexOnly, transcriptChanged, nil
}

// sameTranscriptPrefix reads the shared journal into private scratch. Only an
// unchanged prefix followed by accounting can be discarded as bookkeeping.
func (e Engine) sameTranscriptPrefix(ctx context.Context, c cell.Cell, base string) (bool, error) {
	scratch, err := os.MkdirTemp("", "codeaf-journal-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(scratch)
	target := e.target(c)
	target.Tree, target.CellDir, target.Timeline = scratch, "", filepath.Base(scratch)
	transport := Spawn{Binary: e.Binary}
	if _, err := transport.Do(ctx, target, indexScratchOp{}); err != nil {
		return false, err
	}
	defer func() { _, _ = transport.Do(ctx, target, indexScratchOp{forget: true}) }()
	if _, err := transport.Do(ctx, target, restoreOp{Snapshot: base, Paths: []string{cell.TranscriptPath}}); err != nil {
		return false, err
	}
	saved, err := os.ReadFile(filepath.Join(scratch, cell.TranscriptPath))
	if err != nil {
		return false, err
	}
	file, err := os.Open(rel(c, cell.TranscriptPath))
	if err != nil {
		return false, err
	}
	defer file.Close()
	current := make([]byte, len(saved))
	if _, err := io.ReadFull(file, current); errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return bytes.Equal(saved, current), nil
}

type statEntry struct {
	Size    int64  `json:"s"`
	ModNano int64  `json:"m"`
	Mode    uint32 `json:"p"`
}

type statIndex struct {
	Snapshot       string               `json:"snapshot"`
	Tree           string               `json:"tree"`
	TranscriptEnd  int64                `json:"transcript_end"`
	TranscriptMod  int64                `json:"transcript_mod"`
	TranscriptHash string               `json:"transcript_hash,omitempty"`
	Entries        map[string]statEntry `json:"entries"`
}

func transcriptPrefixHash(path string, end int64) (string, bool) {
	if end < 0 {
		return "", false
	}
	if end == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:]), true
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.CopyN(h, f, end)
	if err != nil || n != end {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// StatIndexPath returns the path to c's materialized stat index file.
func (e Engine) StatIndexPath(c cell.Cell) string {
	return e.statIndexPath(c)
}

func (e Engine) statIndexPath(c cell.Cell) string {
	return filepath.Join(e.LocalDir(c), "stat-index.json")
}

func skipStatRel(relSlash string, d fs.DirEntry) (skip bool, skipDir bool) {
	if relSlash == "." {
		return true, false
	}
	if relSlash == cell.StateDir || strings.HasPrefix(relSlash, cell.StateDir+"/") {
		return true, d.IsDir()
	}
	if relSlash == ".furrow" || strings.HasPrefix(relSlash, ".furrow/") {
		return true, d.IsDir()
	}
	if relSlash == ".git" {
		return true, false
	}
	if strings.HasPrefix(relSlash, ".git/") && relSlash != ".git/index" {
		return true, d.IsDir()
	}
	return false, false
}

// RecordMaterializedStat snapshots the stat metadata (size, mode, mtime) of c's
// materialized tree so a subsequent LocalWork check can verify cleanliness by
// stat without full file hashing on the critical path (R4).
func (e Engine) RecordMaterializedStat(c cell.Cell, snapshot string) {
	e.recordMaterializedStat(c, snapshot)
}

func (e Engine) recordMaterializedStat(c cell.Cell, snapshot string) {
	if snapshot == "" {
		return
	}
	tree := canonicalTree(e.tree(c))
	if tree == "" {
		return
	}
	entries, ok := stampTree(tree)
	if !ok {
		return
	}
	e.writeStatIndex(c, snapshot, tree, entries)
}

// stampWalks counts the walks that stat every file of a chat's folder, so a test
// can state the cost of a seal as a number: ONE WALK PER SEAL, which is what the
// stat index has always cost and what catching an in-place edit may not add to
// (stampscan.go). It is a counter and not a clock because wall time is a
// property of the machine and a count is a property of the code.
var stampWalks atomic.Int64

// stampTree is the stat record of every file under tree that the stat index
// covers: size, modified time and mode, and nothing that needs a file opened.
// It answers false when the walk could not finish, which is "no record" and
// never "an empty folder".
func stampTree(tree string) (map[string]statEntry, bool) {
	stampWalks.Add(1)
	entries := map[string]statEntry{}
	err := filepath.WalkDir(tree, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, rErr := filepath.Rel(tree, p)
		if rErr != nil {
			return rErr
		}
		relSlash := filepath.ToSlash(r)
		if skip, skipDir := skipStatRel(relSlash, d); skip {
			if skipDir {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, iErr := d.Info()
		if iErr != nil {
			return iErr
		}
		entries[relSlash] = statEntry{
			Size:    info.Size(),
			ModNano: info.ModTime().UnixNano(),
			Mode:    uint32(info.Mode()),
		}
		return nil
	})
	return entries, err == nil
}

// writeStatIndex records entries as the stat record of snapshot's folder, with
// the chat's own journal beside them. The files' stamps are whatever the caller
// took; the journal's are read here, at the end of the seal that wrote it.
func (e Engine) writeStatIndex(c cell.Cell, snapshot, tree string, entries map[string]statEntry) {
	tPath := rel(c, cell.TranscriptPath)
	var tEnd, tMod int64
	var tHash string
	if tInfo, err := os.Stat(tPath); err == nil {
		tEnd = tInfo.Size()
		tMod = tInfo.ModTime().UnixNano()
		if head, hErr := Head(c); hErr == nil && head != nil && head.Turn.ID == snapshot && head.Receipt.Transcript.End > 0 && head.Receipt.Transcript.End <= tEnd {
			tEnd = head.Receipt.Transcript.End
		}
		tHash, _ = transcriptPrefixHash(tPath, tEnd)
	}
	idx := statIndex{
		Snapshot:       snapshot,
		Tree:           tree,
		TranscriptEnd:  tEnd,
		TranscriptMod:  tMod,
		TranscriptHash: tHash,
		Entries:        entries,
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		return
	}
	_ = writeFile(e.statIndexPath(c), raw)
}

func (e Engine) rebindMaterializedStat(to cell.Cell) {
	p := e.statIndexPath(to)
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var idx statIndex
	if json.Unmarshal(raw, &idx) != nil {
		return
	}
	idx.Tree = canonicalTree(e.tree(to))
	if tInfo, err := os.Stat(rel(to, cell.TranscriptPath)); err == nil && tInfo.Size() == idx.TranscriptEnd {
		idx.TranscriptMod = tInfo.ModTime().UnixNano()
	}
	if out, err := json.Marshal(idx); err == nil {
		_ = writeFile(p, out)
	}
}

func (e Engine) checkMaterializedStat(c cell.Cell, base string) bool {
	if e.Run != nil {
		return false
	}
	raw, err := os.ReadFile(e.statIndexPath(c))
	if err != nil {
		return false
	}
	var idx statIndex
	if json.Unmarshal(raw, &idx) != nil {
		return false
	}
	tree := canonicalTree(e.tree(c))
	if idx.Snapshot != base || idx.Tree != tree {
		return false
	}
	tPath := rel(c, cell.TranscriptPath)
	tInfo, err := os.Stat(tPath)
	if err != nil {
		if idx.TranscriptEnd != 0 || !os.IsNotExist(err) {
			return false
		}
	} else if tInfo.Size() == idx.TranscriptEnd && tInfo.ModTime().UnixNano() == idx.TranscriptMod {
	} else if tInfo.Size() >= idx.TranscriptEnd && idx.TranscriptHash != "" {
		if gotHash, ok := transcriptPrefixHash(tPath, idx.TranscriptEnd); !ok || gotHash != idx.TranscriptHash {
			return false
		}
	} else {
		return false
	}
	seen := 0
	mismatch := false
	err = filepath.WalkDir(tree, func(p string, d fs.DirEntry, wErr error) error {
		if wErr != nil {
			mismatch = true
			return wErr
		}
		r, rErr := filepath.Rel(tree, p)
		if rErr != nil {
			mismatch = true
			return rErr
		}
		relSlash := filepath.ToSlash(r)
		if skip, skipDir := skipStatRel(relSlash, d); skip {
			if skipDir {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, iErr := d.Info()
		if iErr != nil {
			mismatch = true
			return iErr
		}
		exp, ok := idx.Entries[relSlash]
		if !ok || info.Size() != exp.Size || info.ModTime().UnixNano() != exp.ModNano || uint32(info.Mode()) != exp.Mode {
			mismatch = true
			return fs.SkipAll
		}
		seen++
		return nil
	})
	return err == nil && !mismatch && seen == len(idx.Entries)
}
