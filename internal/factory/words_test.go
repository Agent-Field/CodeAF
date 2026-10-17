package factory

import (
	"strings"
	"testing"
)

func TestGuessTypeAndLabelType(t *testing.T) {
	titles := []struct{ title, label, want string }{
		{"Total double-counts an entry added twice", "bug", "bug"},
		{"Add a CSV export to the ledger command", "feat", "feat"},
		{"Flaky TestTotal on an empty ledger", "bug", "bug"},
		{"Should Add refuse a negative amount?", "question", "question"},
		{"Rename Total to Sum across the package", "chore", "chore"},
		{"Ledger loses entries after 1000 adds", "bug", "bug"},
	}
	for _, c := range titles {
		if got := GuessType(c.title); got != c.want {
			t.Errorf("GuessType(%q) = %q, want %q", c.title, got, c.want)
		}
		if got := LabelType(c.label); got != c.want {
			t.Errorf("LabelType(%q) = %q, want %q", c.label, got, c.want)
		}
	}
	for label, want := range map[string]string{"enhancement": "feat", "Feature": "feat", "maintenance": "chore", "refactor": "chore", "docs": "chore", "documentation": "chore", "help wanted": ""} {
		if got := LabelType(label); got != want {
			t.Errorf("LabelType(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestStageSentence(t *testing.T) {
	recipe := func() []Stage {
		var out []Stage
		for _, n := range []string{"plan", "write", "test", "review", "security", "proof"} {
			out = append(out, Stage{Name: n})
		}
		return out
	}
	names := func(ss []Stage) string {
		s := ""
		for _, x := range ss {
			s += x.Name + ","
		}
		return s
	}
	cases := []struct{ words, name, ask, order string }{
		{"after review, make it neater", "make", "make it neater", "plan,write,test,review,make,security,proof,"},
		{"run the docs check again", "docs", "run the docs check again", "plan,write,test,review,security,docs,proof,"},
		{"check the docs build", "check", "check the docs build", "plan,write,test,review,security,check,proof,"},
		{"before test, then lint it", "lint", "then lint it", "plan,write,lint,test,review,security,proof,"},
		{"first, please read the history", "read", "please read the history", "read,plan,write,test,review,security,proof,"},
		{"last, tidy up", "tidy", "tidy up", "plan,write,test,review,security,tidy,proof,"},
		{"after review, arch: read it for the architecture", "arch", "read it for the architecture", "plan,write,test,review,arch,security,proof,"},
		{"Neaten: make the code neater", "neaten", "make the code neater", "plan,write,test,review,security,neaten,proof,"},
	}
	for _, c := range cases {
		st := ParseStage(c.words)
		if st.Name != c.name || st.Ask != c.ask {
			t.Errorf("ParseStage(%q) = %q / %q, want %q / %q", c.words, st.Name, st.Ask, c.name, c.ask)
		}
		if got := names(AddStageWords(recipe(), c.words)); got != c.order {
			t.Errorf("AddStageWords(%q) = %s, want %s", c.words, got, c.order)
		}
	}
	// A recipe whose last stage is not proof takes `last` at the very end.
	noProof := recipe()[:5]
	if got := names(AddStageWords(noProof, "last, tidy up")); got != "plan,write,test,review,security,tidy," {
		t.Errorf("last without proof = %s", got)
	}
}

// A STAGE IS ONE WORD, and the sentence that says so names the words.
func TestStageWord(t *testing.T) {
	for in, want := range map[string]string{"review": "review", " Arch ": "arch", "e2e": "e2e", "security": "security", "abcdefghijkl": "abcdefghijkl"} {
		if got, err := StageWord(in); err != nil || got != want {
			t.Errorf("StageWord(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	refused := map[string]string{
		"do through":    `a stage is one word · "do through" is two`,
		"read it all":   `a stage is one word · "read it all" is three`,
		"re-view":       `a stage is one word of letters and digits · "re-view" is not`,
		"x":             `a stage is one word of 2 to 12 letters · "x" is not`,
		"abcdefghijklm": `a stage is one word of 2 to 12 letters · "abcdefghijklm" is not`,
		"":              "a stage needs a name",
	}
	for in, want := range refused {
		if _, err := StageWord(in); err == nil || err.Error() != want {
			t.Errorf("StageWord(%q) = %v; want %q", in, err, want)
		}
	}
}

// THE TYPED SENTENCE names its stage with one word and a colon, or by its
// ask's first word; a two-word name is refused in the one-word sentence, and
// the old door ([ParseStage]) reads it as no stage at all.
func TestStageSentenceNamesOneWord(t *testing.T) {
	st, when, err := StageSentence("after review, arch: read it for the architecture")
	if err != nil || st.Name != "arch" || st.Ask != "read it for the architecture" || when != "after review" {
		t.Fatalf("named = %+v %q %v", st, when, err)
	}
	if _, _, err := StageSentence("after review, do through: walk it end to end"); err == nil || err.Error() != `a stage is one word · "do through" is two` {
		t.Fatalf("two-word name = %v", err)
	}
	if ParseStage("after review, do through: walk it end to end").Ask != "" {
		t.Fatal("ParseStage kept a two-word name")
	}
	// A colon inside a longer clause, or one a space does not follow, is the ask's own.
	for _, words := range []string{"check the build: it must pass", "fetch https://example.com/notes"} {
		st, _, err := StageSentence(words)
		if err != nil || st.Ask != words {
			t.Fatalf("%q = %+v %v", words, st, err)
		}
		if _, err := StageWord(st.Name); err != nil {
			t.Fatalf("%q named %q: %v", words, st.Name, err)
		}
	}
	if _, _, err := StageSentence("after review,"); err == nil || err.Error() != "say what the stage should do" {
		t.Fatalf("no ask = %v", err)
	}
	if _, _, err := StageSentence("long: " + strings.Repeat("a", 241)); err == nil || err.Error() != "an ask is at most 240 cells" {
		t.Fatalf("long ask = %v", err)
	}
}

// AT MOST NINE STAGES, and no name twice, on the person's own road.
func TestAddStageSentenceHoldsTheBounds(t *testing.T) {
	var nine []Stage
	for _, n := range []string{"plan", "write", "test", "review", "neaten", "security", "docs", "sign", "proof"} {
		nine = append(nine, Stage{Name: n, On: true})
	}
	if _, err := AddStageSentence(nine, "after review, arch: read it"); err == nil || err.Error() != "the run has nine stages already" {
		t.Fatalf("tenth stage = %v", err)
	}
	got, err := AddStageSentence(nine[:8], "after review, arch: read it")
	if err != nil || len(got) != 9 || got[4].Name != "arch" {
		t.Fatalf("ninth stage = %+v %v", got, err)
	}
	if _, err := AddStageSentence(nine[:8], "review: read it again"); err == nil || !strings.Contains(err.Error(), "already a stage named review") {
		t.Fatalf("a name twice = %v", err)
	}
	if _, err := AddStageSentence(nine[:8], "do through: walk it"); err == nil || err.Error() != `a stage is one word · "do through" is two` {
		t.Fatalf("two words = %v", err)
	}
}
