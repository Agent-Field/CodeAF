package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EVERY PROCESS ON ONE HOME FINGERPRINTS WITH ONE SALT, made by the first to
// ask and read by the rest, kept where only its owner can read it.
func TestTheClockSaltIsMadeOnceAndShared(t *testing.T) {
	root := t.TempDir()
	first, second := clockSalt(root), clockSalt(root)
	if len(first) != 32 || !bytes.Equal(first, second) {
		t.Fatalf("two asks answered %x and %x", first, second)
	}
	info, err := os.Stat(filepath.Join(root, clockSaltName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the salt is kept as %v, %v", info, err)
	}
}

// THE FINGERPRINT FOLLOWS THE KEY AND NEVER CARRIES IT. Two shells with
// different keys are told apart; the presence file a clock reads holds no
// part of either.
func TestTheClockFingerprintFollowsTheKeyWithoutCarryingIt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "sk-or-first-secret")
	first := clockEnvironment(root)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-second-secret")
	second := clockEnvironment(root)
	if first == "" || second == "" || first == second {
		t.Fatalf("the fingerprints are %q and %q", first, second)
	}
	for _, fingerprint := range []string{first, second} {
		if strings.Contains(fingerprint, "secret") {
			t.Fatalf("the fingerprint carries the key: %q", fingerprint)
		}
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-or-first-secret")
	if again := clockEnvironment(root); again != first {
		t.Fatalf("the same key fingerprinted %q and then %q", first, again)
	}
}
