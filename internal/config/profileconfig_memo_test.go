package config

import (
	"os"
	"testing"
	"time"
)

// THE LAW: the profile's config.json is parsed at most once per change.
//
// [APIKeyConfigured] runs on every Enter a person presses and [FirstPrompt] on
// every turn, and both of them read this file. The proof is behavioural rather
// than a counter: the file is rewritten UNDERNEATH the memo with its size and
// its timestamp put back, so nothing about the file says it moved — and a
// reader that answers with the old value is a reader that did not read.
func TestTheProfileConfigIsParsedOncePerChange(t *testing.T) {
	profile := t.TempDir()
	profileConfigMemo.Forget()
	t.Cleanup(profileConfigMemo.Forget)

	if err := WriteChatModel(profile, "first/model"); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelAt(profile); got != "first/model" {
		t.Fatalf("the profile reads %q, want the model just written", got)
	}
	if FirstPrompt(profile) {
		t.Fatal("a profile with a chat model still reads as a first prompt")
	}

	path := BudgetConfigPath(profile)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same length, same timestamp: a file that has changed and cannot say so.
	swapped := replaceKeepingLength(raw, "first/model", "OTHER/model")
	if err := os.WriteFile(path, swapped, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelAt(profile); got != "first/model" {
		t.Fatalf("the profile was re-read for an unchanged file: it now says %q", got)
	}

	// A REAL WRITE IS SEEN AT ONCE, whatever the timestamp says, because every
	// persisted write bumps [SettingsGeneration] and the memo reads it.
	if err := WriteChatModel(profile, "second/model"); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelAt(profile); got != "second/model" {
		t.Fatalf("after a write the profile still says %q", got)
	}
}

// AND A FILE THAT REALLY CHANGED IS READ AGAIN: the memo is keyed on the file as
// well as on this process's writes, so another process editing it by hand is
// picked up the moment its timestamp moves.
func TestAConfigChangedOnDiskIsReadAgain(t *testing.T) {
	profile := t.TempDir()
	profileConfigMemo.Forget()
	t.Cleanup(profileConfigMemo.Forget)

	if err := WriteChatModel(profile, "first/model"); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelAt(profile); got != "first/model" {
		t.Fatalf("the profile reads %q", got)
	}
	path := BudgetConfigPath(profile)
	if err := os.WriteFile(path, []byte(`{"model.talk":"hand/edited"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if got := ChatModelAt(profile); got != "hand/edited" {
		t.Fatalf("a config edited on disk still reads as %q", got)
	}
}

// THE WRITER OWNS ITS OWN MAP. A write reads the file through the same memo, and
// adding its row to the map the memo is holding would rewrite what every other
// reader is about to be told.
func TestAWriteDoesNotRewriteWhatTheMemoIsHolding(t *testing.T) {
	profile := t.TempDir()
	profileConfigMemo.Forget()
	t.Cleanup(profileConfigMemo.Forget)

	if err := WriteChatModel(profile, "first/model"); err != nil {
		t.Fatal(err)
	}
	held, err := readProfileConfig(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := held[KeyDailyBudget]; present {
		t.Fatal("the fixture already carries a budget row")
	}
	if err := WriteDailyBudgetUSD(profile, 12); err != nil {
		t.Fatal(err)
	}
	if _, present := held[KeyDailyBudget]; present {
		t.Fatal("a write added its row to the map a reader was already holding")
	}
}

// replaceKeepingLength swaps one run of bytes for another of the same length, so
// the file's size cannot betray the change.
func replaceKeepingLength(raw []byte, from, to string) []byte {
	if len(from) != len(to) {
		panic("the replacement must be the same length as what it replaces")
	}
	out := append([]byte(nil), raw...)
	for index := 0; index+len(from) <= len(out); index++ {
		if string(out[index:index+len(from)]) == from {
			copy(out[index:], to)
			return out
		}
	}
	panic("the fixture does not contain " + from)
}
