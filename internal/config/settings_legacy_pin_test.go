package config

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
)

func TestSettingsPinNamesTheSpellingThatHoldsIt(t *testing.T) {
	for _, tc := range []struct{ current, former, want string }{
		{"", "off", env.Legacy("CODEAF_TELEMETRY")},
		{"on", "off", "CODEAF_TELEMETRY"},
		{"", "", ""},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Setenv("CODEAF_TELEMETRY", tc.current)
			if tc.current == "" {
				if err := os.Unsetenv("CODEAF_TELEMETRY"); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(env.Legacy("CODEAF_TELEMETRY"), tc.former)
			row := Setting{Env: "CODEAF_TELEMETRY"}
			if got, pinned := row.PinnedBy(); got != tc.want || pinned != (tc.want != "") {
				t.Fatalf("pin = %q, %v; want %q", got, pinned, tc.want)
			}
		})
	}
}
