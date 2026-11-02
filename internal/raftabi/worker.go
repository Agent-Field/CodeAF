package raftabi

import "github.com/Agent-Field/codeaf/internal/clusterstate"

func workerRead(d *decoder) ([]byte, error) {
	cluster, worker, cell, incarnation, now := d.str(), d.str(), d.str(), d.u64(), d.u64()
	rows := map[clusterstate.RecordKey][]byte{}
	for _, k := range clusterstate.WorkerReadKeys(worker, cell) {
		b := d.blob()
		if len(b) == 0 {
			b = nil
		}
		rows[k] = b
	}
	if err := d.end(); err != nil || now > clusterstate.MaxSafeInteger {
		return nil, errFrame
	}
	result, err := clusterstate.ReadWorkerCell(cluster, worker, cell, incarnation, int64(now), func(k clusterstate.RecordKey) ([]byte, error) { return rows[k], nil })
	if err != nil {
		return nil, err
	}
	raw, err := clusterstate.Encode(result)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(raw)
	return e.b, nil
}

func assignmentRead(d *decoder) ([]byte, error) {
	cluster, worker, incarnation, now := d.str(), d.str(), d.u64(), d.u64()
	var ref clusterstate.AssignmentRef
	if clusterstate.Decode(d.blob(), &ref) != nil {
		return nil, errFrame
	}
	rows := map[clusterstate.RecordKey][]byte{}
	for _, k := range clusterstate.AssignmentReadKeys(worker, ref) {
		b := d.blob()
		if len(b) == 0 {
			b = nil
		}
		rows[k] = b
	}
	if d.end() != nil || now > clusterstate.MaxSafeInteger {
		return nil, errFrame
	}
	result, err := clusterstate.ReadWorkerAssignment(cluster, worker, incarnation, ref, int64(now), func(k clusterstate.RecordKey) ([]byte, error) { return rows[k], nil })
	if err != nil {
		return nil, err
	}
	raw, err := clusterstate.Encode(result)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(raw)
	return e.b, nil
}

// clusterIngress uses the same closed Go verifier on both runtimes. The caller
// repeats this verification with committed logical time after its read barrier.
func clusterIngress(d *decoder) ([]byte, error) {
	raw, now := d.blob(), d.u64()
	if d.end() != nil || now > clusterstate.MaxSafeInteger || len(raw) > clusterstate.MaxCommandBytes {
		return nil, errFrame
	}
	var request clusterstate.ClusterRequest
	if err := clusterstate.Decode(raw, &request); err != nil {
		return nil, err
	}
	actor, err := clusterstate.VerifyClusterRequest(request, int64(now))
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.str(actor)
	return e.b, nil
}
