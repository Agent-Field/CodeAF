package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// withListSource installs a Source for one test.
func withListSource(t *testing.T, src chatlist.Source, err error) {
	t.Helper()
	old := cellListSource
	cellListSource = func() (chatlist.Source, error) { return src, err }
	t.Cleanup(func() { cellListSource = old })
}

func TestCellListAll(t *testing.T) {
	withListSource(t, chatlist.Static{
		{Cell: "01AAA", Title: "Nightly index rebuild", Device: "studio", Status: chatlist.Running, DurableAgo: 2 * time.Minute},
		{Cell: "01BBB", Title: "Port the picker", Device: "studio", Status: chatlist.Off, DurableAgo: 3 * time.Hour},
		{Cell: "01CCC", Title: "Fix the flaky test", Device: "laptop", Status: chatlist.Branch, OrphanTurns: 2, DurableAgo: 5 * time.Hour},
		{Cell: "01DDD", Title: "Notes", Device: "laptop", Status: chatlist.Idle, DurableAgo: 26 * time.Hour},
	}, nil)
	var out bytes.Buffer
	if err := runCellIn([]string{"list", "--all"}, &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"01AAA  Nightly index rebuild  studio  running on studio             2m",
		"01BBB  Port the picker        studio  studio off                    3h",
		"01CCC  Fix the flaky test     laptop  2 turns from laptop: discard  5h",
		"01DDD  Notes                  laptop  -                             1d 2h",
	}
	if got := strings.TrimSpace(out.String()); got != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestCellListAllSyncOff(t *testing.T) {
	old := cellListSource
	t.Cleanup(func() { cellListSource = old })
	err := runCellIn([]string{"list", "--all"}, &bytes.Buffer{}, t.TempDir())
	if err == nil || err.Error() != chatlist.SyncOff {
		t.Fatalf("err = %v, want %q", err, chatlist.SyncOff)
	}
}

type unreachableSource struct{}

func (unreachableSource) Rows(context.Context) ([]chatlist.Row, error) {
	return nil, errors.New("no route")
}

func TestCellListAllUnreachable(t *testing.T) {
	withListSource(t, unreachableSource{}, nil)
	err := runCellIn([]string{"list", "--all"}, &bytes.Buffer{}, t.TempDir())
	if err == nil || !strings.HasPrefix(err.Error(), chatlist.Unreachable) {
		t.Fatalf("err = %v", err)
	}
}

func TestCellListNeedsAll(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"list", "--some"}, {"list", "--all", "x"}} {
		if err := runCellIn(args, &bytes.Buffer{}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: err = %v, want usage", args, err)
		}
	}
}
