package raftstore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

func snapshotSource(t *testing.T) (*SQLite, *pb.Snapshot) {
	t.Helper()
	s := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	seed(t, s)
	// Keep a previous complete generation before a joint configuration and a
	// near-limit opaque value force the next snapshot across several chunks.
	_, e := s.CreateSnapshot(testContext)
	must(t, e)
	must(t, s.Persist(testContext, replacement()))
	a, u := nextApply()
	u.Writes = append(u.Writes, Write{Key: Key{State, "large"}, Value: bytes.Repeat([]byte{0, 255, 13, 17}, MaxRecordBytes/4)})
	must(t, s.Apply(testContext, a, u))
	p, e := s.CreateSnapshot(testContext)
	must(t, e)
	return s, p
}
func stageCopy(t *testing.T, source, target *SQLite, p *pb.Snapshot, omit int) string {
	t.Helper()
	id, e := target.BeginSnapshot(testContext, p.Data)
	must(t, e)
	m, e := raftsnapshot.Decode(p.Data)
	must(t, e)
	for offset := uint64(0); offset < m.Chunks; offset++ {
		if int(offset) == omit {
			continue
		}
		b, e := source.ReadSnapshotChunk(testContext, id, offset)
		must(t, e)
		must(t, target.PutSnapshotChunk(testContext, id, offset, b, raftsnapshot.ID(b)))
	}
	return id
}
func installBatch(p *pb.Snapshot) raftcore.PersistBatch {
	return raftcore.PersistBatch{Snapshot: p, HardState: hard(max(4, p.GetMetadata().GetTerm()), 2, p.GetMetadata().GetIndex())}
}
func rowInventory(t *testing.T, s *SQLite) map[Key]string {
	t.Helper()
	out := map[Key]string{}
	rows, e := s.db.Query(`SELECT namespace,key,data FROM records ORDER BY namespace,key`)
	must(t, e)
	defer rows.Close()
	for rows.Next() {
		var k Key
		var b []byte
		must(t, rows.Scan(&k.Namespace, &k.Key, &b))
		out[k] = string(b)
	}
	must(t, rows.Err())
	return out
}
func assertInventory(t *testing.T, a, b map[Key]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatal("R28 record count changed")
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("R13/R17/R28 record changed in %s", k.Namespace)
		}
	}
}

