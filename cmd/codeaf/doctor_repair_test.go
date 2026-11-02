package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestDoctorHoldStoreHelperProcess(t *testing.T) {
	storeDir := os.Getenv("CODEAF_TEST_DOCTOR_HOLD_STORE")
	if storeDir == "" {
		return
	}
	release, err := cellstore.HoldStoreWriter(storeDir, "codeaf chat")
	if err != nil {
		os.Exit(2)
	}
	_, _ = os.Stdout.WriteString("LOCKED\n")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	_ = release()
	os.Exit(0)
}

func startDoctorStoreHolder(t *testing.T, storeDir string) func() {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestDoctorHoldStoreHelperProcess$") //codeaf:plumbing spawns this test binary to hold an engine store lock in another process
	cmd.Env = append(os.Environ(), "CODEAF_TEST_DOCTOR_HOLD_STORE="+storeDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LOCKED" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper failed to hold store %s: line=%q err=%v", storeDir, line, err)
	}
	var stopped bool
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_, _ = stdin.Write([]byte("release\n"))
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	return stop
}

type doctorTestClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *doctorTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *doctorTestClock) SetOffset(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = d
}

func writeLocalChatFolder(t *testing.T, dir, id, title, transcriptBody string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID:        id,
		Title:     title,
		Workspace: dir,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.Place{Dir: dir}.Transcript(), []byte(transcriptBody), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Contract 5: `codeaf doctor` reports a busy engine store and its holder, an
// unfinished take, duplicate Home rows for one chat id, and this computer's own
// lapsed holds that would ask a question on open; without `--fix` it mutates
// nothing; with `--fix` it repairs only what is safe, prints one line per
// change, never touches another computer's live hold, and never deletes a turn
// that exists only in the thing it removes.
func TestDoctorFindsAndRepairsSyncAndEngineStoreSafely(t *testing.T) {
	celltest.Available(t)
	clock := &doctorTestClock{offset: -2 * time.Minute}
	svc := relayserve.New(relayserve.Config{Store: t.TempDir(), Now: clock.Now})
	srv := httptest.NewServer(svc.Handler)
	t.Cleanup(func() {
		srv.Close()
		svc.Close()
	})

	homeA := t.TempDir()
	homeB := t.TempDir()
	t.Setenv(home.EnvVar, homeA)
	t.Setenv(syncsetup.URLVar, srv.URL)

	id, err := identity.Ensure(homeA)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Save(homeB, id); err != nil {
		t.Fatal(err)
	}

	syncA, ok, err := syncsetup.Open(homeA)
	if err != nil || !ok {
		t.Fatalf("syncsetup.Open(homeA) = (%v, %v)", ok, err)
	}
	syncB, ok, err := syncsetup.Open(homeB)
	if err != nil || !ok {
		t.Fatalf("syncsetup.Open(homeB) = (%v, %v)", ok, err)
	}
	ctx := context.Background()
	if err := syncsetup.PutOwnDevice(ctx, syncA.Dir, syncA.Identity, syncA.Device, "desk"); err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.PutOwnDevice(ctx, syncB.Dir, syncB.Identity, syncB.Device, "laptop"); err != nil {
		t.Fatal(err)
	}

	const (
		unfinishedID      = "01J00000000000000000000101"
		uniqueStagingID   = "01J00000000000000000000102"
		otherLiveID       = "01J00000000000000000000103"
		mainChatID        = "01J00000000000000000000104"
		redundantBranchID = "01J00000000000000000000105"
		uniqueBranchID    = "01J00000000000000000000106"
		lapsedSelfID      = "01J00000000000000000000107"
		localDupID        = "01J00000000000000000000108"

		headMain   = "1111111111111111111111111111111111111111111111111111111111111111"
		headUnique = "2222222222222222222222222222222222222222222222222222222222222222"
	)

	makeInit := func(head, parent string, orphans uint32) directory.CellInit {
		return directory.CellInit{
			Head:        head,
			Class:       string(cell.HostBound),
			ParentCell:  parent,
			OrphanTurns: orphans,
			Keys:        map[string]map[string]string{id.CellKeyID(): {id.ID(): ""}},
		}
	}

	// 1. Lapsed hold on this computer (created 2 minutes ago on the relay clock, > 90s LeaseTTL, within 5m skew).
	if _, err := syncA.Dir.Create(ctx, lapsedSelfID, makeInit(headMain, "", 0)); err != nil {
		t.Fatal(err)
	}
	clock.SetOffset(0)

	// 2. Unfinished take on this computer: live lease on syncA with no SendLease receipt, plus empty .taking folder.
	unfinishedView, err := syncA.Dir.Create(ctx, unfinishedID, makeInit(headMain, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	unfinishedTaking := filepath.Join(homeA, "v3", "projects", "copies", unfinishedID+handoff.StagingSuffix)
	if err := os.MkdirAll(unfinishedTaking, 0o700); err != nil {
		t.Fatal(err)
	}

	// 3. Unfinished take whose .taking folder contains a unique unsealed turn absent from main chat (must NOT be deleted!).
	uniqueStagingDir := filepath.Join(homeA, "v3", "projects", "copies", uniqueStagingID+handoff.StagingSuffix)
	writeLocalChatFolder(t, uniqueStagingDir, uniqueStagingID, "Unique Staged Turn", "{\"type\":\"session\",\"id\":\""+uniqueStagingID+"\"}\n{\"role\":\"user\",\"content\":\"only in staging\"}\n")

	// 4. Live hold on another computer (syncB) — must NEVER be touched by doctor --fix!
	otherView, err := syncB.Dir.Create(ctx, otherLiveID, makeInit(headMain, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	// 5. Main chat + two branch copies:
	//    - redundantBranchID has Head == headMain (all turns in main chat -> safe to retire)
	//    - uniqueBranchID has Head == headUnique (turns NOT in main chat -> must NEVER be retired!)
	mainView, err := syncB.Dir.Create(ctx, mainChatID, makeInit(headMain, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := syncB.Dir.Release(ctx, mainChatID, mainView.Cell.Lease.Fence); err != nil {
		t.Fatal(err)
	}
	redBranchView, err := syncA.Dir.Create(ctx, redundantBranchID, makeInit(headMain, mainChatID, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := syncA.Dir.Release(ctx, redundantBranchID, redBranchView.Cell.Lease.Fence); err != nil {
		t.Fatal(err)
	}
	uniqBranchView, err := syncB.Dir.Create(ctx, uniqueBranchID, makeInit(headUnique, mainChatID, 3))
	if err != nil {
		t.Fatal(err)
	}
	if err := syncB.Dir.Release(ctx, uniqueBranchID, uniqBranchView.Cell.Lease.Fence); err != nil {
		t.Fatal(err)
	}

	// 6. Duplicate local Home folders for one chat id on disk.
	header := "{\"type\":\"session\",\"id\":\"" + localDupID + "\"}\n"
	writeLocalChatFolder(t, filepath.Join(homeA, "v3", "projects", "-work-proj", localDupID), localDupID, "Dup Local", header)
	writeLocalChatFolder(t, filepath.Join(homeA, "v3", "projects", "copies", localDupID), localDupID, "Dup Local", header)

	// 7. Busy engine store (held by live foreign process) AND a lock file in another store
	// whose recorded holder is dead. No live process holds that lock, so it is free: doctor
	// neither reports it nor deletes it.
	sharedStore := cellstore.SharedStoreDir()
	stopHolder := startDoctorStoreHolder(t, sharedStore)
	defer stopHolder()

	otherStore := filepath.Join(homeA, "v3", "stores", " stale-check")
	otherStore = strings.ReplaceAll(otherStore, " ", "")
	if err := os.MkdirAll(otherStore, 0o700); err != nil {
		t.Fatal(err)
	}
	staleLockPath := filepath.Join(otherStore, "writer.lock")
	stalePayload, _ := json.Marshal(cellstore.StoreHolder{PID: 99999999, Command: "codeaf chat"})
	if err := os.WriteFile(staleLockPath, append(stalePayload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "graph.db")

	// ── STEP 1: `codeaf doctor` WITHOUT `--fix` reports everything and mutates NOTHING ──
	var reportOut bytes.Buffer
	if err := runDoctorWith([]string{"--db", dbPath}, &reportOut, 0); err != nil {
		t.Fatal(err)
	}
	report := reportOut.String()
	exeBase := filepath.Base(os.Args[0])
	for _, want := range []string{
		"engine store",
		"busy · held by " + exeBase + ", pid ",
		"unfinished continue · chat " + unfinishedID,
		"unfinished continue · chat " + uniqueStagingID,
		"duplicate rows · chat " + localDupID + " (2 local folders)",
		"duplicate rows · chat " + mainChatID + " (branch copy " + redundantBranchID + ", all turns in main chat)",
		"duplicate rows · chat " + mainChatID + " (branch copy " + uniqueBranchID + ", 3 turns not in main chat)",
		"lapsed hold on this computer · chat " + lapsedSelfID,
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("doctor report missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "fixed:") {
		t.Fatalf("doctor without --fix printed fix lines:\n%s", report)
	}
	for _, forbidden := range []string{"daemon", "launchd", "systemd", "service", "brain", "standing watch", "auditor", "verdict", "verified", "refuted"} {
		if strings.Contains(strings.ToLower(report), forbidden) {
			t.Fatalf("doctor output contains forbidden word %q:\n%s", forbidden, report)
		}
	}

	if strings.Contains(report, staleLockPath) {
		t.Fatalf("doctor reported a lock no live process holds:\n%s", report)
	}

	// Verify zero state mutations occurred without --fix!
	if _, err := os.Stat(unfinishedTaking); err != nil {
		t.Fatalf("doctor without --fix deleted .taking dir: %v", err)
	}
	checkUnfinished, err := syncA.Dir.Cell(ctx, unfinishedID)
	if err != nil || checkUnfinished.Cell.Lease.Expires <= checkUnfinished.Now || checkUnfinished.Cell.Lease.Fence != unfinishedView.Cell.Lease.Fence {
		t.Fatalf("doctor without --fix altered unfinished take lease: %+v (err=%v)", checkUnfinished, err)
	}
	checkRedBranch, err := syncA.Dir.Cell(ctx, redundantBranchID)
	if err != nil || checkRedBranch.Cell.Archived {
		t.Fatalf("doctor without --fix archived redundant branch: %+v (err=%v)", checkRedBranch, err)
	}

	// ── STEP 2: `codeaf doctor --fix` repairs what is safe and preserves every safety invariant ──
	var fixOut bytes.Buffer
	if err := runDoctorWith([]string{"--db", dbPath, "--fix"}, &fixOut, 0); err != nil {
		t.Fatal(err)
	}
	fixedText := fixOut.String()
	for _, wantFix := range []string{
		"fixed: released unfinished continue for chat " + unfinishedID,
		"fixed: retired duplicate branch copy " + redundantBranchID + " of chat " + mainChatID,
	} {
		if !strings.Contains(fixedText, wantFix) {
			t.Fatalf("doctor --fix missing %q:\n%s", wantFix, fixedText)
		}
	}
	for _, forbidden := range []string{"daemon", "launchd", "systemd", "service", "brain", "standing watch", "auditor", "verdict", "verified", "refuted"} {
		if strings.Contains(strings.ToLower(fixedText), forbidden) {
			t.Fatalf("doctor --fix output contains forbidden word %q:\n%s", forbidden, fixedText)
		}
	}

	// Safe repairs happened:
	if _, err := os.Stat(staleLockPath); err != nil {
		t.Fatalf("doctor --fix deleted a free lock file: %v", err)
	}
	if _, err := os.Stat(unfinishedTaking); !os.IsNotExist(err) {
		t.Fatalf("doctor --fix did not remove empty .taking dir: %v", err)
	}
	afterUnfinished, err := syncA.Dir.Cell(ctx, unfinishedID)
	if err != nil || afterUnfinished.Cell.Lease.Expires != 0 {
		t.Fatalf("doctor --fix did not release this computer's stale take: %+v (err=%v)", afterUnfinished, err)
	}
	afterRedBranch, err := syncA.Dir.Cell(ctx, redundantBranchID)
	if err != nil || !afterRedBranch.Cell.Archived {
		t.Fatalf("doctor --fix did not retire redundant branch copy: %+v (err=%v)", afterRedBranch, err)
	}

	// Safety Invariant 1: NEVER touches another computer's live hold!
	afterOther, err := syncB.Dir.Cell(ctx, otherLiveID)
	if err != nil {
		t.Fatal(err)
	}
	if afterOther.Cell.Lease.Device != syncB.Device.ID() || afterOther.Cell.Lease.Fence != otherView.Cell.Lease.Fence || afterOther.Cell.Lease.Expires <= afterOther.Now {
		t.Fatalf("doctor --fix touched another computer's live hold: got %+v, want device %s fence %d live", afterOther.Cell.Lease, syncB.Device.ID(), otherView.Cell.Lease.Fence)
	}

	// Safety Invariant 2: NEVER touches a live process's store lock!
	if _, stillBusy := cellstore.InspectStoreBusy(sharedStore, ""); !stillBusy {
		t.Fatal("doctor --fix disturbed a live foreign process's store lock")
	}

	// Safety Invariant 3: NEVER deletes or retires a branch copy or .taking folder with unique turns!
	afterUniqBranch, err := syncB.Dir.Cell(ctx, uniqueBranchID)
	if err != nil || afterUniqBranch.Cell.Archived {
		t.Fatalf("doctor --fix retired branch copy whose turns are NOT in main chat: %+v (err=%v)", afterUniqBranch, err)
	}
	if _, err := os.Stat(session.Place{Dir: uniqueStagingDir}.Transcript()); err != nil {
		t.Fatalf("doctor --fix deleted .taking folder containing unique turns: %v", err)
	}
}

// TestDoctorDoesNotCreateRecordsOnSyncService verifies that read-only
// `codeaf doctor` makes only a read-only listing call under the person's own
// identity and creates no namespaces, devices, cells, or Raft stores on the
// sync service (and makes zero requests when sync is off or unpaired).
func TestDoctorDoesNotCreateRecordsOnSyncService(t *testing.T) {
	celltest.Available(t)
	storeDir := t.TempDir()
	svc := relayserve.New(relayserve.Config{Store: storeDir})
	t.Cleanup(func() { _ = svc.Close() })

	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		svc.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	homeA := t.TempDir()
	t.Setenv(home.EnvVar, homeA)
	t.Setenv(syncsetup.URLVar, srv.URL)

	if _, err := identity.Ensure(homeA); err != nil {
		t.Fatal(err)
	}
	syncA, ok, err := syncsetup.Open(homeA)
	if err != nil || !ok {
		t.Fatalf("syncsetup.Open(homeA) = (%v, %v)", ok, err)
	}
	if err := syncsetup.PutOwnDevice(context.Background(), syncA.Dir, syncA.Identity, syncA.Device, "desk"); err != nil {
		t.Fatal(err)
	}

	readNames := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		slices.Sort(names)
		return names
	}

	dirNamesBefore := readNames(filepath.Join(storeDir, "directory"))
	raftNamesBefore := readNames(filepath.Join(storeDir, "raft"))

	mu.Lock()
	requests = nil
	mu.Unlock()

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	var out bytes.Buffer
	if err := runDoctorWith([]string{"--db", dbPath}, &out, 0); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	gotRequests := slices.Clone(requests)
	requests = nil
	mu.Unlock()

	if !slices.Equal(gotRequests, []string{"GET /v1/dir/list"}) {
		t.Fatalf("doctor HTTP requests = %v, want only [GET /v1/dir/list]", gotRequests)
	}
	if got := readNames(filepath.Join(storeDir, "directory")); !slices.Equal(got, dirNamesBefore) {
		t.Fatalf("relay directory files changed after doctor: got %v, want %v", got, dirNamesBefore)
	}
	if got := readNames(filepath.Join(storeDir, "raft")); !slices.Equal(got, raftNamesBefore) {
		t.Fatalf("relay raft files changed after doctor: got %v, want %v", got, raftNamesBefore)
	}

	// Sync off: zero HTTP requests.
	t.Setenv(syncsetup.URLVar, "off")
	var offOut bytes.Buffer
	if err := runDoctorWith([]string{"--db", dbPath}, &offOut, 0); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	offRequests := slices.Clone(requests)
	requests = nil
	mu.Unlock()
	if len(offRequests) != 0 {
		t.Fatalf("doctor sent %v with CODEAF_SYNC_URL=off, want none", offRequests)
	}

	// Unpaired solo home: zero HTTP requests.
	soloHome := t.TempDir()
	if _, err := identity.EnsureSolo(soloHome); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, soloHome)
	t.Setenv(syncsetup.URLVar, srv.URL)
	var soloOut bytes.Buffer
	if err := runDoctorWith([]string{"--db", dbPath}, &soloOut, 0); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	soloRequests := slices.Clone(requests)
	mu.Unlock()
	if len(soloRequests) != 0 {
		t.Fatalf("doctor sent %v on unpaired solo home, want none", soloRequests)
	}
}
