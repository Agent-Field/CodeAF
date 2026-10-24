package raftstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

var testIdentity = Identity{"store-test", 1, raftcore.ProtocolVersion}
var testContext = context.Background()

func entry(index, term uint64, data string) *pb.Entry {
	return &pb.Entry{Index: proto.Uint64(index), Term: proto.Uint64(term), Type: pb.EntryNormal.Enum(), Data: []byte(data)}
}

func hard(term, vote, commit uint64) *pb.HardState {
	return &pb.HardState{Term: proto.Uint64(term), Vote: proto.Uint64(vote), Commit: proto.Uint64(commit)}
}

func openTest(t *testing.T, path string) *SQLite {
	t.Helper()
	s, err := Open(path, testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func recovered(t *testing.T, s *SQLite) Recovery {
	t.Helper()
	r, err := s.Recover(testContext)
	must(t, err)
	return r
}

func seed(t *testing.T, s *SQLite) {
	t.Helper()
	must(t, s.Persist(testContext, raftcore.PersistBatch{ID: 1, HardState: hard(2, 2, 1), Entries: []*pb.Entry{entry(1, 1, ""), entry(2, 2, "old-two"), entry(3, 2, "old-three")}}))
	must(t, s.Apply(testContext, raftcore.ApplyBatch{ID: 1, Index: 1, Entries: []*pb.Entry{entry(1, 1, "")}, ConfState: &pb.ConfState{Voters: []uint64{1, 2, 3}}}, Update{ExpectedApplied: 0, VisibleVersion: 7, LogicalTime: 1000, Writes: allRows("old")}))
}

func replacement() raftcore.PersistBatch {
	return raftcore.PersistBatch{ID: 2, HardState: hard(3, 3, 3), Entries: []*pb.Entry{entry(2, 3, "new-two"), entry(3, 3, "new-three")}}
}

func allRows(value string) []Write {
	var out []Write
	for _, namespace := range []Namespace{State, Sessions, Membership, Enrollment, Tombstones, Outbox} {
		out = append(out, Write{Key: Key{namespace, "retained"}, Value: []byte(value)})
	}
	return out
}

func nextApply() (raftcore.ApplyBatch, Update) {
	p := replacement()
	return raftcore.ApplyBatch{ID: 2, Index: 3, Entries: p.Entries, ConfState: &pb.ConfState{Voters: []uint64{1, 2, 4}, VotersOutgoing: []uint64{1, 2, 3}, Learners: []uint64{5}, LearnersNext: []uint64{3}, AutoLeave: proto.Bool(true)}}, Update{ExpectedApplied: 1, VisibleVersion: 8, LogicalTime: 1100, Writes: allRows("new")}
}

// R3 requires the whole Ready, including a replaced suffix and commit-only
// hard state, to survive restart before the host acknowledges persistence.
func TestR3ReadyDurabilityAndConflictingSuffix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "member.sqlite")
	s := openTest(t, path)
	if !recovered(t, s).Fresh {
		t.Fatal("new member is not empty")
	}
	seed(t, s)
	must(t, s.Persist(testContext, replacement()))
	r := recovered(t, s)
	if r.Core.Applied != 1 || r.Core.HardState.GetCommit() != 3 || len(r.Core.Entries) != 3 || string(r.Core.Entries[1].Data) != "new-two" {
		t.Fatal("Ready was not stored together with the entire new suffix")
	}
	// Reject committed-prefix replacement, holes and double votes rather than
	// allowing a malformed host batch to rewrite already acknowledged history.
	bad := []raftcore.PersistBatch{
		{HardState: hard(2, 2, 3)},
		{HardState: hard(3, 2, 3)},
		{HardState: hard(3, 0, 3)},
		{HardState: hard(3, 3, 2)},
		{HardState: hard(3, 3, 4)},
		{Entries: []*pb.Entry{entry(5, 3, "hole")}},
		{Entries: []*pb.Entry{entry(2, 3, "changed-commit")}},
		{Entries: []*pb.Entry{entry(1, 1, "")}},
		{Entries: []*pb.Entry{nil}},
	}
	for i, batch := range bad {
		if err := s.Persist(testContext, batch); !errors.Is(err, ErrBatch) {
			t.Fatalf("invalid batch %d: %v", i, err)
		}
	}
	must(t, s.Close())
	s = openTest(t, path)
	r = recovered(t, s)
	if !proto.Equal(r.Core.HardState, hard(3, 3, 3)) || string(r.Core.Entries[2].Data) != "new-three" {
		t.Fatal("restart changed the durable term/vote/suffix")
	}
}

