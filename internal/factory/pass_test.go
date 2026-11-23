package factory

import "testing"

func TestPassWordReadsTheThreeSpellingsAndNothingElse(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "person": PassPerson, " Only a person ": PassPerson, "MANAGER": PassManager,
		"a person or the manager":                PassManager,
		"manager when  the plan names\tone file": "manager when the plan names one file",
	} {
		if got, ok := PassWord(in); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"anyone", "manager when", "auto", "manager when " + string(make([]rune, PassWhenMost+1))} {
		if _, ok := PassWord(bad); ok {
			t.Errorf("%q read", bad)
		}
	}
}

func TestPassOfNeverWidensWhoMayPass(t *testing.T) {
	cases := []struct {
		st         Stage
		kind, cond string
	}{
		{Stage{}, PassManager, ""},
		{Stage{Fixed: true, Pass: "manager"}, PassPerson, ""},
		{Stage{Pass: "person"}, PassPerson, ""},
		{Stage{Pass: "nonsense"}, PassPerson, ""},
		{Stage{Pass: "manager when it is small"}, PassManagerWhen, "it is small"},
	}
	for _, c := range cases {
		if k, cond := PassOf(c.st); k != c.kind || cond != c.cond {
			t.Errorf("%+v: %s %q", c.st, k, cond)
		}
	}
}
