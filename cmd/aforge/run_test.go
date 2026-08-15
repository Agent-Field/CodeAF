package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestHeadlessRailAuthorizationPaths(t *testing.T) {
	rail := store.DailyRail{Base: 20, Spend: 20, Ceiling: 20, Reached: true}
	for _, test := range []struct {
		name          string
		input         string
		interactive   bool
		preauthorized bool
		want          bool
		contains      string
	}{
		{name: "tty yes", input: "y\n", interactive: true, want: true, contains: "Continue? [y/N]"},
		{name: "tty no", input: "n\n", interactive: true, want: false, contains: "Continue? [y/N]"},
		{name: "non tty", interactive: false, want: false, contains: "stdin is not a TTY"},
		{name: "preauthorized", interactive: false, preauthorized: true, want: true, contains: "spend preauthorized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := authorizeHeadlessRail(strings.NewReader(test.input), &output,
				test.interactive, test.preauthorized, rail)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("authorized = %t, want %t", got, test.want)
			}
			if !strings.Contains(output.String(), rail.Question()) || !strings.Contains(output.String(), test.contains) {
				t.Fatalf("stderr = %q, want question and %q", output.String(), test.contains)
			}
		})
	}
}

func TestHeadlessSpendPreauthorizationFlagAndEnvironment(t *testing.T) {
	environment := func(value string) func(string) string {
		return func(name string) string {
			if name == "AFORGE_PREAUTHORIZE_SPEND" {
				return value
			}
			return ""
		}
	}
	for _, test := range []struct {
		name    string
		flagged bool
		env     string
		want    bool
	}{
		{name: "flag", flagged: true, want: true},
		{name: "environment", env: "1", want: true},
		{name: "other environment value", env: "true", want: false},
		{name: "neither", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := spendPreauthorized(test.flagged, environment(test.env)); got != test.want {
				t.Fatalf("preauthorized = %t, want %t", got, test.want)
			}
		})
	}
}
