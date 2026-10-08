package factory

import "testing"

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
		{"after review, make it neater", "make neater", "make it neater", "plan,write,test,review,make neater,security,proof,"},
		{"run the security review again", "security review", "run the security review again", "plan,write,test,review,security,security review,proof,"},
		{"check the docs build", "check docs", "check the docs build", "plan,write,test,review,security,check docs,proof,"},
		{"before test, then lint it", "lint", "then lint it", "plan,write,lint,test,review,security,proof,"},
		{"first, please read the history", "read history", "please read the history", "read history,plan,write,test,review,security,proof,"},
		{"last, tidy up", "tidy up", "tidy up", "plan,write,test,review,security,tidy up,proof,"},
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
	if got := names(AddStageWords(noProof, "last, tidy up")); got != "plan,write,test,review,security,tidy up," {
		t.Errorf("last without proof = %s", got)
	}
}
