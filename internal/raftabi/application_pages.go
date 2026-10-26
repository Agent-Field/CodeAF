package raftabi

import (
	"encoding/binary"
	"errors"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
)

var errReadPage = errors.New("raftabi: next immutable record page")

// A cursor belongs to one synchronous application transaction. Each supplied
// record page and returned byte page keeps the existing evaluation envelope.
// Dropping it on every exit prevents abandoned evaluations retaining buffers.
type pagedEvaluation struct {
	command    []byte
	meta       cs.ApplicationMeta
	rows       map[cs.RecordKey][]byte
	need       *cs.RecordKey
	output     []byte
	offset     int
	projection bool
}

func (r *Runtime) pagedApplication(op uint32, d *decoder) ([]byte, error) {
	e := encoder{}
	if op == 45 {
		if len(r.evaluations) >= MaxHandles {
			return nil, errFrame
		}
		raw, err := hostedProjectionBytes(d)
		if err != nil {
			return nil, err
		}
		r.evaluationSerial++
		id := r.evaluationSerial
		v := &pagedEvaluation{output: raw, projection: true}
		r.evaluations[id] = v
		e.u32(id)
		e.b = append(e.b, v.outputPage()...)
		return e.b, nil
	}
	if op == 41 {
		if len(r.evaluations) >= MaxHandles {
			return nil, errFrame
		}
		v := &pagedEvaluation{command: d.blob(), rows: map[cs.RecordKey][]byte{}}
		v.meta = cs.ApplicationMeta{Cluster: d.str(), Now: int64(d.u64()), Version: d.u64(), Bytes: d.u64(), Ceiling: d.u64(), Index: d.u64(), Term: d.u64()}
		conf := new(pb.ConfState)
		d.message(conf)
		v.meta.Configuration = raftcore.Configuration(conf)
		if d.end() != nil || len(v.command) > cs.MaxCommandBytes {
			return nil, errFrame
		}
		r.evaluationSerial++
		id := r.evaluationSerial
		r.evaluations[id] = v
		payload, err := v.advance()
		if err != nil {
			delete(r.evaluations, id)
			return nil, err
		}
		e.u32(id)
		e.b = append(e.b, payload...)
		return e.b, nil
	}
	id := d.u32()
	if op == 44 {
		if d.end() != nil {
			return nil, errFrame
		}
		delete(r.evaluations, id)
		return e.b, nil
	}
	v := r.evaluations[id]
	if v == nil {
		return nil, errFrame
	}
	if op == 42 {
		present, b := d.u32(), d.blob()
		if d.end() != nil || present > 1 || present == 0 && len(b) > 0 || len(b) > cs.MaxEvaluationBytes || v.need == nil {
			return nil, errFrame
		}
		if present == 0 {
			b = nil
		}
		v.rows[*v.need] = b
		v.need = nil
		payload, err := v.advance()
		if err != nil {
			delete(r.evaluations, id)
		}
		return payload, err
	}
	if d.end() != nil || v.output == nil || v.offset >= v.outputSize() {
		return nil, errFrame
	}
	return v.outputPage(), nil
}
func (v *pagedEvaluation) advance() ([]byte, error) {
	result, err := cs.Evaluate(v.command, v.meta, func(k cs.RecordKey) ([]byte, error) {
		b, ok := v.rows[k]
		if !ok {
			v.need = &k
			return nil, errReadPage
		}
		return b, nil
	})
	if errors.Is(err, errReadPage) {
		e := encoder{}
		e.u32(0)
		e.str(v.need.Namespace)
		e.str(v.need.Key)
		return e.b, nil
	}
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.u64(uint64(result.Now))
	e.u64(result.Version)
	raw, err := cs.EncodeApplicationResult(result.Result)
	if err != nil {
		return nil, err
	}
	e.blob(raw)
	e.u32(uint32(len(result.Writes)))
	for _, w := range result.Writes {
		e.str(w.Namespace)
		e.str(w.Key)
		e.flag(w.Delete)
		e.blob(w.Data)
	}
	v.output = e.b
	v.rows = nil
	return v.outputPage(), nil
}
func (v *pagedEvaluation) outputSize() int {
	size := len(v.output)
	if v.projection {
		size += 4
	}
	return size
}
func (v *pagedEvaluation) outputPage() []byte {
	end := min(v.offset+cs.MaxEvaluationBytes/4, v.outputSize())
	var page []byte
	if !v.projection {
		page = v.output[v.offset:end]
	} else {
		// Prefix the logical blob virtually, without copying its entire contents.
		page = make([]byte, end-v.offset)
		prefix := [4]byte{}
		binary.LittleEndian.PutUint32(prefix[:], uint32(len(v.output)))
		for pos := v.offset; pos < min(end, 4); pos++ {
			page[pos-v.offset] = prefix[pos]
		}
		start := max(v.offset, 4)
		copy(page[start-v.offset:], v.output[start-4:end-4])
	}
	e := encoder{}
	e.u32(1)
	e.flag(end < v.outputSize())
	e.blob(page)
	v.offset = end
	return e.b
}