// R13 and R28 require complete logical-content parity across a streamed
// snapshot; R12 includes every joint configuration field, and R17 exact results.
func TestR12R13R17R28SnapshotReplayAndRetainedGenerations(t *testing.T) {
	source, p := snapshotSource(t)
	before := rowInventory(t, source)
	m, e := raftsnapshot.Decode(p.Data)
	must(t, e)
	if m.Chunks < 4 {
		t.Fatal("R13 fixture did not cross chunk boundaries")
	}
	target := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	id := stageCopy(t, source, target, p, -1)
	delivered, e := target.CompleteSnapshot(testContext, id)
	must(t, e)
	if !proto.Equal(delivered, p) {
		t.Fatal("R14 manifest pointer changed")
	}
	must(t, target.Persist(testContext, installBatch(p)))
	r := recovered(t, target)
	if r.Core.Applied != m.Index || !proto.Equal(r.Core.ConfState, p.Metadata.ConfState) || !proto.Equal(r.Core.Snapshot, p) || len(r.Core.Entries) != 0 {
		t.Fatal("R12/R13 snapshot recovery changed metadata")
	}
	assertInventory(t, before, rowInventory(t, target))
	must(t, target.Apply(testContext, raftcore.ApplyBatch{Snapshot: p, Index: m.Index, ConfState: r.Application.ConfState}, Update{ExpectedApplied: m.Index, VisibleVersion: r.Application.VisibleVersion, LogicalTime: r.Application.LogicalTime}))
	// Preserve a committed suffix for native replay after the snapshot boundary.
	suffix := entry(4, 4, "")
	must(t, target.Persist(testContext, raftcore.PersistBatch{Entries: []*pb.Entry{suffix}, HardState: hard(4, 2, 4)}))
	path := copyDurable(t, targetPath(t, target))
	reopened := openTest(t, path)
	r = recovered(t, reopened)
	if len(r.Core.Entries) != 1 || r.Core.Entries[0].GetIndex() != 4 || r.Core.Applied != 3 {
		t.Fatal("R13 committed suffix was not retained for replay")
	}
	core, e := raftcore.New(raftcore.Config{ClusterUUID: testIdentity.ClusterUUID, NodeID: 1, Protocol: 1, Seed: 99}, r.Core)
	must(t, e)
	effect, e := core.Step(raftcore.Poll{})
	must(t, e)
	if effect.Persist == nil {
		t.Fatal("R13 committed suffix did not replay")
	}
	must(t, reopened.Persist(testContext, *effect.Persist))
	effect, e = core.Step(raftcore.Persisted{BatchID: effect.Persist.ID})
	must(t, e)
	if effect.Apply == nil || effect.Apply.Index != 4 {
		t.Fatal("R13 replay skipped committed suffix")
	}
	must(t, reopened.Apply(testContext, *effect.Apply, Update{ExpectedApplied: 3, VisibleVersion: r.Application.VisibleVersion, LogicalTime: r.Application.LogicalTime}))
	_, e = core.Step(raftcore.Applied{BatchID: effect.Apply.ID})
	must(t, e)
	newer, e := reopened.CreateSnapshot(testContext)
	must(t, e)
	must(t, reopened.Compact(testContext, 4))
	_, e = core.Step(raftcore.CompactSnapshot{Snapshot: newer})
	must(t, e)
	assertInventory(t, before, rowInventory(t, reopened))
	// A third checkpoint retires only consensus chunks from the oldest complete
	// generation. Application rows and the second generation stay byte exact.
	must(t, reopened.Persist(testContext, raftcore.PersistBatch{Entries: []*pb.Entry{entry(5, 4, "")}, HardState: hard(4, 2, 5)}))
	must(t, reopened.Apply(testContext, raftcore.ApplyBatch{Index: 5, Entries: []*pb.Entry{entry(5, 4, "")}, ConfState: r.Application.ConfState}, Update{ExpectedApplied: 4, VisibleVersion: r.Application.VisibleVersion, LogicalTime: r.Application.LogicalTime}))
	_, e = reopened.CreateSnapshot(testContext)
	must(t, e)
	var count int
	must(t, reopened.db.QueryRow(`SELECT count(*) FROM snapshots WHERE complete=1`).Scan(&count))
	if count != 2 {
		t.Fatal("R13 did not retain exactly two complete generations")
	}
	for _, pointer := range []*pb.Snapshot{newer, recovered(t, reopened).Core.Snapshot} {
		b, e := reopened.ReadSnapshotChunk(testContext, raftsnapshot.ID(pointer.Data), 0)
		must(t, e)
		if len(b) == 0 {
			t.Fatal("R13 retained generation has no bytes")
		}
		restored := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
		id := stageCopy(t, reopened, restored, pointer, -1)
		_, e = restored.CompleteSnapshot(testContext, id)
		must(t, e)
		must(t, restored.Persist(testContext, installBatch(pointer)))
		if !proto.Equal(recovered(t, restored).Core.Snapshot, pointer) {
			t.Fatal("R13 retained generation could not restore a fresh member")
		}
		assertInventory(t, before, rowInventory(t, restored))
	}
	assertInventory(t, before, rowInventory(t, reopened))
}

