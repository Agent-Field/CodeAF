package cellindex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"strings"
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
	path := session.UsageLedgerPath()
	if ledgerRows(path, c.ID) > 0 {
		return true
	}
	return ledgerRows(path, usageJournalID(c)) > 0
}

// Behind reports whether the chat spent on calls the ledger has no row for: the
// copy this machine held was older than the one a take just brought.
func (usageIndex) Behind(c cell.Cell, d session.Digest) bool {
	lines, err := missingUsage(c, d)
	return err == nil && len(lines) > 0
}

// Build appends saved calls whose live ledger identity is absent.
func (usageIndex) Build(c cell.Cell, d session.Digest) error {
	lines, err := missingUsage(c, d)
	if err != nil {
		return err
	}
	path := session.UsageLedgerPath()
	for _, line := range lines {
		session.RecordUsage(path, line)
	}
	session.FlushUsage()
	return nil
}

// missingUsage reconciles identities rather than assuming all ledger rows are
// an ordered prefix of saved calls. Legacy calls have no shared identity. For
// those only, retain the old prefix rule after removing identified saved calls
// from the row count. Legacy rows must already be settled and cover the saved
// calls in order. Queued or out-of-order legacy receipts cannot be reconciled
// exactly; their model, tokens and cost are never used as identity.
func missingUsage(c cell.Cell, d session.Digest) ([]session.UsageLine, error) {
	lines, err := usageLines(c, d)
	if err != nil {
		return nil, err
	}
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		return nil, err
	}
	saved, seen := usageIDs(lines), usageIDs(rows)
	skip := max(legacyUsageRows(rows, saved, c.ID), legacyUsageRows(rows, saved, d.ID))
	var missing []session.UsageLine
	for _, line := range lines {
		if line.ID != "" {
			if !seen[line.ID] {
				missing = append(missing, line)
			}
		} else if skip > 0 {
			skip--
		} else {
			missing = append(missing, line)
		}
	}
	return missing, nil
}

func usageIDs(lines []session.UsageLine) map[string]bool {
	ids := make(map[string]bool)
	for _, line := range lines {
		if line.ID != "" {
			ids[line.ID] = true
		}
	}
	return ids
}

func legacyUsageRows(rows []session.UsageLine, saved map[string]bool, owner string) int {
	if owner == "" {
		return 0
	}
	n := 0
	for _, row := range rows {
		if row.Session == owner && !saved[row.ID] {
			n++
		}
	}
	return n
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
		ID: s.ID, At: time.UnixMilli(s.SealedAtMs), Model: s.Model, Role: s.Role, Calls: 1,
		Input: int(s.TokensIn), Output: int(s.TokensOut), USD: float64(s.CostMicroUSD) / 1e6,
		Session: usageOwner(c, d), Workspace: d.Workspace,
	}
}

// ledgerRows counts the ledger rows that name the session, without parsing any
// row. An unreadable ledger answers as if it were full: it is not ours to append to.
func ledgerRows(path string, sessions ...string) int {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		return math.MaxInt
	}
	defer f.Close()
	markers := make([][]byte, 0, len(sessions))
	for _, id := range sessions {
		if id != "" {
			markers = append(markers, []byte(`"session":"`+id+`"`))
		}
	}
	n := 0
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		for _, marker := range markers {
			if bytes.Contains(line, marker) {
				n++
				break
			}
		}
		if err != nil {
			return n
		}
	}
}

// Seeded cells from older builds can have a journal identity different from
// their folder. Live calls use the journal identity; recognize both names so
// reopening never books the receipt again under its folder's name.
func usageJournalID(c cell.Cell) string {
	f, err := os.Open((session.Place{Dir: c.Root}).Transcript())
	if err != nil {
		return ""
	}
	defer f.Close()
	var header struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(f).Decode(&header) != nil {
		return ""
	}
	return strings.TrimSpace(header.ID)
}

func usageOwner(c cell.Cell, d session.Digest) string {
	if d.ID != "" {
		return d.ID
	}
	return c.ID
}