// R12 and R17 require complete configuration and exact dedup bytes to share
// the applied-index transaction. R28 keeps every application namespace intact.
func TestR12R17AppliedReadSetCASAndRecords(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	seed(t, s)
	must(t, s.Persist(testContext, replacement()))
	a, u := nextApply()
	must(t, s.Apply(testContext, a, u))
	r := recovered(t, s)
	if r.Application.VisibleVersion != 8 || r.Application.LogicalTime != 1100 || !proto.Equal(r.Core.ConfState, a.ConfState) {
		t.Fatal("applied metadata/configuration lost fields")
	}
	var keys []Key
	for _, w := range u.Writes {
		keys = append(keys, w.Key)
	}
	keys = append(keys, Key{State, "missing"})
	rows, app, err := s.Read(testContext, 3, keys)
	must(t, err)
	if len(rows) != 6 || app.RecordCount != 6 || app.Applied != 3 {
		t.Fatal("lost retained state or invented a missing record")
	}
	for _, data := range rows {
		if string(data) != "new" {
			t.Fatal("dedup/application result does not match applied index")
		}
	}
	rows[keys[0]][0] = 'X'
	again, _, err := s.Read(testContext, 3, keys[:1])
	must(t, err)
	if string(again[keys[0]]) != "new" {
		t.Fatal("caller mutated retained storage")
	}
	if err := s.Apply(testContext, a, u); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale apply read-set: %v", err)
	}
	if _, _, err := s.Read(testContext, 1, keys); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale storage read-set: %v", err)
	}
	// A second connection observes the same CAS, rather than overwriting rows
	// evaluated before the first connection committed its newer index.
	other, err := Open(databasePath(t, s), testIdentity)
	must(t, err)
	defer other.Close()
	if err := other.Apply(testContext, a, u); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-connection applied CAS: %v", err)
	}
}

func databasePath(t *testing.T, s *SQLite) string {
	t.Helper()
	var seq int
	var name, path string
	must(t, s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path))
	return path
}

// R3's recovery boundary is tested by exiting WITHOUT Close or deferred
// rollback. Parent tests copy only durable database/WAL files, never memory or
// the shared-memory index, into a distinct member directory before recovery.
func TestR3CrashHelper(t *testing.T) {
	mode := os.Getenv("P04_HELPER")
	if mode == "" {
		return
	}
	s, err := Open(os.Getenv("P04_PATH"), testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "flush" {
		for _, step := range []struct {
			name  string
			batch raftcore.PersistBatch
		}{
			{"term-vote", raftcore.PersistBatch{HardState: hard(4, 2, 0), Entries: []*pb.Entry{entry(1, 4, "")}}},
			{"commit-only", raftcore.PersistBatch{HardState: hard(4, 2, 1)}},
		} {
			os.Stderr.WriteString("R3-BEGIN-" + step.name + "\n")
			if err := s.Persist(testContext, step.batch); err != nil {
				t.Fatal(err)
			}
			os.Stderr.WriteString("R3-END-" + step.name + "\n")
		}
		os.Stderr.WriteString("R3-BEGIN-apply\n")
		must(t, s.Apply(testContext, raftcore.ApplyBatch{Index: 1, Entries: []*pb.Entry{entry(1, 4, "")}, ConfState: &pb.ConfState{Voters: []uint64{1, 2, 3}}}, Update{Writes: allRows("durable-result")}))
		os.Stderr.WriteString("R3-END-apply\n")
		os.Exit(0)
	}
	if mode == "apply" {
		must(t, s.Persist(testContext, replacement()))
	}
	cutNumber, err := strconv.Atoi(os.Getenv("P04_NUMBER"))
	must(t, err)
	count := 0
	s.cut = func(phase string) {
		if phase == os.Getenv("P04_PHASE") {
			count++
			if count == cutNumber {
				os.Exit(86)
			}
		}
	}
	if mode == "apply" {
		a, u := nextApply()
		must(t, s.Apply(testContext, a, u))
	} else {
		must(t, s.Persist(testContext, replacement()))
	}
	t.Fatal("requested crash boundary was not reached")
}

func copyDurable(t *testing.T, source string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "member.sqlite")
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(source + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		must(t, err)
		must(t, os.WriteFile(target+suffix, data, 0600))
	}
	return target
}

