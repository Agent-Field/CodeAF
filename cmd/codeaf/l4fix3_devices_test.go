package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-M15a, L4-M15b, fix round 3: Plain roster names remain usable by devbox,
// while duplicate selectors still appear in the list and removal receipts.
func TestL4Fix3RosterNamesAndSelectors(t *testing.T) {
	kind, dir := chatsWith(t, "desk")
	rows, err := kind.rows()
	if err != nil || len(rows) != 2 {
		t.Fatalf("roster: %+v, %v", rows, err)
	}
	for _, row := range rows {
		if row.Name != "desk" {
			t.Fatalf("a selector replaced the roster name: %+v", row)
		}
	}
	table, err := kind.table(time.Unix(1000, 0))
	if err != nil || !strings.Contains(table, "desk #aaaa") || !strings.Contains(table, "desk #bbbb") {
		t.Fatalf("selectors: %q, %v", table, err)
	}
	line, err := kind.stop("desk#bbbb")
	if err != nil || line != pair.ChatsRevokedLine("desk #bbbb") {
		t.Fatalf("removal receipt: %q, %v", line, err)
	}
	listing, err := dir.List(t.Context())
	if err != nil || !listing.Devices[otherComputer].Revoked || listing.Devices[thisComputer].Revoked {
		t.Fatalf("removal selected the wrong device: %+v, %v", listing, err)
	}
	line, err = kind.stop("desk#bbbb")
	if err != nil || line != "desk #bbbb is already removed." {
		t.Fatalf("repeated removal: %q, %v", line, err)
	}
	// The displayed selector, rather than the full device id, still decides
	// alphabetical order after selectors move out of the roster.
	sealed, err := directory.SealName(kind.key, "desk")
	if err != nil {
		t.Fatal(err)
	}
	ordered := chatsKind{key: kind.key, dir: &l4DeviceDirectory{listing: directory.Listing{Devices: map[string]directory.Device{
		"dev_1111bbbb": {Name: sealed},
		"dev_2222aaaa": {Name: sealed},
	}}}}
	table, err = ordered.table(time.Unix(1000, 0))
	if err != nil || strings.Index(table, "desk #aaaa") < 0 || strings.Index(table, "desk #aaaa") > strings.Index(table, "desk #bbbb") {
		t.Fatalf("selector order: %q, %v", table, err)
	}
}
