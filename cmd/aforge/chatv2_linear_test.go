package main

import (
	"flag"
	"testing"
)

// resolveLinear is the seam between the settings registry and RunOptions: an
// explicit --linear on the command line has to outrank the registry, and the
// registry's answer has to come through untouched when the flag was never
// typed.
func TestResolveLinear(t *testing.T) {
	newFlags := func() (*flag.FlagSet, *bool) {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		linear := flags.Bool("linear", false, "")
		return flags, linear
	}

	t.Run("untyped flag falls through to the registry default", func(t *testing.T) {
		t.Setenv("AFORGE_CHAT_LINEAR", "")
		t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
		flags, linear := newFlags()
		if err := flags.Parse(nil); err != nil {
			t.Fatal(err)
		}
		if resolveLinear(flags, *linear) {
			t.Fatal("linear mode should default off")
		}
	})

	t.Run("the environment pin reaches through an untyped flag", func(t *testing.T) {
		t.Setenv("AFORGE_CHAT_LINEAR", "1")
		t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
		flags, linear := newFlags()
		if err := flags.Parse(nil); err != nil {
			t.Fatal(err)
		}
		if !resolveLinear(flags, *linear) {
			t.Fatal("AFORGE_CHAT_LINEAR=1 should enable linear mode")
		}
	})

	t.Run("an explicit --linear=false outranks the pin", func(t *testing.T) {
		t.Setenv("AFORGE_CHAT_LINEAR", "1")
		t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
		flags, linear := newFlags()
		if err := flags.Parse([]string{"--linear=false"}); err != nil {
			t.Fatal(err)
		}
		if resolveLinear(flags, *linear) {
			t.Fatal("an explicit --linear=false should win over the pin")
		}
	})

	t.Run("an explicit --linear outranks a quiet environment", func(t *testing.T) {
		t.Setenv("AFORGE_CHAT_LINEAR", "")
		t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
		flags, linear := newFlags()
		if err := flags.Parse([]string{"--linear"}); err != nil {
			t.Fatal(err)
		}
		if !resolveLinear(flags, *linear) {
			t.Fatal("an explicit --linear should win over a quiet environment")
		}
	})
}
