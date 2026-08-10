package main

import (
	"reflect"
	"testing"
)

// The switch is the only thing standing between the two surfaces, so it is
// tested for both directions: the old path must be unreachable by accident,
// and the new one must be unreachable without asking.
func TestWantChatV2(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(key string) string { return pairs[key] }
	}
	none := env(nil)
	set := env(map[string]string{chatV2Env: "1"})
	off := env(map[string]string{chatV2Env: "0"})

	cases := []struct {
		name string
		args []string
		get  func(string) string
		want bool
		rest []string
	}{
		{name: "plain chat stays on the old surface", args: []string{"--session", "new"}, get: none,
			want: false, rest: []string{"--session", "new"}},
		{name: "--v2 opens the new one", args: []string{"--v2"}, get: none, want: true, rest: []string{}},
		{name: "-v2 too", args: []string{"-v2", "--db", "x"}, get: none, want: true, rest: []string{"--db", "x"}},
		{name: "the environment opens it", args: nil, get: set, want: true, rest: []string{}},
		{name: "an explicit false closes it again", args: []string{"--v2=false"}, get: set,
			want: false, rest: []string{}},
		{name: "a false environment is not a request", args: nil, get: off, want: false, rest: []string{}},
		{name: "the flag outranks a quiet environment", args: []string{"--v2=1"}, get: off,
			want: true, rest: []string{}},
		{name: "a flag that merely starts with v2 is untouched", args: []string{"--v2x"}, get: none,
			want: false, rest: []string{"--v2x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rest := wantChatV2(tc.args, tc.get)
			if got != tc.want {
				t.Fatalf("wantChatV2 = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(rest, tc.rest) {
				t.Fatalf("remaining args = %#v, want %#v", rest, tc.rest)
			}
		})
	}
}

func TestTruthyEnv(t *testing.T) {
	for _, value := range []string{"", "0", "false", "no", "off", " OFF "} {
		if truthyEnv(value) {
			t.Fatalf("%q should not open the surface", value)
		}
	}
	for _, value := range []string{"1", "true", "yes", "on", "anything"} {
		if !truthyEnv(value) {
			t.Fatalf("%q should open the surface", value)
		}
	}
}
