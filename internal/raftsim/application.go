// Package raftsim drives production consensus cores with explicit virtual time,
// durability and faults. It is a development gate, never a directory writer.
package raftsim

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
)

// ProbeApplication exercises real pure lease rules while P09 supplies the full
// authorized writer. Session policy here is a contract fixture, not a second
// production implementation. The checker uses its own rules and state types.
type ProbeApplication struct {
	Now      int64
	Vault    string
	Cells    map[string]clusterstate.Cell
	Revoked  map[string]bool
	Sessions map[string]probeSession
	Padding  []byte
}
type probeSession struct {
	Sequence uint64
	Digest   string
	Closed   bool
	Result   model.Outcome
}

func freshApp() ProbeApplication {
	return ProbeApplication{Cells: map[string]clusterstate.Cell{}, Revoked: map[string]bool{}, Sessions: map[string]probeSession{}}
}
func (a ProbeApplication) copy() ProbeApplication {
	out := ProbeApplication{Now: a.Now, Vault: a.Vault, Cells: map[string]clusterstate.Cell{}, Revoked: map[string]bool{}, Sessions: map[string]probeSession{}, Padding: bytes.Clone(a.Padding)}
	for key, cell := range a.Cells {
		cell.Frames = slices.Clone(cell.Frames)
		if cell.Keys != nil {
			keys := map[string]map[string]string{}
			for id, recipients := range cell.Keys {
				keys[id] = map[string]string{}
				for who, wrapped := range recipients {
					keys[id][who] = wrapped
				}
			}
			cell.Keys = keys
		}
		out.Cells[key] = cell
	}
	for key, value := range a.Revoked {
		out.Revoked[key] = value
	}
	for key, value := range a.Sessions {
		out.Sessions[key] = value
	}
	return out
}
func observed(c clusterstate.Cell) model.Cell {
	return model.Cell{Holder: c.Lease.Device, Fence: c.Lease.Fence, Until: c.Lease.Expires, Head: c.Head, Pending: c.Lease.Pending, ReleasedAt: c.Lease.ReleasedAt}
}
func (a ProbeApplication) read(op model.Call) model.Outcome {
	if a.Revoked[op.Actor] {
		return model.Outcome{Code: "revoked"}
	}
	if op.Kind == "read_vault" {
		return model.Outcome{Code: "ok", Vault: a.Vault}
	}
	c, ok := a.Cells[op.Cell]
	if !ok {
		return model.Outcome{Code: "not_found"}
	}
	return model.Outcome{Code: "ok", Cell: observed(c)}
}
func (a *ProbeApplication) evaluate(op model.Call, brokenFence bool) model.Outcome {
	key := op.Actor + "/" + op.Session
	last, known := a.Sessions[key]
	if op.Session != "" {
		if known {
			if last.Closed || op.Sequence < last.Sequence || op.Sequence == last.Sequence && op.Digest != last.Digest {
				return model.Outcome{Code: "conflict"}
			}
			if op.Sequence == last.Sequence {
				return last.Result
			}
			if op.Sequence != last.Sequence+1 || op.Ack != last.Sequence {
				return model.Outcome{Code: "conflict"}
			}
		} else {
			if op.Sequence != 1 {
				return model.Outcome{Code: "conflict"}
			}
			live := 0
			for _, s := range a.Sessions {
				if !s.Closed {
					live++
				}
			}
			if live == 128 {
				return model.Outcome{Code: "capacity"}
			}
		}
	}
	out := model.Outcome{Code: "ok"}
	if op.Kind != "session_close" {
		a.Now = max(a.Now, op.At)
		out = a.mutate(op, brokenFence)
	}
	if op.Session != "" {
		a.Sessions[key] = probeSession{Sequence: op.Sequence, Digest: op.Digest, Result: out, Closed: op.Kind == "session_close"}
	}
	return out
}
func (a *ProbeApplication) mutate(op model.Call, brokenFence bool) model.Outcome {
	if a.Revoked[op.Actor] {
		return model.Outcome{Code: "revoked"}
	}
	switch op.Kind {
	case "advance_time":
		return model.Outcome{Code: "ok"}
	case "set_vault":
		if a.Vault != op.OldVault {
			return model.Outcome{Code: "cas"}
		}
		a.Vault = op.NewVault
		return model.Outcome{Code: "ok"}
	case "revoke":
		_, err := clusterstate.RevokeOf(clusterstate.Device{}, op.Actor, op.Target)
		if err != nil {
			return model.Outcome{Code: "self_revoke"}
		}
		a.Revoked[op.Target] = true
		return model.Outcome{Code: "ok"}
	}
	c, exists := a.Cells[op.Cell]
	var err error
	if op.Kind == "create" {
		if exists {
			return model.Outcome{Code: "exists"}
		}
		c = clusterstate.Created(clusterstate.CellInit{Head: op.Head}, op.Actor, a.Now)
	} else {
		if !exists {
			return model.Outcome{Code: "not_found"}
		}
		switch op.Kind {
		case "acquire":
			if c.Lease.Fence == ^uint64(0) {
				return model.Outcome{Code: "capacity"}
			}
			expect := &clusterstate.LeaseExpectation{Device: op.Expected.Holder, Fence: op.Expected.Fence}
			if brokenFence {
				expect = nil
			}
			c, err = clusterstate.Acquire(c, op.Actor, a.Now, op.Force, expect)
		case "publish":
			c, err = clusterstate.PublishTo(c, op.Actor, clusterstate.Publish{Fence: op.Fence, OldHead: op.OldHead, Head: op.Head, Pending: op.Pending}, a.Now)
		case "beat":
			c, err = clusterstate.Heartbeat(c, op.Actor, clusterstate.Beat{Fence: op.Fence, Pending: op.Pending}, a.Now)
		case "release":
			at := a.Now
			if op.Quiet {
				at = 0
			}
			c, err = clusterstate.ReleaseOf(c, op.Actor, op.Fence, at)
		default:
			return model.Outcome{Code: "unsupported"}
		}
	}
	if err != nil {
		code := ""
		for _, pair := range []struct {
			err  error
			code string
		}{{clusterstate.ErrLeaseHeld, "lease_held"}, {clusterstate.ErrFenceStale, "fence_stale"}, {clusterstate.ErrHeadMoved, "head_moved"}} {
			if errors.Is(err, pair.err) {
				code = pair.code
				break
			}
		}
		if code == "" {
			panic(err)
		}
		return model.Outcome{Code: code}
	}
	a.Cells[op.Cell] = c
	return model.Outcome{Code: "ok", Cell: observed(c)}
}

