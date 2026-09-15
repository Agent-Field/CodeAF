package main

import (
	"bytes"
	"strings"
	"testing"
)

func invoke(args []string, input string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

type nextSemverTest struct {
	name string
	tags string
	args []string
	want string
}

var nextSemverTests = []nextSemverTest{
	{"rc patch", "v0.1.0\n", []string{"next", "--channel", "rc"}, "v0.1.1-rc.1\n"},
	{"rc minor", "v0.1.0\n", []string{"next", "--channel", "rc", "--component", "minor"}, "v0.2.0-rc.1\n"},
	{"rc major", "v0.1.0\n", []string{"next", "--channel", "rc", "--component", "major"}, "v1.0.0-rc.1\n"},
	{"open line continues", "v0.1.1-rc.2\nv0.1.0\nv0.1.1-rc.1\n", []string{"next", "--channel", "rc", "--component", "major"}, "v0.1.1-rc.3\n"},
	{"highest open line", "v0.2.0-rc.1\nv0.1.0\n", []string{"next", "--channel", "rc"}, "v0.2.0-rc.2\n"},
	{"stable closes patch line", "v0.1.1-rc.2\nv0.1.0\n", []string{"next", "--channel", "stable"}, "v0.1.1\n"},
	{"stable minor starts fresh", "v0.1.1-rc.2\nv0.1.0\n", []string{"next", "--channel", "stable", "--component", "minor"}, "v0.2.0\n"},
	{"stable closes higher line", "v0.2.0-rc.3\nv0.1.0\n", []string{"next", "--channel", "stable"}, "v0.2.0\n"},
	{"no stable rc", "junk\n", []string{"next", "--channel", "rc"}, "v0.0.1-rc.1\n"},
	{"no stable stable", "\n", []string{"next", "--channel", "stable"}, "v0.0.1\n"},
	{"numeric stable", "v0.1.9\nv0.1.10\n", []string{"next", "--channel", "stable"}, "v0.1.11\n"},
	{"numeric rc", "v0.1.11-rc.9\nv0.1.11-rc.10\n", []string{"next", "--channel", "rc"}, "v0.1.11-rc.11\n"},
	{"closed rc ignored", "v0.2.0-rc.5\nv0.2.0\n", []string{"next", "--channel", "rc"}, "v0.2.1-rc.1\n"},
	{"junk ignored", "build-abc\nsalvage/x\ncheckpoint/no\nv1.2\nV1.2.3\n", []string{"next", "--channel", "stable"}, "v0.0.1\n"},
	{"oversized rc counter ignored", "v0.1.0\nv0.1.1-rc.999999999999999999999999999999999999\n", []string{"next", "--channel", "rc"}, "v0.1.1-rc.1\n"},
	{"unbumpable stable component ignored", "v0.0.9223372036854775807\n", []string{"next", "--channel", "stable"}, "v0.0.1\n"},
}

func TestNextSemverTags(t *testing.T) {
	for _, test := range nextSemverTests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := invoke(test.args, test.tags)
			if code != 0 || stdout != test.want || stderr != "" {
				t.Fatalf("code %d stdout %q stderr %q, want 0 %q empty", code, stdout, stderr, test.want)
			}
		})
	}
}

func TestNextSemverAnswersAreNotExistingTags(t *testing.T) {
	for _, test := range nextSemverTests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := invoke(test.args, test.tags)
			if code != 0 || stderr != "" {
				t.Fatalf("code %d stderr %q", code, stderr)
			}
			answer := strings.TrimSpace(stdout)
			for _, tag := range strings.Fields(test.tags) {
				if answer == tag {
					t.Fatalf("printed existing tag %q", answer)
				}
			}
		})
	}
}

func TestNextRefusesAnExistingAnswer(t *testing.T) {
	err := requireUnused("v0.1.1", []string{"v0.1.0", "v0.1.1"})
	if err == nil || !strings.Contains(err.Error(), "v0.1.1") {
		t.Fatalf("collision did not name its tag: %v", err)
	}
}

func TestChannelTags(t *testing.T) {
	for _, channel := range []string{"dev", "staging"} {
		code, stdout, stderr := invoke([]string{"next", "--channel", channel, "--sha", "8E67E05FB1C21234567890", "--date", "20260910"}, "reader must not matter")
		want := channel + "-20260910-8e67e05fb1c2\n"
		if code != 0 || stdout != want || stderr != "" {
			t.Fatalf("%s: code %d stdout %q stderr %q", channel, code, stdout, stderr)
		}
	}
}

func TestKind(t *testing.T) {
	tests := map[string]string{
		"v1.2.3": "stable", "v1.2.3-rc.10": "rc",
		"dev-20260910-8e67e05fb1c2": "dev", "staging-20260910-8e67e05fb1c2": "staging",
	}
	for tag, want := range tests {
		code, stdout, stderr := invoke([]string{"kind", tag}, "")
		if code != 0 || stdout != want+"\n" || stderr != "" {
			t.Fatalf("%s: code %d stdout %q stderr %q", tag, code, stdout, stderr)
		}
	}
	code, stdout, _ := invoke([]string{"kind", "build-old"}, "")
	if code != 1 || stdout != "other\n" {
		t.Fatalf("other: code %d stdout %q", code, stdout)
	}
}

func TestUsageErrorsUseExitTwoAndKeepStdoutEmpty(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"next", "--channel", "dev"},
		{"next", "--channel", "staging", "--sha", "abcdefabcdef"},
		{"next", "--channel", "stable", "--component", "nope"},
		{"kind"},
		{"prune", "--keep", "2"},
	} {
		code, stdout, stderr := invoke(args, "")
		if code != 2 || stdout != "" || !strings.Contains(stderr, "codeaf-release") || len(strings.Split(stderr, "\n")) > 18 {
			t.Fatalf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}
