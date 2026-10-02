package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/lease"
)

func TestResidentHostReadsLikeEveryOtherComputerLabel(t *testing.T) {
	cases := map[string]string{"studio.local": "studio", " desk ": "desk", "": "unknown host"}
	for host, want := range cases {
		if got := residentHost(&lease.Resident{Host: host}); got != want {
			t.Errorf("residentHost(%q) = %q; want %q", host, got, want)
		}
	}
}
