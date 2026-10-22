package main

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

func mintOpened(t *testing.T, bucket, workspace string) session.Place {
	celltest.Available(t)
	t.Helper()
	place, err := v3MintSession(bucket, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := openV3Agent(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}, workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("session did not open: %v", err)
	}
	// An agent left open keeps writing into the session folder after the test
	// returns, which races the temp-dir cleanup and fails the test at random.
	t.Cleanup(func() { _ = agent.Close() })
	return place
}

// TestNewChatIsACellWhenTheFlagIsOn proves C23 at the real chat mint door.
func TestNewChatIsACellWhenTheFlagIsOn(t *testing.T) {
	t.Setenv(cell.EnvVar, "1")
	bucket, workspace := t.TempDir(), t.TempDir()
	place := mintOpened(t, bucket, workspace)

	want := filepath.Join(place.Dir, cell.TranscriptPath)
	if place.Transcript() != want {
		t.Fatalf("transcript = %s, want %s", place.Transcript(), want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("transcript not written in .cell: %v", err)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, "transcript.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("a legacy transcript sits beside the cell: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(place.Dir, cell.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), bucket) || strings.Contains(string(raw), workspace) {
		t.Fatalf("cell meta holds an absolute path: %s", raw)
	}

	c, err := cell.OpenAt(place.Dir, place.ID())
	if err != nil || c.Meta().Class != cell.HostBound {
		t.Fatalf("new chat class = %s, %v; want host-bound (C23)", c.Meta().Class, err)
	}

	// Resume and listing find it through the same scan every launch uses.
	spoken, empty := v3ScanBucket(bucket)
	if len(spoken)+len(empty) != 1 || append(spoken, empty...)[0].dir != place.Dir {
		t.Fatalf("listing = %v %v, want the cell", spoken, empty)
	}
	if folder, ok := session.FolderOf(place.Transcript()); !ok || folder != place.Dir {
		t.Fatalf("FolderOf = %q %v", folder, ok)
	}
	if got := v3PlaceOf(place.Transcript(), workspace); got.Dir != place.Dir {
		t.Fatalf("v3PlaceOf dir = %q, want %q", got.Dir, place.Dir)
	}
}

func TestNewChatIsALegacyFolderWhenTheFlagIsOff(t *testing.T) {
	t.Setenv(cell.EnvVar, "0")
	bucket, workspace := t.TempDir(), t.TempDir()
	place := mintOpened(t, bucket, workspace)

	if want := filepath.Join(place.Dir, "transcript.jsonl"); place.Transcript() != want {
		t.Fatalf("transcript = %s, want %s", place.Transcript(), want)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, cell.StateDir)); !os.IsNotExist(err) {
		t.Fatalf("flag off must not create .cell: %v", err)
	}
}

