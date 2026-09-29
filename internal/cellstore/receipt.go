package cellstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"

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
func newReceipt(info TurnInfo, transcript Range) Receipt {
	r := Receipt{V: schemaV, Transcript: transcript, Calls: []Call{}, ModelCalls: info.Models}
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

// ModelCalls is every model call the cell's chain ever sealed, oldest first.
// The chain is append-only (L12), so a rewound-past turn's calls are still
// here: spend already paid never disappears.
func ModelCalls(c cell.Cell) ([]SealedCall, error) {
	turns, err := Turns(c)
	if err != nil {
		return nil, err
	}
	var all []SealedCall
	for _, t := range turns {
		r, err := readReceipt(c, t.Receipt)
		if err != nil {
			return nil, fmt.Errorf("turn %s: %w", t.ID, err)
		}
		for _, m := range r.ModelCalls {
			all = append(all, SealedCall{ModelCall: m, SealedAtMs: t.SealedAtMs})
		}
	}
	return all, nil
}
