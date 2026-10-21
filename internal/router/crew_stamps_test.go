package router

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

func TestCrewStampReadsOnceAcrossCallersAndClockChanges(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	path, err := statePath(dir, "router-events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	record := &CrewRecord{TaskClass: "bugfix", Seats: map[string]string{"worker": "model"}}
	row := Event{At: now, Call: "one", Class: CrewClass, Crew: record, Final: true, Outcome: CrewAccepted, Cost: 1}
	write := func(cost float64) []byte {
		row.Cost = cost
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		return append(raw, '\n')
	}
	if err := os.WriteFile(path, write(1), 0600); err != nil {
		t.Fatal(err)
	}
	before := crewRows.Reads()
	check := func(cost float64, reads uint64) {
		t.Helper()
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				got := ReadCrewLog(dir, now)
				if got.SpentUSD != cost {
					t.Errorf("cost=%v want %v", got.SpentUSD, cost)
				}
			})
		}
		wg.Wait()
		if got := crewRows.Reads() - before; got != reads {
			t.Fatalf("reads=%d want %d", got, reads)
		}
	}
	check(1, 1)
	log := ReadCrewLog(dir, now)
	log.Recent[0].Record.Seats["worker"] = "mutated"
	log.LastGood.Seats["worker"] = "mutated"
	if ReadCrewLog(dir, now).Recent[0].Record.Seats["worker"] != "model" {
		t.Fatal("caller mutated cached crew")
	}
	if ReadCrewLog(dir, now.AddDate(0, 0, 1)).SpentUSD != 0 {
		t.Fatal("cached spend survived midnight")
	}
	info, _ := os.Stat(path)
	next := path + ".next"
	if err := os.WriteFile(next, write(2), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(next, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	check(2, 2)
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	check(0, 3)
	if err := os.WriteFile(path, write(3), 0600); err != nil {
		t.Fatal(err)
	}
	check(3, 4)
}

func TestCrewRoutesExpireWithoutReadingAgain(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	LogCrewRoute(dir, "route", CrewRecord{}, CrewRouteOutcome{Seat: "worker", Send: "model", Kind: "busy"})
	before := crewRows.Reads()
	if len(ReadCrewLog(dir, now).Routes) != 1 {
		t.Fatal("route missing")
	}
	if len(ReadCrewLog(dir, now.Add(crewOutcomesFor+time.Hour)).Routes) != 0 {
		t.Fatal("route did not expire")
	}
	if crewRows.Reads() != before+1 {
		t.Fatal("clock change re-read the log")
	}
}
