package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// A CANVAS'S PICTURE MAKER IS KEPT IN THE PROFILE EVERY OTHER SETTING IS KEPT
// IN, WHEN NOTHING MOVED THE PROFILE. An ordinary install never sets
// CODEAF_PROFILE_DIR; its profile is the state root's own. #2849 joined the
// per-canvas store onto that empty variable, so pictures.maker.set for one
// canvas failed with "mkdir : no such file or directory" on every normal
// install while Everywhere worked, and the reader then saw nothing either.
// This drives the desk's own door, the registered app server, with the
// variable unset, and asks the same door back.
func TestAppACanvasPictureMakerIsKeptWithNoProfileVariable(t *testing.T) {
	root := resolvedTempDir(t)
	state := filepath.Join(root, "state")
	t.Setenv(home.EnvVar, state)
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("AFORGE_PROFILE_DIR", "") // legacy-name
	t.Setenv("CODEAF_IMAGE_MODEL", "")
	t.Setenv("AFORGE_IMAGE_MODEL", "") // legacy-name
	for _, name := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
	}
	canvas := filepath.Join(root, "canvases", "spring")
	if err := os.MkdirAll(canvas, 0o700); err != nil {
		t.Fatal(err)
	}

	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	next := 0
	call := func(method string, params any) appwire.PictureMakers {
		t.Helper()
		next++
		encoded, _ := json.Marshal(params)
		if _, err := io.WriteString(inW, `{"id":`+strconv.Itoa(next)+`,"method":"`+method+`","params":`+string(encoded)+"}\n"); err != nil {
			t.Fatal(err)
		}
		var answer struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		for scanner.Scan() {
			if err := json.Unmarshal(scanner.Bytes(), &answer); err == nil && answer.ID == next {
				break
			}
		}
		if answer.Error != nil {
			t.Fatalf("%s refused: %+v", method, *answer.Error)
		}
		var got appwire.PictureMakers
		if err := json.Unmarshal(answer.Result, &got); err != nil {
			t.Fatalf("%s result %s: %v", method, answer.Result, err)
		}
		return got
	}

	set := call("pictures.maker.set", map[string]string{"scope": "canvas", "workspace": canvas, "model": "seed/pro"})
	if set.Canvas == nil || set.Canvas.Model != "seed/pro" || set.Canvas.Folder != canvas {
		t.Fatalf("the canvas's choice is not in the answer: %+v", set.Canvas)
	}
	if _, err := os.Stat(home.Join("canvas-pictures.json")); err != nil {
		t.Fatalf("the choice is not kept beside config.json in the state root: %v", err)
	}
	// Asked again, from a folder inside the canvas, the same door reads it back.
	listed := call("pictures.makers", map[string]string{"workspace": filepath.Join(canvas, "notes")})
	if listed.Canvas == nil || listed.Canvas.Model != "seed/pro" || listed.Canvas.Folder != canvas {
		t.Fatalf("the canvas's choice is not read back: %+v", listed.Canvas)
	}
	// Everywhere was already right; it stays in the same profile.
	call("pictures.maker.set", map[string]string{"scope": "everywhere", "model": "vendor/other"})
	if model, _ := config.EverywherePictureMaker(""); model != "vendor/other" {
		t.Fatalf("Everywhere = %q", model)
	}
	// Giving the canvas back to Everywhere clears it.
	cleared := call("pictures.maker.set", map[string]string{"scope": "canvas", "workspace": canvas, "model": ""})
	if cleared.Canvas == nil || cleared.Canvas.Model != "" || cleared.Everywhere.Model != "vendor/other" {
		t.Fatalf("cleared = canvas %+v everywhere %+v", cleared.Canvas, cleared.Everywhere)
	}
}
