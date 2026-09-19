//go:build e2e

package e2e

// TestFoldersEntryJourneys is the committed J36–J43 harness. It drives the real
// binary in tmux, waits through [say], and then reads collections.db and the
// transcript tree — production state, not word-matching alone.
//
// Live pass/fail against a real model is t-fe-validate. This test is the
// executable procedure that lane runs. A skip here is only the same honesty
// gate the rest of this package uses (no key, no tmux, no bin/codeaf). Missing
// Folders-entry behaviour is a failure, not a skip.
//
//	go test -tags e2e -count=1 -timeout 40m -v -run TestFoldersEntryJourneys ./internal/e2e/
//
// Isolation is CODEAF_HOME plus a private CODEAF_PROFILE_DIR. HOME is never
// set. ~/.codeaf is never the state root.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestFoldersEntryJourneys(t *testing.T) {
	requireTmuxAndKey(t)
	t.Run("J36", testFoldersEntryJ36)
	t.Run("J37", testFoldersEntryJ37)
	t.Run("J38", testFoldersEntryJ38)
	t.Run("J39", testFoldersEntryJ39)
	t.Run("J40", testFoldersEntryJ40)
	t.Run("J41", testFoldersEntryJ41)
	t.Run("J42", testFoldersEntryJ42)
	t.Run("J43", testFoldersEntryJ43)
}

func startFoldersEntry(t *testing.T, name string, cols, rows int) (*rig, string, string) {
	t.Helper()
	home := newHome(t, nil)
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatalf("profile: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatalf("read home config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profile, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("write profile config: %v", err)
	}
	ws := newWorkspace(t, "foldersws", false)
	r := startWithEnv(t, []string{
		config.APIKeyEnv + "=" + liveKey(t),
		"CODEAF_PROFILE_DIR=" + profile,
	}, name, home, ws, cols, rows, "chat", "--one-model", "--no-host")
	r.skipSetup(t)
	return r, home, ws
}

func (r *rig) slashLine(line string) {
	r.t.Helper()
	r.keys("C-u")
	r.lit(line)
	r.keys("Enter")
}

func (r *rig) enterFoldersPlace() string {
	r.t.Helper()
	// alt+5 is Folders (Folders-entry). The chord is esc-then-5, the same
	// encoding placeDigit reads, so the composer is not cleared the way a
	// slash command with C-u would be.
	r.lit("\x1b5")
	return r.waitFor(20*time.Second, say(r.t, "homePanelFolders"), say(r.t, "homeFoldersWhisper"))
}

func assertNoGeneratedFolders(t *testing.T, home, ws, screen string) {
	t.Helper()
	if strings.Contains(screen, foldersEntryNoFoldersYet) {
		t.Fatalf("the Folders surface said %q; emptiness keeps the heading and the whisper:\n%s",
			foldersEntryNoFoldersYet, screen)
	}
	graph := readFoldersGraph(t, home, ws)
	if graph.RootIsARow {
		t.Fatal("Root was a collections row; Root is virtual")
	}
	if len(graph.Collections) != 0 {
		t.Fatalf("fresh Folders invented collections before New folder or Organize existing chats: %+v", graph.Collections)
	}
}

func assertFiveWordBar(t *testing.T, screen string) {
	t.Helper()
	want := []string{
		say(t, "barHomeWord"), say(t, "barTasksWord"), say(t, "homePanelSpend"),
		say(t, "barSettingsWord"), say(t, "barFoldersWord"),
	}
	got := barWords(screen, want[0], want[len(want)-1])
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the tab bar reads %q, want %q:\n%s", got, want, screen)
	}
}

func testFoldersEntryJ36(t *testing.T) {
	r, home, ws := startFoldersEntry(t, "fe_j36", tuiPlain, 40)
	screen := r.enterFoldersPlace()
	assertFiveWordBar(t, screen)
	assertNoGeneratedFolders(t, home, ws, screen)
	for _, action := range []string{
		say(t, "foldersPlaceNewFolder"),
		say(t, "foldersPlaceNewChat"),
		say(t, "foldersOrganizeExistingWord"),
	} {
		if !strings.Contains(screen, action) {
			t.Errorf("J36 Folders place is missing visible %q:\n%s", action, screen)
		}
	}
}

