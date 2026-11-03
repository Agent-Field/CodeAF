package config

import (
	"bytes"
	"os"
	"strings"
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

func TestUnreadableProfileRefusesSpendingAndSettingsWrites(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000 files")
	}
	t.Setenv(dailyBudgetEnv, "")
	profile := t.TempDir()
	path := BudgetConfigPath(profile)
	original := []byte(`{"model.talk":"keep/model","daily_budget_usd":1}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	if limit, err := DailyBudgetUSDAt(profile); err == nil {
		t.Errorf("unreadable saved spending authority returned %v without an error", limit)
	}
	if err := WriteChatModel(profile, "new/model"); err == nil || !strings.Contains(err.Error(), "preserve existing file") {
		t.Errorf("settings write must refuse to replace unreadable config: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("settings write changed unreadable config: %s", got)
	}
}

func TestUnreadableProfileAuthoritiesDoNotUseOrdinaryFallback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000 files")
	}
	t.Setenv(dailyBudgetEnv, "")
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			profile := t.TempDir()
			path := BudgetConfigPath(profile)
			original := []byte(`{"model.talk":"keep/model","daily_budget_usd":1,"tool_approval_mode":"prompt"}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if warm {
				if got, err := DailyBudgetBaseUSDAt(profile); err != nil || got != 1 {
					t.Fatalf("warm budget: %v, %v", got, err)
				}
			}
			if err := os.Chmod(path, 0000); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(path, 0600)
			if _, err := DailyBudgetBaseUSDAt(profile); err == nil {
				t.Error("base budget hid read failure")
			}
			if _, err := DailyBudgetAuthorityEstablished(profile); err == nil {
				t.Error("budget authority hid read failure")
			}
			if _, err := ReadPreferences(profile); err == nil {
				t.Error("sync preferences hid read failure")
			}
			if _, err := ReadProviderKeys(profile); err == nil {
				t.Error("sync provider keys hid read failure")
			}
			if got := ToolApprovalModeAt(profile); got != "prompt" {
				t.Errorf("tool policy = %q; want prompt", got)
			}
			if _, err := HeadlessToolApprovalModeAt(t.TempDir(), profile); err == nil {
				t.Error("headless tool policy hid read failure")
			}
			if err := os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			if got, err := DailyBudgetUSDAt(profile); err != nil || got != 1 {
				t.Fatalf("restored budget: %v, %v", got, err)
			}
		})
	}
}