// R3 cuts each Ready persistence statement, then both sides of COMMIT. R12
// and R17 cut every application row, metadata and configuration statement.
func TestR3R12R17CrashEveryTransactionBoundary(t *testing.T) {
	for _, mode := range []string{"persist", "apply"} {
		cuts := map[string]int{"after-begin": 1, "before-commit": 1, "after-commit": 1}
		if mode == "persist" {
			cuts["after-truncate"], cuts["after-entry"], cuts["after-hardstate"], cuts["after-log-tip"] = 1, 2, 1, 1
		} else {
			cuts["after-row"], cuts["after-application-header"], cuts["after-configuration"] = 6, 1, 1
		}
		var phases []string
		for phase := range cuts {
			phases = append(phases, phase)
		}
		slices.Sort(phases)
		for _, phase := range phases {
			for number := 1; number <= cuts[phase]; number++ {
				t.Run(mode+"/"+phase+"/"+strconv.Itoa(number), func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "member.sqlite")
					s := openTest(t, path)
					seed(t, s)
					must(t, s.Close())
					cmd := exec.Command(os.Args[0], "-test.run=^TestR3CrashHelper$", "-test.count=1")
					cmd.Env = append(os.Environ(), "P04_HELPER="+mode, "P04_PATH="+path, "P04_PHASE="+phase, "P04_NUMBER="+strconv.Itoa(number))
					output, err := cmd.CombinedOutput()
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 86 {
						t.Fatalf("helper did not crash at %s: %v %s", phase, err, output)
					}
					s = openTest(t, copyDurable(t, path))
					r := recovered(t, s)
					committed := phase == "after-commit"
					wantNewLog := mode == "apply" || committed
					want := hard(2, 2, 1)
					data := "old-two"
					if wantNewLog {
						want, data = hard(3, 3, 3), "new-two"
					}
					if !proto.Equal(r.Core.HardState, want) || len(r.Core.Entries) != 3 || string(r.Core.Entries[1].Data) != data {
						t.Fatal("recovered a partial Ready or conflicting suffix")
					}
					wantApplied, wantRow, wantVersion, wantNow := uint64(1), "old", uint64(7), int64(1000)
					conf := &pb.ConfState{Voters: []uint64{1, 2, 3}}
					if mode == "apply" && committed {
						wantApplied, wantRow, wantVersion, wantNow = 3, "new", 8, 1100
						a, _ := nextApply()
						conf = a.ConfState
					}
					if r.Core.Applied != wantApplied || r.Application.VisibleVersion != wantVersion || r.Application.LogicalTime != wantNow || !proto.Equal(conf, r.Core.ConfState) {
						t.Fatal("applied metadata or joint configuration was partially committed")
					}
					var keys []Key
					for _, w := range allRows("") {
						keys = append(keys, w.Key)
					}
					rows, _, err := s.Read(testContext, wantApplied, keys)
					must(t, err)
					if len(rows) != len(keys) {
						t.Fatal("lost a retained application record")
					}
					for _, value := range rows {
						if string(value) != wantRow {
							t.Fatal("rows/dedup outcome is ahead of or behind the applied index")
						}
					}
				})
			}
		}
	}
}