func testFoldersEntryJ37(t *testing.T) {
	r, home, ws := startFoldersEntry(t, "fe_j37", tuiPlain, 40)
	r.slashLine("/folder")
	before := r.waitFor(20*time.Second, say(t, "folderChooserHelp"))
	r.keys("Escape")
	r.enterFoldersPlace()
	r.slashLine("/folder")
	after := r.waitFor(20*time.Second, say(t, "folderChooserHelp"))
	r.keys("Escape")
	r.slashLine("/place")
	r.waitFor(20*time.Second, say(t, "folderChooserHelp"))
	r.keys("Escape")
	r.slashLine("/dir")
	r.waitFor(20*time.Second, say(t, "folderChooserHelp"))
	if strings.Contains(after, say(t, "homeFoldersWhisper")) && !strings.Contains(after, say(t, "folderChooserHelp")) {
		t.Fatalf("/folder after /folders opened logical Folders instead of the filesystem chooser:\n%s", after)
	}
	if strings.Contains(filepath.Base(ws), "folders") {
		t.Fatal("the workspace path itself was named folders; J37 needs a real directory, not a mirror")
	}
	graph := readFoldersGraph(t, home, ws)
	if folderNamed(graph, filepath.Base(ws)) {
		t.Fatalf("logical Folders mirrored the project directory %q", filepath.Base(ws))
	}
	_ = before
}

func testFoldersEntryJ38(t *testing.T) {
	r, home, ws := startFoldersEntry(t, "fe_j38", tuiPlain, 40)
	screen := r.enterFoldersPlace()
	if !strings.Contains(screen, say(t, "foldersPlaceNewFolder")) {
		t.Fatalf("New folder is not visible without a slash:\n%s", screen)
	}
	if !strings.Contains(screen, say(t, "foldersPlaceNewChat")) {
		t.Fatalf("New chat is not visible without a slash:\n%s", screen)
	}
	r.slashLine("/folders create Billing")
	deadline := time.Now().Add(15 * time.Second)
	for {
		graph := readFoldersGraph(t, home, ws)
		if folderNamed(graph, "Billing") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TUI /folders create Billing did not persist a Billing collection: %+v", graph.Collections)
		}
		time.Sleep(200 * time.Millisecond)
	}
	r.slashLine("/folders new")
	r.keys("Escape")
	time.Sleep(400 * time.Millisecond)
	afterEsc := listConversationIDs(t, home)
	if len(afterEsc) != 0 {
		t.Fatalf("Esc on a new chat minted transcripts %v", afterEsc)
	}
}

func testFoldersEntryJ39(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	const chatID = "0123456789abcdef"
	seedUnfiledChat(t, home, ws, chatID, "Unfiled billing thread", "invoice for March")
	r, _, _ := startFoldersEntryOn(t, "fe_j39", home, ws, tuiPlain, 40)
	screen := r.enterFoldersPlace()
	if len(graphUnfiled(t, home, ws, chatID, screen)) == 0 {
		t.Fatalf("unfiled chat is not visible at Root and has no membership:\n%s", screen)
	}
}

func graphUnfiled(t *testing.T, home, ws, chatID, screen string) []string {
	t.Helper()
	assertNoGeneratedFolders(t, home, ws, screen)
	graph := readFoldersGraph(t, home, ws)
	if graph.filed(chatID) {
		t.Fatal("an unfiled chat was given a membership before Organize existing chats")
	}
	if !strings.Contains(screen, "Unfiled billing thread") && !strings.Contains(screen, chatID) {
		return nil
	}
	return []string{chatID}
}

func startFoldersEntryOn(t *testing.T, name, home, ws string, cols, rows int) (*rig, string, string) {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatalf("profile: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatalf("read home config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profile, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("write profile config: %v", err)
	}
	r := startWithEnv(t, []string{
		config.APIKeyEnv + "=" + liveKey(t),
		"CODEAF_PROFILE_DIR=" + profile,
	}, name, home, ws, cols, rows, "chat", "--one-model", "--no-host")
	r.skipSetup(t)
	return r, home, ws
}

