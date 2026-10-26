package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// LocalWork says whether continuing a remote chat needs a local-work choice.
// Startup can seal unchanged files with fresh bookkeeping, which is a local
// save but not something a person needs to keep as a branch. Compare
// against the shared boundary as well as the current head, so an editor's work
// captured by that startup save is never mistaken for mere bookkeeping.
func (e Engine) LocalWork(ctx context.Context, c cell.Cell) (bool, error) {
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil {
		return false, err
	}
	if len(recovery.Completed) > 0 {
		log.Printf("continuation needs a choice: %d completed calls have not been saved", len(recovery.Completed))
		return true, nil
	}
	base, err := sharedBoundary(c)
	if err != nil {
		return false, err
	}
	if base == "" {
		return true, nil
	}
	head, err := Head(c)
	if err != nil || head == nil {
		return head == nil, err
	}
	if work, err := transcriptWork(c, head.Receipt.Transcript.End, sizeOf(rel(c, cell.TranscriptPath))); err != nil || work {
		return work, err
	}
	if head.Turn.ID != base {
		work, err := unsharedCalls(c, base)
		if err != nil || work {
			return work, err
		}
	}
	if err := e.screenCapture(ctx, c); err != nil {
		return false, err
	}
	// The shared snapshot belongs to this chat. A workspace's latest head can
	// belong to another chat that opened in the same working folder.
	out, err := (Spawn{Binary: e.Binary}).Do(ctx, e.target(c), planOp{Snapshot: base})
	if err != nil {
		return false, err
	}
	return e.workInPlan(ctx, c, base, out)
}

// workInPlan asks Git about semantic index entries only when the engine found
// nothing beyond its directory and index. Other changed paths stay a choice.
func (e Engine) workInPlan(ctx context.Context, c cell.Cell, base string, raw []byte) (bool, error) {
	work, indexOnly, err := localPlanWork(raw)
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
		if len(receipt.Calls) > 0 || len(receipt.ModelCalls) > 0 {
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
			Usage struct {
				Aux bool `json:"aux"`
			} `json:"usage"`
			Call struct {
				Role string `json:"role"`
			} `json:"call"`
		}
		if json.Unmarshal(scan.Bytes(), &record) != nil {
			return true, nil
		}
		switch {
		case record.Type == "pace":
		case record.Type == "usage" && record.Usage.Aux:
		case record.Type == "call" && record.Call.Role != "":
		default:
			return true, nil
		}
	}
	return false, scan.Err()
}

// localPlanWork ignores only reproducible capture bookkeeping. Conversation
// truth, unknown state and all workspace files still count as local work.
func localPlanWork(raw []byte) (bool, bool, error) {
	var plan struct {
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return false, false, err
	}
	indexOnly := false
	for _, change := range plan.Changes {
		name := path.Clean(change.Path)
		if name == cell.StateDir || strings.HasPrefix(name, ReceiptsDir+"/") || name == ReceiptsDir || strings.HasPrefix(name, cell.EnvPath+"/") || name == cell.EnvPath {
			continue
		}
		switch name {
		case TurnsPath, publishedPath, adoptedPath, cell.CopyOfferedPath, cell.StateDir + "/copy-ready":
			continue
		}
		if name == ".git" || name == ".git/index" {
			indexOnly = true
			continue
		}
		log.Printf("continuation needs a choice for changed path: %s", name)
		return true, false, nil
	}
	return indexOnly, indexOnly, nil
}
