package cellindex

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

// usageIndex is this session's share of the machine-wide usage ledger: one row
// per usage line the transcript journaled.
//
// The ledger row a live call writes also carries lane timings, seat and task
// ids that no transcript line holds; a rebuilt row leaves them absent, which
// the ledger reads as "nobody said". Totals match; row granularity is the
// transcript's.
type usageIndex struct{}

func (usageIndex) Name() string { return "usage.jsonl" }

// Present reports whether the ledger already names the session in any row.
func (usageIndex) Present(c cell.Cell) bool {
	return ledgerNames(session.UsageLedgerPath(), []byte(`"session":"`+c.ID+`"`))
}

func (usageIndex) Build(_ cell.Cell, d session.Digest) error {
	path := session.UsageLedgerPath()
	for _, line := range d.Usage {
		session.RecordUsage(path, line)
	}
	session.FlushUsage()
	return nil
}

// ledgerNames scans the ledger for a marker without parsing any row.
func ledgerNames(path string, marker []byte) bool {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		return true // an unreadable ledger is not ours to append to
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, marker) {
			return true
		}
		if err != nil {
			return false
		}
	}
}
