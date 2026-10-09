package desktopbridge

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestWorkViewRoutesRelayTheEngine(t *testing.T) {
	var asked []string
	b, _, path := richFixture(t, func(c *Connection) {
		c.Local = true
		c.ReadText = func(p string) (remote.TextFile, error) {
			switch p {
			case "a.go":
				return remote.TextFile{Path: "a.go", Name: "a.go", Text: "x"}, nil
			case "":
				return remote.TextFile{}, errors.New("engine: nothing was named")
			}
			return remote.TextFile{}, errors.New("engine: " + p + " is outside this conversation's workspace")
		}
		c.FindFiles = func(q string, n int) (remote.FoundFiles, error) {
			asked = append(asked, q)
			return remote.FoundFiles{Files: []remote.FoundFile{{Path: "a/lexer.go", Name: "lexer.go", Dir: "a"}}}, nil
		}
		c.DiffChanges = func(p []string) (remote.ChangedFiles, error) {
			asked = append(asked, strings.Join(p, ","))
			return remote.ChangedFiles{Git: true, Files: []remote.ChangedFile{{Path: "a.go", Added: 12, Deleted: 3}}}, nil
		}
		c.DiffFile = func(p string) (remote.FileDiff, error) {
			return remote.FileDiff{Path: p, Added: 12, Deleted: 3, Hunks: []remote.DiffHunk{}}, nil
		}
		c.StatPaths = func(p []string) ([]remote.PathFact, error) {
			return []remote.PathFact{{Path: p[0], Exists: strings.HasSuffix(p[0], "a.go")}}, nil
		}
	})
	if w := request(b, "GET", path+"/files/text?path=a.go", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"text":"x"`) {
		t.Fatalf("text: %d %s", w.Code, w.Body.String())
	}
	if w := request(b, "GET", path+"/files/text?path=/etc/passwd", ""); w.Code != 403 {
		t.Fatalf("outside: %d", w.Code)
	}
	if w := request(b, "GET", path+"/files/text", ""); w.Code != 400 {
		t.Fatalf("blank: %d", w.Code)
	}
	if w := request(b, "GET", path+"/files/find?q=lex&limit=5", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "lexer.go") || asked[0] != "lex" {
		t.Fatalf("find: %d %s", w.Code, w.Body.String())
	}
	if w := request(b, "GET", path+"/changes?path=a.go&path=b.go", ""); w.Code != 200 || asked[1] != "a.go,b.go" {
		t.Fatalf("changes: %d %s %v", w.Code, w.Body.String(), asked)
	}
	if w := request(b, "GET", path+"/diff?path=a.go", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"added":12`) {
		t.Fatalf("diff: %d %s", w.Code, w.Body.String())
	}
	var target EditorTarget
	w := request(b, "GET", path+"/files/locate?path=a.go", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &target) != nil || !target.Local || !strings.HasSuffix(target.Abs, "/a.go") || target.Host == "" {
		t.Fatalf("locate: %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []string{"../x.go", "/etc/passwd", "gone.go"} {
		if w := request(b, "GET", path+"/files/locate?path="+bad, ""); w.Code != 403 && w.Code != 404 {
			t.Errorf("locate %q: %d", bad, w.Code)
		}
	}
	if w := request(b, "POST", path+"/diff?path=a.go", "{}"); w.Code != 405 {
		t.Fatalf("method: %d", w.Code)
	}
}

func TestWorkViewRoutesRefuseWhenEngineCannot(t *testing.T) {
	b, _, path := richFixture(t, func(c *Connection) {})
	for _, route := range []string{"/files/text?path=a", "/files/find?q=a", "/changes", "/diff?path=a"} {
		if w := request(b, "GET", path+route, ""); w.Code != 409 {
			t.Errorf("%s: %d", route, w.Code)
		}
	}
}
