package tui3

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// The display may start empty or retain this computer's pre-restart hold.
// Ownership reads have the newer head and B's released or lapsed lease.
type r5HomeSource struct {
	display chatlist.Static
	fresh   *continuationListing
}

func (s *r5HomeSource) Rows(ctx context.Context) ([]chatlist.Row, error) {
	return s.display.Rows(ctx)
}

func (s *r5HomeSource) RowsForSend(ctx context.Context) ([]chatlist.Row, uint64, error) {
	rows, err := s.fresh.Rows(ctx)
	return rows, 10, err
}

func TestIdleOpen_R5_FirstOpenAfterRestartTakesFreeChat(t *testing.T) {
	for _, expires := range []int64{0, 500} {
		for _, cached := range []bool{false, true} {
			name := map[bool]string{false: "first directory read pending", true: "previous own hold cached"}[cached]
			name += map[bool]string{true: "/released", false: "/lapsed"}[expires == 0]
			t.Run(name, func(t *testing.T) {
				lab := newHomeLab(t)
				workspace := lab.workspace("project")
				file := lab.session("project", heldCell, "Mine", workspace, time.Now())
				original, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				lease := directory.Lease{Device: "B", Fence: 3, Expires: expires}
				if expires == 0 {
					lease.ReleasedAt = 750
				}
				listing := directory.Listing{
					Now:     1000,
					Devices: map[string]directory.Device{"A": {}, "B": {Name: "laptop"}},
					Cells: map[string]directory.Cell{heldCell: {
						Head:  "newest",
						Lease: lease,
					}},
				}
				row := chatlist.Rows(listing, "A", func(s string) (string, error) { return s, nil })[0]
				fresh := &continuationListing{row: row}
				source := &r5HomeSource{fresh: fresh}
				if cached {
					source.display = chatlist.Static{{Cell: heldCell, Status: chatlist.Here, Mine: true, DeviceID: "A", Fence: 1, Head: "old"}}
				}
				taker := &walkOpenTaker{source: fresh, file: file, journal: string(original) + `{"type":"message","role":"assistant","content":"newest journal from B"}` + "\n"}
				// A new surface starts on Home with only A's transcript on disk.
				// There is no retained viewer, displaced notice or send hold.
				a := lab.app("")
				keepSurfaceWorkersInTest(t, a)
				a.width, a.height = 200, 70
				a.machines, a.taker = source, taker
				drain(t, a, a.openHome())
				for i, line := range a.home.lines {
					if line.kind == homeSession && line.row.Transcript == file {
						a.home.cursor = i
						drive(t, a, key("enter"))
						if taker.opens != 1 || len(taker.cells) != 0 {
							t.Fatalf("first Home open used local viewer: automatic opens=%d forced takes=%v", taker.opens, taker.cells)
						}
						raw, err := os.ReadFile(file)
						if err != nil || string(raw) != taker.journal {
							t.Fatalf("newest head not restored: %q, %v", raw, err)
						}
						rows, _ := fresh.Rows(t.Context())
						if !rows[0].Mine || rows[0].Status != chatlist.Here || rows[0].Fence != 4 {
							t.Fatalf("no new ownership fence: %+v", rows)
						}
						if a.file != file || strings.Contains(homeText(a), continueAsk) {
							t.Fatalf("first open did not arrive directly: file=%q", a.file)
						}
						return
					}
				}
				t.Fatal("fixture did not expose the persisted local Home row")
			})
		}
	}
}

func TestIdleOpen_R5_LocalRowKeepsLiveConsentAndUnpairedOpening(t *testing.T) {
	for _, mode := range []string{"live", "unpaired", "unreachable", "own free"} {
		t.Run(mode, func(t *testing.T) {
			row := chatlist.Row{Cell: heldCell, Status: chatlist.Running, DeviceID: "B", Device: "laptop", Fence: 3, Head: "newest"}
			if mode != "live" {
				row.Status = chatlist.Idle
			}
			if mode == "own free" {
				row.Mine, row.Released, row.AutoOpen = true, true, true
			}
			fresh := &continuationListing{row: row}
			if mode == "unreachable" {
				fresh.err = errors.New("fixture relay unavailable")
			}
			source := &r5HomeSource{fresh: fresh}
			taker := &walkOpenTaker{source: fresh}
			lab := newHomeLab(t)
			workspace := lab.workspace("project")
			file := lab.session("project", heldCell, "Mine", workspace, time.Now())
			taker.file = file
			journal, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			taker.journal = string(journal)
			a := lab.app("")
			keepSurfaceWorkersInTest(t, a)
			a.width, a.height = 200, 70
			a.machines, a.taker = source, taker
			drain(t, a, a.openHome())
			found := false
			for i, line := range a.home.lines {
				if line.kind == homeSession && line.row.Transcript == file {
					a.home.cursor, found = i, true
					break
				}
			}
			if !found {
				t.Fatal("missing local row")
			}
			drive(t, a, key("enter"))
			if len(taker.cells) != 0 {
				t.Fatalf("forced take: %v", taker.cells)
			}
			switch mode {
			case "live":
				if taker.opens != 0 || !a.at(pageHome) || !strings.Contains(homeText(a), continueAsk) {
					t.Fatal("live hold did not retain consent")
				}
			case "unpaired":
				if taker.opens != 0 || a.file != file {
					t.Fatal("unpaired local opening changed")
				}
			case "unreachable":
				if taker.opens != 0 || !a.at(pageHome) || a.file != "" {
					t.Fatal("unavailable ownership opened a stale local viewer")
				}
			case "own free":
				rows, _ := fresh.Rows(t.Context())
				if taker.opens != 1 || a.file != file || rows[0].Fence != 4 {
					t.Fatalf("own free reopen reused the old fence: opens=%d rows=%+v", taker.opens, rows)
				}
			}
		})
	}
}
