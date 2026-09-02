package main

import (
	"errors"
	"testing"
)

// A FAILURE KEEPS ITS OWN EVIDENCE WITHOUT BEING ASKED.
//
// The table is the whole decision, which is why it is one function and not
// four: the only run whose private store is thrown away is the one that worked
// and was not asked to keep it. Everything else — asked on purpose, the debug
// switch, a failure, a partial, a run that never reached an outcome at all —
// leaves the record on disk, because the person who wants it only finds out
// they wanted it after the thing went wrong.
func TestAFailedHeadlessRunKeepsItsRecord(t *testing.T) {
	for _, row := range []struct {
		name      string
		asked     bool
		debugging bool
		outcome   headlessOutcome
		err       error
		keep      bool
	}{
		{name: "a clean run is deleted", keep: false},
		{name: "a clean run asked to be kept", asked: true, keep: true},
		{name: "a clean run under the debug switch", debugging: true, keep: true},
		{name: "a run that failed", outcome: headlessOutcome{status: exitFailed}, keep: true},
		{name: "a run that landed partial", outcome: headlessOutcome{status: exitPartial}, keep: true},
		{name: "a run that never reached an outcome", err: errors.New("the store would not open"), keep: true},
		{name: "a failure asked to be kept", asked: true, outcome: headlessOutcome{status: exitFailed}, keep: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := keepPrivateStore(row.asked, row.debugging, errandSucceeded(row.outcome, row.err))
			if got != row.keep {
				t.Fatalf("keep = %v, want %v", got, row.keep)
			}
		})
	}
}

// The debug switch is read straight from the environment, so the spelling of
// "off" is worth pinning: a person who set it to 0 to turn it off did not mean
// to turn it on.
func TestTheDebugSwitchIsOffWhenItSaysSo(t *testing.T) {
	for value, on := range map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"FALSE": false,
		"off":   false,
		"Off":   false,
		"1":     true,
		"true":  true,
		"yes":   true,
	} {
		env := func(string) string { return value }
		if got := debugRecordOn(env); got != on {
			t.Fatalf("AFORGE_DEBUG=%q read as %v, want %v", value, got, on)
		}
	}
}

// `aforge exec` runs one leaf, and every model call it makes belongs to that
// leaf. The key is what puts the node on the call-log row: without it the whole
// run's rows name no work at all, and a person reading the record afterwards
// cannot tell an exec call from a call with nothing behind it.
func TestTheExecTaskCarriesANodeKey(t *testing.T) {
	task := execTask("count the lines in notes.txt", "be brief", "/tmp/work")
	if task.NodeKey == "" {
		t.Fatal("the exec task names no node, so every row it writes is anonymous")
	}
	if task.NodeKey != execNodeKey {
		t.Fatalf("node key = %q, want %q", task.NodeKey, execNodeKey)
	}
}