// legacyFolder writes a session folder the way a build without cells does: the
// real mint and the real agent, with a task journal beside the transcript.
func legacyFolder(t *testing.T, bucket, workspace string) session.Place {
	t.Helper()
	t.Setenv(cell.EnvVar, "0")
	place, err := v3MintSession(bucket, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := openV3Agent(v3TestConfig(workspace, place), workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("legacy session did not open: %v", err)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(place.NodeJournals(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(place.NodeJournals(), "n1.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return place
}

func v3TestConfig(workspace string, place session.Place) session.Config {
	return session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}
}

// TestLegacyFolderResumesWithoutMigration proves C24 through the actual open
// and command doors, using the top-level names and JSONL layout dev writes.
func TestLegacyFolderResumesWithoutMigration(t *testing.T) {
	celltest.Available(t)
	for _, enabled := range []string{"0", "1"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv(cell.EnvVar, enabled)
			t.Setenv("CODEAF_SYNC_URL", "off")
			workspace := t.TempDir()
			dir := filepath.Join(t.TempDir(), "0123456789abcdef")
			files := map[string]string{
				"transcript.jsonl": `{"type":"session","version":1,"id":"0123456789abcdef","model":"test/model"}` + "\n" + `{"type":"message","role":"user","content":"old words"}` + "\n" + `{"type":"message","role":"assistant","content":"old answer"}` + "\n",
				"state.json":       `{ "type":"state", "version":1, "records":[] }` + "\n", "card.json": "{}\n", "tasks.json": `{ "type":"tasks", "version":1, "seq":0, "nodes":[] }` + "\n",
				"tasks/n1.jsonl": "{}\n", "meta.json": `{"id":"0123456789abcdef","workspace":"` + workspace + `"}` + "\n",
			}
			for name, body := range files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			place := session.Place{Dir: dir, Workspace: workspace}
			agent, cfg, _, err := openV3Agent(v3TestConfig(workspace, place), workspace, v3OpenSession)
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			if cfg.Seat != nil || cfg.Seals != nil || cfg.SessionFile != filepath.Join(dir, "transcript.jsonl") {
				t.Fatalf("legacy chat acquired a cell: %+v", cfg)
			}
			for name, body := range files {
				raw, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(raw) != body {
					t.Fatalf("open changed %s: %q, %v", name, raw, err)
				}
			}
			events, err := agent.SubmitBash(t.Context(), "!printf continued")
			if err != nil {
				t.Fatal(err)
			}
			for event := range events {
				if event.Kind == session.EventToolFailed {
					t.Fatalf("command: %+v", event)
				}
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "transcript.jsonl"))
			if err != nil || !strings.HasPrefix(string(raw), files["transcript.jsonl"]) || !strings.Contains(string(raw), "continued") {
				t.Fatalf("turn was not appended: %q %v", raw, err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".cell")); !os.IsNotExist(err) {
				t.Fatalf("open created .cell: %v", err)
			}
		})
	}
}

func TestLegacyFolderStaysLegacyWhenTheFlagIsOff(t *testing.T) {
	bucket, workspace := t.TempDir(), t.TempDir()
	place := legacyFolder(t, bucket, workspace)

	cfg := v3Migrated(v3TestConfig(workspace, place))
	if cfg.SessionFile != place.Transcript() {
		t.Fatalf("session file moved: %s", cfg.SessionFile)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, cell.StateDir)); !os.IsNotExist(err) {
		t.Fatalf("flag off must not create .cell: %v", err)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, "transcript.jsonl")); err != nil {
		t.Fatalf("legacy transcript gone: %v", err)
	}
}

func seatClass(t *testing.T, cfg session.Config, dir string) executor.Class {
	t.Helper()
	if cfg.Seat == nil {
		t.Fatal("the session has no seat")
	}
	local, ok := cfg.Seat.In(dir).(executor.Local)
	if !ok {
		t.Fatalf("seat runner is %T; an unsealed workspace must hand out a plain local", cfg.Seat.In(dir))
	}
	return local.Class
}

// TestSeatFollowsTheCellsClass proves C23 preserves an existing sandboxed
// cell, including its metadata, when the conversation resumes.
func TestSeatFollowsTheCellsClass(t *testing.T) {
	celltest.Available(t)
	t.Setenv(cell.EnvVar, "1")
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	before, err := os.ReadFile(filepath.Join(c.Root, cell.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	cfg := v3Seated(v3Migrated(session.Config{Place: session.Place{Dir: c.Root, Workspace: workspace}}))
	after, err := os.ReadFile(filepath.Join(c.Root, cell.MetaPath))
	if err != nil || string(before) != string(after) {
		t.Fatalf("resuming rewrote the declared class: %s, %v", after, err)
	}
	if got := seatClass(t, cfg, workspace); got != executor.Sandboxed {
		t.Fatalf("class = %d, want sandboxed", got)
	}
}

// Two sessions in one process on the same path each keep the class their own
// cell declared: nothing is looked up by path.
func TestTwoSessionsOnOnePathKeepTheirOwnClasses(t *testing.T) {
	celltest.Available(t)
	t.Setenv(cell.EnvVar, "1")
	workspace := t.TempDir()
	seated := func(class cell.Class) session.Config {
		c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: class})
		if err != nil {
			t.Fatal(err)
		}
		return v3Seated(session.Config{Place: session.Place{Dir: c.Root, Workspace: workspace}})
	}
	sandboxed, host := seated(cell.Sandboxed), seated(cell.HostBound)
	if a, b := seatClass(t, sandboxed, workspace), seatClass(t, host, workspace); a != executor.Sandboxed || b != executor.HostBound {
		t.Fatalf("classes = %d and %d, want sandboxed and host-bound", a, b)
	}
}

