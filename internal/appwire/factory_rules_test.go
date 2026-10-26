package appwire

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// factory.rules_set is bounded before anything is sent: a connection, all
// three lists present (an omitted one is never read as empty), and lines the
// factory would keep.
func TestRulesSetIsBoundedBeforeAnythingIsSent(t *testing.T) {
	list := func(s ...string) *[]string { return &s }
	_, clean, err := CheckRulesSet(FactoryRulesSetParams{Factory: "local", Rules: list(" a ", "A"), AskFirst: list(), Never: list("infra/")})
	if err != nil {
		t.Fatal(err)
	}
	if want := (factory.HouseRules{Rules: []string{"a"}, AskFirst: []string{}, Never: []string{"infra/"}}); !reflect.DeepEqual(clean, want) {
		t.Fatalf("cleaned %#v, want %#v", clean, want)
	}
	for name, p := range map[string]FactoryRulesSetParams{
		"no factory": {Rules: list(), AskFirst: list(), Never: list()},
		"no never":   {Factory: "local", Rules: list(), AskFirst: list()},
		"two lines":  {Factory: "local", Rules: list("a\nb"), AskFirst: list(), Never: list()},
	} {
		if _, _, err := CheckRulesSet(p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, ok := methods["factory.rules"]; !ok {
		t.Fatal("factory.rules is not served")
	}
	if _, ok := methods["factory.rules_set"]; !ok {
		t.Fatal("factory.rules_set is not served")
	}
}
