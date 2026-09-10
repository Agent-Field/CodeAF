package session

// THE ONE READING OF A DECLARED WRITE SCOPE, PROVED DIRECTLY.
//
// These laws used to be asserted through `fork`'s door — the verb that handed
// scopes out — and the door is gone. The functions are not: [writeGuard]
// enforces every scoped agent through them (orchestrate.go), and a quick task's
// `files` claim is normalized by them at admission. So the proof stands here,
// on the functions themselves, where the next door to hand out a scope will
// find it.
//
// The measured failure they exist for is in fork.go's own comment: a scope
// declared in ABSOLUTE paths was accepted at the door and then compared against
// workspace-relative writes by the guard, so every write in every slice was
// refused and the model reported the tool refusing files that were plainly its
// own. Nothing had gone wrong except that two readings of one string disagreed.

import (
	"reflect"
	"strings"
	"testing"
)

// AN ABSOLUTE PATH UNDER THE WORKSPACE IS THE SAME SCOPE, converted rather than
// refused: it names the same file, and a refusal would spend a round teaching a
// model a spelling.
func TestAnAbsoluteScopeUnderTheWorkspaceIsTheSameScope(t *testing.T) {
	const workspace = "/w/project"
	for _, spelling := range []string{
		"src/parser.rs",
		"/w/project/src/parser.rs",
		"./src/parser.rs",
		"src/../src/parser.rs",
	} {
		got, refusal := normalizeWriteScope(workspace, []string{spelling})
		if refusal != "" {
			t.Errorf("%q was refused: %s", spelling, refusal)
			continue
		}
		if !reflect.DeepEqual(got, []string{"src/parser.rs"}) {
			t.Errorf("%q normalized to %v, want the guard's own form", spelling, got)
		}
	}
}

// A SCOPE THAT IS NOT A SLICE OF THE WORKING COPY IS REFUSED AT THE DOOR, and
// the refusal names the offending path and the form that is wanted — a model
// told only "no" re-sends the same declaration.
func TestAScopeOutsideTheWorkingCopyIsRefused(t *testing.T) {
	const workspace = "/w/project"
	for _, bad := range []string{"/etc/hosts", "../elsewhere", "..", ".", "/"} {
		got, refusal := normalizeWriteScope(workspace, []string{bad})
		if refusal == "" {
			t.Errorf("%q was accepted as %v", bad, got)
			continue
		}
		if !strings.Contains(refusal, strings.TrimSpace(bad)) {
			t.Errorf("the refusal of %q does not name it: %s", bad, refusal)
		}
		if !strings.Contains(refusal, workspace) {
			t.Errorf("the refusal of %q does not say what the working copy is: %s", bad, refusal)
		}
	}
}

// A BLANK ENTRY IS A FORMATTING SLIP AND NOT A CLAIM, so it is dropped rather
// than refused; a scope that is nothing but blanks comes back empty for its
// caller to answer in its own words.
func TestABlankScopeEntryIsDroppedRatherThanRefused(t *testing.T) {
	got, refusal := normalizeWriteScope("/w/project", []string{"docs", "", "  "})
	if refusal != "" {
		t.Fatalf("a stray blank refused the whole scope: %s", refusal)
	}
	if !reflect.DeepEqual(got, []string{"docs"}) {
		t.Fatalf("the scope read as %v, want the one real path", got)
	}
	if got, refusal := normalizeWriteScope("/w/project", []string{"", " "}); refusal != "" || len(got) != 0 {
		t.Fatalf("a scope of nothing but blanks answered %v / %q, want empty and no refusal", got, refusal)
	}
}

// AND AN AGENT WITH NO WORKSPACE IS SAID SO, rather than told its working copy
// is ".". It is a real case in tests and in a door that never set one.
func TestAnAbsoluteScopeWithNoWorkspaceSaysThereIsNone(t *testing.T) {
	_, refusal := normalizeWriteScope("", []string{"/etc/hosts"})
	if !strings.Contains(refusal, "not set for this agent") {
		t.Fatalf("the refusal reads %q", refusal)
	}
}

// THE GUARD'S OWN READING DROPS WHAT IT CANNOT NORMALIZE rather than passing it
// through raw. Dropping is the safe direction: an entry naming something outside
// the working copy can be satisfied by no path inside it, so keeping it would
// admit nothing, while passing it through raw is what let an absolute
// declaration sit in a scope silently matching no write at all.
func TestTheGuardsReadingDropsWhatItCannotNormalize(t *testing.T) {
	got := scopeAsGuarded("/w/project", []string{"/w/project/src", "/etc/hosts", "docs", "."})
	if !reflect.DeepEqual(got, []string{"src", "docs"}) {
		t.Fatalf("the guard reads the scope as %v, want the two paths inside the working copy", got)
	}
}

// TWO SCOPES SHARING A PATH ARE THE ONE SHAPE A SHARED WORKING COPY CANNOT MAKE
// SAFE, and a directory claims everything under it in both directions.
func TestCollidingScopesAreFoundInBothDirections(t *testing.T) {
	for _, one := range []struct {
		left, right []string
		want        string
	}{
		{[]string{"src/parser.rs"}, []string{"src/parser.rs"}, "src/parser.rs"},
		{[]string{"src"}, []string{"src/parser.rs"}, "src/parser.rs"},
		{[]string{"src/parser.rs"}, []string{"src"}, "src/parser.rs"},
	} {
		path, collides := forkScopesCollide(one.left, one.right)
		if !collides || path != one.want {
			t.Errorf("%v against %v answered %q/%v, want %q", one.left, one.right, path, collides, one.want)
		}
	}
	if path, collides := forkScopesCollide([]string{"src"}, []string{"docs"}); collides {
		t.Errorf("disjoint scopes collided over %q", path)
	}
	if _, collides := forkScopesCollide(nil, []string{"docs"}); collides {
		t.Error("an empty scope collided with something, though it claims nothing")
	}
}
