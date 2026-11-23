package home

import (
	"os"
	"path/filepath"
	"testing"
)

// SPEC-3 item 4: alias spellings share the witness even after descendants vanish.
func TestFixRound4WitnessAliasesSurviveRemoval(t *testing.T) {
	for _, viaAlias := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical", true: "alias"}[viaAlias], func(t *testing.T) {
			base := t.TempDir()
			real := filepath.Join(base, "real")
			alias := filepath.Join(base, "alias")
			if err := os.Mkdir(real, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, alias); err != nil {
				t.Fatal(err)
			}
			physical := filepath.Join(real, "hh", "b")
			spelled := filepath.Join(alias, "hh", "b")
			if err := os.MkdirAll(physical, 0700); err != nil {
				t.Fatal(err)
			}
			if viaAlias {
				Witness(spelled)
			} else {
				Witness(physical)
			}
			if Removed(physical) || Removed(spelled) {
				t.Fatal("intact alias refused")
			}
			if err := os.Rename(filepath.Join(real, "hh"), filepath.Join(real, "old")); err != nil {
				t.Fatal(err)
			}
			if !Removed(physical) || !Removed(spelled) {
				t.Fatal("alias lost its witness after ancestor removal")
			}
			if err := os.MkdirAll(physical, 0700); err != nil {
				t.Fatal(err)
			}
			if !Removed(physical) || !Removed(spelled) {
				t.Fatal("replacement accepted through alias")
			}
		})
	}
}

// SPEC-3 item 4: a dangling symlink still names the witnessed home it pointed at.
func TestFixRound4DanglingHomeAliasKeepsWitness(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	alias := filepath.Join(base, "alias")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	Witness(real)
	if err := os.Rename(real, real+".old"); err != nil {
		t.Fatal(err)
	}
	if !Removed(alias) {
		t.Fatal("dangling alias forgot the witnessed target")
	}
}