func TestNoSeatWhenCellsAreOffOrTheFolderIsNoCell(t *testing.T) {
	celltest.Available(t)
	t.Setenv(cell.EnvVar, "0")
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: c.Root, Workspace: t.TempDir()}
	if cfg := v3Seated(session.Config{Place: place, Seat: executor.Host}); cfg.Seat != nil {
		t.Fatal("flag off must leave the session on the host, and clear a stale seat")
	}
	t.Setenv(cell.EnvVar, "1")
	other := session.Place{Dir: t.TempDir(), Workspace: t.TempDir()}
	if cfg := v3Seated(session.Config{Place: other}); cfg.Seat != nil {
		t.Fatal("a folder that is no cell must keep the legacy host rule")
	}
}

// TestC23NewChatCommandsHaveOpenNetworkWithoutConfinement proves C23 using
// the mint and seat doors, with the executor's jail and observer seams so no
// network connection or working kernel jail is needed.
func TestC23NewChatCommandsHaveOpenNetworkWithoutConfinement(t *testing.T) {
	celltest.Available(t)
	t.Setenv(cell.EnvVar, "1")
	t.Setenv("CODEAF_HOME", t.TempDir())
	workspace := t.TempDir()
	place, err := v3MintSession(t.TempDir(), workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cell.OpenAt(place.Dir, place.ID())
	if err != nil || c.Meta().Class != cell.HostBound {
		t.Fatalf("minted class = %s, %v; want host-bound", c.Meta().Class, err)
	}
	seat, _, _ := v3SeatOf(place, nil)
	if seat == nil {
		t.Fatal("new chat has no seat")
	}
	local, ok := seat.In(workspace).(executor.Local)
	if !ok {
		t.Fatalf("runner = %T, want local", seat.In(workspace))
	}
	probe := &c23CommandProbe{}
	local.Jail, local.Observer = probe, probe
	res, err := local.Exec(context.Background(), executor.ExecRequest{Argv: []string{"sh", "-c", "printf ordinary"}}, nil)
	if err != nil || res.Exit != 0 || string(res.Stdout) != "ordinary" {
		t.Fatalf("ordinary command: %+v, %v", res, err)
	}
	if probe.confined || !probe.request.Net.Open || probe.request.Setup || probe.request.Class != executor.HostBound || res.JailDegraded {
		t.Fatalf("ordinary call policy = %+v, confined = %v, degraded = %v", probe.request, probe.confined, res.JailDegraded)
	}
}

type c23CommandProbe struct {
	confined bool
	request  executor.ExecRequest
}

func (p *c23CommandProbe) Confine(*exec.Cmd, executor.ExecRequest) error {
	p.confined = true
	return errors.New("C23: a new chat must not enter a jail")
}

func (p *c23CommandProbe) Observe(req executor.ExecRequest, _ executor.ExecResult) {
	p.request = req
}

// TestC23NewChatCanWriteToASiblingThroughItsSeat proves C23 through a real
// sealed call, so cells being on cannot narrow the host's write access.
func TestC23NewChatCanWriteToASiblingThroughItsSeat(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv(cell.EnvVar, "1")
	t.Setenv("CODEAF_HOME", t.TempDir())
	parent := t.TempDir()
	workspace, sibling := filepath.Join(parent, "workspace"), filepath.Join(parent, "sibling")
	for _, dir := range []string{workspace, sibling} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	place, err := v3MintSession(t.TempDir(), workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	var sealError error
	seat, _, _ := v3SeatOf(place, func(line string) { t.Log(line) })
	if seat == nil {
		t.Fatal("new chat has no seat")
	}
	path := filepath.Join(sibling, "x")
	var result executor.ExecResult
	err = seat.Around(context.Background(), executor.Call{Tool: "bash"}, func() ([]byte, bool) {
		result, sealError = seat.In(workspace).Exec(context.Background(), executor.ExecRequest{Argv: []string{"touch", path}}, nil)
		return result.Stdout, sealError != nil || result.Exit != 0
	})
	if err != nil || sealError != nil || result.Exit != 0 {
		t.Fatalf("sibling write = %+v, %v, %v", result, sealError, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sibling file was not written: %v", err)
	}
}
