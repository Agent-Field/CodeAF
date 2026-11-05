package teams

import (
	"encoding/json"
	"fmt"
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

// F1 preserves the existing sharing fixture and history contract.
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

// R1/F2 preserves original team names and both managers in either merge order.
func TestFirstMergeKeepsBothTeamNamesAndGlobalManagersDeterministically(t *testing.T) {
	at := time.Unix(10, 0)
	left := []Team{
		{ID: "a-root", Name: RootName, Root: true, Made: at, Manager: "a-manager", Members: []Member{{Key: "a-manager", Word: "a manager", Handle: "boss"}}},
		{ID: "a-harbor", Name: "harbor", Made: at},
	}
	right := []Team{
		{ID: "b-root", Name: RootName, Root: true, Made: at.Add(time.Second), Manager: "b-manager", Members: []Member{{Key: "b-manager", Word: "b manager", Handle: "boss"}}},
		{ID: "b-harbor", Name: "harbor", Made: at.Add(time.Second)},
	}
	// Each computer starts from its own valid file before either sees the other.
	originals := [][]Team{left, right}
	for i, original := range originals {
		dir := t.TempDir()
		if err := Update(dir, func(f *File) error {
			for _, t := range original {
				f.Teams = append(f.Teams, t.Clone())
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		file, err := Snapshot(dir)
		if err != nil {
			t.Fatal(err)
		}
		originals[i] = file.Teams
	}
	left, right = originals[0], originals[1]
	var previous []byte
	for _, order := range [][]Team{append(append([]Team{}, left...), right...), append(append([]Team{}, right...), left...)} {
		dir := t.TempDir()
		if err := Update(dir, func(f *File) error {
			for _, team := range order {
				f.Teams = append(f.Teams, team.Clone())
			}
			f.RepairMerged()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		f, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"a-harbor", "b-harbor"} {
			team, ok := f.Team(id)
			if !ok || team.Name != "harbor" {
				t.Fatalf("merge changed an original name: %+v", team)
			}
		}
		root, _ := f.Root()
		other, _ := f.Team("b-root")
		if root.ID != "a-root" || root.Manager != "a-manager" || other.Root || other.Manager != "b-manager" || other.Parent != root.ID || other.FormerManager != "" {
			t.Fatalf("leadership lost: root=%+v other=%+v", root, other)
		}
		raw, _ := json.Marshal(f.Teams)
		if previous != nil && string(previous) != string(raw) {
			t.Fatalf("merge order changed bytes:\n%s\n%s", previous, raw)
		}
		previous = raw
		f.RepairMerged()
		again, _ := json.Marshal(f.Teams)
		if string(raw) != string(again) {
			t.Fatal("repair is not idempotent")
		}
	}
}

// R2/F2 records cleared appointments only when the former-manager slot can hold them.
func TestMergePreservesClearedManagerAppointment(t *testing.T) {
	for _, former := range []string{"", "shared", "earlier"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("former=%s/missing=%v", former, missing), func(t *testing.T) {
				f := &File{Teams: []Team{
					{ID: "a", Name: "a", Manager: "shared", Members: []Member{{Key: "shared", Word: "manager"}}},
					{ID: "b", Name: "b", Manager: "shared", FormerManager: former, Members: []Member{{Key: "shared", Word: "manager"}}},
				}}
				if missing {
					f.Teams[1].Members = nil
				}
				f.RepairMerged()
				want := former
				if want == "" {
					want = "shared"
				}
				if f.Teams[1].Manager != "" || f.Teams[1].FormerManager != want {
					t.Fatalf("cleared appointment replaced history: %+v", f.Teams[1])
				}
			})
		}
	}
}

// R2/F2 keeps an active manager out of former appointments when its second root is demoted.
func TestMergeDemotedRootKeepsFormerManager(t *testing.T) {
	for _, former := range []string{"", "earlier"} {
		f := &File{Teams: []Team{
			{ID: "a", Name: RootName, Root: true},
			{ID: "b", Name: RootName, Root: true, Manager: "active", FormerManager: former, Members: []Member{{Key: "active", Word: "manager"}}},
		}}
		f.RepairMerged()
		if f.Teams[1].Root || f.Teams[1].Manager != "active" || f.Teams[1].FormerManager != former {
			t.Fatalf("demotion changed appointments: %+v", f.Teams[1])
		}
	}
}
