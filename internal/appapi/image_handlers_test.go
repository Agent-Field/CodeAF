package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestAppImageHandler(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	sessionDir := filepath.Join(base, "sessions", "mine")
	otherSession := filepath.Join(base, "sessions", "other")
	sibling := filepath.Join(base, "workspace-other")
	for _, dir := range []string{workspace, sessionDir, otherSession, sibling} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	imageData := appImageFixtures(t)
	files := map[string][]byte{
		filepath.Join(workspace, "plot.png"):       imageData["image/png"],
		filepath.Join(workspace, "plot.PNG"):       imageData["image/png"],
		filepath.Join(workspace, "picture.jpg"):    imageData["image/jpeg"],
		filepath.Join(workspace, "picture.jpeg"):   imageData["image/jpeg"],
		filepath.Join(workspace, "picture.gif"):    imageData["image/gif"],
		filepath.Join(workspace, "picture.webp"):   imageData["image/webp"],
		filepath.Join(workspace, "text.txt"):       []byte("ordinary text"),
		filepath.Join(workspace, "disguised.txt"):  imageData["image/png"],
		filepath.Join(workspace, "fake.png"):       []byte("ordinary text"),
		filepath.Join(workspace, "mismatch.png"):   imageData["image/jpeg"],
		filepath.Join(workspace, "picture.svg"):    []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		filepath.Join(workspace, "disguised.png"):  []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		filepath.Join(sessionDir, "saved.png"):     imageData["image/png"],
		filepath.Join(otherSession, "saved.png"):   imageData["image/png"],
		filepath.Join(sibling, "plot.png"):         imageData["image/png"],
		filepath.Join(base, "outside.png"):         imageData["image/png"],
		filepath.Join(workspace, "at-limit.png"):   imageData["image/png"],
		filepath.Join(workspace, "over-limit.png"): imageData["image/png"],
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, size := range map[string]int64{"at-limit.png": appImageLimit, "over-limit.png": appImageLimit + 1} {
		if err := os.Truncate(filepath.Join(workspace, name), size); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{
		"outside-link.png":         filepath.Join(base, "outside.png"),
		"outside-missing-link.png": filepath.Join(base, "missing.png"),
		"outside-directory":        sibling,
		"inside-link.png":          filepath.Join(workspace, "plot.png"),
		"session-link.png":         filepath.Join(sessionDir, "saved.png"),
		"text-link.png":            filepath.Join(workspace, "disguised.txt"),
	} {
		if err := os.Symlink(target, filepath.Join(workspace, name)); err != nil {
			t.Fatal(err)
		}
	}
	workspaceLink := filepath.Join(base, "workspace-link")
	if err := os.Symlink(workspace, workspaceLink); err != nil {
		t.Fatal(err)
	}
	registry := NewAgentRegistry(filepath.Join(base, "home"), func(context.Context, AgentOpenRequest) (AgentConnection, error) {
		t.Fatal("reading an image must not open an engine connection")
		return AgentConnection{}, nil
	})
	err := registry.disk(func(d *agentDisk) error {
		d.Records["mine"] = agentRecord{Workspace: workspace, SessionFile: filepath.Join(sessionDir, "transcript.jsonl")}
		d.Records["linked"] = agentRecord{Workspace: workspaceLink}
		d.Records["no-workspace"] = agentRecord{SessionFile: filepath.Join(sessionDir, "transcript.jsonl")}
		d.Records["no-roots"] = agentRecord{}
		return nil
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	server := New(testToken, Options{Agents: registry})
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		name, agent, path string
		noAuth            bool
		status            int
		contentType       string
	}{
		{name: "workspace absolute", path: filepath.Join(workspace, "plot.png"), status: 200, contentType: "image/png"},
		{name: "workspace relative", path: "plot.png", status: 200, contentType: "image/png"},
		{name: "uppercase extension", path: "plot.PNG", status: 200, contentType: "image/png"},
		{name: "jpeg short extension", path: "picture.jpg", status: 200, contentType: "image/jpeg"},
		{name: "jpeg", path: "picture.jpeg", status: 200, contentType: "image/jpeg"},
		{name: "gif", path: "picture.gif", status: 200, contentType: "image/gif"},
		{name: "webp", path: "picture.webp", status: 200, contentType: "image/webp"},
		{name: "session absolute", path: filepath.Join(sessionDir, "saved.png"), status: 200, contentType: "image/png"},
		{name: "session without workspace", agent: "no-workspace", path: filepath.Join(sessionDir, "saved.png"), status: 200, contentType: "image/png"},
		{name: "parent escape", path: "../outside.png", status: 403},
		{name: "missing parent escape", path: "../missing.png", status: 403},
		{name: "symlink escape", path: "outside-link.png", status: 403},
		{name: "missing symlink target outside", path: "outside-missing-link.png", status: 403},
		{name: "missing file through directory symlink outside", path: "outside-directory/missing.png", status: 403},
		{name: "prefix sibling", path: filepath.Join(sibling, "plot.png"), status: 403},
		{name: "another session", path: filepath.Join(otherSession, "saved.png"), status: 403},
		{name: "symlink inside workspace", path: "inside-link.png", status: 200, contentType: "image/png"},
		{name: "symlink inside session", path: "session-link.png", status: 200, contentType: "image/png"},
		{name: "symlinked workspace", agent: "linked", path: "plot.png", status: 200, contentType: "image/png"},
		{name: "text", path: "text.txt", status: 415},
		{name: "image with text extension", path: "disguised.txt", status: 415},
		{name: "symlink to text extension", path: "text-link.png", status: 415},
		{name: "png with text bytes", path: "fake.png", status: 415},
		{name: "extension does not match bytes", path: "mismatch.png", status: 415},
		{name: "svg", path: "picture.svg", status: 415},
		{name: "svg with png extension", path: "disguised.png", status: 415},
		{name: "directory", path: workspace, status: 415},
		{name: "at size limit", path: "at-limit.png", status: 200, contentType: "image/png"},
		{name: "over size limit", path: "over-limit.png", status: 413},
		{name: "missing file", path: "missing.png", status: 404},
		{name: "missing parent directory", path: "missing-directory/missing.png", status: 404},
		{name: "missing path", status: 422},
		{name: "unknown agent", agent: "unknown", path: "plot.png", status: 404},
		{name: "no authentication", path: "plot.png", noAuth: true, status: 401},
		{name: "relative without workspace", agent: "no-workspace", path: "plot.png", status: 403},
		{name: "no implicit root", agent: "no-roots", path: filepath.Join(workspace, "plot.png"), status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.agent
			if id == "" {
				id = "mine"
			}
			headers := bearer()
			if tc.noAuth {
				headers = nil
			}
			path := "/app/v1/agents/" + id + "/image?path=" + url.QueryEscape(tc.path)
			w := request(t, server, http.MethodGet, path, "", headers, "127.0.0.1:4000")
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %.500s", w.Code, tc.status, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("image response may be cached")
			}
			if tc.status != 200 {
				var body ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error == "" || body.Reason == "" {
					t.Fatalf("missing JSON error: %s (%v)", w.Body, err)
				}
				return
			}
			if w.Header().Get("Content-Type") != tc.contentType || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("image headers: %v", w.Header())
			}
			if tc.name == "at size limit" {
				if w.Body.Len() != appImageLimit {
					t.Fatalf("size = %d, want %d", w.Body.Len(), appImageLimit)
				}
			} else if !bytes.Equal(w.Body.Bytes(), imageData[tc.contentType]) {
				t.Fatal("response changed image bytes")
			}
		})
	}
}

func appImageFixtures(t *testing.T) map[string][]byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	result := map[string][]byte{}
	for kind, encode := range map[string]func(*bytes.Buffer) error{
		"image/png":  func(b *bytes.Buffer) error { return png.Encode(b, canvas) },
		"image/jpeg": func(b *bytes.Buffer) error { return jpeg.Encode(b, canvas, nil) },
		"image/gif":  func(b *bytes.Buffer) error { return gif.Encode(b, canvas, nil) },
	} {
		var data bytes.Buffer
		if err := encode(&data); err != nil {
			t.Fatal(err)
		}
		result[kind] = data.Bytes()
	}
	// The handler sniffs the WebP signature rather than decoding its pixels.
	result["image/webp"] = []byte("RIFF\x0c\x00\x00\x00WEBPVP8 \x00\x00\x00\x00")
	return result
}