// command keeps real canonical admission in the loop. Extra fixture policy
// inputs (session acknowledgments) live in the replay catalog, not a wire fork.
func command(op model.Call, id int) ([]byte, error) {
	change := clusterstate.Mutation{}
	switch op.Kind {
	case "create":
		change.Create = &clusterstate.CreateCell{Cell: op.Cell, Init: clusterstate.CellInit{Head: op.Head}, Receipt: "durable-probe-receipt"}
	case "acquire":
		change.Acquire = &clusterstate.AcquireCell{Cell: op.Cell, Force: op.Force, Expect: clusterstate.LeaseExpectation{Device: op.Expected.Holder, Fence: op.Expected.Fence}}
	case "publish":
		change.Publish = &clusterstate.PublishCell{Cell: op.Cell, Publish: clusterstate.Publish{Fence: op.Fence, OldHead: op.OldHead, Head: op.Head, Pending: op.Pending}, Receipt: "durable-probe-receipt"}
	case "beat":
		change.Beat = &clusterstate.BeatCell{Cell: op.Cell, Beat: clusterstate.Beat{Fence: op.Fence, Pending: op.Pending}}
	case "release":
		change.Release = &clusterstate.ReleaseCell{Cell: op.Cell, Fence: op.Fence, Quiet: op.Quiet}
	case "revoke":
		change.Revoke = &clusterstate.DeviceID{Device: op.Target}
	case "set_vault":
		change.SetVault = &clusterstate.VaultCAS{Old: op.OldVault, New: op.NewVault}
	case "advance_time", "session_close":
		change.AdvanceTime = &clusterstate.AdvanceTime{}
	default:
		return nil, fmt.Errorf("unsupported probe operation %s", op.Kind)
	}
	session, digest, sequence := op.Session, op.Digest, op.Sequence
	if session == "" {
		session = fmt.Sprintf("unsequenced-%d", id)
		digest = session
		sequence = 1
	}
	return clusterstate.EncodeCommand(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: clusterUUID, Actor: op.Actor, Session: session, Sequence: sequence, RequestDigest: digest, Now: op.At, Change: change})
}
