package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	windowsPictureForward   = "c:/Users/abira/Pictures/Screenshots/Screenshot (1).png"
	windowsPictureBackward  = `C:\Users\abira\Pictures\Screenshots\Screenshot (1).png`
	windowsPictureFileURL   = "file:///C:/Users/abira/Pictures/Screenshots/Screenshot%20(1).png"
	windowsOrdinaryForward  = "c:/Users/abira/Documents/server.log"
	windowsMissingForward   = "c:/Users/abira/Pictures/Screenshots/missing.png"
	windowsPictureName      = "Screenshot (1).png"
	windowsOrdinaryFileName = "server.log"
)

func wslPathLab(t *testing.T, picture, ordinary bool) (*app, string) {
	t.Helper()
	mount := t.TempDir()
	if picture {
		path := filepath.Join(mount, "c", "Users", "abira", "Pictures", "Screenshots", windowsPictureName)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, 12), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ordinary {
		path := filepath.Join(mount, "c", "Users", "abira", "Documents", windowsOrdinaryFileName)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, 12), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, _, _ := attachLab(t, nil)
	a.wsl = wslPaths{inside: true, distro: "Ubuntu", root: mount}
	return a, mount
}

// V1: A bracketed Windows-path paste inside WSL becomes the same picture or file chip as a Linux-path drop.
func TestAWindowsPathPasteInsideWSLLandsOnTheTray(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     string
		picture  bool
		ordinary bool
		file     bool
		draft    string
	}{
		{
			name:    "picture",
			path:    "'" + windowsPictureForward + "'",
			picture: true,
			draft:   "[image #1] ",
		},
		{
			name:     "ordinary file",
			path:     "'" + windowsOrdinaryForward + "'",
			ordinary: true,
			file:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := wslPathLab(t, tc.picture, tc.ordinary)
			pasteText(t, a, tc.path)

			if len(a.chips) != 1 || a.chips[0].file != tc.file {
				t.Fatalf("the paste left chips %#v, want one file=%v chip", a.chips, tc.file)
			}
			if got := a.input.String(); got != tc.draft {
				t.Fatalf("the draft is %q, want %q", got, tc.draft)
			}
		})
	}
}

// V2: Bare and quoted Windows drive paths typed one rune at a time settle onto the tray.
func TestEveryTypedWindowsDriveSpellingLandsOnTheTray(t *testing.T) {
	for _, typed := range []string{
		windowsPictureBackward,
		`"` + windowsPictureBackward + `"`,
		"'" + windowsPictureForward + "'",
	} {
		a, _ := wslPathLab(t, true, false)
		at := a.now()
		a.clock = func() time.Time { return at }
		typeBurst(a, typed)
		at = at.Add(2 * dropQuiet)
		drive(t, a, dropMsg{})

		if want := []string{windowsPictureName}; !equalStrings(chipNames(a), want) {
			t.Fatalf("typing %q attached %v, want %v", typed, chipNames(a), want)
		}
		if got := a.input.String(); got != "[image #1] " {
			t.Fatalf("typing %q left the draft %q", typed, got)
		}
	}
}

// V3: A file URL whose decoded path begins with a Windows drive resolves through the WSL mount.
func TestAWindowsFileURLPasteResolvesInsideWSL(t *testing.T) {
	a, _ := wslPathLab(t, true, false)
	pasteText(t, a, windowsPictureFileURL)

	if want := []string{windowsPictureName}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
}

// V4: The attach command uses the WSL translation and keeps its existing missing-file refusal.
func TestAttachUsesWindowsPathsInsideWSL(t *testing.T) {
	a, _ := wslPathLab(t, true, false)
	typeText(t, a, "/attach "+windowsPictureForward)
	drive(t, a, key("enter"))
	if want := []string{windowsPictureName}; !equalStrings(chipNames(a), want) {
		t.Fatalf("/attach added %v, want %v", chipNames(a), want)
	}

	a, _ = wslPathLab(t, false, false)
	typeText(t, a, "/attach "+windowsMissingForward)
	drive(t, a, key("enter"))
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, "no such file:") || !strings.Contains(got, "missing.png") {
		t.Fatalf("the missing file did not get the existing refusal:\n%s", got)
	}
}

// V5: Outside WSL, a Windows drop stays text and names only the missing file in the existing note.
func TestAWindowsPathPasteOutsideWSLStaysText(t *testing.T) {
	a, _, _ := attachLab(t, nil)
	a.wsl = wslPaths{}
	pasted := "'" + windowsPictureForward + "'"
	pasteText(t, a, pasted)

	if got := a.input.String(); got != pasted {
		t.Fatalf("the draft is %q, want the untouched paste %q", got, pasted)
	}
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, windowsPictureName+" is not on this machine") {
		t.Fatalf("the missing Windows drop was not named by its base name:\n%s", got)
	}
}

// Review finding 2: A missing bare Windows path pasted by the terminal stays one file, keeps its text, and never reaches the model.
func TestAMissingBareWindowsPathPasteNamesOneFileAndStaysPut(t *testing.T) {
	a, agent, _ := attachLab(t, nil)
	a.wsl = wslPaths{}
	pasteText(t, a, windowsPictureBackward)

	assertMissingWindowsDrop(t, a, agent, windowsPictureBackward)
}

