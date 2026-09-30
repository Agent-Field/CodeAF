package chatlist

import (
	"testing"
	"time"
)

func TestSameIgnoresReadTimeButNotTheTurn(t *testing.T) {
	a := []Row{{Cell: "c", Status: Idle, DurableAt: 100, DurableAgo: time.Second}}
	sameLater := []Row{{Cell: "c", Status: Idle, DurableAt: 100, DurableAgo: time.Minute}}
	newTurn := []Row{{Cell: "c", Status: Idle, DurableAt: 200, DurableAgo: time.Second}}
	if !Same(a, sameLater) {
		t.Fatal("two reads of an unchanged list differed")
	}
	if Same(a, newTurn) || Same(a, nil) {
		t.Fatal("a new turn or a missing row counted as unchanged")
	}
}