func targetPath(t *testing.T, s *SQLite) string {
	t.Helper()
	var seq int
	var name, path string
	must(t, s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	return path
}

// R13 requires every missing chunk and corrupt chunk to leave the previously
// installed generation authoritative. A transfer cannot acknowledge upload as
// installation, and a retry cannot overwrite accepted bytes with other bytes.
func TestR13PartialCorruptAndConflictingSnapshots(t *testing.T) {
	source, p := snapshotSource(t)
	m, e := raftsnapshot.Decode(p.Data)
	must(t, e)
	for offset := 0; offset < int(m.Chunks); offset++ {
		t.Run(fmt.Sprintf("missing-%d", offset), func(t *testing.T) {
			target := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
			seed(t, target)
			old, e := target.CreateSnapshot(testContext)
			must(t, e)
			id := stageCopy(t, source, target, p, offset)
			if _, e = target.CompleteSnapshot(testContext, id); !errors.Is(e, ErrSnapshot) {
				t.Fatalf("R13 incomplete snapshot completed: %v", e)
			}
			if e = target.Persist(testContext, installBatch(p)); !errors.Is(e, ErrSnapshot) {
				t.Fatal("R13 incomplete snapshot installed")
			}
			restart := openTest(t, copyDurable(t, targetPath(t, target)))
			if !proto.Equal(recovered(t, restart).Core.Snapshot, old) {
				t.Fatal("R13 incomplete staging replaced the last complete generation")
			}
		})
		t.Run(fmt.Sprintf("corrupt-%d", offset), func(t *testing.T) {
			target := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
			id := stageCopy(t, source, target, p, -1)
			b, e := source.ReadSnapshotChunk(testContext, id, uint64(offset))
			must(t, e)
			b[0] ^= 1
			if e = target.PutSnapshotChunk(testContext, id, uint64(offset), b, raftsnapshot.ID(b)); !errors.Is(e, ErrSnapshot) {
				t.Fatal("R13 conflicting chunk replaced durable bytes")
			}
			_, e = target.db.Exec(`UPDATE snapshot_chunks SET data=? WHERE generation=? AND offset=?`, b, id, indexKey(uint64(offset)))
			must(t, e)
			if _, e = target.CompleteSnapshot(testContext, id); e == nil {
				t.Fatal("R13 corrupt chunk completed")
			}
		})
	}
	target := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	id := stageCopy(t, source, target, p, -1)
	_, e = target.CompleteSnapshot(testContext, id)
	must(t, e)
	must(t, target.Persist(testContext, installBatch(p)))
	_, e = target.db.Exec(`DELETE FROM snapshot_chunks WHERE generation=? AND offset=?`, id, indexKey(0))
	must(t, e)
	if _, e = target.Recover(testContext); !errors.Is(e, ErrCorrupt) {
		t.Fatal("R13 corrupt authoritative snapshot did not refuse recovery")
	}
}

// R13's process helper exits at actual SQLite boundaries without Close or a
// deferred rollback, preserving only durable database/WAL bytes for the parent.
func TestR13SnapshotCrashHelper(t *testing.T) {
	mode := os.Getenv("P05_MODE")
	if mode == "" {
		return
	}
	s, e := Open(os.Getenv("P05_PATH"), testIdentity)
	must(t, e)
	number, e := strconv.Atoi(os.Getenv("P05_NUMBER"))
	must(t, e)
	count := 0
	s.cut = func(phase string) {
		if phase == os.Getenv("P05_PHASE") {
			count++
			if count == number {
				os.Exit(86)
			}
		}
	}
	switch mode {
	case "begin":
		data, e2 := os.ReadFile(os.Getenv("P05_POINTER"))
		must(t, e2)
		p := &pb.Snapshot{}
		must(t, proto.Unmarshal(data, p))
		_, e = s.BeginSnapshot(testContext, p.Data)
	case "create":
		_, e = s.CreateSnapshot(testContext)
	case "compact":
		e = s.Compact(testContext, 3)
	case "install":
		data, e2 := os.ReadFile(os.Getenv("P05_POINTER"))
		must(t, e2)
		p := &pb.Snapshot{}
		must(t, proto.Unmarshal(data, p))
		e = s.Persist(testContext, installBatch(p))
	case "complete":
		_, e = s.CompleteSnapshot(testContext, os.Getenv("P05_ID"))
	case "chunk":
		data, e2 := os.ReadFile(os.Getenv("P05_CHUNK"))
		must(t, e2)
		e = s.PutSnapshotChunk(testContext, os.Getenv("P05_ID"), 0, data, raftsnapshot.ID(data))
	}
	must(t, e)
	t.Fatal("R13 requested snapshot crash boundary not reached")
}

// R3/R13 cover every chunk, record and pointer switch plus both sides of the
// install COMMIT. Recovery observes one complete application generation.
func TestR3R13SnapshotCrashBoundaries(t *testing.T) {
	source, p := snapshotSource(t)
	m, e := raftsnapshot.Decode(p.Data)
	must(t, e)
	for _, mode := range []string{"begin", "create", "install", "compact", "complete", "chunk"} {
		phases := []struct {
			name  string
			count int
		}{{"after-begin", 1}, {"before-commit", 1}, {"after-commit", 1}}
		switch mode {
		case "begin":
			phases = append(phases, struct {
				name  string
				count int
			}{"after-snapshot-stage", 1})
		case "create":
			phases = append(phases, struct {
				name  string
				count int
			}{"after-snapshot-source-row", 7}, struct {
				name  string
				count int
			}{"after-snapshot-created-chunk", int(m.Chunks)}, struct {
				name  string
				count int
			}{"after-snapshot-pointer", 1})
		case "install":
			for _, phase := range []string{"after-snapshot-clear", "after-snapshot-application", "after-snapshot-pointer", "after-snapshot-log", "after-hardstate", "after-log-tip"} {
				phases = append(phases, struct {
					name  string
					count int
				}{phase, 1})
			}
			phases = append(phases, struct {
				name  string
				count int
			}{"after-snapshot-row", 7})
		case "compact":
			phases = append(phases, struct {
				name  string
				count int
			}{"after-compaction", 1})
		case "complete":
			phases = append(phases, struct {
				name  string
				count int
			}{"after-snapshot-complete", 1})
		case "chunk":
			phases = append(phases, struct {
				name  string
				count int
			}{"after-snapshot-chunk", 1})
		}
		for _, phase := range phases {
			for number := 1; number <= phase.count; number++ {
				t.Run(fmt.Sprintf("%s/%s/%d", mode, phase.name, number), func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "member.sqlite")
					s := openTest(t, path)
					seed(t, s)
					old, e := s.CreateSnapshot(testContext)
					must(t, e)
					var id string
					if mode == "create" || mode == "compact" {
						must(t, s.Persist(testContext, replacement()))
						a, u := nextApply()
						u.Writes = append(u.Writes, Write{Key: Key{State, "large"}, Value: bytes.Repeat([]byte{0, 255, 13, 17}, MaxRecordBytes/4)})
						must(t, s.Apply(testContext, a, u))
						if mode == "compact" {
							_, e = s.CreateSnapshot(testContext)
							must(t, e)
						}
					} else if mode != "begin" {
						omit := -1
						if mode == "chunk" {
							omit = 0
						}
						id = stageCopy(t, source, s, p, omit)
						if mode == "install" {
							_, e = s.CompleteSnapshot(testContext, id)
							must(t, e)
						}
					}
					pointer := filepath.Join(t.TempDir(), "pointer")
					b, e := wire.Marshal(p)
					must(t, e)
					must(t, os.WriteFile(pointer, b, 0600))
					chunk := filepath.Join(t.TempDir(), "chunk")
					b, e = source.ReadSnapshotChunk(testContext, raftsnapshot.ID(p.Data), 0)
					must(t, e)
					must(t, os.WriteFile(chunk, b, 0600))
					must(t, s.Close())
					cmd := exec.Command(os.Args[0], "-test.run=^TestR13SnapshotCrashHelper$", "-test.count=1")
					cmd.Env = append(os.Environ(), "P05_MODE="+mode, "P05_PATH="+path, "P05_PHASE="+phase.name, "P05_NUMBER="+strconv.Itoa(number), "P05_ID="+id, "P05_POINTER="+pointer, "P05_CHUNK="+chunk)
					output, e := cmd.CombinedOutput()
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != 86 {
						t.Fatalf("R13 process did not cut: %v %s", e, output)
					}
					restored := openTest(t, copyDurable(t, path))
					r := recovered(t, restored)
					committed := phase.name == "after-commit"
					if mode == "install" {
						if committed {
							if !proto.Equal(r.Core.Snapshot, p) || r.Application.Applied != 3 {
								t.Fatal("R13 completed install not durable")
							}
							assertInventory(t, rowInventory(t, source), rowInventory(t, restored))
						} else {
							if !proto.Equal(r.Core.Snapshot, old) || r.Application.Applied != 1 {
								t.Fatal("R13 partial install escaped")
							}
							for _, v := range rowInventory(t, restored) {
								if v != "old" {
									t.Fatal("R13 old application partially replaced")
								}
							}
						}
					}
					if mode == "create" {
						want := old
						if committed {
							want = p
						}
						if !proto.Equal(r.Core.Snapshot, want) {
							t.Fatal("R13 snapshot publication was not atomic")
						}
						assertInventory(t, rowInventory(t, source), rowInventory(t, restored))
					}
					if mode == "compact" {
						if !proto.Equal(r.Core.Snapshot, p) {
							t.Fatal("R13 compaction changed checkpoint")
						}
						var count int
						must(t, restored.db.QueryRow(`SELECT count(*) FROM raft_log`).Scan(&count))
						if (count == 0) != committed {
							t.Fatal("R13 compaction cut changed retained suffix")
						}
					}
					if mode == "begin" {
						if !proto.Equal(r.Core.Snapshot, old) {
							t.Fatal("R13 staging changed installed snapshot")
						}
						var count int
						must(t, restored.db.QueryRow(`SELECT count(*) FROM snapshots WHERE id=?`, raftsnapshot.ID(p.Data)).Scan(&count))
						if (count == 1) != committed {
							t.Fatal("R13 stage publication was not atomic")
						}
					}
					if mode == "complete" || mode == "chunk" {
						if !proto.Equal(r.Core.Snapshot, old) {
							t.Fatal("R13 upload changed installed snapshot")
						}
						var complete int
						must(t, restored.db.QueryRow(`SELECT complete FROM snapshots WHERE id=?`, id).Scan(&complete))
						if mode == "complete" && (complete == 1) != committed {
							t.Fatal("R13 completion was not atomic")
						}
						if mode == "chunk" {
							var count int
							must(t, restored.db.QueryRow(`SELECT count(*) FROM snapshot_chunks WHERE generation=? AND offset=?`, id, indexKey(0)).Scan(&count))
							if (count == 1) != committed {
								t.Fatal("R13 chunk was not atomic")
							}
						}
					}
				})
			}
		}
	}
}

