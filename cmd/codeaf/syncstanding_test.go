package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// countingDir answers every listing as a relay that has stopped this device,
// and counts how often it was asked.
type countingDir struct {
	directory.Client
	asked int
}

func (c *countingDir) List(context.Context) (directory.Listing, error) {
	c.asked++
	return directory.Listing{}, wireauth.ErrRevoked
}

func TestCheckStandingAsksOnlyAPairedHome(t *testing.T) {
	cases := []struct {
		name   string
		paired bool
		asked  int
		said   int
	}{{"fresh home sends nothing", false, 0, 0}, {"paired home asks once and says so", true, 1, 1}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.paired {
				if err := identity.MarkPaired(home); err != nil {
					t.Fatal(err)
				}
			}
			dir, lines := &countingDir{}, []string{}
			checkStanding(home, dir, func(l string) { lines = append(lines, l) })
			if dir.asked != tc.asked || len(lines) != tc.said {
				t.Errorf("asked %d said %v, want %d and %d lines", dir.asked, lines, tc.asked, tc.said)
			}
		})
	}
}
