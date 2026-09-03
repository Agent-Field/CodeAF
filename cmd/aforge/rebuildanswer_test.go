package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// SAYING NO IS NOT AN ERROR, AND NOBODY BEING THERE IS ITS OWN RUNG.
//
// `aforge rebuild` asks before it throws away everything the store worked out
// from the journal. Answering `n` used to leave on a rung that means the work
// did not stand, and running it from a pipe — where there is no keyboard to
// answer with — came back as `error: rebuild cancelled` on exit 1, which tells
// a script that a DESTRUCTIVE command failed to start when in truth it declined
// to guess. The ladder already had the right word for the second case: `4 needed
// an answer and nobody was there`.
func TestDecliningARebuildIsNotAnErrorAndNoKeyboardIsItsOwnRung(t *testing.T) {
	// `aside` is the package's one commentary stream (streams.go); swapping it
	// is how a test reads what a person would have seen on stderr.
	kept := aside
	t.Cleanup(func() { aside = kept })

	for _, c := range []struct {
		what   string
		typed  string
		want   exitStatus
		phrase string
	}{
		{"a person who said no", "n\n", exitDone, "nothing was changed."},
		{"a person who said nothing", "\n", exitDone, "nothing was changed."},
		{"a pipe with nobody on the end", "", exitUnanswered, "--yes"},
	} {
		var said, out bytes.Buffer
		aside = &said
		// The store only has to EXIST to get as far as the question; it is not
		// opened until the answer is yes, which is the whole point of asking.
		store := t.TempDir() + "/graph.db"
		if err := os.WriteFile(store, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		err := runRebuildWith([]string{"--db", store}, strings.NewReader(c.typed), &out)
		var got exitStatus = exitDone
		if err != nil {
			if !errors.As(err, &got) {
				t.Fatalf("%s: rebuild came back with a plain error rather than a rung of the ladder: %v", c.what, err)
			}
		}
		if got != c.want {
			t.Fatalf("%s: rebuild left on exit %d, want %d — a script reading the ladder is told "+
				"the wrong thing about a destructive command it did not run. It said: %q",
				c.what, got, c.want, said.String())
		}
		if !strings.Contains(said.String(), c.phrase) {
			t.Fatalf("%s: the aside never says %q:\n%s", c.what, c.phrase, said.String())
		}
		if out.Len() != 0 {
			t.Fatalf("%s: a question and its outcome reached STDOUT, where the answer belongs:\n%s", c.what, out.String())
		}
	}
}
