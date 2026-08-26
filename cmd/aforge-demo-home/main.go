package main

// aforge-demo-home builds a DEMO HOME: a throwaway state directory with
// something on every one of v3's places, so the surface can be looked at full
// rather than empty.
//
// WHY IT IS NOT A FIXTURE FOR TESTS AND NOT A SEED FOR A REAL MACHINE. The
// owner opened the standing page, the memory page and the spend page on their
// own machine and saw nothing, because on their machine those stores are new
// and empty — which is the emptiness law working exactly as designed. The
// answer is not to write invented standing orders, invented memories and
// invented spending into ~/.aforge, which would make a person's own record a
// lie; it is a SECOND home, somewhere else, that the surface can be pointed at.
// So this program never touches the real state root: it writes one directory,
// the one it was given, and the launcher hands that directory to the binary as
// HOME.
//
// WHY IT IS A SEPARATE BINARY. cmd/aforge has a clean place for a hidden verb —
// `engine` and `tick` both live there — but the seeding below is several hundred
// lines and the shipped binary is on a checked-in byte budget (SIZE-BUDGET,
// PERF.md). A developer target must not spend the product's weight, so this is
// its own main package and `bin/aforge` does not change by a byte.
//
// EVERYTHING IS WRITTEN THROUGH THE ENGINE'S OWN WRITERS — session.SaveMeta,
// session.RecordUsage, session.RecordArtifact, standing.Store, store.Store — so
// what the surface reads back is what the product itself produces. The one
// exception is stated where it is made ([writeTranscript]).

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "aforge-demo-home:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("aforge-demo-home", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() { fmt.Fprint(flags.Output(), usageText) }
	into := flags.String("into", "", "the directory to build the demo home in; empty makes a fresh temporary one")
	launch := flags.String("launch", "", "an aforge binary to run against the demo home once it is built")
	reuse := flags.Bool("keep", false, "reuse a directory that already holds a demo home instead of refusing it")
	if err := flags.Parse(args); err != nil {
		return err
	}

	dir, fresh, err := demoDir(*into, *reuse)
	if err != nil {
		return err
	}
	now := time.Now()
	if fresh {
		built, err := seedDemoHome(dir, now)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, built.line())
	} else {
		fmt.Fprintf(os.Stderr, "reusing the demo home already in %s\n", dir)
	}

	// The directory itself goes to STDOUT and nothing else does, so a shell can
	// take the whole of stdout as the path.
	fmt.Println(dir)
	// AND THE WAY BACK TO IT IS PRINTED WHETHER OR NOT WE OPEN IT NOW. A demo
	// home built into a temporary directory has a name nobody chose and nobody
	// would guess, and the second look at it is the one that matters — the one
	// where the owner shows somebody else, or comes back after a night.
	fmt.Fprintf(os.Stderr, "\ncome back to this one with:\n  make demo-home DEMO_HOME=%s KEEP=1\n", dir)

	if *launch == "" {
		fmt.Fprintf(os.Stderr, "\nor open it by hand with:\n  cd %s && HOME=%s %s\n",
			filepath.Join(dir, firstProjectName), dir, surfaceBeside())
		return nil
	}
	return launchAgainst(dir, *launch)
}

// surfaceBeside is the aforge binary sitting next to this one, for the sentence
// that tells somebody how to open the home by hand. It answers the ordinary
// spelling when there is nothing beside us — a path that is not there is worse
// than a path somebody has to fill in.
func surfaceBeside() string {
	self, err := os.Executable()
	if err != nil {
		return "bin/aforge"
	}
	beside := filepath.Join(filepath.Dir(self), "aforge")
	if info, err := os.Stat(beside); err != nil || info.IsDir() {
		return "bin/aforge"
	}
	return beside
}

const usageText = `aforge-demo-home — build a home with something on every place, for looking at

  aforge-demo-home [--into dir] [--keep] [--launch path/to/aforge]

  --into    where to build it; a fresh temporary directory when not given
  --keep    reuse a directory that already holds a demo home rather than refusing
            to write over it, so a second launch sees what the first one left
  --launch  run that binary against the demo home, with HOME pointed at it and a
            presence heartbeat running beside it, and return when it exits

It writes inside --into and nowhere else. The real state root (~/.aforge) is
never opened.
`

// demoDir resolves where to build, and reports whether it needs building.
//
// A DIRECTORY THAT ALREADY HOLDS ONE IS REFUSED WITHOUT --keep. The whole point
// of this program is that it is safe to run, and the way it could stop being
// safe is somebody typing --into over a directory that is not a demo home at
// all. So an existing directory is only accepted when it is empty, or when the
// caller says --keep and the directory looks like something this program wrote.
func demoDir(into string, reuse bool) (dir string, fresh bool, err error) {
	if into == "" {
		made, err := os.MkdirTemp("", "aforge-demo-home-")
		if err != nil {
			return "", false, fmt.Errorf("make a directory to build in: %w", err)
		}
		return made, true, nil
	}
	dir, err = filepath.Abs(into)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", into, err)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", false, fmt.Errorf("make %s: %w", dir, err)
		}
		return dir, true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", dir, err)
	}
	if len(entries) == 0 {
		return dir, true, nil
	}
	if !reuse {
		return "", false, fmt.Errorf("%s is not empty; pass --keep to reuse a demo home that is already there", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, ".aforge", "v3", "projects")); err != nil {
		return "", false, fmt.Errorf("%s is not empty and does not hold a demo home", dir)
	}
	return dir, false, nil
}

// launchAgainst runs the surface against the demo home and keeps the live
// conversations alive underneath it.
//
// THE HEARTBEAT IS WHY THIS IS NOT JUST `HOME=dir aforge`. A presence file is
// believed for three heartbeats and no longer (internal/session's
// taskpresence.go), which is what stops a killed window from haunting a surface
// forever — and it means a presence file written once by a seeder is stale
// fifteen seconds later. The two clauses the owner is being shown, `1 want you`
// and `1 moving`, would be gone before they had finished reading the screen. So
// this restamps them on the same cadence a live session does, for exactly as
// long as the surface is open.
func launchAgainst(dir, binary string) error {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", binary, err)
	}
	stop := startPresenceBeat(dir)
	defer stop()

	// The surface opens on the project the owner is most likely to want, so the
	// foot of home says `here ~/aforge-v2` rather than naming this repository.
	cwd := filepath.Join(dir, firstProjectName)
	surface := exec.Command(binary)
	surface.Dir = cwd
	surface.Stdin, surface.Stdout, surface.Stderr = os.Stdin, os.Stdout, os.Stderr
	surface.Env = append(demoEnviron(), "HOME="+dir, "PWD="+cwd)

	// A ctrl+c belongs to the surface and not to this process: the surface is
	// the one holding the terminal, and it has its own answer for the key.
	signal.Ignore(syscall.SIGINT)
	err = surface.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// The surface's own exit code is not this program's failure.
		return nil
	}
	return err
}

// demoEnviron is this process's environment with the aforge pins that would
// point the surface back at the real machine removed. HOME is set by the
// caller; AFORGE_HOME would override it outright, which is the one variable
// that could silently send a demo launch at the owner's own state root.
func demoEnviron() []string {
	var kept []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, home.EnvVar+"=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
