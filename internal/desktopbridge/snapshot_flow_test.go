package desktopbridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSnapshotFlow(t *testing.T) {
	b, a, id := stateFixture(t)
	s := b.sessions[id]
	const entryCount = 2000
	const outputBytes = 100 << 10
	// Sharing the immutable body keeps this scale fixture cheap on a shared box.
	// Every entry still represents a distinct call with a 100 KiB recorded result.
	body := strings.Repeat("x", outputBytes)
	entries := make([]session.DisplayEntry, entryCount)
	for i := range entries {
		entries[i] = session.DisplayEntry{Role: "tool", Tool: "bash", CallID: fmt.Sprintf("call-%04d", i), Output: body}
	}
	a.mu.Lock()
	a.entries = entries
	a.mu.Unlock()
	path := "/api/engine/sessions/" + id

	t.Run("16 KiB inline boundary", func(t *testing.T) {
		bridge, agent, sessionID := stateFixture(t)
		const inlineBytes = 16 << 10
		agent.mu.Lock()
		agent.entries = []session.DisplayEntry{
			{Role: "tool", Tool: "bash", CallID: "at-cap", Output: body[:inlineBytes]},
			{Role: "tool", Tool: "bash", CallID: "over-cap", Output: body[:inlineBytes+1]},
		}
		agent.mu.Unlock()
		w := request(bridge, http.MethodGet, "/api/engine/sessions/"+sessionID+"?since=0", "")
		var tail SnapshotTail
		if w.Code != http.StatusOK {
			t.Fatalf("boundary: HTTP %d", w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &tail); err != nil {
			t.Fatal(err)
		}
		if len(tail.Entries) != 2 {
			t.Fatalf("boundary entries = %d, want 2", len(tail.Entries))
		}
		if tail.Entries[0].Output != body[:inlineBytes] || tail.Entries[0].OutputOmitted {
			t.Fatal("16 KiB output must remain inline")
		}
		if tail.Entries[1].Output != "" || !tail.Entries[1].OutputOmitted || tail.Entries[1].OutputBytes != inlineBytes+1 {
			t.Fatal("output above 16 KiB must be omitted with its byte count")
		}
	})

	t.Run("tail and on-demand outputs", func(t *testing.T) {
		for _, since := range []int{0, 1990, entryCount} {
			w := request(b, http.MethodGet, fmt.Sprintf("%s?since=%d", path, since), "")
			if w.Code != http.StatusOK {
				t.Fatalf("since %d: HTTP %d", since, w.Code)
			}
			var tail SnapshotTail
			if err := json.Unmarshal(w.Body.Bytes(), &tail); err != nil {
				t.Fatal(err)
			}
			if tail.From != since || tail.Reset || tail.Header.EntryCount != entryCount || len(tail.Entries) != entryCount-since {
				t.Fatalf("since %d: from=%d reset=%t count=%d tail=%d", since, tail.From, tail.Reset, tail.Header.EntryCount, len(tail.Entries))
			}
			for i, entry := range tail.Entries {
				if entry.CallID != entries[since+i].CallID || entry.Output != "" || !entry.OutputOmitted || entry.OutputBytes != outputBytes {
					t.Fatalf("since %d entry %d: call=%q omitted=%t bytes=%d inline=%d", since, i, entry.CallID, entry.OutputOmitted, entry.OutputBytes, len(entry.Output))
				}
			}
		}
		for _, i := range []int{0, 1990, entryCount - 1} {
			w := request(b, http.MethodGet, path+"/tools/"+entries[i].CallID, "")
			var output ToolOutput
			if w.Code != http.StatusOK {
				t.Fatalf("tool %d: HTTP %d", i, w.Code)
			}
			if err := json.Unmarshal(w.Body.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			if output.Output != body {
				t.Fatalf("tool %d: fetched %d bytes, want %d", i, len(output.Output), outputBytes)
			}
		}
		if a.Transcript()[entryCount-1].Output != body {
			t.Fatal("snapshot elision changed the canonical transcript")
		}
	})

	t.Run("header ring byte bound", func(t *testing.T) {
		if MaxReplay != 2048 {
			t.Fatalf("replay capacity = %d, want 2048", MaxReplay)
		}
		// The first record carries the elided entries. Repeated state updates
		// must retain only headers, including after that first record ages out.
		var backing reflect.Value
		for i := 0; i <= MaxReplay; i++ {
			snapshot := s.snapshot()
			s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
			if i == MaxReplay-1 || i == MaxReplay {
				s.mu.Lock()
				if len(s.records) != MaxReplay {
					s.mu.Unlock()
					t.Fatalf("ring records = %d, want %d", len(s.records), MaxReplay)
				}
				current := reflect.ValueOf(s.records)
				if i == MaxReplay-1 {
					backing = current.Slice(0, current.Cap())
				}
				// Slicing an evicted record off the front still retains the
				// backing allocation, including its otherwise hidden prefix.
				if current.Pointer() >= backing.Pointer() && current.Pointer() < backing.Pointer()+uintptr(backing.Cap())*backing.Type().Elem().Size() {
					current = backing
				}
				bytes := snapshotFlowBytes(current)
				// Two MiB includes struct storage, slice capacity, strings and
				// pointer targets, even counting shared strings repeatedly. It
				// excludes allocator metadata and the agent's canonical transcript.
				const bound = 2 << 20
				for j, record := range s.records {
					if record.Snapshot == nil || record.Snapshot.Header.EntryCount != entryCount || record.Snapshot.Header.Seq != record.Seq {
						s.mu.Unlock()
						t.Fatalf("record %d lost its header", j)
					}
					if (i == MaxReplay || j > 0) && len(record.Snapshot.Entries) != 0 {
						s.mu.Unlock()
						t.Fatalf("record %d retained %d transcript entries", j, len(record.Snapshot.Entries))
					}
				}
				s.mu.Unlock()
				if bytes >= bound {
					t.Fatalf("ring footprint = %d bytes, must stay below %d", bytes, bound)
				}
				t.Logf("after %d snapshots: ring footprint %d bytes < %d", i+1, bytes, bound)
			}
		}
	})
}

// snapshotFlowBytes counts reachable value storage rather than serialized JSON,
// so even a transcript accidentally hidden from the wire consumes the budget.
// Slice capacity is counted, and shared targets are counted per reference to
// keep the estimate conservative without heap sampling or clock dependence.
func snapshotFlowBytes(v reflect.Value) uint64 {
	bytes := uint64(v.Type().Size())
	switch v.Kind() {
	case reflect.String:
		bytes += uint64(v.Len())
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			bytes += snapshotFlowBytes(v.Elem())
		}
	case reflect.Slice:
		bytes += uint64(v.Cap()) * uint64(v.Type().Elem().Size())
		for i := 0; i < v.Cap(); i++ {
			bytes += snapshotFlowBytes(v.Slice(0, v.Cap()).Index(i)) - uint64(v.Type().Elem().Size())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			bytes += snapshotFlowBytes(field) - uint64(field.Type().Size())
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			bytes += snapshotFlowBytes(v.Index(i)) - uint64(v.Type().Elem().Size())
		}
	case reflect.Map:
		// Replay headers currently have no maps. A new map needs an explicit
		// accounting policy rather than silently bypassing this bound.
		if !v.IsNil() {
			panic("snapshot ring byte counter needs map accounting")
		}
	}
	return bytes
}
