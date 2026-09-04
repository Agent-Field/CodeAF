package config

import "testing"

// The key this file writes is the settings row's own key, and the two are
// spelled in two places — a constant here, a derivation there. One of them
// drifting is how a value gets written under a name nothing reads back.
func TestChatModelKeyIsTheRow(t *testing.T) {
	if got := ModelSettingKey("talk"); got != KeyChatModel {
		t.Fatalf("the talk row's key is %q and this file writes %q", got, KeyChatModel)
	}
}

// The round trip: nobody has chosen, somebody chooses, the choice comes back.
func TestTheChatModelSurvivesASaveAndALoad(t *testing.T) {
	dir := t.TempDir()

	if got := ChatModelAt(dir); got != "" {
		t.Fatalf("an untouched profile answered %q, want nothing at all", got)
	}
	if err := WriteChatModel(dir, "  vendor/chosen  "); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := ChatModelAt(dir); got != "vendor/chosen" {
		t.Fatalf("the profile answered %q, want the model that was chosen", got)
	}

	// A second choice replaces the first rather than accumulating.
	if err := WriteChatModel(dir, "vendor/second"); err != nil {
		t.Fatalf("write again: %v", err)
	}
	if got := ChatModelAt(dir); got != "vendor/second" {
		t.Fatalf("the profile answered %q after a second choice", got)
	}
}

// The write goes through the one writer, so the rows around it survive. A
// preference that quietly emptied somebody's approval rules would be worse than
// a preference that did not persist at all.
func TestWritingTheChatModelLeavesTheOtherRowsAlone(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValue(dir, KeyToolApprovals, "bash:deny"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteChatModel(dir, "vendor/chosen"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, ok := persistedString(dir, KeyToolApprovals); !ok || got != "bash:deny" {
		t.Fatalf("the approvals row reads %q (ok=%v) after a model was chosen", got, ok)
	}
}

// A fresh profile has never chosen a talk model, so its first prompt is still
// on the shipped default. Choosing one is what ends that — not finishing setup,
// not pasting a key.
func TestAFreshProfileIsAFirstPromptUntilAModelIsChosen(t *testing.T) {
	dir := t.TempDir()
	if !FirstPrompt(dir) {
		t.Fatal("a profile nobody has touched is still on its first prompt")
	}
	if ChatModelAt(dir) != "" {
		t.Fatal("a first prompt must not invent a saved talk model")
	}
	if DefaultModel == "" {
		t.Fatal("the shipped default talk model is empty, so a first prompt has nowhere to open")
	}
	if err := WriteChatModel(dir, "vendor/chosen"); err != nil {
		t.Fatal(err)
	}
	if FirstPrompt(dir) {
		t.Fatal("a profile that chose a model is no longer a first prompt")
	}
}
