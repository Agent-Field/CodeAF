package raftabi

import (
	"bytes"
	"encoding/json"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R25/R14 preserve a logical legacy view larger than one ABI buffer while
// keeping each returned page bounded and retiring its cursor on completion.
func TestR25PagedProjectionPreservesExpandedLegacyView(t *testing.T) {
	runtime := New()
	first := create(t, runtime)
	result := cs.Result{Schema: cs.SchemaVersion, Code: cs.OK, View: &cs.CellView{Cell: cs.Cell{Title: string(bytes.Repeat([]byte("<"), cs.MaxV1BodyBytes-1024))}}}
	raw, err := cs.EncodeApplicationResult(result)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(result.View)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) <= MaxBuffer {
		t.Fatal("fixture does not exceed the ABI envelope")
	}
	request := request(45)
	request.str("view")
	request.blob(raw)
	response := answer(t, runtime.Call(request.b))
	id := response.u32()
	var output []byte
	for {
		if response.u32() != 1 {
			t.Fatal("projection requested a record")
		}
		more, page := response.u32(), response.blob()
		if len(page) > cs.MaxEvaluationBytes/4 || response.end() != nil {
			t.Fatal("unbounded or malformed result page")
		}
		output = append(output, page...)
		if more == 0 {
			break
		}
		next := requestFrame(43, id)
		response = answer(t, runtime.Call(next))
	}
	decoded := decoder{b: output}
	if !bytes.Equal(decoded.blob(), want) || decoded.end() != nil {
		t.Fatal("expanded view changed across pages")
	}
	answer(t, runtime.Call(requestFrame(44, id)))
	answer(t, runtime.Call(requestFrame(44, id)))
	if len(runtime.evaluations) != 0 {
		t.Fatal("completed cursor retained bytes")
	}
	stale := decoder{b: runtime.Call(requestFrame(43, id))}
	stale.u32()
	if stale.str() == "" {
		t.Fatal("retired cursor still served bytes")
	}
	if next := create(t, runtime); next != first+1 {
		t.Fatal("application cursor consumed a consensus handle identity")
	}
}

func requestFrame(operation, id uint32) []byte {
	e := request(operation)
	e.u32(id)
	return e.b
}
