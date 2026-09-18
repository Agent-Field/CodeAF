package archive

import "testing"

func TestExt(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"a/b/c.txt", "txt"},
		{"a/b/c.tar.gz", "gz"},
		{"noext", ""},
		{"a.dir/file", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := Ext(c.path); got != c.want {
			t.Errorf("Ext(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
