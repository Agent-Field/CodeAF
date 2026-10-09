package desktopbridge

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

func stubEditors(t *testing.T, list func(string) ([]Editor, error), start func(string, string) error, display bool) {
	t.Helper()
	prevList, prevStart, prevDisplay := listEditors, startEditor, displayUp
	listEditors = list
	if start != nil {
		startEditor = start
	}
	displayUp = func() bool { return display }
	t.Cleanup(func() {
		listEditors, startEditor, displayUp = prevList, prevStart, prevDisplay
	})
}

func editorFixture(t *testing.T, local bool) (*Bridge, string) {
	t.Helper()
	b, _, path := richFixture(t, func(c *Connection) {
		c.Local = local
		c.StatPaths = func(paths []string) ([]remote.PathFact, error) {
			abs := paths[0]
			ok := strings.HasSuffix(abs, "/a.go") || strings.HasSuffix(abs, "/note.txt")
			return []remote.PathFact{{Path: abs, Exists: ok}}, nil
		}
	})
	return b, path
}

func TestEditorsListsDefaultFirst(t *testing.T) {
	var asked []string
	stubEditors(t, func(abs string) ([]Editor, error) {
		asked = append(asked, abs)
		list := []Editor{{ID: "preview.desktop", Name: "Preview"}}
		for i := 0; i < 10; i++ {
			list = append(list, Editor{ID: string(rune('a'+i)) + ".desktop", Name: string(rune('a' + i))})
		}
		list = append(list, Editor{ID: "photoshop.desktop", Name: "Photoshop", Default: true})
		return list, nil
	}, nil, true)
	b, path := editorFixture(t, true)
	w := request(b, "GET", path+"/editors?path=a.go", "")
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	var got editorList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Local || !got.Open || got.Reason != "" {
		t.Fatalf("local answer: %+v", got)
	}
	if len(got.Editors) != editorCap {
		t.Fatalf("cap: %d editors", len(got.Editors))
	}
	if got.Editors[0].ID != "photoshop.desktop" || !got.Editors[0].Default || got.Editors[0].Name != "Photoshop" {
		t.Fatalf("default first: %+v", got.Editors[0])
	}
	defaults := 0
	for _, editor := range got.Editors {
		if editor.Default {
			defaults++
		}
		if !safeEditorID(editor.ID) {
			t.Fatalf("unsafe id %q", editor.ID)
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults: %d", defaults)
	}
	if len(asked) != 1 || !strings.HasSuffix(asked[0], "/a.go") {
		t.Fatalf("lister saw %v", asked)
	}
}

func TestEditorsRefusesPathOutsideWorkspace(t *testing.T) {
	called := 0
	stubEditors(t, func(string) ([]Editor, error) {
		called++
		return []Editor{{ID: "code.desktop", Name: "Code", Default: true}}, nil
	}, nil, true)
	b, path := editorFixture(t, true)
	for _, bad := range []string{"../x.go", "/etc/passwd", "gone.go"} {
		w := request(b, "GET", path+"/editors?path="+bad, "")
		if w.Code != 403 && w.Code != 404 {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body.String())
		}
	}
	if w := request(b, "GET", path+"/editors", ""); w.Code != 400 {
		t.Fatalf("blank: %d", w.Code)
	}
	if called != 0 {
		t.Fatalf("lister ran %d times for a refused path", called)
	}
}

func TestEditorsRemoteIsEmpty(t *testing.T) {
	called := 0
	stubEditors(t, func(string) ([]Editor, error) {
		called++
		return []Editor{{ID: "code.desktop", Name: "Code", Default: true}}, nil
	}, nil, true)
	b, path := editorFixture(t, false)
	w := request(b, "GET", path+"/editors?path=a.go", "")
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	var got editorList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Local || got.Open || len(got.Editors) != 0 || got.Reason != reasonRemote {
		t.Fatalf("remote: %+v", got)
	}
	if called != 0 {
		t.Fatal("a remote engine listed this machine's editors")
	}
}

