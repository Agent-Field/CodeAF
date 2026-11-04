package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestIsProofStage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		until string
		want  bool
	}{
		{name: "provenance"},
		{name: "prover"},
		{name: "proved"},
		{name: "prove2x"},
		{name: "prove 2"},
		{name: ""},
		{name: "proof", want: true},
		{name: "Proof", want: true},
		{name: "prove", want: true},
		{name: "prove2", want: true},
		{name: "prove123", want: true},
		{name: " \tPrOvE2\n", want: true},
		{name: " \tPROOF\n", want: true},
		{name: "provenance", until: factory.UntilProven, want: true},
		{name: "anything", until: " proven ", want: true},
	} {
		t.Run(tc.name+"/"+tc.until, func(t *testing.T) {
			got := factory.IsProofStage(factory.Stage{Name: tc.name, Until: tc.until})
			if got != tc.want {
				t.Errorf("IsProofStage(name=%q, until=%q) = %t; want %t", tc.name, tc.until, got, tc.want)
			}
		})
	}
}
