package cellindex

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/session"
)

// usageIndex is this session's share of the machine-wide usage ledger: one row
// per model call the receipt chain sealed, so a rewind never drops spend. A cell
// sealed before receipts carried model calls falls back to the usage lines the
// transcript journaled.
//
// The ledger row a live call writes also carries lane timings, seat and task
// ids that no receipt holds; a rebuilt row leaves them absent, which the ledger
// reads as "nobody said". Totals match; a receipt keeps cost in whole micro
// dollars, so a rebuilt row is the live one rounded to that unit.
type usageIndex struct{}

func (usageIndex) Name() string { return "usage.jsonl" }

// Present reports whether the ledger already names the session in any row.
func (usageIndex) Present(c cell.Cell) bool {
	return ledgerRows(session.UsageLedgerPath(), c.ID) > 0
}

// Behind reports whether the chat spent on calls the ledger has no row for: the
// copy this machine held was older than the one a take just brought.
func (usageIndex) Behind(c cell.Cell, d session.Digest) bool {
	lines, err := usageLines(c, d)
	if err != nil {
		return false
	}
	missing, err := missingUsage(session.UsageLedgerPath(), lines)
	return err == nil && len(missing) > 0
}

// Reconstruction compares call identities across the whole machine ledger.
// A parent's row count cannot say whether its workers already paid for a call.
var usageRebuildMu sync.Mutex

func (usageIndex) Build(c cell.Cell, d session.Digest) error {
	usageRebuildMu.Lock()
	defer usageRebuildMu.Unlock()
	if !session.FlushUsage() {
		return os.ErrDeadlineExceeded
	}
	lines, err := usageLines(c, d)
	if err != nil {
		return err
	}
	path := session.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+session.UsageLedgerLockSuffix, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	lines, err = missingUsage(path, lines)
	if err != nil {
		return err
	}
	for _, line := range lines {
		session.RecordUsage(path, line)
	}
	if !session.FlushUsage() {
		return os.ErrDeadlineExceeded
	}
	return nil
}

// missingUsage applies one rule to every actual call: keep it only if the
// ledger lacks its identity. Fold summaries never enter the call stream.
// Older receipts lacking identity retain their session-prefix compatibility.
func missingUsage(path string, lines []session.UsageLine) ([]session.UsageLine, error) {
	restoreRemoteUsageIDs(path, lines)
	paid, err := session.ReadUsage(path, time.Time{})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	seen := map[string]bool{}
	legacy := map[string]int{}
	identified := map[string]bool{}
	for _, line := range lines {
		if line.CallID != "" {
			identified[line.CallID] = true
		}
	}
	for _, line := range paid {
		if id := session.UsageCallID(line); id != "" {
			seen[id] = true
		}
		if line.RemoteEvent != "" {
			seen["remote:"+line.RemoteEvent] = true
		} else if !identified[session.UsageCallID(line)] {
			legacy[line.Session]++
		}
	}
	var missing []session.UsageLine
	for _, line := range lines {
		id := line.CallID
		if id == "" && line.RemoteEvent != "" {
			id = "remote:" + line.RemoteEvent
		}
		if id != "" {
			if seen[id] || (line.RemoteEvent != "" && seen["remote:"+line.RemoteEvent]) {
				continue
			}
			seen[id] = true
			if line.RemoteEvent != "" {
				seen["remote:"+line.RemoteEvent] = true
			}
		} else if legacy[line.Session] > 0 {
			legacy[line.Session]--
			continue
		}
		missing = append(missing, line)
	}
	return missing, nil
}

// usageLines is what the session spent: the receipt chain's model calls, which
// no rewind removes; and, for a cell sealed before receipts carried them, the
// transcript's usage lines.
func usageLines(c cell.Cell, d session.Digest) ([]session.UsageLine, error) {
	sealed, err := cellstore.ModelCalls(c)
	if err != nil || len(sealed) == 0 {
		return d.Usage, err
	}
	lines := make([]session.UsageLine, len(sealed))
	for i, s := range sealed {
		lines[i] = usageOfCall(c, d, s)
	}
	// Mirrored charges bypass the local recorder; their identities live in the
	// carried transcript alongside the local receipt chain.
	for _, line := range d.Usage {
		if line.RemoteEvent != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func usageOfCall(c cell.Cell, d session.Digest, s cellstore.SealedCall) session.UsageLine {
	return session.UsageLine{
		CallID: s.CallID, At: time.UnixMilli(s.SealedAtMs), Model: s.Model, Role: s.Role, Calls: 1,
		Input: int(s.TokensIn), Output: int(s.TokensOut), USD: float64(s.CostMicroUSD) / 1e6,
		Session: c.ID, Workspace: d.Workspace,
	}
}

// ledgerRows counts the ledger rows that name the session, without parsing any
// row. An unreadable ledger answers as if it were full: it is not ours to append to.
func ledgerRows(path, session string) int {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		return math.MaxInt
	}
	defer f.Close()
	marker := []byte(`"session":"` + session + `"`)
	n := 0
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, marker) {
			n++
		}
		if err != nil {
			return n
		}
	}
}