func TestEditorsOpenRejectsRendererCommand(t *testing.T) {
	var launched []string
	stubEditors(t, func(string) ([]Editor, error) {
		return []Editor{{ID: "code.desktop", Name: "Code", Default: true}}, nil
	}, func(id, abs string) error {
		launched = append(launched, id+" "+abs)
		if strings.ContainsAny(id, " \t;&|$`") || strings.Contains(id, "..") {
			t.Errorf("launcher received %q", id)
		}
		return nil
	}, true)
	b, path := editorFixture(t, true)
	for _, id := range []string{"bash -c id", "code.desktop;rm -rf /", "$(reboot)", "../../bin/sh", "sh"} {
		body := `{"path":"a.go","id":` + jsonString(id) + `}`
		w := request(b, "POST", path+"/editors/open", body)
		if w.Code != 400 {
			t.Errorf("%q: %d %s", id, w.Code, w.Body.String())
		}
	}
	w := request(b, "POST", path+"/editors/open", `{"path":"a.go","id":"other.desktop"}`)
	if w.Code != 400 {
		t.Fatalf("unknown id: %d %s", w.Code, w.Body.String())
	}
	if len(launched) != 0 {
		t.Fatalf("launched %v", launched)
	}
	ok := request(b, "POST", path+"/editors/open", `{"path":"a.go","id":"code.desktop"}`)
	if ok.Code != 200 || len(launched) != 1 || !strings.HasSuffix(launched[0], "/a.go") || !strings.HasPrefix(launched[0], "code.desktop ") {
		t.Fatalf("accepted launch: %d %s launched %v", ok.Code, ok.Body.String(), launched)
	}
}

func TestEditorsOpenRefusesOutsideRemoteAndHeadless(t *testing.T) {
	launched := 0
	stubEditors(t, func(string) ([]Editor, error) {
		return []Editor{{ID: "code.desktop", Name: "Code", Default: true}}, nil
	}, func(string, string) error {
		launched++
		return nil
	}, true)
	b, path := editorFixture(t, true)
	if w := request(b, "POST", path+"/editors/open", `{"path":"/etc/passwd","id":"code.desktop"}`); w.Code != 403 {
		t.Fatalf("outside: %d %s", w.Code, w.Body.String())
	}
	remote, remotePath := editorFixture(t, false)
	if w := request(remote, "POST", remotePath+"/editors/open", `{"path":"a.go","id":"code.desktop"}`); w.Code != 409 || !strings.Contains(w.Body.String(), reasonRemote) {
		t.Fatalf("remote: %d %s", w.Code, w.Body.String())
	}
	displayUp = func() bool { return false }
	if w := request(b, "POST", path+"/editors/open", `{"path":"a.go","id":"code.desktop"}`); w.Code != 409 || !strings.Contains(w.Body.String(), reasonNoDisplay) {
		t.Fatalf("headless: %d %s", w.Code, w.Body.String())
	}
	listed := request(b, "GET", path+"/editors?path=a.go", "")
	var got editorList
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &got) != nil || !got.Local || got.Open || got.Reason != reasonNoDisplay || len(got.Editors) != 1 {
		t.Fatalf("headless list: %d %+v", listed.Code, got)
	}
	if launched != 0 {
		t.Fatalf("launched %d times", launched)
	}
}

func TestLaunchPlanKeepsTheCommandOffTheShell(t *testing.T) {
	prev := lookPath
	lookPath = func(name string) (string, error) {
		switch name {
		case "gtk-launch":
			return "/usr/bin/gtk-launch", nil
		case "open":
			return "/usr/bin/open", nil
		}
		return "", errors.New("absent")
	}
	t.Cleanup(func() { lookPath = prev })
	bin, args, err := launchPlan("code.desktop", "/project/a.go")
	want := []string{"/usr/bin/gtk-launch", "code.desktop", "/project/a.go"}
	if runtime.GOOS == "darwin" {
		// The bundle id goes to -b and the path follows --, so neither can be read as an option.
		want = []string{"/usr/bin/open", "-b", "code.desktop", "--", "/project/a.go"}
	}
	if err != nil || strings.Join(append([]string{bin}, args...), " ") != strings.Join(want, " ") {
		t.Fatalf("argv: %s %v %v", bin, args, err)
	}
	if _, _, err := launchPlan("code.desktop;touch /tmp/x", "/project/a.go"); err == nil {
		t.Fatal("a metacharacter id was planned")
	}
	if _, _, err := launchPlan("code.desktop", ""); err == nil {
		t.Fatal("an empty path was planned")
	}
}

