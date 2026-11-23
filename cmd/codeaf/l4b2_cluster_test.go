package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// L4-M14e: An older service's unnamed members are unknown computers, while an evidenced removal is named honestly.
func TestL4B2ClusterWithoutLabels(t *testing.T) {
	s := clusterctl.Status{Self: 1, Leader: 2, Configuration: cs.Configuration{Voters: []uint64{1, 2, 3}, Learners: []uint64{4, 5}}}
	for _, labels := range []bool{false, true} {
		names := map[uint64]string{1: "laptop-a"}
		if labels {
			s.Members = []cs.MemberLabel{{Node: 2, Device: "gone"}}
			names[2] = ""
		}
		var out bytes.Buffer
		if err := printClusterStatus(&out, s, names); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if labels {
			if !strings.Contains(text, "coordinating    a computer no longer in your devices") {
				t.Fatal(text)
			}
		} else {
			if strings.Contains(text, "no longer") || !strings.Contains(text, "coordinating    another computer") || !strings.Contains(text, "2 other computers") {
				t.Fatal(text)
			}
		}
		if strings.Contains(text, "[2 3]") || strings.Contains(text, "computer 2") {
			t.Fatal(text)
		}
	}
	// A chosen device name that matches the fallback is still a known name.
	s.Members = nil
	var out bytes.Buffer
	if err := printClusterStatus(&out, s, map[uint64]string{1: "laptop-a", 2: "another computer"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "deciding        laptop-a · 2 other computers") {
		t.Fatal("a chosen name was counted as unknown")
	}

}
