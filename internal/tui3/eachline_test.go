package tui3

import (
	"reflect"
	"testing"
)

func TestEachLineSaysOneNotePerLineAndSkipsBlanks(t *testing.T) {
	var said []string
	eachLine("first\n\n  second  \nthird", func(line string) { said = append(said, line) })
	if want := []string{"first", "second", "third"}; !reflect.DeepEqual(said, want) {
		t.Fatalf("said %q, want %q", said, want)
	}
}