// R11/R13 require a learner that missed the compacted prefix to install the
// snapshot while the healthy voter majority keeps committing ordinary writes.
func TestR11R13NativeLaggingLearnerDoesNotPinLog(t *testing.T) {
	g := newNativeGroup(t)
	leader := g.elect()
	if leader == 2 {
		g.call(leader, raftcore.Transfer{Target: 1})
		g.pump()
		leader = g.elect()
	}
	change, e := wire.Marshal(&pb.ConfChangeV2{Changes: []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(3)}}})
	must(t, e)
	g.call(leader, raftcore.Configure{Change: change})
	g.pump()
	conf := recovered(t, g.stores[0]).Core.ConfState
	if len(conf.Voters) != 2 || conf.Voters[0] != 1 || conf.Voters[1] != 2 || len(conf.Learners) != 1 || conf.Learners[0] != 3 || len(conf.VotersOutgoing) != 0 {
		t.Fatalf("R11 fixture did not finish learner demotion: %v", conf)
	}
	g.propose(leader, 1, clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: "cell", Init: clusterstate.CellInit{Head: "head-1"}, Receipt: "uploaded"}})
	g.crash(2)
	g.maintain = true
	for seq := uint64(2); seq <= 88; seq++ {
		g.propose(leader, seq, clusterstate.Mutation{Publish: &clusterstate.PublishCell{Cell: "cell", Publish: clusterstate.Publish{OldHead: fmt.Sprintf("head-%d", seq-1), Head: fmt.Sprintf("head-%d", seq), Fence: 1, Title: strings.Repeat("x", 53<<10)}, Receipt: "uploaded"}})
	}
	for _, i := range []int{0, 1} {
		r := recovered(t, g.stores[i])
		if r.Core.Snapshot.GetMetadata().GetIndex() == 0 {
			t.Fatal("R11 applied byte threshold did not checkpoint")
		}
		var size uint64
		must(t, g.stores[i].db.QueryRow(`SELECT coalesce(sum(length(data)),0) FROM raft_log`).Scan(&size))
		if size > raftcore.SnapshotLogBytes+raftcore.MaxMessageBytes {
			t.Fatal("R11 learner pinned the durable log")
		}
		if len(r.Core.Entries) > 20 {
			t.Fatal("R11 recovery still materialized compacted history")
		}
	}
	g.start(2)
	g.pump()
	for range 3 {
		g.tick()
	}
	if g.snapshotsSent == 0 || g.cell(2).Head != "head-88" || g.cell(2).Lease.Fence != 1 {
		t.Fatal("R11/R13 learner did not install and replay snapshot")
	}
	if g.status[2].Role == raft.StateLeader {
		t.Fatal("R12 learner regained a vote")
	}
	g.crash(2)
	g.start(2)
	g.pump()
	if g.cell(2).Head != "head-88" {
		t.Fatal("R13 installed learner snapshot did not survive native restart")
	}
}

