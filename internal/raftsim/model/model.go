// Package model is the independent sequential ownership/vault oracle for the
// fault simulator. It deliberately imports no production state types or rules.
// P06 supplies history checking, unknown timeout outcomes and assignment cases.
package model

// State describes observable coordination, independent of Raft internals.
// Calls advance committed time explicitly; the model never reads a host clock.
type State struct {
	Now     int64
	Vault   string
	Cells   map[string]Cell
	Revoked map[string]bool
}

type Cell struct {
	Holder     string
	Fence      uint64
	Until      int64
	Head       string
	Pending    uint32
	ReleasedAt int64
}

// Call contains only the small ownership/vault domain. Expected is an observed
// holder/fence, not a fresh read substituted by the checker behind the caller.
type Call struct {
	Kind     string
	Actor    string
	Cell     string
	At       int64
	Expected Cell
	Force    bool
	Fence    uint64
	Head     string
	OldHead  string
	Pending  uint32
	Quiet    bool
	OldVault string
	NewVault string
	Target   string
}

type Outcome struct {
	Code  string
	Cell  Cell
	Vault string
}

// Step returns a new state so alternative history orders cannot mutate one
// another. The independent 90-second lease specification is intentional: using
// the production constant or apply function would hide a shared defect.
func Step(s State, op Call) (State, Outcome) {
	next := State{Now: s.Now, Vault: s.Vault, Cells: map[string]Cell{}, Revoked: map[string]bool{}}
	for k, v := range s.Cells {
		next.Cells[k] = v
	}
	for k, v := range s.Revoked {
		next.Revoked[k] = v
	}
	if op.At > next.Now {
		next.Now = op.At
	}
	if next.Revoked[op.Actor] {
		return next, Outcome{Code: "revoked"}
	}
	c, exists := next.Cells[op.Cell]
	out := Outcome{Code: "ok"}
	switch op.Kind {
	case "advance_time":
		return next, out
	case "set_vault":
		if op.OldVault != next.Vault {
			return next, Outcome{Code: "cas"}
		}
		next.Vault = op.NewVault
		return next, out
	case "read_vault":
		return next, Outcome{Code: "ok", Vault: next.Vault}
	case "revoke":
		if op.Target == op.Actor {
			return next, Outcome{Code: "self_revoke"}
		}
		next.Revoked[op.Target] = true
		return next, out
	case "create":
		if exists {
			return next, Outcome{Code: "exists"}
		}
		c = Cell{Holder: op.Actor, Fence: 1, Head: op.Head, Until: next.Now + 90_000}
	default:
		if !exists {
			return next, Outcome{Code: "not_found"}
		}
		switch op.Kind {
		case "read":
			return next, Outcome{Code: "ok", Cell: c}
		case "acquire":
			if op.Expected.Holder != c.Holder || op.Expected.Fence != c.Fence ||
				(!op.Force && c.Until > next.Now && c.Holder != op.Actor) {
				return next, Outcome{Code: "lease_held"}
			}
			if c.Fence == ^uint64(0) {
				return next, Outcome{Code: "capacity"}
			}
			c.Holder, c.Fence, c.Until = op.Actor, c.Fence+1, next.Now+90_000
			c.Pending, c.ReleasedAt = 0, 0
		case "beat", "publish", "release":
			if c.Holder != op.Actor || c.Fence != op.Fence {
				return next, Outcome{Code: "fence_stale"}
			}
			switch op.Kind {
			case "publish":
				if c.Head != op.OldHead {
					return next, Outcome{Code: "head_moved"}
				}
				c.Head = op.Head
				fallthrough
			case "beat":
				c.Until, c.Pending = next.Now+90_000, op.Pending
			case "release":
				c.Until, c.Pending = 0, 0
				c.ReleasedAt = 0
				if !op.Quiet {
					c.ReleasedAt = next.Now
				}
			}
		default:
			return next, Outcome{Code: "unsupported"}
		}
	}
	next.Cells[op.Cell] = c
	out.Cell = c
	return next, out
}
