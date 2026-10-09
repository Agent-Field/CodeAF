package desktopbridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
		if name == "gtk-launch" {
			return "/usr/bin/gtk-launch", nil
		}
		return "", errors.New("absent")
	}
	t.Cleanup(func() { lookPath = prev })
	bin, args, err := launchPlan("code.desktop", "/project/a.go")
	if err != nil || bin != "/usr/bin/gtk-launch" || len(args) != 2 || args[0] != "code.desktop" || args[1] != "/project/a.go" {
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
	found := scanDesktops([]string{apps}, "text/plain", "code.desktop")
	found = capEditors(found)
	if len(found) != 2 || found[0].ID != "code.desktop" || !found[0].Default || found[0].Name != "Code" {
		t.Fatalf("parsed: %+v", found)
	}
	if found[1].Name == "Hidden" || strings.Contains(found[0].Name, "bash") || strings.Contains(found[0].ID, "Exec") {
		t.Fatalf("exec or hidden leaked: %+v", found)
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
