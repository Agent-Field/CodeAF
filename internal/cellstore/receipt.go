package cellstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// hashHex is the id of a byte string. SHA-256 stands in until the store's
// BLAKE3 lands, as in internal/inventory; only this function changes then.
func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newReceipt builds a turn's receipt from what its calls did.
func newReceipt(info TurnInfo, transcript Range, at time.Time) Receipt {
	r := Receipt{V: schemaV, Transcript: transcript, Calls: []Call{}, ModelCalls: info.Models, SealedAtMs: at.UnixMilli()}
	for _, e := range info.Calls {
		r.Calls = append(r.Calls, e.Call)
		r.Services = append(r.Services, e.Services...)
	}
	return r
}

// quality is the seal rule: exact iff no call left a process alive.
func quality(info TurnInfo) Quality {
	for _, e := range info.Calls {
		if !e.Exact {
			return Turbulent
		}
	}
	return Quiescent
}

// encode is the receipt's persisted bytes and their id.
func (r Receipt) encode() (raw []byte, id string, err error) {
	raw, err = json.Marshal(r)
	return raw, hashHex(raw), err
}

// serviceRec drops everything machine-specific from a service: argv[0] is a
// basename (L1).
func serviceRec(pid int, argv []string, ports []int) ServiceRec {
	out := append([]string(nil), argv...)
	if len(out) > 0 {
		out[0] = filepath.Base(out[0])
	}
	return ServiceRec{PID: pid, Argv: out, Ports: ports}
}

// modelCallOf is a model call as the receipt holds it.
func modelCallOf(m executor.ModelCall) ModelCall {
	return ModelCall{Model: m.Model, Role: m.Role, TokensIn: uint32(m.TokensIn), TokensOut: uint32(m.TokensOut),
		TokensCached: uint32(m.TokensCached), CostMicroUSD: m.CostMicroUSD}
}

// SealedCall is a model call and the instant of the seal that recorded it.
type SealedCall struct {
	ModelCall
	SealedAtMs int64
}

// ModelCalls is every model call the cell ever sealed, oldest first. The chain
// is append-only (L12), so a rewound-past turn's calls are still here: spend
// already paid never disappears.
//
// A CHAIN THAT CAME FROM ANOTHER MACHINE HAS A HOLE AT EACH HEAD IT ADOPTED. The
// seal writes its receipt into the tree it snapshots and its line into the log
// after, so the log a take carries does not name the newest turn, and the log
// the next seal grows never will. That turn's receipt is in the tree all the
// same, and so are the calls in it: receipts the log does not name are read
// too, in the order of the instant each was sealed.
func ModelCalls(c cell.Cell) ([]SealedCall, error) {
	turns, err := Turns(c)
	if err != nil {
		return nil, err
	}
	named := make(map[string]bool, len(turns))
	var all []SealedCall
	for _, t := range turns {
		named[t.Receipt] = true
		r, err := readReceipt(c, t.Receipt)
		if err != nil {
			return nil, fmt.Errorf("turn %s: %w", t.ID, err)
		}
		all = append(all, sealedCalls(r, t.SealedAtMs)...)
	}
	unnamed, err := unnamedReceipts(c, named)
	if err != nil {
		return nil, err
	}
	all = append(all, unnamed...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].SealedAtMs < all[j].SealedAtMs })
	return all, nil
}

func sealedCalls(r Receipt, at int64) []SealedCall {
	out := make([]SealedCall, len(r.ModelCalls))
	for i, m := range r.ModelCalls {
		out[i] = SealedCall{ModelCall: m, SealedAtMs: at}
	}
	return out
}

// unnamedReceipts is the model calls of the receipts in the tree that no line of
// the log names. A receipt from before receipts said when they were sealed is
// dated by its file.
func unnamedReceipts(c cell.Cell, named map[string]bool) ([]SealedCall, error) {
	entries, err := os.ReadDir(rel(c, ReceiptsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SealedCall
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if named[id] || id == e.Name() {
			continue
		}
		r, err := readReceipt(c, id)
		if err != nil {
			return nil, fmt.Errorf("receipt %s: %w", id, err)
		}
		at := r.SealedAtMs
		if info, err := e.Info(); at == 0 && err == nil {
			at = info.ModTime().UnixMilli()
		}
		out = append(out, sealedCalls(r, at)...)
	}
	return out, nil
}
