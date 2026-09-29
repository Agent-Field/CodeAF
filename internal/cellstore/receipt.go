package cellstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
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