// Review finding 2: A missing bare Windows path typed by the terminal takes the drop door at enter and never reaches the model.
func TestAMissingTypedWindowsPathNamesOneFileAndNeverSubmits(t *testing.T) {
	a, agent, _ := attachLab(t, nil)
	a.wsl = wslPaths{}
	typeBurst(a, windowsPictureBackward)
	drive(t, a, key("enter"))

	assertMissingWindowsDrop(t, a, agent, windowsPictureBackward)
}

func assertMissingWindowsDrop(t *testing.T, a *app, agent *imageAgent, path string) {
	t.Helper()
	if got := a.input.String(); got != path {
		t.Fatalf("the missing path left the draft as %q, want %q", got, path)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the missing path attached chips %#v", a.chips)
	}
	if a.drop.open {
		t.Fatal("the missing path left its keystroke fold open after enter")
	}
	if agent.calls != 0 || len(agent.sent) != 0 {
		t.Fatalf("the missing path reached the model: image calls=%d sent=%q", agent.calls, agent.sent)
	}
	want := "· " + windowsPictureName + " is not on this machine"
	for _, row := range plainRows(a) {
		if strings.TrimSpace(row) == want {
			return
		}
	}
	t.Fatalf("the rendered note is not exactly %q:\n%s", want, strings.Join(plainRows(a), "\n"))
}

// V6: WSL UNC paths resolve only when their distro names this WSL instance.
func TestWSLUNCPathsResolveForThisDistro(t *testing.T) {
	local := filepath.Join(t.TempDir(), "home", "abir", "pic.png")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, make([]byte, 12), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"wsl.localhost", "wsl$"} {
		path := wslUNCForLocalPath(host, "Ubuntu", local)
		t.Run(host+" matches", func(t *testing.T) {
			a, _, _ := attachLab(t, nil)
			a.wsl = wslPaths{inside: true, distro: "Ubuntu", root: "/mnt"}
			pasteText(t, a, path)

			if want := []string{"pic.png"}; !equalStrings(chipNames(a), want) {
				t.Fatalf("the UNC paste attached %v, want %v", chipNames(a), want)
			}
		})
		t.Run(host+" differs", func(t *testing.T) {
			a, _, _ := attachLab(t, nil)
			a.wsl = wslPaths{inside: true, distro: "Debian", root: "/mnt"}
			pasteText(t, a, path)

			if len(a.chips) != 0 {
				t.Fatalf("another distro's UNC path attached chips %#v", a.chips)
			}
			if got := a.input.String(); got != path {
				t.Fatalf("another distro's UNC path left the draft as %q, want %q", got, path)
			}
		})
	}
}

func wslUNCForLocalPath(host, distro, local string) string {
	rest := strings.ReplaceAll(strings.TrimPrefix(filepath.ToSlash(local), "/"), "/", `\`)
	return `\\` + host + `\` + distro + `\` + rest
}

// V6 supplement: Forward-slash UNC input and an unnamed WSL distro retain the pure translation contract.
func TestWSLUNCForwardSlashesAndAnUnnamedDistroTranslate(t *testing.T) {
	for _, path := range []string{
		`//wsl.localhost/Ubuntu/home/abir/pic.png`,
		`//wsl$/Ubuntu/home/abir/pic.png`,
	} {
		if got := windowsPathHere(path, wslPaths{inside: true, root: "/mnt"}); got != "/home/abir/pic.png" {
			t.Errorf("an unnamed WSL distro resolved %q to %q", path, got)
		}
	}
}

// V7: The automount root in the configured wsl.conf replaces the default mount root.
func TestWSLAutomountRootComesFromTheNamedConfig(t *testing.T) {
	mount := t.TempDir()
	local := filepath.Join(mount, "c", "x")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, make([]byte, 12), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(t.TempDir(), "wsl.conf")
	if err := os.WriteFile(conf, []byte("[automount]\nroot = "+mount+"/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := detectWSLPathsAt(func(name string) string {
		if name == "WSL_DISTRO_NAME" {
			return "Ubuntu"
		}
		return ""
	}, filepath.Join(t.TempDir(), "version"), conf)
	a, _, _ := attachLab(t, nil)
	a.wsl = paths
	pasteText(t, a, "c:/x")

	if want := []string{"x"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("the configured mount attached %v, want %v", chipNames(a), want)
	}
	if got := a.chips[0].path; got != local {
		t.Fatalf("the configured mount attached %q, want %q", got, local)
	}
}

// V8: A settled Windows drive drop asks the disk once for its one candidate.
func TestAWindowsDriveDropPaysOneBoundedDiskReading(t *testing.T) {
	a, _ := wslPathLab(t, false, true)
	at := a.now()
	a.clock = func() time.Time { return at }
	typeBurst(a, `C:\Users\abira\Documents\server.log`)
	if a.drop.armed != 1 || a.drop.looked != 0 {
		t.Fatalf("the arriving drive path armed %d wakeups and made %d looks, want 1 and 0", a.drop.armed, a.drop.looked)
	}
	at = at.Add(2 * dropQuiet)
	drive(t, a, dropMsg{})
	if a.drop.looked != 1 {
		t.Fatalf("the settled drive path asked the disk %d times, want once", a.drop.looked)
	}
	if want := []string{windowsOrdinaryFileName}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
}
