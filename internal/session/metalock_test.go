package session

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
)

func metaLockAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
	})
	return a
}

func seedMetaLockConversation(t *testing.T, dir string) {
	t.Helper()
	if err := SaveMeta(dir, Meta{
		ID:        filepath.Base(dir),
		Title:     "previous identity",
		Workspace: filepath.Dir(dir),
		Model:     "test/model",
		Created:   time.Unix(1_700_000_000, 0),
	}); err != nil {
		t.Fatal(err)
	}
}

// Two windows on one conversation folder never overwrite each other's
// identity. The rendezvous proves that the patches themselves cannot overlap,
// and every completed pair must leave both fields from that round on disk.
func TestTwoWindowsOnOneConversationFolderNeverOverwriteEachOthersIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	first := metaLockAgent(t, dir)
	second := metaLockAgent(t, dir)
	first.mu.Lock()
	first.stampUserLocked("diagnose parser migration failures")
	first.mu.Unlock()

	const rounds = 40
	const rendezvous = 2 * time.Millisecond
	var overlaps atomic.Int32
	losses := 0
	for i := 0; i < rounds; i++ {
		title := fmt.Sprintf("round %d name", i)
		spent, tokens := float64(i+1)/100, (i+1)*7
		var inside atomic.Int32
		meet := func() func() {
			if inside.Add(1) > 1 {
				overlaps.Add(1)
			}
			deadline := time.Now().Add(rendezvous)
			for inside.Load() < 2 && time.Now().Before(deadline) {
				time.Sleep(50 * time.Microsecond)
			}
			return func() { inside.Add(-1) }
		}

		var writers sync.WaitGroup
		writers.Add(2)
		go func() {
			defer writers.Done()
			first.updateMeta(dir, first.metaSnapshot(), func(meta *Meta) {
				leave := meet()
				defer leave()
				meta.Title = title
			})
		}()
		go func() {
			defer writers.Done()
			second.updateMeta(dir, second.metaSnapshot(), func(meta *Meta) {
				leave := meet()
				defer leave()
				meta.SpentUSD, meta.Tokens = spent, tokens
			})
		}()
		writers.Wait()

		meta, err := LoadMeta(dir)
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if meta.Title != title || meta.SpentUSD != spent || meta.Tokens != tokens {
			losses++
		}
	}
	if got := overlaps.Load(); got != 0 || losses != 0 {
		t.Fatalf("metadata patches overlapped %d times and lost fields in %d/%d rounds", got, losses, rounds)
	}
}

const (
	metaLockProcessRoleEnv    = "AFORGE_META_LOCK_TEST_ROLE"
	metaLockProcessDirEnv     = "AFORGE_META_LOCK_TEST_DIR"
	metaLockProcessBarrierEnv = "AFORGE_META_LOCK_TEST_BARRIER"
	metaLockProcessRounds     = 60
)

// Two processes on one conversation folder keep both fields. The children use
// the real title and spend stamps after a per-round file rendezvous.
func TestTwoProcessesOnOneConversationFolderKeepBothFields(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	barrier := filepath.Join(t.TempDir(), "barrier")
	seedMetaLockConversation(t, dir)
	if err := os.MkdirAll(barrier, 0o700); err != nil {
		t.Fatal(err)
	}

	type child struct {
		role string
		cmd  *exec.Cmd
		out  bytes.Buffer
	}
	children := []*child{{role: "title"}, {role: "spend"}}
	for _, child := range children {
		child.cmd = exec.Command(os.Args[0], "-test.run=^TestTwoProcessesOnOneConversationFolderKeepBothFieldsChild$", "-test.count=1")
		child.cmd.Env = append(os.Environ(),
			metaLockProcessRoleEnv+"="+child.role,
			metaLockProcessDirEnv+"="+dir,
			metaLockProcessBarrierEnv+"="+barrier,
		)
		child.cmd.Stdout = &child.out
		child.cmd.Stderr = &child.out
		if err := child.cmd.Start(); err != nil {
			t.Fatalf("start %s child: %v", child.role, err)
		}
	}
	for _, child := range children {
		if err := child.cmd.Wait(); err != nil {
			t.Fatalf("%s child: %v\n%s", child.role, err, child.out.String())
		}
		// A child whose name no longer matches the filter exits 0 having run
		// nothing, and this test would pass by writing to one file from one
		// process. Its own tally is the proof that both of them were there.
		if tally := fmt.Sprintf("rounds=%d", metaLockProcessRounds); !strings.Contains(child.out.String(), tally) {
			t.Fatalf("%s child never reported %s:\n%s", child.role, tally, child.out.String())
		}
	}
}

