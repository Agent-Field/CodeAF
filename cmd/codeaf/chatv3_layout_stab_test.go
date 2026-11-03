package main

import (
	"reflect"
	"testing"
	"time"
)

// E-M3-a: a spoken transcript proves the folder must stay without consulting
// the workstream owner, even when its last-spoken metadata is absent.
func TestStabEM3SpokenSessionsSkipWorkstreamOwnership(t *testing.T) {
	dir := writeV3Session(t, t.TempDir(), "spoken", "Keep this conversation", time.Time{})
	previous := v3WorkstreamKeeps
	t.Cleanup(func() { v3WorkstreamKeeps = previous })
	lookups := 0
	v3WorkstreamKeeps = func(string) bool {
		lookups++
		return false
	}
	if v3EmptyUnassignedSession(dir) {
		t.Fatal("a spoken conversation was considered empty")
	}
	if lookups != 0 {
		t.Fatalf("spoken conversation made %d ownership lookups, want 0", lookups)
	}
}

// E-M3-b: only a provably empty folder needs an ownership lookup, and a kept
// answer must prevent its reuse or removal.
func TestStabEM3EmptySessionsCheckWorkstreamOwnershipOnce(t *testing.T) {
	dir := writeV3Session(t, t.TempDir(), "empty", "", time.Time{})
	for _, tc := range []struct {
		name  string
		kept  bool
		empty bool
	}{
		{"kept", true, false},
		{"unowned", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := v3WorkstreamKeeps
			t.Cleanup(func() { v3WorkstreamKeeps = previous })
			lookups := 0
			v3WorkstreamKeeps = func(got string) bool {
				lookups++
				if got != dir {
					t.Errorf("ownership lookup for %q, want %q", got, dir)
				}
				return tc.kept
			}
			if empty := v3EmptyUnassignedSession(dir); empty != tc.empty {
				t.Fatalf("empty = %v, want %v when kept = %v", empty, tc.empty, tc.kept)
			}
			if lookups != 1 {
				t.Fatalf("empty conversation made %d ownership lookups, want 1", lookups)
			}
		})
	}
}

// E-M3-c: scanning several spoken folders and one empty folder asks about
// workstream ownership only for the empty candidate.
func TestStabEM3BucketScanOnlyLooksUpEmptyCandidates(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	bucket := t.TempDir()
	for _, id := range []string{"00000000000000a1", "00000000000000a2", "00000000000000a3"} {
		writeV3Session(t, bucket, id, "Keep this conversation", time.Time{})
	}
	dir := writeV3Session(t, bucket, "00000000000000a4", "", time.Time{})
	previous := v3WorkstreamKeeps
	t.Cleanup(func() { v3WorkstreamKeeps = previous })
	var lookups []string
	v3WorkstreamKeeps = func(dir string) bool {
		lookups = append(lookups, dir)
		return false
	}
	spoken, empty := v3ScanBucket(bucket)
	if len(spoken) != 3 || len(empty) != 1 || empty[0].dir != dir {
		t.Fatalf("bucket scan returned spoken %v, empty %v", spoken, empty)
	}
	if !reflect.DeepEqual(lookups, []string{dir}) {
		t.Fatalf("ownership lookups = %v, want only %q", lookups, dir)
	}
}
