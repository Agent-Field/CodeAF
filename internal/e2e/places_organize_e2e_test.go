//go:build e2e

package e2e

// places_organize_e2e_test.go drives Organize now through the real desktop
// bridge: a demo home plus sixty synthetic chats, one plan, one receipt, undo,
// a typed guess, a month lens, and a hand move. The model on every journal
// line is the lane's model. It SKIPS without a key or a built binary.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestPlacesOrganizeAddOnGuessLensAndUndo(t *testing.T) {
	liveKey(t)
	bin := binary(t)
	root := repoRoot(t)

	demo := t.TempDir()
	// go run keeps the program a literal. A path in a variable is a dynamic
	// exec, which the host-guard law refuses even when the program is not codeaf.
	seeded := exec.Command("go", "run", "./cmd/codeaf-demo-home", "--into", demo)
	seeded.Dir = root
	if out, err := seeded.CombinedOutput(); err != nil {
		t.Fatalf("seed the demo home: %v\n%s", err, out)
	}
	state := filepath.Join(demo, ".codeaf")
	if raw, err := os.ReadFile(personConfig()); err == nil {
		if err := os.WriteFile(filepath.Join(state, "config.json"), raw, 0o600); err != nil {
			t.Fatalf("copy the profile: %v", err)
		}
	}
	t.Setenv(home.EnvVar, state)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	if err := config.WriteAPIKey("", liveKey(t)); err != nil {
		t.Fatalf("write the key: %v", err)
	}
	pinEveryRole(t)

	chatID := writeSyntheticChats(t, 60)
	door := startOrganizerBridge(t, bin, state)

	wall := time.Now()
	var started struct {
		Run     string `json:"run"`
		Started bool   `json:"started"`
	}
	if code := door.call("POST", "/places/organize", map[string]any{"mode": "add-on"}, &started); code != 200 || !started.Started {
		t.Fatalf("organize now: status %d", code)
	}
	var snap organizeSnap
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		snap = organizeSnap{}
		if code := door.call("GET", "/places/organize", nil, &snap); code != 200 {
			t.Fatalf("organize status %d", code)
		}
		if !snap.Running && snap.Plan.ID != "" {
			break
		}
		if !snap.Running && snap.Last.Outcome != "" && snap.Plan.ID == "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	elapsed := time.Since(wall)
	if snap.Running || snap.Plan.ID == "" {
		t.Fatalf("no plan after %s (running %v outcome %q summary %q)", elapsed.Round(time.Second), snap.Running, snap.Last.Outcome, snap.Last.Summary)
	}
	var accepted struct {
		Receipt  string `json:"receipt"`
		Receipts []struct {
			ID string `json:"id"`
		} `json:"receipts"`
	}
	if code := door.call("POST", "/places/proposals/"+snap.Plan.ID+"/accept", map[string]any{}, &accepted); code != 200 || accepted.Receipt == "" || len(accepted.Receipts) != 1 || accepted.Receipts[0].ID != accepted.Receipt {
		t.Fatalf("accept: receipt %q count %d", accepted.Receipt, len(accepted.Receipts))
	}
	var undone struct {
		Undone int `json:"undone"`
	}
	if code := door.call("POST", "/places/undo", map[string]any{"token": accepted.Receipt}, &undone); code != 200 || undone.Undone < 1 {
		t.Fatalf("undo: status not 200 or nothing undone (%d)", undone.Undone)
	}

	var created struct {
		Place struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"place"`
	}
	if code := door.call("POST", "/places", map[string]any{"name": "Kiln"}, &created); code != 200 || created.Place.ID == "" {
		t.Fatalf("create place: status %d", code)
	}
	folder := t.TempDir()
	if code := door.call("POST", "/places/"+created.Place.ID+"/sources", map[string]any{"kind": "folder", "ref": folder}, nil); code != 200 {
		t.Fatalf("folder source: status not 200")
	}
	// The first guess pays for the local table if it was not already warm.
	door.call("POST", "/places/guess", map[string]any{"draft": "kiln notes for the week", "workspace": folder}, nil)
	guessStart := time.Now()
	var guess struct {
		Place string `json:"place"`
		Name  string `json:"name"`
	}
	if code := door.call("POST", "/places/guess", map[string]any{"draft": "kiln notes for the week", "workspace": folder}, &guess); code != 200 || guess.Place == "" || guess.Name == "" {
		t.Fatalf("guess %+v", guess)
	}
	if guessTook := time.Since(guessStart); guessTook >= placeorg.GuessBudget {
		t.Fatalf("guess took %s, budget %s", guessTook, placeorg.GuessBudget)
	}

	var steer struct {
		Understood bool `json:"understood"`
	}
	if code := door.call("POST", "/places/steer", map[string]any{"scope": created.Place.ID, "sentence": "organise this by month"}, &steer); code != 200 || !steer.Understood {
		t.Fatalf("steer understood %v", steer.Understood)
	}
	var lenses struct {
		Lenses []struct {
			Name string `json:"name"`
		} `json:"lenses"`
	}
	if code := door.call("GET", "/places/"+created.Place.ID+"/lenses", nil, &lenses); code != 200 {
		t.Fatalf("lenses status not 200")
	}
	sawMonth := false
	for _, lens := range lenses.Lenses {
		if lens.Name == "by month" {
			sawMonth = true
		}
	}
	if !sawMonth {
		t.Fatalf("lenses %+v", lenses.Lenses)
	}

	if code := door.call("POST", "/places/"+created.Place.ID+"/members", map[string]any{"chats": []string{chatID}, "addedBy": "you"}, nil); code != 200 {
		t.Fatalf("hand move status not 200")
	}
	journal := readOrganizeJournal(t, filepath.Join(state, "desktop", "places-journal.jsonl"))
	if len(journal) == 0 {
		t.Fatal("the journal is empty")
	}
	exemplar := false
	for _, line := range journal {
		if line.Model != desktopPlacesModel {
			t.Fatalf("journal line %s model %q", line.Kind, line.Model)
		}
		if line.Pick != nil && line.Pick.Model != "" && line.Pick.Model != desktopPlacesModel {
			t.Fatalf("pick model %q", line.Pick.Model)
		}
		if line.Kind == string(placeorg.EntryCorrection) && line.Op != nil && line.Op.Kind == string(placeorg.OpExemplar) {
			exemplar = true
		}
	}
	if !exemplar {
		t.Fatal("a hand move did not journal an exemplar")
	}
	t.Logf("places organize e2e: wall %s cost $%.4f calls %d chats %d model %s",
		elapsed.Round(time.Millisecond), snap.Last.Spent.USD, snap.Last.Spent.Calls, snap.Last.Chats, desktopPlacesModel)
}

type organizeSnap struct {
	Running bool `json:"running"`
	Plan    struct {
		ID string `json:"id"`
	} `json:"plan"`
	Last struct {
		Outcome string `json:"outcome"`
		Summary string `json:"summary"`
		Chats   int    `json:"chats"`
		Spent   struct {
			Calls int     `json:"Calls"`
			USD   float64 `json:"USD"`
		} `json:"spent"`
	} `json:"last"`
}

type organizeJournalLine struct {
	Kind  string `json:"kind"`
	Model string `json:"Model"`
	Pick  *struct {
		Model string `json:"Model"`
	} `json:"pick"`
	Op *struct {
		Kind string `json:"kind"`
	} `json:"op"`
}

func readOrganizeJournal(t *testing.T, path string) []organizeJournalLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []organizeJournalLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var line organizeJournalLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("journal line: %v", err)
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func startOrganizerBridge(t *testing.T, bin, state string) *bridgeDoor {
	t.Helper()
	cmd := guardedCommand(t, context.Background(), state, append(os.Environ(), home.EnvVar+"="+state, "CODEAF_DESKTOP_TOKEN="), bin, "desktop-bridge", "--listen", "127.0.0.1:0", "--workspace", t.TempDir())
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
		t.Fatalf("bad handshake line (%v)", err)
	}
	return &bridgeDoor{t: t, url: hello.URL, token: hello.Token}
}

// writeSyntheticChats adds n conversations in four topics beside the demo
// home and returns one id a later hand move can file. The openings are long
// on purpose: a short first message is set aside and never enters a place.
func writeSyntheticChats(t *testing.T, n int) string {
	t.Helper()
	topics := []string{"kiln", "orchard", "harbor", "ledger"}
	workspace := filepath.Join(t.TempDir(), "synthetic-notes")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	encoded := strings.ReplaceAll(filepath.Clean(workspace), string(filepath.Separator), "-")
	encoded = strings.ReplaceAll(encoded, ":", "-")
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	bucket := filepath.Join(session.PlacesRoot(), encoded)
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		t.Fatal(err)
	}
	var first string
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		topic := topics[i%len(topics)]
		id := session.NewSessionID()
		if first == "" {
			first = id
		}
		dir := filepath.Join(bucket, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		opening := fmt.Sprintf("Please keep a running record of the %s notes for item %d, including the measurements, the exceptions, and what changed since the previous entry in this series.", topic, i)
		body := fmt.Sprintf(`{"type":"session","version":1,"id":%q,"cwd":%q,"timestamp":%q}
{"type":"message","role":"user","content":%q,"timestamp":%q}
{"type":"message","role":"assistant","content":"Recorded.","timestamp":%q}
`, id, workspace, now.Format(time.RFC3339Nano), opening, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(dir, session.Meta{
			ID: id, Title: fmt.Sprintf("%s notes %d", topic, i), Workspace: workspace,
			Created: now, LastUserAt: now, Tokens: 120,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return first
}
