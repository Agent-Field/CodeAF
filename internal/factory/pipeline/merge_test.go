package pipeline_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// AUTO-MERGE IS OFF unless a file turns it on: the defaults, an empty file and
// a file that never names merge all merge only when a person presses Merge.
func TestAutoMergeIsOffUnlessTheFileTurnsItOn(t *testing.T) {
	if got := pipeline.Defaults().Merge; got != (factory.MergePolicy{}) {
		t.Fatalf("defaults merge = %+v", got)
	}
	for _, text := range []string{"", "default_budget: 4\n", "merge:\n  method: squash\n"} {
		f, err := pipeline.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		if f.Merge.Auto || f.Recipe().Merge.Auto {
			t.Fatalf("%q turned auto-merge on: %+v", text, f.Merge)
		}
	}
}

func TestMergeKeyReadsAutoAndMethod(t *testing.T) {
	for _, tc := range []struct {
		text string
		want factory.MergePolicy
	}{
		{"merge:\n  auto: on\n", factory.MergePolicy{Auto: true}},
		{"merge:\n  auto: true\n  method: Squash\n", factory.MergePolicy{Auto: true, Method: factory.MergeMethodSquash}},
		{"merge:\n  auto: off\n  method: rebase\n", factory.MergePolicy{Method: factory.MergeMethodRebase}},
	} {
		f, err := pipeline.Parse(tc.text)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Problems) != 0 {
			t.Fatalf("%q problems = %v", tc.text, f.Problems)
		}
		if f.Merge != tc.want || f.Recipe().Merge != tc.want {
			t.Fatalf("%q merge = %+v recipe %+v, want %+v", tc.text, f.Merge, f.Recipe().Merge, tc.want)
		}
	}
}

// A word the reader cannot read is a problem, and auto-merge stays off.
func TestMergeKeyRefusesWordsItCannotRead(t *testing.T) {
	f, err := pipeline.Parse("merge:\n  auto: sometimes\n  method: octopus\n  when: friday\n")
	if err != nil {
		t.Fatal(err)
	}
	if f.Merge.Auto || f.Merge.Method != "" {
		t.Fatalf("merge = %+v", f.Merge)
	}
	var whys []string
	for _, p := range f.Problems {
		whys = append(whys, p.Where+": "+p.Why)
	}
	got := strings.Join(whys, "\n")
	for _, want := range []string{"merge.auto: auto-merge is on or off", "merge.method: method is merge, squash or rebase", "merge.when: not a merge key"} {
		if !strings.Contains(got, want) {
			t.Fatalf("problems %q lack %q", got, want)
		}
	}
}

// A PERSONAL FILE MAY ONLY TURN AUTO-MERGE OFF. Turning it on, or changing the
// repository's method, is ignored and named.
func TestPersonalFileMayOnlyTurnAutoMergeOff(t *testing.T) {
	on, _ := pipeline.Parse("merge:\n  auto: on\n  method: squash\n")
	off, _ := pipeline.Parse("")

	personalOff, err := pipeline.ParsePersonal("merge:\n  auto: off\n")
	if err != nil {
		t.Fatal(err)
	}
	got, probs := pipeline.Merge(on, personalOff)
	if got.Merge.Auto || got.Merge.Method != factory.MergeMethodSquash || len(probs) != 0 {
		t.Fatalf("off over on: merge %+v problems %v", got.Merge, probs)
	}

	personalOn, err := pipeline.ParsePersonal("merge:\n  auto: on\n  method: rebase\n")
	if err != nil {
		t.Fatal(err)
	}
	got, probs = pipeline.Merge(off, personalOn)
	if got.Merge.Auto || got.Merge.Method != "" {
		t.Fatalf("on over off: merge %+v", got.Merge)
	}
	text := ""
	for _, p := range probs {
		text += p.Where + ": " + p.Why + "\n"
	}
	for _, want := range []string{"merge.auto: ignored; a personal file cannot turn auto-merge on", "merge.method: ignored; a personal file cannot change how the repository merges"} {
		if !strings.Contains(text, want) {
			t.Fatalf("problems %q lack %q", text, want)
		}
	}

	// A personal file that says nothing about merging changes nothing.
	silent, _ := pipeline.ParsePersonal("rules:\n  - tests pass before anything posts\n")
	if got, _ := pipeline.Merge(on, silent); got.Merge != on.Merge {
		t.Fatalf("silent personal file changed merge to %+v", got.Merge)
	}
}

// Writing a file keeps what it says about merging, and a file that said
// nothing is written without the key.
func TestEncodeKeepsMergeOnlyWhereTheFileSaidIt(t *testing.T) {
	for _, tc := range []struct {
		text, want, absent string
	}{
		{"merge:\n  auto: on\n  method: squash\n", "merge:\n    auto: \"on\"\n    method: squash\n", ""},
		{"merge:\n  auto: off\n", "merge:\n    auto: \"off\"\n", ""},
		{"default_budget: 4\n", "", "merge"},
	} {
		f, err := pipeline.Parse(tc.text)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pipeline.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want != "" && !strings.Contains(string(out), tc.want) {
			t.Fatalf("%q encoded as\n%s\nwant %q", tc.text, out, tc.want)
		}
		if tc.absent != "" && strings.Contains(string(out), tc.absent) {
			t.Fatalf("%q encoded with %q:\n%s", tc.text, tc.absent, out)
		}
		again, err := pipeline.Parse(string(out))
		if err != nil {
			t.Fatal(err)
		}
		if again.Merge != f.Merge {
			t.Fatalf("round trip %q: %+v != %+v", tc.text, again.Merge, f.Merge)
		}
	}
}
