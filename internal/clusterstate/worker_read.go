package clusterstate

import "bytes"

// WorkerRead is a selected-chat response. The sealed grant is returned only
// after current committed authorization, never as part of an identity listing.
type WorkerRead struct {
	Code  Code      `json:"code"`
	View  *CellView `json:"view,omitempty"`
	Grant []byte    `json:"grant,omitempty"`
}

func WorkerReadKeys(worker, cell string) []RecordKey {
	return []RecordKey{IdentityKey(), AuthorityKey(worker), DeviceKey(worker), WorkerKey(worker), CellKey(cell)}
}

// ReadWorkerCell is shared by native and hosted adapters. The host owns the
// fresh quorum barrier and supplies one immutable applied read-set, with time
// already committed. Neither a local replica nor this evaluator is a barrier.
func ReadWorkerCell(cluster, worker, cell string, incarnation uint64, now int64, read ReadRecord) (WorkerRead, error) {
	rows := make([][]byte, 5)
	size := 0
	for i, k := range WorkerReadKeys(worker, cell) {
		b, e := read(k)
		if e != nil {
			return WorkerRead{}, e
		}
		size += len(k.Namespace) + len(k.Key) + len(b)
		if size > MaxEvaluationBytes {
			return WorkerRead{}, ErrDependency
		}
		rows[i] = b
	}
	var identity IdentityRec
	var a Authority
	var d Device
	var w WorkerRecord
	var c Cell
	for _, v := range []struct {
		i    int
		into any
	}{{0, &identity}, {1, &a}, {3, &w}} {
		if rows[v.i] == nil {
			return WorkerRead{Code: Unauthorized}, nil
		}
		if Decode(rows[v.i], v.into) != nil {
			return WorkerRead{}, ErrDependency
		}
	}
	if rows[2] != nil && Decode(rows[2], &d) != nil {
		return WorkerRead{}, ErrDependency
	}
	code := WorkerCanRead(cluster, worker, cell, incarnation, now, identity, a, d, w)
	if code != OK {
		return WorkerRead{Code: code}, nil
	}
	if rows[4] == nil {
		return WorkerRead{Code: NotFound}, nil
	}
	if Decode(rows[4], &c) != nil {
		return WorkerRead{}, ErrDependency
	}
	grant := w.Grants[cell]
	if len(grant) == 0 {
		return WorkerRead{}, ErrDependency
	}
	return WorkerRead{Code: OK, View: &CellView{Now: now, Cell: c}, Grant: bytes.Clone(grant)}, nil
}
