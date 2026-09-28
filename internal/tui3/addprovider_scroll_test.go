package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAddProviderSelectionStaysVisibleInShortTerminal(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	for _, height := range []int{1, 3, 5} {
		t.Run(fmt.Sprintf("rows_%d", height), func(t *testing.T) {
			p := addProviderPanel{open: true}
			p.rebuild(nil, nil)
			assertVisible := func() {
				t.Helper()
				selected, ok := p.current()
				if !ok {
					t.Fatal("selection landed on a heading")
				}
				rows := p.draw(100, height, a.pal, -1)
				if len(rows) > height {
					t.Fatalf("%d rows exceeds %d", len(rows), height)
				}
				if !strings.Contains(strings.Join(rows, "\n"), selected.title) {
					t.Fatalf("selected %q is invisible in %d rows: %q", selected.title, height, rows)
				}
			}
			for range len(p.items) {
				assertVisible()
				p.move(1)
			}
			if selected, _ := p.current(); !selected.custom {
				t.Fatal("custom address row was not reachable")
			}
			for range len(p.items) {
				assertVisible()
				p.move(-1)
			}
		})
	}
}

func TestProviderDiscoveryPreservesSelectionAndIgnoresOldPanel(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.openAddProvider(false)
	old := a.addPanel.probeContext
	for range len(a.addPanel.items) {
		a.addPanel.move(1)
	}
	a.Update(localServersProbedMsg{ctx: old, probes: []LocalServerProbe{{Name: "local example", Address: "http://127.0.0.1:1234/v1"}}})
	if item, _ := a.addPanel.current(); !item.custom {
		t.Fatal("discovery moved the selected custom provider")
	}
	a.addPanel.close()
	if old.Err() != context.Canceled {
		t.Fatal("closing panel did not cancel discovery")
	}
	a.openAddProvider(false)
	a.Update(localServersProbedMsg{ctx: old})
	if !a.addPanel.loading {
		t.Fatal("old probe completed the new panel")
	}
	a.addPanel.close()
}
