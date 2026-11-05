package approval

import (
	"reflect"
	"testing"
)

func TestCommandWordsKeepsABracedVariableInOneWord(t *testing.T) {
	got := CommandWords(`cat ${HOME}/.netrc && echo "${X}y"`)
	want := [][]string{{"cat", "${HOME}/.netrc"}, {"echo", "${X}y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