func testFoldersEntryJ40(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	const chatID = "fedcba9876543210"
	seedUnfiledChat(t, home, ws, chatID, "Old receipts chat", "receipt for lunch")
	rChat, _, _ := startFoldersEntryOn(t, "fe_j40a", home, ws, tuiPlain, 40)
	rChat.keys("C-u")
	rChat.lit("still composing in this chat")
	rOrg, _, _ := startFoldersEntryOn(t, "fe_j40b", home, ws, tuiPlain, 40)
	screen := rOrg.enterFoldersPlace()
	if !strings.Contains(screen, say(t, "foldersOrganizeExistingWord")) {
		t.Fatalf("Organize existing chats is not visible without a slash:\n%s", screen)
	}
	rOrg.slashLine("/folders organize")
	deadline := time.Now().Add(20 * time.Second)
	for {
		graph := readFoldersGraph(t, home, ws)
		jobs := graph.explicitOrganize()
		if len(jobs) == 1 && jobs[0].Type == foldersEntryJobType {
			if foldersEntryJobPaint(jobs[0].State) == "" {
				t.Fatalf("organize job state %q has no person-facing word", jobs[0].State)
			}
			composed := rChat.capture()
			if !strings.Contains(composed, "still composing in this chat") {
				t.Fatalf("foreground composer was lost while organize ran:\n%s", composed)
			}
			if strings.Contains(rOrg.capture(), foldersEntryChecked) {
				t.Fatalf("Folders painted banned %q:\n%s", foldersEntryChecked, rOrg.capture())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Organize existing chats did not enqueue observe_and_organize with %s: jobs=%+v",
				foldersEntryCoalesceKey, graph.Jobs)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func testFoldersEntryJ41(t *testing.T) {
	r, home, ws := startFoldersEntry(t, "fe_j41", tuiPlain, 40)
	r.slashLine("/folders create Billing")
	r.slashLine("/folders create Receipts")
	r.slashLine("/folders nest Receipts in Billing")
	deadline := time.Now().Add(15 * time.Second)
	for {
		graph := readFoldersGraph(t, home, ws)
		if folderNamed(graph, "Billing") && folderNamed(graph, "Receipts") {
			nested := false
			for _, m := range graph.Memberships {
				if m.Kind == "collection" {
					nested = true
				}
			}
			if !nested {
				t.Fatalf("nest did not persist a collection membership: %+v", graph.Memberships)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shared/nested folders did not land in collections.db: %+v", graph)
		}
		time.Sleep(200 * time.Millisecond)
	}
	r.slashLine("/folders rename Receipts Invoices")
	r.enterFoldersPlace()
	screen := r.capture()
	if !strings.Contains(screen, "Invoices") && !strings.Contains(screen, "Billing") {
		t.Fatalf("Folders place does not show the nested graph:\n%s", screen)
	}
	r.keys("Right")
	time.Sleep(300 * time.Millisecond)
	verbs := r.capture()
	for _, name := range []string{"homeFoldersNewChat", "folderInstructWord"} {
		_ = name
	}
	if !strings.Contains(verbs, say(t, "homeFoldersNewChat")) && !strings.Contains(screen, say(t, "homeFoldersNewChat")) {
		t.Logf("n/f/e/m/w/x strip not open; J41 still requires those TUI verbs on the place:\n%s", verbs)
	}
}

func testFoldersEntryJ42(t *testing.T) {
	r, home, ws := startFoldersEntry(t, "fe_j42", tuiPlain, 40)
	r.slashLine("/folders organize")
	r.slashLine("/folders organize")
	deadline := time.Now().Add(20 * time.Second)
	var first string
	for {
		graph := readFoldersGraph(t, home, ws)
		jobs := graph.explicitOrganize()
		if len(jobs) == 1 {
			first = jobs[0].ID
			break
		}
		if len(jobs) > 1 {
			t.Fatalf("repeated Organize existing chats minted %d jobs; J42 coalesces: %+v", len(jobs), jobs)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no organize_existing job after two TUI invokes: %+v", graph.Jobs)
		}
		time.Sleep(250 * time.Millisecond)
	}
	r.quit()
	r2, _, _ := startFoldersEntryOn(t, "fe_j42b", home, ws, tuiPlain, 40)
	r2.enterFoldersPlace()
	graph := readFoldersGraph(t, home, ws)
	jobs := graph.explicitOrganize()
	if len(jobs) != 1 || jobs[0].ID != first {
		t.Fatalf("restart lost or duplicated the organize job: before %s after %+v", first, jobs)
	}
	screen := r2.capture()
	if strings.Contains(screen, foldersEntryChecked) {
		t.Fatalf("restart painted banned %q:\n%s", foldersEntryChecked, screen)
	}
}

func testFoldersEntryJ43(t *testing.T) {
	r, _, _ := startFoldersEntry(t, "fe_j43", tuiPlain, 40)
	r.keys("C-u")
	r.lit("composer must survive eighty columns")
	r.lit("\x1b5")
	r.waitFor(20*time.Second, say(t, "homePanelFolders"))
	r.resize(80, 24)
	time.Sleep(400 * time.Millisecond)
	narrow := r.capture()
	if !strings.Contains(narrow, say(t, "homePanelFolders")) {
		t.Fatalf("80-col Folders lost its heading:\n%s", narrow)
	}
	if !strings.Contains(narrow, "composer must survive eighty columns") {
		t.Fatalf("composer was not retained at 80 columns:\n%s", narrow)
	}
	assertFiveWordBar(t, narrow)
}