// R13/R28 forbid compaction without an applied durable checkpoint, and reject
// unexpected schema/identity or damaged manifests without deleting evidence.
func TestR13R28SnapshotAdmissionAndCompactionRefusals(t *testing.T) {
	s, p := snapshotSource(t)
	if e := s.Compact(testContext, 2); !errors.Is(e, ErrSnapshot) {
		t.Fatal("R13 compacted an uncovered index")
	}
	before := rowInventory(t, s)
	must(t, s.Compact(testContext, 3))
	assertInventory(t, before, rowInventory(t, s))
	for _, bad := range [][]byte{[]byte(`{}`), append(bytes.Clone(p.Data), ' '), bytes.Replace(p.Data, []byte(`"cluster":"store-test"`), []byte(`"cluster":"other"`), 1)} {
		if _, e := s.BeginSnapshot(testContext, bad); !errors.Is(e, ErrSnapshot) {
			t.Fatal("R13 invalid manifest admitted")
		}
	}
}

// R11 requires both entry-count and byte triggers to measure applied history,
// and never compact uncommitted work or retained application records.
func TestR11R13AppliedEntryTriggerAndUncommittedSuffix(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	entries := make([]*pb.Entry, raftcore.SnapshotEntries+1)
	for i := range entries {
		entries[i] = entry(uint64(i+1), 1, "")
	}
	must(t, s.Persist(testContext, raftcore.PersistBatch{Entries: entries, HardState: hard(1, 1, raftcore.SnapshotEntries)}))
	due, e := s.SnapshotDue(testContext)
	must(t, e)
	if due {
		t.Fatal("R11 uncommitted/unapplied entries triggered a checkpoint")
	}
	must(t, s.Apply(testContext, raftcore.ApplyBatch{Index: raftcore.SnapshotEntries, Entries: entries[:raftcore.SnapshotEntries], ConfState: &pb.ConfState{Voters: []uint64{1, 2, 3}}}, Update{Writes: allRows("retained")}))
	due, e = s.SnapshotDue(testContext)
	must(t, e)
	if !due {
		t.Fatal("R11 applied entry threshold did not trigger")
	}
	p, e := s.CreateSnapshot(testContext)
	must(t, e)
	must(t, s.Compact(testContext, p.GetMetadata().GetIndex()))
	r := recovered(t, s)
	if len(r.Core.Entries) != 1 || r.Core.Entries[0].GetIndex() != raftcore.SnapshotEntries+1 {
		t.Fatal("R13 compaction removed uncommitted suffix")
	}
	due, e = s.SnapshotDue(testContext)
	must(t, e)
	if due {
		t.Fatal("R11 compacted history counted toward next trigger")
	}
	if len(rowInventory(t, s)) != 6 {
		t.Fatal("R28 compaction removed retained records")
	}
}