func TestTwoProcessesOnOneConversationFolderKeepBothFieldsChild(t *testing.T) {
	role := os.Getenv(metaLockProcessRoleEnv)
	if role == "" {
		t.Skip("metadata lock process helper")
	}
	dir := os.Getenv(metaLockProcessDirEnv)
	barrier := os.Getenv(metaLockProcessBarrierEnv)
	if (role != "title" && role != "spend") || dir == "" || barrier == "" {
		t.Fatalf("invalid helper environment: role=%q dir=%q barrier=%q", role, dir, barrier)
	}
	a := metaLockAgent(t, dir)
	other := "title"
	if role == "title" {
		other = "spend"
	}

	losses := 0
	for i := 0; i < metaLockProcessRounds; i++ {
		ready := filepath.Join(barrier, fmt.Sprintf("%s.%d", role, i))
		if err := os.WriteFile(ready, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		waitForMetaLockBarrier(t, filepath.Join(barrier, fmt.Sprintf("%s.%d", other, i)))

		title := fmt.Sprintf("process round %d name", i)
		spent, tokens := float64(i+1)/100, (i+1)*11
		if role == "title" {
			a.stampTitle(title)
		} else {
			a.writeSpend(dir, spent, tokens)
		}
		done := filepath.Join(barrier, fmt.Sprintf("%s.done.%d", role, i))
		if err := os.WriteFile(done, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		waitForMetaLockBarrier(t, filepath.Join(barrier, fmt.Sprintf("%s.done.%d", other, i)))

		if role == "title" {
			meta, err := LoadMeta(dir)
			if err != nil {
				t.Fatal(err)
			}
			if meta.Title != title || meta.SpentUSD != spent || meta.Tokens != tokens {
				losses++
			}
		}
	}
	if losses != 0 {
		t.Fatalf("losses=%d/%d", losses, metaLockProcessRounds)
	}
	fmt.Printf("%s child rounds=%d\n", role, metaLockProcessRounds)
}

func waitForMetaLockBarrier(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(time.Millisecond)
	}
}

// Putting a conversation away from home while its turn is sealed keeps both.
// The archive door must wait for the spend patch and then preserve its total.
func TestPuttingAConversationAwayFromHomeWhileItsTurnIsSealedKeepsBoth(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	a := metaLockAgent(t, dir)
	a.mu.Lock()
	a.stampUserLocked("diagnose parser migration failures")
	a.mu.Unlock()

	entered := make(chan struct{})
	archived := make(chan error, 1)
	go func() {
		<-entered
		archived <- SetArchived(dir, true)
	}()
	a.updateMeta(dir, a.metaSnapshot(), func(meta *Meta) {
		close(entered)
		select {
		case err := <-archived:
			archived <- err
		case <-time.After(30 * time.Millisecond):
		}
		meta.SpentUSD, meta.Tokens = .25, 140
	})
	if err := <-archived; err != nil {
		t.Fatalf("archive: %v", err)
	}
	meta, err := LoadMeta(dir)
	if err != nil || !meta.Archived || meta.SpentUSD != .25 || meta.Tokens != 140 {
		t.Fatalf("archive or sealed turn was lost: %+v %v", meta, err)
	}
}

// A folder whose lock cannot be taken still stamps. Metadata is a citation, so
// an unavailable lock keeps the old best-effort behavior instead of refusing.
func TestAFolderWhoseLockCannotBeTakenStillStamps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	seedMetaLockConversation(t, dir)
	if err := os.Mkdir(filepath.Join(dir, placeMetaLock), 0o700); err != nil {
		t.Fatal(err)
	}
	a := metaLockAgent(t, dir)
	a.stampTitle("the lock did not refuse this title")
	meta, err := LoadMeta(dir)
	if err != nil || meta.Title != "the lock did not refuse this title" {
		t.Fatalf("stamp with unavailable lock: %+v %v", meta, err)
	}
}

// A holder that never lets go does not park the stamp. The bounded wait falls
// back to the same unlocked write the conversation used before locks existed.
func TestAHolderThatNeverLetsGoDoesNotParkTheStamp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	seedMetaLockConversation(t, dir)
	oldPatience := metaLockPatience
	metaLockPatience = 20 * time.Millisecond
	t.Cleanup(func() { metaLockPatience = oldPatience })

	lock, err := os.OpenFile(filepath.Join(dir, placeMetaLock), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
	})
	if err := filelock.Lock(lock, true, true); err != nil {
		t.Fatalf("hold metadata lock: %v", err)
	}

	a := metaLockAgent(t, dir)
	started := time.Now()
	a.stampTitle("the patient stamp went ahead")
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("stamp waited %s for a holder that did not leave", elapsed)
	}
	meta, err := LoadMeta(dir)
	if err != nil || meta.Title != "the patient stamp went ahead" {
		t.Fatalf("stamp after bounded wait: %+v %v", meta, err)
	}
}

// A reader mid-write still sees a whole identity. SaveMeta keeps the previous
// complete file visible until the transaction's replacement is ready.
func TestAReaderMidWriteStillSeesAWholeIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	seedMetaLockConversation(t, dir)
	a := metaLockAgent(t, dir)
	entered, release, written := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		a.updateMeta(dir, a.metaSnapshot(), func(meta *Meta) {
			meta.Title = "replacement identity"
			close(entered)
			<-release
		})
		close(written)
	}()
	<-entered

	type result struct {
		meta Meta
		err  error
	}
	read := make(chan result, 1)
	go func() {
		meta, err := LoadMeta(dir)
		read <- result{meta: meta, err: err}
	}()
	var previous result
	select {
	case previous = <-read:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("reader waited for a metadata writer")
	}
	close(release)
	<-written
	if previous.err != nil || previous.meta.ID != filepath.Base(dir) || previous.meta.Title != "previous identity" || previous.meta.Workspace == "" || previous.meta.Model != "test/model" || previous.meta.Created.IsZero() {
		t.Fatalf("reader saw an incomplete previous identity: %+v %v", previous.meta, previous.err)
	}
	meta, err := LoadMeta(dir)
	if err != nil || meta.Title != "replacement identity" {
		t.Fatalf("replacement identity did not land: %+v %v", meta, err)
	}
}