// R3 requires real fsync/fdatasync on the WAL, including a commit-only Ready.
// This Linux proof observes the production SQLite VFS; PRAGMA alone would not
// show whether acknowledgments actually follow successful flush syscalls.
func TestR3TermVoteCommitAndApplicationFsync(t *testing.T) {
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("the syscall durability probe requires Linux strace")
	}
	path := filepath.Join(t.TempDir(), "member.sqlite")
	s := openTest(t, path)
	must(t, s.Close())
	trace := filepath.Join(t.TempDir(), "flush.trace")
	cmd := exec.Command(strace, "-f", "-yy", "-s", "200", "-e", "trace=fsync,fdatasync,write,pwrite64", "-o", trace, os.Args[0], "-test.run=^TestR3CrashHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "P04_HELPER=flush", "P04_PATH="+path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("syscall durability probe: %v %s", err, output)
	}
	data, err := os.ReadFile(trace)
	must(t, err)
	for _, phase := range []string{"term-vote", "commit-only", "apply"} {
		_, tail, found := strings.Cut(string(data), "R3-BEGIN-"+phase)
		if !found {
			t.Fatalf("missing before marker for %s", phase)
		}
		segment, _, found := strings.Cut(tail, "R3-END-"+phase)
		if !found {
			t.Fatalf("missing durable return marker for %s", phase)
		}
		flushed := false
		for _, line := range strings.Split(segment, "\n") {
			if strings.Contains(line, path+"-wal") && (strings.Contains(line, "fsync(") || strings.Contains(line, "fdatasync(")) && strings.HasSuffix(line, "= 0") {
				flushed = true
			}
		}
		if !flushed {
			t.Fatalf("no successful WAL flush before %s return: %s", phase, segment)
		}
	}
	s = openTest(t, copyDurable(t, path))
	r := recovered(t, s)
	if !proto.Equal(r.Core.HardState, hard(4, 2, 1)) || r.Core.Applied != 1 {
		t.Fatal("term, vote, committed application did not survive abrupt process exit")
	}
}

// R13 requires retaining corrupt stores rather than recreating them. Missing
// rows and log tails are corruption too, even when remaining row hashes match.
func TestR13CorruptStoreRefusalAndEvidence(t *testing.T) {
	for name, damage := range map[string]string{
		"log-bytes":     `UPDATE raft_log SET data=x'01' WHERE idx=x'0000000000000002'`,
		"log-hole":      `DELETE FROM raft_log WHERE idx=x'0000000000000002'`,
		"log-tail":      `DELETE FROM raft_log WHERE idx=x'0000000000000003'`,
		"hardstate":     `UPDATE meta SET data=x'0102' WHERE key='hardstate'`,
		"confstate":     `UPDATE meta SET checksum=x'00' WHERE key='confstate'`,
		"applied":       `UPDATE meta SET data=x'00' WHERE key='application'`,
		"state-bytes":   `UPDATE records SET data=x'00' WHERE namespace='state'`,
		"dedup-absent":  `DELETE FROM records WHERE namespace='sessions'`,
		"missing-meta":  `DELETE FROM meta WHERE key='manifest'`,
		"missing-table": `DROP TABLE records`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "member.sqlite")
			s := openTest(t, path)
			seed(t, s)
			_, err := s.db.Exec(damage)
			must(t, err)
			must(t, s.Close())
			before, err := os.ReadFile(path)
			must(t, err)
			_, err = Open(path, testIdentity)
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("damaged store opened: %v", err)
			}
			after, err := os.ReadFile(path)
			must(t, err)
			if !bytes.Equal(before, after) {
				t.Fatal("refusal rewrote authoritative evidence")
			}
		})
	}
	for _, data := range [][]byte{nil, []byte("not a SQLite database")} {
		path := filepath.Join(t.TempDir(), "member.sqlite")
		must(t, os.WriteFile(path, data, 0600))
		_, err := Open(path, testIdentity)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("invalid file silently initialized: %v", err)
		}
		got, err := os.ReadFile(path)
		must(t, err)
		if !bytes.Equal(got, data) {
			t.Fatal("invalid file changed")
		}
	}
}

// R3 and R12 prohibit restoring another node's authority from the same file.
func TestR3MemberIdentityAndFullUint64IndexKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "member ?#%.sqlite")
	s := openTest(t, path)
	seed(t, s)
	must(t, s.Close())
	for _, identity := range []Identity{{"other-cluster", 1, raftcore.ProtocolVersion}, {testIdentity.ClusterUUID, 2, raftcore.ProtocolVersion}, {testIdentity.ClusterUUID, 1, raftcore.ProtocolVersion + 1}} {
		if _, err := Open(path, identity); !errors.Is(err, ErrIdentity) {
			t.Fatalf("member/cluster/protocol mismatch: %v", err)
		}
	}
	indexes := []uint64{0, 1, 1<<53 - 1, 1 << 53, 1<<63 - 1, 1 << 63, ^uint64(0)}
	for i := 1; i < len(indexes); i++ {
		if bytes.Compare(indexKey(indexes[i-1]), indexKey(indexes[i])) >= 0 {
			t.Fatal("uint64 index collation loses order")
		}
	}
}