// R3/R13 require a chunk write failure to stop participation, even when it
// occurs inside the streaming row encoder before manifest publication.
func TestR3R13SnapshotWriteFailureStopsMember(t *testing.T) {
	s, p := snapshotSource(t)
	must(t, s.Persist(testContext, raftcore.PersistBatch{Entries: []*pb.Entry{entry(4, 4, "")}, HardState: hard(4, 2, 4)}))
	a := recovered(t, s).Application
	must(t, s.Apply(testContext, raftcore.ApplyBatch{Index: 4, Entries: []*pb.Entry{entry(4, 4, "")}, ConfState: a.ConfState}, Update{ExpectedApplied: 3, VisibleVersion: a.VisibleVersion, LogicalTime: a.LogicalTime}))
	_, e := s.db.Exec(`CREATE TRIGGER fail_chunk BEFORE INSERT ON snapshot_chunks BEGIN SELECT RAISE(ABORT,'fixture storage failure'); END`)
	must(t, e)
	if _, e = s.CreateSnapshot(testContext); e == nil {
		t.Fatal("R3 failed chunk write reported successful snapshot")
	}
	if e = s.Persist(testContext, raftcore.PersistBatch{}); !errors.Is(e, ErrStopped) {
		t.Fatal("R3 failed chunk writer left member participating")
	}
	restored := openTest(t, copyDurable(t, targetPath(t, s)))
	r := recovered(t, restored)
	if !proto.Equal(r.Core.Snapshot, p) || r.Core.Applied != 4 {
		t.Fatal("R13 failed checkpoint changed authoritative bytes")
	}
}

