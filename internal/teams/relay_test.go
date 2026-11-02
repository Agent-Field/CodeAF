package teams

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMergeRepairsParentsHandlesManagerAndHomeDeterministically(t *testing.T) {
	early := time.Unix(10, 0)
	f := &File{Teams: []Team{
		{ID: "bb", Parent: "aa", Members: []Member{{Key: "cell:y", Word: "storage", Handle: "same", JoinedAt: early.Add(time.Second)}, {Key: "cell:x", Word: "storage", Handle: "same", JoinedAt: early}}, Manager: "gone"},
		{ID: "aa", Parent: "bb"},
	}}
	f.RepairMerged()
	if f.Teams[0].ID != "aa" || f.Teams[0].Parent != "" || f.Teams[1].Parent != "aa" {
		t.Fatalf("parent loop: %+v", f.Teams)
	}
	team := f.Teams[1]
	if team.Manager != "" || team.Members[1].Handle != "same" || team.Members[0].Handle == "same" {
		t.Fatalf("membership repair: %+v", team)
	}
	before, _ := json.Marshal(f.Teams)
	f.RepairMerged()
	after, _ := json.Marshal(f.Teams)
	if string(before) != string(after) {
		t.Fatal("repair is not stable")
	}
}

func TestMergeKeepsOneReportingHomeAndRepairsConcurrentManagerDuties(t *testing.T) {
	f := &File{Teams: []Team{
		{ID: "bb", Manager: "manager-b", Members: []Member{{Key: "manager-b", Word: "leader-b"}, {Key: "shared", Word: "storage", Home: true}}},
		{ID: "aa", Manager: "manager-a", Members: []Member{{Key: "manager-a", Word: "leader-a"}, {Key: "shared", Word: "storage", Home: true}}},
	}}
	f.RepairMerged()
	count := 0
	for _, team := range f.Teams {
		for _, m := range team.Members {
			if m.Key == "shared" && m.Home {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("%d reporting homes survived", count)
	}
	for i := range f.Teams {
		f.Teams[i].Manager = "shared"
	}
	f.RepairMerged()
	if f.Teams[0].Manager != "shared" || f.Teams[1].Manager != "" {
		t.Fatal("concurrent manager duties were not repaired deterministically")
	}
}
