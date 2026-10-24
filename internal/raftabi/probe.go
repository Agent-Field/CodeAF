package raftabi

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

// ProbeApply exercises real lease rules over one immutable host-supplied row.
// It is a feasibility seam, NOT an authorized directory writer: P09 must add
// the complete dependency read-set, committed policy, dedup and atomic writes.
// Keeping this probe narrow avoids treating prototype policy as production.
func ProbeApply(command, row []byte) ([]byte, error) {
	if len(command) > raftcore.MaxMessageBytes || len(row) > 1<<20 {
		return nil, errFrame
	}
	c, err := clusterstate.DecodeCommand(command)
	if err != nil {
		return nil, err
	}
	var old clusterstate.Row
	if len(row) > 0 {
		old, err = clusterstate.DecodeRow(row)
		if err != nil {
			return nil, err
		}
	}
	next := old
	switch {
	case c.Change.Create != nil:
		if len(row) != 0 {
			return nil, clusterstate.ErrExists
		}
		v := c.Change.Create
		cell := clusterstate.Created(v.Init, c.Actor, c.Now)
		next = clusterstate.Row{Schema: clusterstate.SchemaVersion, Key: v.Cell, Cell: &cell}
	case c.Change.Acquire != nil:
		v := c.Change.Acquire
		if old.Cell == nil || old.Key != v.Cell {
			return nil, clusterstate.ErrNotFound
		}
		cell, err := clusterstate.Acquire(*old.Cell, c.Actor, c.Now, v.Force, &v.Expect)
		if err != nil {
			return nil, err
		}
		next.Cell = &cell
	case c.Change.Release != nil:
		v := c.Change.Release
		if old.Cell == nil || old.Key != v.Cell {
			return nil, clusterstate.ErrNotFound
		}
		at := c.Now
		if v.Quiet {
			at = 0
		}
		cell, err := clusterstate.ReleaseOf(*old.Cell, c.Actor, v.Fence, at)
		if err != nil {
			return nil, err
		}
		next.Cell = &cell
	default:
		return nil, errors.New("raftabi: unsupported probe command")
	}
	return clusterstate.EncodeRow(next)
}

// RoundTripRow proves the one-row representation without accumulating an
// identity in the reactor. The host inventories and streams rows separately.
func RoundTripRow(row []byte) ([]byte, error) {
	if len(row) > 1<<20 {
		return nil, errFrame
	}
	v, err := clusterstate.DecodeRow(row)
	if err != nil {
		return nil, err
	}
	return clusterstate.EncodeRow(v)
}
