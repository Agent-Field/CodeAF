package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

type syncOffFrameCapture struct {
	mu sync.Mutex
	bytes.Buffer
}

func (c *syncOffFrameCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Buffer.Write(p)
}

func (c *syncOffFrameCapture) Plain() string {
	c.mu.Lock()
	raw := c.Buffer.String()
	c.mu.Unlock()
	return ansi.Strip(raw)
}

// TestSyncOffHomeKeepsEveryChatOpensAndWrites verifies Contract 1 end to end:
// with CODEAF_SYNC_URL=off (or sync otherwise off), Home lists every chat on
// this computer — current ones in `.moving` and legacy ones alike — each once,
// never talks to the sync service, and each chat opens and can be written.
func TestSyncOffHomeKeepsEveryChatOpensAndWrites(t *testing.T) {
	celltest.Available(t)

	var modelTurns atomic.Int64
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"test/model"}]}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		seq := modelTurns.Add(1)
		reply := fmt.Sprintf("sync-off-reply-%d", seq)
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if json.Unmarshal(body, &req) == nil {
			for i := len(req.Messages) - 1; i >= 0; i-- {
				if req.Messages[i].Role == "user" {
					if s, ok := req.Messages[i].Content.(string); ok && strings.TrimSpace(s) != "" {
						reply = "ack:" + strings.TrimSpace(s)
						break
					}
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", reply)
	}))
	t.Cleanup(modelServer.Close)

	for _, mode := range []string{"CODEAF_SYNC_URL_off", "sync_otherwise_off"} {
		t.Run(mode, func(t *testing.T) {
			var relayRequests atomic.Int64
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				relayRequests.Add(1)
				http.Error(w, "sync is off; relay must not be contacted", http.StatusForbidden)
			}))
			t.Cleanup(relay.Close)

			shortHome, err := os.MkdirTemp("", "af2072")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				sockets, _ := filepath.Glob(filepath.Join(shortHome, "v3", "engine-*.sock"))
				for _, socket := range sockets {
					_ = (cellstore.Daemon{Socket: socket}).Stop(context.Background())
				}
				_ = os.RemoveAll(shortHome)
			})

			t.Setenv("HOME", shortHome)
			t.Setenv(home.EnvVar, shortHome)
			t.Setenv("CODEAF_PROFILE_DIR", shortHome)
			t.Setenv("OPENROUTER_API_KEY", "smoke-test-key")
			t.Setenv("CODEAF_BASE_URL", modelServer.URL)
			t.Setenv("CODEAF_MODEL", "test/model")
			t.Setenv("CODEAF_DAILY_BUDGET", "0")
			t.Setenv("CODEAF_TELEMETRY", "off")
			t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "")
			t.Setenv("DO_NOT_TRACK", "1")
			t.Setenv(codeupdate.NoUpdateCheckEnv, "1")
			t.Setenv("CODEAF_NO_AUTOMATIONS", "1")

			workspace := t.TempDir()
			for _, dir := range []string{"objects", "refs/heads"} {
				if err := os.MkdirAll(filepath.Join(workspace, ".git", dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(workspace, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			bucket, err := v3ProjectDir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			baseAt := time.Now().UTC().Truncate(time.Second)

			seedSpokenTranscript := func(path, id, firstPrompt, firstReply string) {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				body := fmt.Sprintf(
					"{\"type\":\"session\",\"version\":1,\"id\":%q}\n"+
						"{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n"+
						"{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n",
					id, firstPrompt, firstReply,
				)
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// 1. Uncopied legacy chat at <bucket>/<legacyID>.
			const legacyID = "01J00000000000000000LEGACY"
			legacyDir := filepath.Join(bucket, legacyID)
			seedSpokenTranscript(filepath.Join(legacyDir, "transcript.jsonl"), legacyID, "Legacy Uncopied Chat", "initial legacy answer")
			if err := session.SaveMeta(legacyDir, session.Meta{
				ID:         legacyID,
				Title:      "Legacy Uncopied Chat",
				Workspace:  workspace,
				Created:    baseAt.Add(-10 * time.Minute),
				LastUserAt: baseAt.Add(-4 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}

			// 2. Legacy chat at <bucket>/<origID> with a completed copy in <bucket>/.moving/<hash>.
			const copiedOrigID = "01J00000000000000000COPIED"
			copiedOrigDir := filepath.Join(bucket, copiedOrigID)
			seedSpokenTranscript(filepath.Join(copiedOrigDir, "transcript.jsonl"), copiedOrigID, "Stale Original Title", "before copy answer")
			if err := session.SaveMeta(copiedOrigDir, session.Meta{
				ID:         copiedOrigID,
				Title:      "Stale Original Title",
				Workspace:  workspace,
				Created:    baseAt.Add(-9 * time.Minute),
				LastUserAt: baseAt.Add(-8 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}
			copiedCell, err := cell.CopyLegacy(context.Background(), copiedOrigDir, func(ctx context.Context, c cell.Cell) error {
				return session.PrepareCopy(ctx, copiedOrigDir, c, nil)
			})
			if err != nil {
				t.Fatalf("CopyLegacy: %v", err)
			}
			seedSpokenTranscript(filepath.Join(copiedCell.Root, filepath.FromSlash(cell.TranscriptPath)), copiedCell.ID, "Copied Moving Chat", "after copy answer")
			if err := session.SaveMeta(copiedCell.Root, session.Meta{
				ID:         copiedCell.ID,
				Title:      "Copied Moving Chat",
				Workspace:  workspace,
				Created:    baseAt.Add(-9 * time.Minute),
				LastUserAt: baseAt.Add(-3 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}

			// 3. Standalone current chat in <bucket>/.moving/<id> with no top-level legacy folder.
			standaloneCell, err := cell.CreateIn(filepath.Join(bucket, ".moving"), cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatalf("CreateIn(.moving): %v", err)
			}
			seedSpokenTranscript(filepath.Join(standaloneCell.Root, filepath.FromSlash(cell.TranscriptPath)), standaloneCell.ID, "Standalone Moving Chat", "initial moving answer")
			if err := session.SaveMeta(standaloneCell.Root, session.Meta{
				ID:         standaloneCell.ID,
				Title:      "Standalone Moving Chat",
				Workspace:  workspace,
				Created:    baseAt.Add(-7 * time.Minute),
				LastUserAt: baseAt.Add(-2 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}

			// 4. Legacy chat whose `.moving` scratch copy has NOT finished (`copy-ready` absent).
			const unfinishedID = "01J0000000000000UNFINISHED"
			unfinishedDir := filepath.Join(bucket, unfinishedID)
			seedSpokenTranscript(filepath.Join(unfinishedDir, "transcript.jsonl"), unfinishedID, "Unfinished Copy Chat", "unfinished copy answer")
			if err := session.SaveMeta(unfinishedDir, session.Meta{
				ID:         unfinishedID,
				Title:      "Unfinished Copy Chat",
				Workspace:  workspace,
				Created:    baseAt.Add(-6 * time.Minute),
				LastUserAt: baseAt.Add(-1 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}
			unfinishedScratch := cell.CopyRoot(unfinishedDir)
			if err := os.WriteFile(unfinishedScratch+".lock", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			seedSpokenTranscript(filepath.Join(unfinishedScratch, filepath.FromSlash(cell.TranscriptPath)), filepath.Base(unfinishedScratch), "half made", "half made")
			if err := session.SaveMeta(unfinishedScratch, session.Meta{
				ID:         filepath.Base(unfinishedScratch),
				Title:      "Half Made Scratch Copy",
				Workspace:  workspace,
				Created:    baseAt.Add(-5 * time.Minute),
				LastUserAt: baseAt,
			}); err != nil {
				t.Fatal(err)
			}

			// Verify v3NextSession from a `.moving` chat mints into the project bucket, never nested `.moving/.moving`.
			sibling, err := v3NextSession(session.Place{Dir: standaloneCell.Root, Workspace: workspace}, workspace)
			if err != nil {
				t.Fatalf("v3NextSession from .moving chat: %v", err)
			}
			if session.BucketOf(sibling.Transcript()) != filepath.Clean(bucket) || strings.Contains(sibling.Dir, filepath.Join(".moving", ".moving")) {
				t.Fatalf("v3NextSession minted sibling in wrong directory: %s (bucket=%s)", sibling.Dir, bucket)
			}
			_ = os.RemoveAll(sibling.Dir)

			// Seed an identity and a saved hold receipt from an earlier sync session so
			// turning sync off exercises previously-held `.moving` chats as well.
			var id identity.Identity
			switch mode {
			case "CODEAF_SYNC_URL_off":
				id, err = identity.Ensure(shortHome)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv(syncsetup.URLVar, "off")
			case "sync_otherwise_off":
				// Solo unpaired home with a configured relay URL: sync is quiet/off
				// and must neither talk to the service nor block local chats.
				id, err = identity.EnsureSolo(shortHome)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv(syncsetup.URLVar, relay.URL)
			}
			dev, err := identity.LoadDevice(shortHome)
			if err != nil {
				t.Fatal(err)
			}
			ledger := cellstore.LedgerName(relay.URL, id.ID())
			holdSum := sha256.Sum256([]byte(ledger + "/" + dev.ID() + "/" + standaloneCell.ID))
			holdPath := filepath.Join(shortHome, "v3", "sync", "holds", hex.EncodeToString(holdSum[:]))
			if err := os.MkdirAll(filepath.Dir(holdPath), 0o700); err != nil {
				t.Fatal(err)
			}
			holdRaw, _ := json.Marshal(map[string]any{"cell": standaloneCell.ID, "fence": 9})
			if err := os.WriteFile(holdPath, holdRaw, 0o600); err != nil {
				t.Fatal(err)
			}

			// Check 1: ReadHome lists every chat on this computer, each once.
			world := session.ReadHome()
			rows := world.Sessions()
			if len(rows) != 4 {
				t.Fatalf("ReadHome() returned %d chats, want 4: %+v", len(rows), rows)
			}
			wantByTitle := map[string]string{
				"Legacy Uncopied Chat":   filepath.Join(legacyDir, "transcript.jsonl"),
				"Copied Moving Chat":     filepath.Join(copiedCell.Root, filepath.FromSlash(cell.TranscriptPath)),
				"Standalone Moving Chat": filepath.Join(standaloneCell.Root, filepath.FromSlash(cell.TranscriptPath)),
				"Unfinished Copy Chat":   filepath.Join(unfinishedDir, "transcript.jsonl"),
			}
			seenTitles := map[string]int{}
			for _, row := range rows {
				seenTitles[row.Title]++
				wantTranscript, ok := wantByTitle[row.Title]
				if !ok {
					t.Fatalf("unexpected row on Home: title=%q id=%q transcript=%q", row.Title, row.ID, row.Transcript)
				}
				if filepath.Clean(row.Transcript) != filepath.Clean(wantTranscript) {
					t.Fatalf("row %q transcript = %s, want %s", row.Title, row.Transcript, wantTranscript)
				}
			}
			for title := range wantByTitle {
				if seenTitles[title] != 1 {
					t.Fatalf("Home listed %q %d times, want 1", title, seenTitles[title])
				}
			}

			// Opening the legacy original path with sync off still resolves to its existing `.moving` copy.
			resolvedCopy, err := movablePlace(context.Background(), session.Place{Dir: copiedOrigDir, Workspace: workspace})
			if err != nil {
				t.Fatalf("movablePlace(copiedOrigDir): %v", err)
			}
			if filepath.Clean(resolvedCopy.Dir) != filepath.Clean(copiedCell.Root) {
				t.Fatalf("movablePlace(copiedOrigDir) = %s, want existing copy %s", resolvedCopy.Dir, copiedCell.Root)
			}

			// Check 2: Drive the real Home TUI listing through runSurface (`Landing: true`).
			oldRun := runSurfaceProgram
			oldExecutable := surfaceExecutable
			oldRunningExecutable := surfaceRunningExecutable
			t.Cleanup(func() {
				runSurfaceProgram = oldRun
				surfaceExecutable = oldExecutable
				surfaceRunningExecutable = oldRunningExecutable
			})
			fakeExe := filepath.Join(t.TempDir(), "codeaf")
			if err := os.WriteFile(fakeExe, []byte("exe"), 0o755); err != nil {
				t.Fatal(err)
			}
			surfaceExecutable = func(func() (string, error)) (string, error) { return fakeExe, nil }
			surfaceRunningExecutable = func() (string, error) { return fakeExe, nil }

			runWindow := func(landing bool, sessionFile string, interact func(in io.Writer, frame *syncOffFrameCapture)) {
				proc, err := openV3ProcessWith("chat", true)
				if err != nil {
					t.Fatalf("openV3ProcessWith: %v", err)
				}
				defer proc.closeAll()

				seed := v3Options{
					Model:       "test/model",
					Interactive: true,
					Yolo:        true,
					OneModel:    true,
					Session:     sessionFile,
				}
				launch, err := openV3Launch(proc, seed)
				if err != nil {
					t.Fatalf("openV3Launch(%q): %v", sessionFile, err)
				}
				cfg := launch.Config
				cfg.AskConsent = true
				cfg, open := v3Shape(cfg, v3LanesHere())
				agent, cfg, _, err := openV3Agent(cfg, launch.Project, open)
				if err != nil {
					t.Fatalf("openV3Agent(%q): %v", sessionFile, err)
				}
				proc.track(agent)
				settled := *launch
				settled.Config, settled.Place = cfg, cfg.Place
				settled.SessionFile = cfg.SessionFile
				seam := &v3Seam{proc: proc, boot: &settled, seed: seed}

				frame := &syncOffFrameCapture{}
				inR, inW := io.Pipe()
				defer inR.Close()
				defer inW.Close()

				runSurfaceProgram = func(ctx context.Context, options tui3.Options) error {
					options.Input = inR
					options.Output = frame
					options.Width, options.Height = 120, 40
					return tui3.Run(ctx, options)
				}

				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() {
					done <- runSurface(ctx, tui3.Options{
						Agent:       agent,
						ProfileDir:  proc.ProfileDir,
						Workspace:   cfg.Workspace,
						Owned:       cfg.Place.Owned,
						SessionFile: cfg.SessionFile,
						Resumed:     launch.Resumed,
						Landing:     landing,
						Start:       seam.start,
						Open:        seam.resume,
					})
				}()

				interact(inW, frame)
				cancel()
				if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
					t.Fatalf("runSurface exit: %v", err)
				}
			}

			// Boot a fresh landing window in `workspace` and verify the real Home listing renders every chat once.
			origWD, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(workspace); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(origWD) })

			runWindow(true, "", func(_ io.Writer, frame *syncOffFrameCapture) {
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					plain := frame.Plain()
					allPresent := true
					for title := range wantByTitle {
						if !strings.Contains(plain, title) {
							allPresent = false
							break
						}
					}
					if allPresent {
						for _, unwanted := range []string{"Stale Original Title", "Half Made Scratch Copy"} {
							if strings.Contains(plain, unwanted) {
								t.Fatalf("Home rendered hidden/stale chat %q:\n%s", unwanted, plain)
							}
						}
						return
					}
					time.Sleep(25 * time.Millisecond)
				}
				t.Fatalf("Home screen did not list all 4 chats before deadline:\n%s", frame.Plain())
			})

			// Check 3: Open each chat on Home and write a new turn through the real surface.
			for _, title := range []string{
				"Legacy Uncopied Chat",
				"Copied Moving Chat",
				"Standalone Moving Chat",
				"Unfinished Copy Chat",
			} {
				transcriptPath := wantByTitle[title]
				prompt := "write while sync off for " + title
				expectedReply := "ack:" + prompt

				runWindow(false, transcriptPath, func(in io.Writer, frame *syncOffFrameCapture) {
					deadline := time.Now().Add(8 * time.Second)
					for time.Now().Before(deadline) {
						if frame.Plain() != "" {
							break
						}
						time.Sleep(20 * time.Millisecond)
					}
					if _, err := io.WriteString(in, prompt+"\r"); err != nil {
						t.Fatalf("write input for %q: %v", title, err)
					}
					writeDeadline := time.Now().Add(15 * time.Second)
					for time.Now().Before(writeDeadline) {
						raw, readErr := os.ReadFile(transcriptPath)
						if readErr == nil &&
							strings.Contains(string(raw), prompt) &&
							strings.Contains(string(raw), expectedReply) {
							return
						}
						time.Sleep(30 * time.Millisecond)
					}
					raw, _ := os.ReadFile(transcriptPath)
					t.Fatalf("chat %q (%s) did not persist written turn:\ntranscript:\n%s\nscreen:\n%s", title, transcriptPath, string(raw), frame.Plain())
				})
			}

			// Check 4: Home still lists each of the 4 chats once and zero requests touched the sync service.
			afterRows := session.ReadHome().Sessions()
			if len(afterRows) != 4 {
				t.Fatalf("ReadHome() after writing returned %d chats, want 4: %+v", len(afterRows), afterRows)
			}
			if got := relayRequests.Load(); got != 0 {
				t.Fatalf("sync off contacted the sync service %d times, want 0", got)
			}
		})
	}
}