func TestDesktopEntriesDefaultFirstAndIgnoreExec(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDesktop := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(apps, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktop("preview.desktop", "[Desktop Entry]\nType=Application\nName=Preview\nMimeType=text/plain;\nExec=preview %f\n")
	writeDesktop("code.desktop", "[Desktop Entry]\nType=Application\nName=Code\nMimeType=text/plain;text/x-go;\nExec=bash -c evil %f\n")
	writeDesktop("hidden.desktop", "[Desktop Entry]\nType=Application\nName=Hidden\nHidden=true\nMimeType=text/plain;\nExec=hidden %f\n")
	found := scanDesktops([]string{apps}, []string{"text/plain"}, "code.desktop")
	found = capEditors(found)
	if len(found) != 2 || found[0].ID != "code.desktop" || !found[0].Default || found[0].Name != "Code" {
		t.Fatalf("parsed: %+v", found)
	}
	if found[1].Name == "Hidden" || strings.Contains(found[0].Name, "bash") || strings.Contains(found[0].ID, "Exec") {
		t.Fatalf("exec or hidden leaked: %+v", found)
	}
}

func TestSourceFilesFindTextEditorsThroughTheirParentType(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mime", "subclasses"), []byte("application/x-shellscript text/plain\nbad;rm text/plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parents := mimeParents([]string{root})
	if got := mimeFamily("text/x-go", parents); strings.Join(got, ",") != "text/x-go,text/plain" {
		t.Fatalf("text family: %v", got)
	}
	if got := mimeFamily("application/x-shellscript", parents); strings.Join(got, ",") != "application/x-shellscript,text/plain" {
		t.Fatalf("subclass family: %v", got)
	}
	if got := mimeFamily("image/png", parents); strings.Join(got, ",") != "image/png" {
		t.Fatalf("an image gained a parent: %v", got)
	}
	if _, bad := parents["bad;rm"]; bad {
		t.Fatal("an unsafe type was read from the table")
	}

	apps := filepath.Join(root, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(apps, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("gedit.desktop", "[Desktop Entry]\nType=Application\nName=Text Editor\nMimeType=text/plain;\nTryExec=gedit\n")
	write("gone.desktop", "[Desktop Entry]\nType=Application\nName=Uninstalled\nMimeType=text/plain;\nTryExec=not-installed-anywhere\n")
	prev := lookPath
	lookPath = func(name string) (string, error) {
		if name == "gedit" {
			return "/usr/bin/gedit", nil
		}
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { lookPath = prev })
	found := scanDesktops([]string{apps}, mimeFamily("text/x-go", parents), "")
	if len(found) != 1 || found[0].ID != "gedit.desktop" {
		t.Fatalf("source file handlers: %+v", found)
	}
}

func TestLinuxDiscoveryDoesNotInventEditors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux discovery")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := discoverLinux(path)
	if err != nil {
		if len(found) != 0 {
			t.Fatalf("list error still returned %+v", found)
		}
		t.Logf("no editor list on this machine: %v", err)
		return
	}
	for _, editor := range found {
		if editor.Name == "Photoshop" || editor.ID == "photoshop.desktop" || !safeEditorID(editor.ID) || strings.ContainsAny(editor.Name, "\r\n") {
			t.Fatalf("invented or unsafe editor %+v", editor)
		}
	}
	t.Logf("discovered %d handler(s)", len(found))
}

func jsonString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestDarwinRowsKeepOnlyPlainBundleIDs(t *testing.T) {
	out := "com.todesktop.230313mzl4w4u92\tCursor\t1\ncom.apple.TextEdit\tTextEdit\t0\nbad;id\tEvil\t0\ncom.apple.TextEdit\tTextEdit\t0\n\tNoID\t0\ncom.apple.Numbers\t\t0\nshort\n"
	got := parseDarwinRows(out)
	if len(got) != 2 || got[0].ID != "com.todesktop.230313mzl4w4u92" || !got[0].Default || got[1].Name != "TextEdit" || got[1].Default {
		t.Fatalf("rows: %+v", got)
	}
	if ranked := capEditors(got); ranked[0].Name != "Cursor" {
		t.Fatalf("default not first: %+v", ranked)
	}
}

func TestLooksTextReadsTheBytesNotTheName(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if !looksText(write("main.go", []byte("package main\n// héllo\n"))) {
		t.Error("a Go source is text")
	}
	if looksText(write("tool.txt", []byte("ab\x00cd"))) {
		t.Error("a NUL byte is not text, whatever the name says")
	}
	if looksText(write("bad.go", []byte{0xff, 0xfe, 0xfd, 'a', 'b'})) {
		t.Error("invalid UTF-8 is not text")
	}
	if looksText(filepath.Join(dir, "missing.go")) {
		t.Error("a missing file is not text")
	}
}

// TestDarwinDiscoveryAndLaunchOnThisMac runs only on a Mac. It asks Launch
// Services for a Go source's editors and, with CODEAF_EDITOR_LAUNCH_TEST=1 in
// a logged-in GUI session, starts the default one through the same launch
// plan the route uses and waits for that application to be running.
func TestDarwinDiscoveryAndLaunchOnThisMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS discovery")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := discoverDarwin(path)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	ranked := capEditors(found)
	t.Logf("editors for main.go: %+v", ranked)
	if len(ranked) == 0 || !ranked[0].Default {
		t.Fatalf("a text file on a Mac has at least TextEdit, default first: %+v", ranked)
	}
	for _, editor := range ranked {
		if !safeEditorID(editor.ID) || !strings.Contains(editor.ID, ".") {
			t.Fatalf("not a bundle id: %+v", editor)
		}
	}
	if os.Getenv("CODEAF_EDITOR_LAUNCH_TEST") != "1" {
		t.Log("set CODEAF_EDITOR_LAUNCH_TEST=1 in a GUI session to launch the default editor")
		return
	}
	running := func() string {
		out, _ := exec.Command("lsappinfo", "find", "bundleid="+ranked[0].ID).Output()
		return strings.TrimSpace(string(out))
	}
	before := running()
	if before != "" {
		t.Logf("%s was already running (%s); the launch hands it the file", ranked[0].ID, before)
	}
	if err := startEditorOS(ranked[0].ID, path); err != nil {
		t.Fatalf("launch %s: %v", ranked[0].ID, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if now := running(); now != "" {
			t.Logf("running: %s %s (before: %q)", ranked[0].ID, now, before)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("%s did not start", ranked[0].ID)
}

// TestDarwinOpenRouteStartsTheChosenEditor goes through the real routes on a
// Mac: GET /editors lists what Launch Services offers for a Go source, and
// POST /editors/open with TextEdit's id starts TextEdit. It runs only with
// CODEAF_EDITOR_LAUNCH_TEST=1 in a logged-in GUI session, because it opens a
// window on that screen.
func TestDarwinOpenRouteStartsTheChosenEditor(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("CODEAF_EDITOR_LAUNCH_TEST") != "1" {
		t.Skip("a Mac GUI session with CODEAF_EDITOR_LAUNCH_TEST=1")
	}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _, path := richFixture(t, func(c *Connection) {
		c.Local = true
		c.Welcome.Workspace = workspace
		c.StatPaths = func(paths []string) ([]remote.PathFact, error) {
			info, err := os.Stat(paths[0])
			return []remote.PathFact{{Path: paths[0], Exists: err == nil, Dir: err == nil && info.IsDir()}}, nil
		}
	})
	w := request(b, "GET", path+"/editors?path=main.go", "")
	var got editorList
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || !got.Open {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	t.Logf("route listed: %+v", got.Editors)
	const textEdit = "com.apple.TextEdit"
	if !editorKnown(got.Editors, textEdit) {
		t.Fatalf("TextEdit is not offered for a Go source: %+v", got.Editors)
	}
	running := func() string {
		out, _ := exec.Command("lsappinfo", "find", "bundleid="+textEdit).Output()
		return strings.TrimSpace(string(out))
	}
	before := running()
	w = request(b, "POST", path+"/editors/open", `{"path":"main.go","id":"`+textEdit+`"}`)
	if w.Code != 200 {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if now := running(); now != "" && now != before {
			t.Logf("TextEdit started by the route: %s", now)
			return
		}
		if before != "" {
			t.Logf("TextEdit was already running (%s); the route was accepted", before)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("TextEdit did not start")
}
