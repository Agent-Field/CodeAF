package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// The unattended fence hides the secret files of the folder the run's state is
// in. Under the desktop app's --home that is the app's, and the person's own
// ~/.codeaf, which holds the command line's keys, must stay hidden too. A run
// in the person's own folder has nothing more to hide.
func TestTheFenceNamesTheCommandLinesFolderWhenTheStateIsElsewhere(t *testing.T) {
	person := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(t.TempDir(), "app-state"))
	if got, want := otherCodeafHomes(person), []string{filepath.Join(person, ".codeaf")}; !slices.Equal(got, want) {
		t.Fatalf("other codeaf folders under the app's --home = %q, want %q", got, want)
	}
	t.Setenv(home.EnvVar, filepath.Join(person, ".codeaf"))
	if got := otherCodeafHomes(person); got != nil {
		t.Fatalf("the person's own codeaf folder names %q as another one", got)
	}
	if got := otherCodeafHomes(""); got != nil {
		t.Fatalf("no home folder named %q", got)
	}
}
