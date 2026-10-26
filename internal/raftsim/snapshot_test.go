package raftsim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"google.golang.org/protobuf/proto"
)

func snapshotGroup(t *testing.T) *Simulator {
	t.Helper()
	s := group(t, Config{Seed: 1301, Nodes: 4, PaddingBytes: 400 << 10})
	leader := s.Leader()
	s.Do(Action{Kind: "configure", Node: leader, Other: 3, Count: 1})
	s.Drain()
	create(t, s, "a")
	create(t, s, "b")
	createSession := model.Call{Kind: "acquire", Actor: "b", Cell: "a", Expected: observed(s.Nodes[leader].Stable.App.Cells["a"]), Force: true, Session: "retained", Sequence: 1, Digest: "same"}
	submit(t, s, leader, createSession)
	submit(t, s, leader, model.Call{Kind: "set_vault", Actor: "owner", NewVault: "opaque-vault"})
	submit(t, s, leader, model.Call{Kind: "revoke", Actor: "owner", Target: "removed"})
	isolate(s, 3, true)
	for i := 0; i < 3; i++ {
		s.Do(Action{Kind: "snapshot", Node: i})
	}
	submit(t, s, leader, model.Call{Kind: "publish", Actor: "b", Cell: "a", Fence: 2, OldHead: "head-0", Head: "after-snapshot"})
	for i := 0; i < 3; i++ {
		s.Do(Action{Kind: "snapshot", Node: i})
	}
	return s
}

func TestR13PartialCorruptChunksAndBothRetainedGenerations(t *testing.T) {
	s := snapshotGroup(t)
	leader := s.Leader()
	d := s.Nodes[leader].Stable
	if len(d.Generations) != 2 {
		t.Fatal("must keep two complete generations")
	}
	for gi, g := range d.Generations {
		if len(g.Chunks) < 2 {
			t.Fatal("fixture must cross chunk boundaries")
		}
		original, err := decodeGeneration(g)
		if err != nil {
			t.Fatal(err)
		}
		if len(original.Padding) != 400<<10 || !original.Revoked["removed"] || original.Vault != "opaque-vault" || original.Sessions["b/retained"].Result.Cell.Fence != 2 {
			t.Fatal("snapshot lost observable records")
		}
		for chunk := range g.Chunks {
			t.Run(fmt.Sprintf("generation-%d/chunk-%d", gi, chunk), func(t *testing.T) {
				for _, kind := range []string{"missing", "corrupt"} {
					broken := cloneGeneration(g)
					if kind == "missing" {
						broken.Chunks = slices.Delete(broken.Chunks, chunk, chunk+1)
					} else {
						broken.Chunks[chunk][0] ^= 1
					}
					if _, err := decodeGeneration(broken); err == nil {
						t.Fatal("accepted", kind)
					}
					corrupted := d.copy()
					corrupted.Generations[gi] = broken
					if _, err := corrupted.recovered(); err == nil {
						t.Fatal("corrupt complete authority was repaired or ignored")
					}
				}
				partial := d.copy()
				partial.Generations = append(partial.Generations, generation{Pointer: clonePB(g.Pointer), Chunks: g.Chunks[:chunk], Complete: false})
				before := partial.App.copy()
				if _, err := partial.recovered(); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(encodedApp(before), encodedApp(partial.App)) {
					t.Fatal("partial snapshot changed application authority")
				}
			})
		}
	}
	finish(t, s)
}
func encodedApp(a ProbeApplication) []byte { b, _ := json.Marshal(a); return b }

func TestR13CrashEachSnapshotChunkAndInstallBoundary(t *testing.T) {
	cases := []struct {
		phase      string
		occurrence int
	}{{"snapshot-begin", 1}, {"snapshot-chunk", 1}, {"snapshot-chunk", 2}, {"snapshot-chunk", 3}, {"snapshot-complete", 1}, {"persist-write", 1}, {"persist-flush", 1}, {"persist-ack", 1}, {"apply-write", 1}, {"apply-flush", 1}, {"apply-ack", 1}}
	for _, test := range cases {
		t.Run(fmt.Sprintf("%s-%d", test.phase, test.occurrence), func(t *testing.T) {
			s := snapshotGroup(t)
			leader := s.Leader()
			target := 3
			before := encodedApp(s.Nodes[target].Stable.App)
			s.Do(Action{Kind: "arm-cut", Node: target, Other: test.occurrence, Count: 1, Request: model.Call{Kind: test.phase}})
			isolate(s, target, false)
			s.Ticks(2)
			s.Drain()
			if !s.CutHit {
				t.Fatalf("snapshot cut not reached: %s #%d", test.phase, test.occurrence)
			}
			if strings.HasPrefix(test.phase, "snapshot-") && !bytes.Equal(before, encodedApp(s.Nodes[target].Stable.App)) {
				t.Fatal("incomplete transfer changed application")
			}
			s.Do(Action{Kind: "restart", Node: target})
			s.Ticks(3)
			s.Drain()
			if s.Err != nil {
				finish(t, s)
			}
			if !bytes.Equal(encodedApp(s.Nodes[target].Stable.App), encodedApp(s.Nodes[leader].Stable.App)) {
				t.Logf("leader=%d target index=%d status=%+v leader index=%d status=%+v queue=%d core=%v", leader, s.Nodes[target].Stable.Applied, s.Nodes[target].Status, s.Nodes[leader].Stable.Applied, s.Nodes[leader].Status, len(s.queue), s.Err)
				for _, trace := range s.Trace[max(0, len(s.Trace)-80):] {
					t.Logf("%+v", trace)
				}
				t.Fatal("snapshot recovery lost state/fences/revocations/dedup")
			}
			if !proto.Equal(s.Nodes[target].Stable.Conf, s.Nodes[leader].Stable.Conf) {
				t.Fatal("snapshot recovery lost learner configuration")
			}
			finish(t, s)
		})
	}
}
