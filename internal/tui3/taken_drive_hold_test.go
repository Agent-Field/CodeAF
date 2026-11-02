package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type arrivingDriveSource struct {
	*continuationListing
	fence uint64
}

func (s *arrivingDriveSource) SendLease(string) (string, uint64, error) {
	return heldCell, s.fence, nil
}

func TestTakenChatFollowsItsLocalDriveLease(t *testing.T) {
	for _, holder := range []string{"this drive", "another local drive", "another device"} {
		t.Run(holder, func(t *testing.T) {
			src := &arrivingDriveSource{continuationListing: &continuationListing{
				row: leftOffRow(heldCell, "other device", chatlist.Running, time.Minute),
			}}
			src.row.Fence = 10
			taker := &fakeTaker{taken: Taken{Fence: 11, Device: "this device"}}
			a, _ := heldLab(t, src, taker)
			taker.taken.Transcript = a.file
			taker.on = func(string) {
				src.fence = 11
				src.change(func(row *chatlist.Row) {
					row.Mine, row.Status, row.Fence = true, chatlist.Here, 11
					row.Device, row.DeviceID = "this device", "dev_here"
				})
			}
			standOn(t, a, heldCell)
			confirm(t, a)
			if a.at(pageHome) || len(taker.cells) != 1 {
				t.Fatal("continuation did not open the taken chat")
			}
			arrived := a.agent.(*switchAgent).fakeAgent
			src.change(func(row *chatlist.Row) {
				row.Fence = 12
				if holder == "another device" {
					row.Mine, row.Status = false, chatlist.Running
					row.Device, row.DeviceID = "other device", "dev_other"
				}
			})
			if holder == "this drive" {
				src.fence = 12
			}
			drain(t, a, a.askMachines())
			notice := false
			for _, entry := range a.entries {
				if entry.kind == entryNote && strings.Contains(plain(entry.text), "this window now only shows it") {
					notice = true
				}
			}
			if notice != (holder == "another device") {
				t.Errorf("displacement notice = %v for %s", notice, holder)
			}
			if holder == "this drive" && a.machineRead.sendHolds[heldCell].fence != 12 {
				t.Error("the window did not follow its drive's publishing hold")
			}
			a.input.setText("send after continuing")
			drive(t, a, key("enter"))
			_, card := a.homeAsking()
			if holder == "this drive" {
				if card || len(arrived.sent) != 1 {
					t.Errorf("held send: card=%v messages=%v", card, arrived.sent)
				}
			} else if !card || len(arrived.sent) != 0 {
				t.Errorf("displaced send: card=%v messages=%v", card, arrived.sent)
			}
		})
	}
}
