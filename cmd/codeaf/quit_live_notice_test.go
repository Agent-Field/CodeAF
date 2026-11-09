package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type observedLiveNoticeWriter struct {
	io.Writer
	delivered chan struct{}
	once      sync.Once
}

func (w *observedLiveNoticeWriter) Write(raw []byte) (int, error) {
	n, err := w.Writer.Write(raw)
	if err == nil && bytes.Contains(raw, []byte(`"kind":"notice"`)) {
		w.once.Do(func() { close(w.delivered) })
	}
	return n, err
}

func TestQuittingKeepsALiveSavingSentenceAfterTheLastSurfaceDrain(t *testing.T) {
	for _, shown := range []bool{false, true} {
		t.Run(map[bool]string{false: "after final drain", true: "already shown"}[shown], func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			t.Setenv(syncsetup.URLVar, "off")
			t.Cleanup(stopFinalSaveProgress)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			watch := &cellstore.SealWatch{}
			far := &storageOffNoticeAgent{watch: watch}
			surface, engine := remote.Pipe()
			writer := &observedLiveNoticeWriter{Writer: engine, delivered: make(chan struct{})}
			served := make(chan error, 1)
			guard.Go("quit notice fixture engine", func() {
				defer engine.Close()
				served <- remote.Serve(engine, writer, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{Agent: far}, nil
				}})
			})
			client, err := remote.Dial(surface, "fixture", remote.Hello{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close(); <-served }()
			input, typed := io.Pipe()
			defer input.Close()
			defer typed.Close()
			screen := &surfaceFrameWriter{ready: make(chan struct{})}
			previousRun := runSurfaceProgram
			runSurfaceProgram = func(ctx context.Context, options tui3.Options) error {
				options.Input, options.Output = input, screen
				options.UpdateCheck = nil
				return tui3.Run(ctx, options)
			}
			defer func() { runSurfaceProgram = previousRun }()
			line := chatlist.Superseded("desk")
			inject := func() {
				watch.Note(line)
				select {
				case <-writer.delivered:
					// A subsequent reply proves the reader queued the earlier live frame.
					if _, err := client.Ping(); err != nil {
						t.Error(err)
					}
				case <-ctx.Done():
					t.Error("live sentence was never delivered")
				}
			}
			done := make(chan error, 1)
			guard.Go("quit notice fixture window", func() {
				done <- runSurface(ctx, tui3.Options{
					Agent: client.Agent(), Host: "fixture", ProfileDir: home.Dir(),
					Workspace: home.Dir(), SessionFile: filepath.Join(home.Dir(), "transcript.jsonl"),
					Input: input, Output: screen, Width: 160, Height: 35, Linear: true,
					Link: tui3.LinkSeam{Notice: client.TakeNotice},
					ClosingStarted: func(<-chan struct{}) {
						if !shown {
							// Quit has drained the surface, and has not started closing the engine yet.
							inject()
						}
					},
				})
			})
			select {
			case <-screen.ready:
			case err := <-done:
				t.Fatalf("window closed before painting: %v", err)
			case <-ctx.Done():
				t.Fatal("window never painted")
			}
			if shown {
				if _, err := io.WriteString(typed, "fixture turn\r"); err != nil {
					t.Fatal(err)
				}
				inject()
				waitStorageOffBoundary(t, "live sentence shown in the window", func() bool {
					return strings.Contains(screen.String(), line)
				})
			}
			if _, err := io.WriteString(typed, "/quit\r"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			oldOut, oldFlush := os.Stdout, flushFinalSaves
			out, err := os.CreateTemp(t.TempDir(), "receipt")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			os.Stdout, flushFinalSaves = out, func(context.Context) error { return nil }
			defer func() { os.Stdout, flushFinalSaves = oldOut, oldFlush }()
			_, _ = io.WriteString(out, "screen restored\n")
			_ = finishFinalSaving(t.Context())
			_ = finishFinalSaving(t.Context())
			raw, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if shown {
				want = 0
			}
			if strings.Count(string(raw), line) != want {
				t.Fatalf("restored-terminal report %q, want sentence count %d", raw, want)
			}
			if !shown && strings.Contains(screen.String(), line) {
				t.Fatal("the sentence also appeared in the closing window")
			}
		})
	}
}