// R3 forbids progress after a disk error. A real SQLite page quota exercises
// the production error path; the core must receive no fabricated completion.
func TestR3StorageFailureStopsParticipation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "member.sqlite")
	s := openTest(t, path)
	seed(t, s)
	var pages int
	must(t, s.db.QueryRow(`PRAGMA page_count`).Scan(&pages))
	var limit int
	must(t, s.db.QueryRow(`PRAGMA max_page_count=`+strconv.Itoa(pages)).Scan(&limit))
	p := replacement()
	p.Entries[0].Data = bytes.Repeat([]byte{0x80}, 2<<20)
	if err := s.Persist(testContext, p); err == nil || errors.Is(err, ErrBatch) {
		t.Fatalf("SQLite disk quota did not produce a storage failure: %v", err)
	}
	if err := s.Persist(testContext, raftcore.PersistBatch{}); !errors.Is(err, ErrStopped) {
		t.Fatalf("failed store still permits acknowledgment: %v", err)
	}
	if _, err := s.Recover(testContext); !errors.Is(err, ErrStopped) {
		t.Fatalf("failed handle recovered without reopening: %v", err)
	}
	must(t, s.Close())
	s = openTest(t, path)
	r := recovered(t, s)
	if !proto.Equal(r.Core.HardState, hard(2, 2, 1)) || string(r.Core.Entries[1].Data) != "old-two" {
		t.Fatal("disk failure committed half of the conflicting suffix")
	}
	c, err := raftcore.New(raftcore.Config{ClusterUUID: testIdentity.ClusterUUID, NodeID: 1, Protocol: raftcore.ProtocolVersion, Seed: 9, Bootstrap: []uint64{1, 2, 3}}, raftcore.RecoveredState{})
	must(t, err)
	e, err := c.Step(raftcore.Poll{})
	must(t, err)
	if e.Persist == nil {
		t.Fatal("no persistence dependency to fail")
	}
	e, err = c.Step(raftcore.StorageFailed{BatchID: e.Persist.ID})
	if !errors.Is(err, raftcore.ErrStopped) || len(e.Send) != 0 || e.Apply != nil || len(e.Reads) != 0 {
		t.Fatal("storage failure released a dependent acknowledgment")
	}
}

// R3 requires refusing incomplete or invented application work. R17 requires
// a failed row write to roll back earlier rows and the dedup result together.
func TestR3R17InvalidApplyRollsBackAndEmptyReadyDoesNotWrite(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	seed(t, s)
	// A dependency-only Ready must not open a write transaction or flush a WAL.
	s.cut = func(string) { t.Fatal("empty Ready started a transaction") }
	must(t, s.Persist(testContext, raftcore.PersistBatch{ID: 2}))
	s.cut = nil
	must(t, s.Persist(testContext, replacement()))
	for _, name := range []string{"uncommitted", "hole", "wrong-entry", "wrong-end", "clock", "version", "duplicate-key", "unknown-namespace", "large-row", "nil-configuration"} {
		t.Run(name, func(t *testing.T) {
			a, u := nextApply()
			switch name {
			case "uncommitted":
				a.Index = 4
			case "hole":
				a.Entries = a.Entries[1:]
			case "wrong-entry":
				a.Entries[0] = entry(2, 3, "invented")
			case "wrong-end":
				a.Index = 2
			case "clock":
				u.LogicalTime = 999
			case "version":
				u.VisibleVersion = 6
			case "duplicate-key":
				u.Writes = append(u.Writes, u.Writes[0])
			case "unknown-namespace":
				u.Writes = append(u.Writes, Write{Key: Key{"unknown", "no"}, Value: []byte("refused")})
			case "large-row":
				u.Writes = append(u.Writes, Write{Key: Key{State, "large"}, Value: make([]byte, MaxRecordBytes+1)})
			case "nil-configuration":
				a.ConfState = nil
			}
			if err := s.Apply(testContext, a, u); !errors.Is(err, ErrBatch) {
				t.Fatalf("invalid apply succeeded: %v", err)
			}
			r := recovered(t, s)
			if r.Core.Applied != 1 || r.Application.VisibleVersion != 7 {
				t.Fatal("refused apply advanced durable state")
			}
			rows, _, err := s.Read(testContext, 1, []Key{{State, "retained"}, {Sessions, "retained"}})
			must(t, err)
			for _, value := range rows {
				if string(value) != "old" {
					t.Fatal("refused late write leaked an earlier row or dedup result")
				}
			}
		})
	}
	// An application storage error, unlike a validation refusal, stops the
	// handle. The earlier row write still rolls back with the rest of the set.
	_, err := s.db.Exec(`CREATE TRIGGER fail_session BEFORE UPDATE ON records WHEN NEW.namespace='sessions' BEGIN SELECT RAISE(ABORT,'injected storage failure'); END`)
	must(t, err)
	a, u := nextApply()
	if err := s.Apply(testContext, a, u); err == nil || errors.Is(err, ErrBatch) {
		t.Fatalf("application storage failure: %v", err)
	}
	if err := s.Apply(testContext, a, u); !errors.Is(err, ErrStopped) {
		t.Fatalf("failed application handle kept participating: %v", err)
	}
	path := databasePath(t, s)
	must(t, s.Close())
	s = openTest(t, path)
	if recovered(t, s).Core.Applied != 1 {
		t.Fatal("application storage failure moved the applied index")
	}
}

