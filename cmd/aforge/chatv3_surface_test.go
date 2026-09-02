package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A SURFACE THAT OWNS THE TERMINAL OWNS THE LOGGER, AND IT OWNS IT FROM ONE
// PLACE.
//
// The defect this guards was not a wrong redirect — it was four doors, one of
// which remembered (#404). The in-process door parked the standard logger in
// the profile's chat.log; the ssh, relay and unix-socket doors left it on
// stderr, so a recovered fault or a checkpoint warning tore through the alt
// screen and was gone with the next repaint. The next door will be written by
// copying one of these, and the copy is only safe while running the surface is
// a call rather than a paragraph a door has to remember. So this reads the
// source: [tui3.Run] is reached from chatv3_surface.go and from nowhere else in
// this command.
func TestOnlyTheSurfaceHelperRunsTheV3Surface(t *testing.T) {
	const helper = "chatv3_surface.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		calls := strings.Count(string(raw), "tui3.Run(")
		switch {
		case name == helper && calls == 1:
			found = true
		case name == helper:
			t.Errorf("%s runs the surface %d times, want exactly 1 — the helper is the "+
				"one path and it is a single sentence", name, calls)
			found = calls > 0
		case calls > 0:
			t.Errorf("%s calls tui3.Run itself; every door goes through runSurface in %s, "+
				"which is where the byte meter is armed and the standard logger is parked "+
				"in the profile's chat.log for as long as the surface owns the terminal",
				name, helper)
		}
	}
	if !found {
		t.Fatalf("no door runs the surface at all: %s does not call tui3.Run", helper)
	}

	// And the four doors take it. A door that stopped calling runSurface without
	// calling tui3.Run either stopped opening the surface or grew a third way to
	// do it, and both are worth a red line.
	for _, door := range []string{
		"chatv3.go", "chatv3_host.go", "chatv3_at.go", "chatv3_local.go",
	} {
		raw, err := os.ReadFile(door)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "runSurface(") {
			t.Errorf("%s opens the v3 surface and never calls runSurface", door)
		}
	}
}

// The redirect itself, asked the three ways it can be met: the ordinary one,
// the way back out, and the profile that cannot take the file.
func TestTheSurfaceLoggerLandsInTheProfileAndGivesTheTerminalBack(t *testing.T) {
	// Whatever this process's logger was set to is restored whichever way the
	// subtests end; the standard logger is global, and a test that left it in a
	// temporary directory would take the rest of the package with it.
	before := log.Writer()
	t.Cleanup(func() { log.SetOutput(before) })

	t.Run("a line written while the surface is up lands in the profile's chat.log", func(t *testing.T) {
		profile := t.TempDir()
		terminal := &bytes.Buffer{}
		log.SetOutput(terminal)
		ran := false
		err := withSurfaceLogger(profile, func() error {
			ran = true
			log.Printf("a checkpoint warning nobody should have to read off the frame")
			if log.Writer() == terminal {
				t.Error("the logger is still on the terminal while the surface owns it")
			}
			return nil
		})
		if err != nil || !ran {
			t.Fatalf("run the surface: ran=%v err=%v", ran, err)
		}
		kept, err := os.ReadFile(filepath.Join(profile, "chat.log"))
		if err != nil {
			t.Fatalf("read the profile's chat.log: %v", err)
		}
		if !strings.Contains(string(kept), "a checkpoint warning") {
			t.Errorf("chat.log does not hold the line: %q", string(kept))
		}
		if terminal.Len() != 0 {
			t.Errorf("the line tore through the frame anyway: %q", terminal.String())
		}
		if log.Writer() != terminal {
			t.Error("the terminal did not get its logger back when the surface handed it over")
		}
	})

	t.Run("the error the surface answers with is the one the door sees", func(t *testing.T) {
		profile := t.TempDir()
		want := errSurfaceTest
		if got := withSurfaceLogger(profile, func() error { return want }); got != want {
			t.Errorf("withSurfaceLogger answered %v, want the surface's own %v", got, want)
		}
	})

	t.Run("a profile that cannot take the file keeps stderr and still runs", func(t *testing.T) {
		// A FILE USED AS A DIRECTORY: chat.log cannot be created under it, which
		// is the shape a read-only or occupied profile takes. A lost frame is
		// better than a lost warning, so the surface runs and the logger stays
		// where it was.
		blocked := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocked, []byte("in the way"), 0o644); err != nil {
			t.Fatal(err)
		}
		terminal := &bytes.Buffer{}
		log.SetOutput(terminal)
		ran := false
		err := withSurfaceLogger(blocked, func() error {
			ran = true
			if log.Writer() != terminal {
				t.Error("the logger moved off the terminal with nowhere to move to")
			}
			log.Printf("a warning that had to go somewhere")
			return nil
		})
		if err != nil || !ran {
			t.Fatalf("run the surface anyway: ran=%v err=%v", ran, err)
		}
		if !strings.Contains(terminal.String(), "a warning that had to go somewhere") {
			t.Errorf("the warning was lost rather than kept on the terminal: %q", terminal.String())
		}
	})
}

// errSurfaceTest stands in for whatever the surface itself failed with.
var errSurfaceTest = errSurface("the surface stopped")

type errSurface string

func (e errSurface) Error() string { return string(e) }
