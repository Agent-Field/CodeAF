package tui3

import "testing"

func TestTeamNameAnswerWakesAnIdlePaintClock(t *testing.T) {
	a, namer, tiles := namingApp(t)
	namer.name = "textkit"
	cmd := a.wallStartNaming(tiles)
	if cmd == nil {
		t.Fatal("team naming did not start")
	}
	a.painting = false
	namerDoors(t, a, cmd)
	if !a.wall.nameAsking && a.wall.name != "textkit" {
		t.Fatalf("team name did not land: asking=%v name=%q", a.wall.nameAsking, a.wall.name)
	}
	if !a.painting {
		t.Fatal("team name answer did not wake the idle paint clock")
	}
}
