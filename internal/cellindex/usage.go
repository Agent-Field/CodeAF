package cellindex

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
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
	return err == nil && len(lines) > ledgerRows(session.UsageLedgerPath(), c.ID)
}

// Build appends the rows the ledger lacks. Rows are in call order and a copy
// holds a prefix of them, so the rows already held are the first ones.
func (usageIndex) Build(c cell.Cell, d session.Digest) error {
	lines, err := usageLines(c, d)
	if err != nil {
		return err
	}
	path := session.UsageLedgerPath()
	for _, line := range lines[min(ledgerRows(path, c.ID), len(lines)):] {
		session.RecordUsage(path, line)
	}
	session.FlushUsage()
	return nil
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
	return lines, nil
}

func usageOfCall(c cell.Cell, d session.Digest, s cellstore.SealedCall) session.UsageLine {
	return session.UsageLine{
		At: time.UnixMilli(s.SealedAtMs), Model: s.Model, Role: s.Role, Calls: 1,
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