// R13 binds chunks to the manifest's whole-stream digest even when each
// individual transport hash is valid. R11 bounds completed staging to one slot.
func TestR11R13StreamDigestAndPendingGenerationAdmission(t *testing.T) {
	source, p := snapshotSource(t)
	original, e := raftsnapshot.Decode(p.Data)
	must(t, e)
	target := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	bad := original
	bad.Digest = strings.Repeat("0", 64)
	data, e := bad.Encode()
	must(t, e)
	id, e := target.BeginSnapshot(testContext, data)
	must(t, e)
	for offset := uint64(0); offset < original.Chunks; offset++ {
		b, e := source.ReadSnapshotChunk(testContext, raftsnapshot.ID(p.Data), offset)
		must(t, e)
		must(t, target.PutSnapshotChunk(testContext, id, offset, b, raftsnapshot.ID(b)))
	}
	if _, e = target.CompleteSnapshot(testContext, id); !errors.Is(e, ErrSnapshot) {
		t.Fatal("R13 individually valid chunks bypassed the stream digest")
	}
	if !recovered(t, target).Fresh {
		t.Fatal("R13 rejected stream became authoritative")
	}
	id = stageCopy(t, source, target, p, -1)
	_, e = target.CompleteSnapshot(testContext, id)
	must(t, e)
	newer := original
	newer.Index++
	data, e = newer.Encode()
	must(t, e)
	if _, e = target.BeginSnapshot(testContext, data); !errors.Is(e, ErrSnapshot) {
		t.Fatal("R11 a second completed transfer displaced the staging slot")
	}
	must(t, target.Persist(testContext, installBatch(p)))
	// Installing the pending generation releases the slot without deleting its
	// complete checkpoint or any application records.
	_, e = target.BeginSnapshot(testContext, data)
	must(t, e)
	if !proto.Equal(recovered(t, target).Core.Snapshot, p) {
		t.Fatal("R13 new staging changed installed generation")
	}
}

// R13 permits log catch-up to reach a completed but uninstalled transfer's
// exact checkpoint after restart. Local snapshot creation must remain resumable.
func TestR13LocalCheckpointReusesCompletedTransfer(t *testing.T) {
	source, p := snapshotSource(t)
	target := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	seed(t, target)
	id := stageCopy(t, source, target, p, -1)
	_, e := target.CompleteSnapshot(testContext, id)
	must(t, e)
	must(t, target.Persist(testContext, replacement()))
	a, u := nextApply()
	u.Writes = append(u.Writes, Write{Key: Key{State, "large"}, Value: bytes.Repeat([]byte{0, 255, 13, 17}, MaxRecordBytes/4)})
	must(t, target.Apply(testContext, a, u))
	local, e := target.CreateSnapshot(testContext)
	must(t, e)
	if !proto.Equal(local, p) {
		t.Fatal("R13 local checkpoint differs from the identical completed transfer")
	}
	must(t, target.Compact(testContext, 3))
	assertInventory(t, rowInventory(t, source), rowInventory(t, target))
}
