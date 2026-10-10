//go:build e2e

package e2e

// desktop_places_e2e_test.go proves the one claim every unit test in
// internal/desktopbridge can only imitate: a place's instruction, typed by a
// person into the desktop, reaches a REAL model and changes what it says.
//
// It drives the built product the way the desktop shell does — `codeaf
// desktop-bridge` as a child process, its loopback HTTP door, a bearer token
// read from the one line it prints — and nothing in between is a fake. The
// build is never done here (`make build` is the door), the key is resolved
// through [liveKey], and it SKIPS green without either.
//
//	go test -tags e2e -run TestDesktopPlaceInstructionReachesTheModel -v ./internal/e2e/

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// desktopPlacesModel is the one model every role of this lane rides, so a run
// costs a fraction of a cent and no seat falls back to something else.
const desktopPlacesModel = "deepseek/deepseek-v4.1-flash"

// placeInstruction is the sentence the place carries; the assertion below counts
// the words the model answers with.
const placeInstruction = "Always answer in exactly three words"

// bridgeDoor is a running desktop-bridge and the two facts a client needs.
type bridgeDoor struct {
	t     *testing.T
	url   string
	token string
}

// startDesktopBridge launches the built binary on a throwaway home and returns
// once it has printed its handshake line. The child is killed with the test.
func startDesktopBridge(t *testing.T) *bridgeDoor {
	t.Helper()
	bin := binary(t)
	w := newWorld(t) // key, throwaway CODEAF_HOME, profile — all via the product's own roads
	pinEveryRole(t)
	cmd := exec.Command(bin, "desktop-bridge", "--listen", "127.0.0.1:0", "--workspace", t.TempDir())
	cmd.Env = append(os.Environ(), "CODEAF_HOME="+w.home, "CODEAF_DESKTOP_TOKEN=")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start desktop-bridge: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(out).ReadBytes('\n')
	if err != nil {
		t.Fatalf("no handshake line from desktop-bridge: %v\n%s", err, stderr.String())
	}
	var hello struct{ URL, Token string }
	if err := json.Unmarshal(line, &hello); err != nil || hello.URL == "" {
		t.Fatalf("bad handshake line (%v)", err) // the line holds the token: never echoed
	}
	return &bridgeDoor{t: t, url: hello.URL, token: hello.Token}
}

// pinEveryRole writes the lane's model into the talk row and every tier seat of
// the throwaway profile that [newWorld] just made current.
func pinEveryRole(t *testing.T) {
	t.Helper()
	if err := config.WriteChatModel("", desktopPlacesModel); err != nil {
		t.Fatalf("write the talk model: %v", err)
	}
	registry := config.NewSettings(config.SettingsOptions{})
	for _, key := range []string{config.KeyTierReflexModel, config.KeyTierLowModel, config.KeyTierWorkerModel, config.KeyTierHighModel, config.KeyTierMastermindModel} {
		row, ok := registry.Row(key)
		if !ok {
			t.Fatalf("the settings registry has no row %q", key)
		}
		if err := row.Apply(desktopPlacesModel); err != nil {
			t.Fatalf("pin %s: %v", key, err)
		}
	}
}

// call performs one authenticated request and decodes a JSON answer into out.
func (d *bridgeDoor) call(method, path string, body, out any) int {
	d.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, d.url+"/api/engine"+path, reader)
	if err != nil {
		d.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		d.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && res.StatusCode < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			d.t.Fatalf("%s %s: undecodable answer: %v", method, path, err)
		}
	} else if res.StatusCode >= 300 {
		d.t.Logf("%s %s -> %d %s", method, path, res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return res.StatusCode
}

type desktopSnapshot struct {
	Running bool
	Entries []struct{ Role, Text string }
}

func TestDesktopPlaceInstructionReachesTheModel(t *testing.T) {
	door := startDesktopBridge(t)

	var created struct{ Place struct{ ID string } }
	if code := door.call("POST", "/places", map[string]any{"name": "Terse", "instructions": placeInstruction}, &created); code != 200 || created.Place.ID == "" {
		t.Fatalf("create place: status %d", code)
	}
	placeID := created.Place.ID

	var chat struct{ ID string }
	if code := door.call("POST", "/sessions", map[string]any{"placeId": placeID}, &chat); code != 200 || chat.ID == "" {
		t.Fatalf("open a chat in the place: status %d", code)
	}
	base := "/sessions/" + chat.ID

	if code := door.call("POST", base+"/turn", map[string]any{"text": "What colour is a ripe banana?"}, nil); code >= 300 {
		t.Fatalf("send the question: status %d", code)
	}

	// The turn runs on the engine; poll the transcript until it has stopped and
	// an assistant entry with words exists.
	var answer string
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var snap desktopSnapshot
		door.call("GET", base, nil, &snap)
		if !snap.Running {
			for _, e := range snap.Entries {
				if e.Role == "assistant" && strings.TrimSpace(e.Text) != "" {
					answer = strings.TrimSpace(e.Text)
				}
			}
			if answer != "" {
				break
			}
		}
		time.Sleep(time.Second)
	}
	if answer == "" {
		t.Fatal("no assistant answer arrived before the deadline")
	}
	t.Logf("assistant said: %q", answer)
	if words := strings.Fields(answer); len(words) != 3 {
		t.Fatalf("the place said three words; the model answered with %d: %q", len(words), answer)
	}

	var using struct {
		Engine struct{ Places bool }
		Bundle struct {
			Places       []struct{ ID, Name string }
			Instructions []struct{ Text string }
		}
	}
	if code := door.call("GET", base+"/using", nil, &using); code != 200 {
		t.Fatalf("using: status %d", code)
	}
	if !using.Engine.Places {
		t.Fatal("the engine reports it does not read the place graph")
	}
	listed := false
	for _, p := range using.Bundle.Places {
		listed = listed || p.ID == placeID
	}
	if !listed {
		t.Fatalf("/using does not list the place %s: %+v", placeID, using.Bundle.Places)
	}
}
