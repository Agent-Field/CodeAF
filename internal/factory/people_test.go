package factory

import (
	"reflect"
	"testing"
)

func TestCodeOwnersLastMatchDirectoriesAndUnsupportedOwners(t *testing.T) {
	rules := ParseCodeOwners("* @all\n/docs/ @writer\n*.go @go-owner\n/internal/**/auth.go @security @mei\n/internal/generated/\n!ignored @bad\n[abc] @bad\n/team/ @org/team\n")
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"README.md", []string{"all"}}, {"docs/a.md", []string{"writer"}}, {"nested/docs/a.md", []string{"all"}},
		{"docs/a.go", []string{"go-owner"}}, {"internal/auth.go", []string{"mei", "security"}},
		{"internal/a/b/auth.go", []string{"mei", "security"}}, {"internal/generated/a.go", []string{}}, {"team/a.go", []string{}},
	} {
		if got := OwnersFor(rules, []string{tc.path}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.path, got, tc.want)
		}
	}
}