// R3 requires replacing the conflicting suffix, including deleting a longer
// uncommitted tail, rather than retaining entries from the displaced leader.
func TestR3ShorterConflictingSuffixSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "member.sqlite")
	s := openTest(t, path)
	seed(t, s)
	p := replacement()
	p.Entries = p.Entries[:1]
	p.HardState = hard(3, 3, 2)
	must(t, s.Persist(testContext, p))
	must(t, s.Close())
	s = openTest(t, path)
	r := recovered(t, s)
	if len(r.Core.Entries) != 2 || r.Core.Entries[1].GetTerm() != 3 || r.Core.HardState.GetCommit() != 2 {
		t.Fatal("old uncommitted tail survived replacement")
	}
}

// R13's snapshot lifecycle belongs to P05. Until then a snapshot cannot receive
// a false success that would permit dependent replication acknowledgments.
func TestR13SnapshotInputFailsClosed(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	seed(t, s)
	snap := &pb.Snapshot{Data: []byte("not-installed"), Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(2), Term: proto.Uint64(2)}}
	if err := s.Persist(testContext, raftcore.PersistBatch{Snapshot: snap}); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot persistence falsely succeeded: %v", err)
	}
	if err := s.Apply(testContext, raftcore.ApplyBatch{Snapshot: snap, Index: 2}, Update{ExpectedApplied: 1}); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot application falsely succeeded: %v", err)
	}
	if recovered(t, s).Core.Applied != 1 {
		t.Fatal("snapshot refusal modified authoritative state")
	}
}

// R28 preserves records outside an explicitly authorized application write-set;
// the store never interprets compaction or pressure as permission to erase them.
func TestR28ExplicitWriteSetAndReadEnvelope(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "member.sqlite"))
	seed(t, s)
	must(t, s.Persist(testContext, replacement()))
	a, u := nextApply()
	u.Writes = []Write{
		{Key: Key{State, "retained"}, Delete: true},
		{Key: Key{State, "empty"}},
		{Key: Key{State, "large-a"}, Value: make([]byte, MaxRecordBytes/2+1)},
		{Key: Key{State, "large-b"}, Value: make([]byte, MaxRecordBytes/2+1)},
	}
	must(t, s.Apply(testContext, a, u))
	r := recovered(t, s)
	if r.Application.RecordCount != 8 {
		t.Fatal("explicit writes changed an unmentioned namespace")
	}
	keys := []Key{{State, "retained"}, {State, "empty"}, {Tombstones, "retained"}, {Sessions, "retained"}}
	rows, _, err := s.Read(testContext, 3, keys)
	must(t, err)
	if _, ok := rows[keys[0]]; ok {
		t.Fatal("explicitly deleted row survived")
	}
	if empty, ok := rows[keys[1]]; !ok || len(empty) != 0 {
		t.Fatal("empty opaque value was confused with an absent row")
	}
	if string(rows[keys[2]]) != "old" || string(rows[keys[3]]) != "old" {
		t.Fatal("unmentioned tombstone or dedup record was discarded")
	}
	if rows, _, err := s.Read(testContext, 3, []Key{{State, "large-a"}, {State, "large-b"}}); !errors.Is(err, ErrBatch) || rows != nil {
		t.Fatalf("oversized evaluation read-set escaped: %v", err)
	}
}
