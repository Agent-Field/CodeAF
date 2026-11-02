package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestC2DesktopStoresWaitForWindowsReaders(t *testing.T) {
	writers := map[string]func(*testing.T, string) (string, func(string) error){
		"settings": func(t *testing.T, dir string) (string, func(string) error) {
			s := &appState{path: filepath.Join(dir, "state.json")}
			return s.path, func(words string) error {
				return s.write(func(f *appStateFile) error { f.WorkFolder = words; return nil })
			}
		},
		"crews": func(t *testing.T, dir string) (string, func(string) error) {
			l := &appCrewLayer{path: filepath.Join(dir, "crews.json")}
			return l.path, func(words string) error {
				return l.write(func(f *appCrewLayerFile) error { f.crew("crew").Goal = words; return nil })
			}
		},
		"factories": func(t *testing.T, dir string) (string, func(string) error) {
			h := &appFactoryHub{dir: func() string { return dir }}
			return h.path(), func(words string) error { h.rows = []appFactoryRow{{ID: words}}; return h.save() }
		},
		"notice-index": func(t *testing.T, dir string) (string, func(string) error) {
			l := &appNoticeLog{dir: dir}
			path := filepath.Join(dir, "notice-index.json")
			return path, func(words string) error {
				ix := newAppNoticeIndex()
				ix.Newest[words] = 1
				l.writeIndex(path, ix)
				return nil
			}
		},
		"ledger-export": func(t *testing.T, dir string) (string, func(string) error) {
			path := filepath.Join(dir, "ledger.jsonl")
			return path, func(words string) error {
				_, err := appWriteWhole(path, true, func(w io.Writer) error { _, err := io.WriteString(w, words); return err })
				return err
			}
		},
		"footers": func(t *testing.T, dir string) (string, func(string) error) {
			journal := filepath.Join(dir, "session.jsonl")
			if err := os.WriteFile(journal, []byte("{\"type\":\"session\",\"version\":1,\"id\":\"c2\"}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			b := &appBackend{now: time.Now}
			a := &appAgent{transcript: journal}
			return filepath.Join(dir, appFootersName), func(words string) error {
				return b.keepAppEntry(a, appwire.Entry{ID: words, Text: words}, "")
			}
		},
	}
	for name, factory := range writers {
		t.Run(name, func(t *testing.T) {
			path, write := factory(t, t.TempDir())
			if err := write("before"); err != nil {
				t.Fatal(err)
			}
			reader, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			returned, closed := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(closed)
				select {
				case <-time.After(100 * time.Millisecond):
				case <-returned:
				}
				_ = reader.Close()
			}()
			defer func() { close(returned); <-closed }()
			if err := write("after"); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil || !bytes.Contains(raw, []byte("after")) {
				t.Fatalf("new state did not survive the reader: %s (%v)", raw, err)
			}
		})
	}
}
