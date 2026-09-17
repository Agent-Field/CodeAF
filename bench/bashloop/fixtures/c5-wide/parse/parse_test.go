package parse

import (
	"reflect"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a,b,c,", []string{"a", "b", "c", ""}},
		{",", []string{"", ""}},
		{"", []string{""}},
	}
	for _, c := range cases {
		if got := SplitCSV(c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitCSV(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}
