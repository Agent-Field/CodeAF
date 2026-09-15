package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPrunePrintsExpiredChannelTagsOldestFirst(t *testing.T) {
	var input strings.Builder
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < channelRetention+3; i++ {
		fmt.Fprintf(&input, "dev-20260101-%012x\t%s\n", i, start.Add(time.Duration(i)*time.Hour).Format(time.RFC3339))
	}
	// Another channel never consumes this channel's allowance.
	fmt.Fprintf(&input, "staging-20260101-%012x\t%s\n", 1, start.Add(-time.Hour).Format(time.RFC3339))

	code, stdout, stderr := invoke([]string{"prune", "--channel", "dev"}, input.String())
	want := "dev-20260101-000000000000\ndev-20260101-000000000001\ndev-20260101-000000000002\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestPruneBreaksCreationTiesByTag(t *testing.T) {
	stamp := "2026-01-01T00:00:00Z"
	var input strings.Builder
	for i := channelRetention; i >= 0; i-- {
		fmt.Fprintf(&input, "staging-20260101-%012x\t%s\n", i, stamp)
	}
	code, stdout, stderr := invoke([]string{"prune", "--channel", "staging"}, input.String())
	if code != 0 || stdout != "staging-20260101-000000000000\n" || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestPrunePrintsNothingWithinRetention(t *testing.T) {
	code, stdout, stderr := invoke([]string{"prune", "--channel", "dev"}, "v1.0.0\t2026-01-01T00:00:00Z\n")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestPruneRefusesMalformedInput(t *testing.T) {
	code, stdout, stderr := invoke([]string{"prune", "--channel", "dev"}, "dev-20260101-000000000000 not-a-date\n")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "line 1") {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}
