package chatlist

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

const self = "dev_" + "aaaaaaaabbbbbbbbccccccccdddddddd"
const other = "dev_" + "11223344556677889900aabbccddeeff"

// plain is an Opener that treats every sealed name as already open.
func plain(s string) (string, error) { return s, nil }

func listing(cells map[string]directory.Cell) directory.Listing {
	return directory.Listing{
		Now: 1_000_000,
		Devices: map[string]directory.Device{
			self:  {Name: "laptop"},
			other: {Name: "studio"},
		},
		Cells: cells,
	}
}

func cell(dev string, expires int64, mut func(*directory.Cell)) directory.Cell {
	c := directory.Cell{Title: "t", DurableAt: 990_000, Lease: directory.Lease{Device: dev, Expires: expires}}
	if mut != nil {
		mut(&c)
	}
	return c
}

func TestRowsStatusTable(t *testing.T) {
	live, dead := int64(1_010_000), int64(900_000)
	cases := []struct {
		name string
		c    directory.Cell
		want Status
	}{
		{"here", cell(self, live, nil), Here},
		{"running", cell(other, live, nil), Running},
		{"off", cell(other, dead, nil), Off},
		{"idle", cell(other, 0, nil), Idle},
		{"branch beats here", cell(self, live, func(c *directory.Cell) { c.OrphanTurns = 2 }), Branch},
		{"branch beats idle", cell(other, 0, func(c *directory.Cell) { c.OrphanTurns = 1 }), Branch},
		{"expiry exactly now is off", cell(other, 1_000_000, nil), Off},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := Rows(listing(map[string]directory.Cell{"c1": tc.c}), self, plain)
			if len(rows) != 1 || rows[0].Status != tc.want {
				t.Fatalf("got %+v, want status %s", rows, tc.want)
			}
		})
	}
}

func TestRowsUseDirectoryClock(t *testing.T) {
	// Rows takes no wall clock at all, so the age is the directory's alone.
	rows := Rows(listing(map[string]directory.Cell{"c1": cell(other, 0, nil)}), self, plain)
	if got := rows[0].DurableAgo; got != 10*time.Second {
		t.Fatalf("DurableAgo = %v, want 10s", got)
	}
}

func TestRowWithUnknownDevice(t *testing.T) {
	l := listing(map[string]directory.Cell{"c1": cell("dev_deadbeef00112233445566778899aabb", 1_010_000, nil)})
	r := Rows(l, self, plain)[0]
	if r.Device != "deadbeef" || r.Status != Running {
		t.Fatalf("got %+v", r)
	}
}

func TestRowNamesThatFailToOpen(t *testing.T) {
	bad := func(string) (string, error) { return "", errors.New("wrong key") }
	r := Rows(listing(map[string]directory.Cell{"c1": cell(other, 1_010_000, nil)}), self, bad)[0]
	if r.Title != "untitled" || r.Device != "11223344" {
		t.Fatalf("got title %q device %q", r.Title, r.Device)
	}
}

func TestRowsOrder(t *testing.T) {
	at := func(ms int64) directory.Cell {
		return cell(other, 0, func(c *directory.Cell) { c.DurableAt = ms })
	}
	rows := Rows(listing(map[string]directory.Cell{
		"b": at(500_000), "a": at(500_000), "new": at(900_000), "old": at(100_000),
	}), self, plain)
	var got []string
	for _, r := range rows {
		got = append(got, r.Cell)
	}
	want := []string{"new", "a", "b", "old"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

func TestArchivedHidden(t *testing.T) {
	arch := cell(other, 0, func(c *directory.Cell) { c.Archived, c.OrphanTurns = true, 3 })
	rows := Rows(listing(map[string]directory.Cell{"gone": arch, "kept": cell(other, 0, nil)}), self, plain)
	if len(rows) != 1 || rows[0].Cell != "kept" {
		t.Fatalf("got %+v", rows)
	}
}

func TestRowsCarryCounts(t *testing.T) {
	c := cell(other, 1_010_000, func(c *directory.Cell) { c.Lease.Pending, c.ParentCell = 4, "p1" })
	r := Rows(listing(map[string]directory.Cell{"c": c}), self, plain)[0]
	if r.Pending != 4 || r.Parent != "p1" || r.DeviceID != other {
		t.Fatalf("got %+v", r)
	}
}

func TestStaticIsASource(t *testing.T) {
	var s Source = Static{{Cell: "x"}}
	rows, err := s.Rows(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("got %v %v", rows, err)
	}
}
