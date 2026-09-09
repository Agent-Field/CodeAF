package verify

import (
	"reflect"
	"testing"
)

func TestCommandFailureComparesTheFailuresInsideOneRedCommand(t *testing.T) {
	tests := []struct {
		name       string
		beforeRed  bool
		before     []string
		after      []string
		wantNames  []string
		wantChange bool
	}{
		{name: "red A before and red B after", beforeRed: true, before: []string{"tests/a.py::test_a"}, after: []string{"tests/b.py::test_b"}, wantNames: []string{"tests/b.py::test_b"}, wantChange: true},
		{name: "the same old failure", beforeRed: true, before: []string{"tests/a.py::test_a"}, after: []string{"tests/a.py::test_a"}},
		{name: "unparsed red remains uncertain", beforeRed: true},
		{name: "a non-test validation turned red", beforeRed: false, wantChange: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed := NewCommandFailure(test.beforeRed, test.before, test.after)
			if changed != test.wantChange || !reflect.DeepEqual(got, test.wantNames) {
				t.Fatalf("NewCommandFailure = (%#v, %v), want (%#v, %v)", got, changed, test.wantNames, test.wantChange)
			}
		})
	}
}
