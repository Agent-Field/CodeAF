package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestIdleRelease_R3_AppTakeSameHolderNewFenceSaysPickedUpAgain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		different bool
		during    bool
		refusal   error
	}{
		{name: "same holder before take"},
		{name: "different holder before take", different: true},
		{name: "same holder during take lease held", during: true, refusal: directory.ErrLeaseHeld},
		{name: "same holder during take fence stale", during: true, refusal: directory.ErrFenceStale},
		{name: "different holder during take", different: true, during: true, refusal: directory.ErrLeaseHeld},
	} {
		t.Run(tc.name, func(t *testing.T) {
			celltest.Available(t)
			appDevicesRelay(t)
			aHome, bHome, cHome := t.TempDir(), t.TempDir(), t.TempDir()
			id, err := identity.Ensure(aHome)
			if err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{bHome, cHome} {
				if _, err := identity.Adopt(dir, id, false); err != nil {
					t.Fatal(err)
				}
			}
			a, b, c := appDeviceSync(t, aHome), appDeviceSync(t, bHome), appDeviceSync(t, cHome)
			for s, name := range map[*syncsetup.Sync]string{a: "Studio Mac", b: "Desktop", c: "Tower"} {
				if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, name); err != nil {
					t.Fatal(err)
				}
			}
			chat, err := cell.NewID(time.Now())
			if err != nil {
				t.Fatal(err)
			}
			initial, err := a.Dir.Create(t.Context(), chat, directory.CellInit{Head: "saved-head"})
			if err != nil {
				t.Fatal(err)
			}
			_, _, wire := appDeviceBackend(t, bHome)
			findRow := func() appwire.ChatRow {
				for _, row := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
					if row.Cell == chat {
						return row
					}
				}
				t.Fatal("chat is missing from the listing")
				return appwire.ChatRow{}
			}
			seen := findRow().Seen
			holder := a
			if tc.different {
				holder = c
			}
			var before directory.CellView
			advance := func() error {
				if err := directory.ReleaseQuiet(t.Context(), a.Dir, chat, initial.Cell.Lease.Fence); err != nil {
					return err
				}
				before, err = holder.Dir.Acquire(t.Context(), chat, directory.AcquireOpts{})
				return err
			}
			originalTake, originalOpen := appTakeSeen, appTakeOpen
			t.Cleanup(func() { appTakeSeen, appTakeOpen = originalTake, originalOpen })
			takeCalls, opened := 0, false
			appTakeSeen = func(context.Context, *syncsetup.Sync, string, *directory.LeaseExpectation, func(syncsetup.MoveProgress)) (syncsetup.Continued, error) {
				takeCalls++
				if err := advance(); err != nil {
					return syncsetup.Continued{}, err
				}
				return syncsetup.Continued{}, tc.refusal
			}
			appTakeOpen = func(*appBackend, string) (appwire.Agent, error) {
				opened = true
				return appwire.Agent{}, nil
			}
			if !tc.during {
				if err := advance(); err != nil {
					t.Fatal(err)
				}
			}
			_, refusal := wire.call("agents.take", appwire.TakeParams{Cell: chat, Seen: seen})
			row := findRow()
			want := row.Holder.Name + " picked this chat up again"
			if tc.different {
				want = chatlist.LostRace
			}
			if refusal == nil || refusal.Code != appwire.CodeConflict || refusal.Message != want {
				t.Fatalf("agents.take refusal = %+v, want conflict %q", refusal, want)
			}
			var detail appwire.HolderChanged
			if err := json.Unmarshal(e2JSON(refusal.Detail), &detail); err != nil || detail.Kind != "holder_changed" {
				t.Fatalf("refusal detail = %+v, error = %v", refusal.Detail, err)
			}
			after, err := a.Dir.Cell(t.Context(), chat)
			if err != nil || !reflect.DeepEqual(after.Cell, before.Cell) || opened || row.Here || row.Taking {
				t.Fatalf("refusal changed the chat: before=%+v after=%+v opened=%v row=%+v error=%v", before.Cell, after.Cell, opened, row, err)
			}
			if wantCalls := map[bool]int{false: 0, true: 1}[tc.during]; takeCalls != wantCalls {
				t.Fatalf("take calls = %d, want %d", takeCalls, wantCalls)
			}
		})
	}
}
