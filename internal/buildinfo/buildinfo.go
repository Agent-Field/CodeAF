// Package buildinfo names the source and moment that produced this aforge.
package buildinfo

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// These words are filled by the repository's build targets. They stay private
// so every surface reads the same formatted stamp through String.
var (
	rev     string
	dirty   string
	builtAt string
)

var current = resolve(rev, dirty, builtAt, debug.ReadBuildInfo)

// Info is the immutable identity carried by this running program.
type Info struct {
	Revision string
	Dirty    bool
	BuiltAt  time.Time
}

// String returns the compact build identity shown to a person and kept on disk.
func String() string {
	return current.String()
}

// Identity names the engine a running process is, as far as a build can be told
// from another one.
//
// A BUILD THAT CAN NAME ITS SOURCE IS THAT SOURCE AND NOTHING ELSE. Two `make
// build` runs on one unmodified commit produce the same program, so they produce
// the same engine — and the door that decides whether to attach must not read a
// difference nobody made. It did: the build moment was in here, so a window built
// thirteen seconds after the engine it met was told it had met an older aforge.
//
// A BUILD THAT CANNOT NAME ITS SOURCE KEEPS THE MOMENT IT WAS MADE. A modified
// tree spells the same revision before and after an edit, and an unstamped build
// spells none at all; the moment is then the only thing that tells one rebuild
// from the next, and somebody rebuilding what they just edited must still get a
// new engine.
func (info Info) Identity() string {
	// The two shapes can never be mistaken for one another: a source names
	// itself with no slash in it, and the moment always carries two.
	if source := info.source(); source != "" && !info.Dirty {
		return source
	}
	return fmt.Sprintf("%s/%t/%s", info.source(), info.Dirty, info.BuiltAt.UTC().Format(time.RFC3339Nano))
}

// Identity names the engine this process is. [Info.Identity] is the rule.
func Identity() string { return current.Identity() }

// Revision returns the stable source identity without the build-time details.
func Revision() string {
	return current.source()
}

// source is the revision as it was written down, and it is read through here by
// everything in this file so that the trimming is decided in one place.
func (info Info) source() string {
	return strings.TrimSpace(info.Revision)
}

// String formats a build identity without inventing absent facts.
func (info Info) String() string {
	revision := info.source()
	if revision == "" {
		revision = "dev"
	}
	if info.Dirty {
		revision += " (dirty)"
	}
	if !info.BuiltAt.IsZero() {
		revision += " built " + info.BuiltAt.Local().Format("2006-01-02 15:04")
	}
	return revision
}

func resolve(linkRev, linkDirty, linkBuiltAt string, read func() (*debug.BuildInfo, bool)) Info {
	info := Info{
		Revision: strings.TrimSpace(linkRev),
		Dirty:    strings.EqualFold(strings.TrimSpace(linkDirty), "true"),
		BuiltAt:  parseBuiltAt(linkBuiltAt),
	}
	if info.Revision != "" {
		return info
	}

	build, ok := read()
	if !ok || build == nil {
		return info
	}
	if build.Main.Version != "" && build.Main.Version != "(devel)" {
		info.Revision = build.Main.Version
	}
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Revision == "" {
				info.Revision = shortRevision(setting.Value)
			}
		case "vcs.modified":
			info.Dirty = setting.Value == "true"
		}
	}
	return info
}

func parseBuiltAt(value string) time.Time {
	stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return stamp
}

func shortRevision(value string) string {
	value = strings.TrimSpace(value)
	const displayedRevisionLength = 8
	if len(value) > displayedRevisionLength {
		return value[:displayedRevisionLength]
	}
	return value
}

// Detector notices each replacement of one running executable once.
type Detector struct {
	path      string
	started   time.Time
	stat      func(string) (os.FileInfo, error)
	mu        sync.Mutex
	lastBuild time.Time
}

// NewDetector makes a detector whose clock boundary is the process start.
func NewDetector(path string, started time.Time) *Detector {
	return &Detector{path: path, started: started, stat: os.Stat}
}

// Notice returns one calm restart sentence for each newer file at path.
func (detector *Detector) Notice() string {
	if detector == nil || detector.path == "" || detector.stat == nil {
		return ""
	}
	file, err := detector.stat(detector.path)
	if err != nil {
		return ""
	}
	modified := file.ModTime()

	detector.mu.Lock()
	defer detector.mu.Unlock()
	if !modified.After(detector.started) || !modified.After(detector.lastBuild) {
		return ""
	}
	detector.lastBuild = modified
	return fmt.Sprintf("a newer aforge was built at %s — restart to use it", modified.Local().Format("15:04"))
}

var running = newRunningDetector()

func newRunningDetector() *Detector {
	started := time.Now()
	path, err := os.Executable()
	if err != nil {
		return NewDetector("", started)
	}
	return NewDetector(path, started)
}

// StaleNotice reports when this process's executable has been replaced.
func StaleNotice() string {
	return running.Notice()
}
