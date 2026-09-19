//go:build e2e

package e2e

// TestFoldersColumnsJourneys is the committed J44–J49 + F09/F10 harness.
// It drives the real binary in tmux with visible actions (click / chords /
// arrows), then reads collections.db, config.json and the transcript tree.
// Slash commands are never the pass. Live pass/fail against a real model is
// t-rx-validate. A skip here is only the honesty gate (no key, no tmux, no
// bin/codeaf). Missing columns, wakeup, cursor or timings is a failure.
//
//	go test -tags e2e -count=1 -timeout 40m -v -run TestFoldersColumnsJourneys ./internal/e2e/
//
// Isolation is CODEAF_HOME plus a private CODEAF_PROFILE_DIR. HOME is never
// set. ~/.codeaf is never the state root.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

// foldersReactiveWakeBudget is how long F10 will wait for start after enqueue.
// It must stay well under standing.Interval (five minutes): a start that only
// arrives on the tick is the defect F10 exists to catch.
const foldersReactiveWakeBudget = 25 * time.Second

func TestFoldersColumnsJourneys(t *testing.T) {
	requireTmuxAndKey(t)
	t.Run("J44", testFoldersColumnsJ44)
	t.Run("J45", testFoldersColumnsJ45)
	t.Run("J46", testFoldersColumnsJ46)
	t.Run("J47", testFoldersColumnsJ47)
	t.Run("J48", testFoldersColumnsJ48)
	t.Run("J49", testFoldersColumnsJ49)
	t.Run("F09", testFoldersColumnsF09)
	t.Run("F10", testFoldersColumnsF10)
}

func startColumns(t *testing.T, name string, cols, rows int) (*rig, string, string, string) {
	t.Helper()
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	return startColumnsOn(t, name, home, ws, cols, rows)
}

func startColumnsOn(t *testing.T, name, home, ws string, cols, rows int) (*rig, string, string, string) {
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
	return r, home, ws, profile
}

func seedColumnGraph(t *testing.T, home, ws string) (receiptsID, chatID string) {
	t.Helper()
	chatID = "cafebabeface0001"
	seedUnfiledChat(t, home, ws, chatID, "March invoice thread", "invoice for the March billing cycle")
	path := collectionsDB(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir collections: %v", err)
	}
	store, err := workspace.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	billing, err := store.Create(ctx, "Billing")
	if err != nil {
		t.Fatalf("billing: %v", err)
	}
	receipts, err := store.Create(ctx, "Receipts")
	if err != nil {
		t.Fatalf("receipts: %v", err)
	}
	invoices, err := store.Create(ctx, "Invoices")
	if err != nil {
		t.Fatalf("invoices: %v", err)
	}
	security, err := store.Create(ctx, "Security")
	if err != nil {
		t.Fatalf("security: %v", err)
	}
	for _, nest := range []struct{ parent, child string }{
		{billing.ID, receipts.ID},
		{receipts.ID, invoices.ID},
		{security.ID, receipts.ID},
	} {
		if err := store.Add(ctx, nest.parent, workspace.Ref{Kind: workspace.CollectionKind, ID: nest.child}); err != nil {
			t.Fatalf("nest: %v", err)
		}
	}
	if err := store.Add(ctx, receipts.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: chatID}); err != nil {
		t.Fatalf("file chat: %v", err)
	}
	if _, err := store.PutGuidance(ctx, workspace.Guidance{
		ScopeID: billing.ID,
		Text:    "File emailed receipts under Billing.",
		Origin:  workspace.OriginPerson,
		Actor:   "person",
	}); err != nil {
		t.Fatalf("guidance: %v", err)
	}
	return receipts.ID, chatID
}

func seedUnfiledBacklog(t *testing.T, home, ws string, n int) {
	t.Helper()
	if n < foldersEntrySurveyCap+1 {
		t.Fatalf("F09 needs more than %d unfiled chats, got %d", foldersEntrySurveyCap, n)
	}
	for i := 0; i < n; i++ {
		body := "hi"
		if i == n-1 {
			body = "please file the March invoices with Billing"
		}
		seedUnfiledChat(t, home, ws, uniqueChatID(i), "Unfiled chat "+string(rune('A'+i)), body)
	}
}

func uniqueChatID(i int) string {
	const hexdigits = "0123456789abcdef"
	id := []byte("aaaaaaaaaaaaaa00")
	id[14] = hexdigits[(i/16)%16]
	id[15] = hexdigits[i%16]
	return string(id)
}

func (r *rig) clickWord(word string) bool {
	r.t.Helper()
	for i, line := range r.lines() {
		if j := strings.Index(line, word); j >= 0 {
			r.mouseClick(j+1, i+1)
			return true
		}
	}
	return false
}

func (r *rig) visibleAction(word string) {
	r.t.Helper()
	if r.clickWord(word) {
		return
	}
	r.t.Fatalf("visible action %q is not on the Folders place (slash is not the pass):\n%s", word, r.capture())
}

func (r *rig) drillRight() {
	r.keys("Right")
	time.Sleep(300 * time.Millisecond)
}

func (r *rig) stripShiftRight() {
	r.keys("S-Right")
	time.Sleep(300 * time.Millisecond)
}

func waitJobTimings(t *testing.T, home, ws, profile string, within time.Duration) (foldersJob, foldersJobInstants, foldersGraph) {
	t.Helper()
	deadline := time.Now().Add(within)
	var last foldersGraph
	for {
		last = readFoldersGraph(t, home, ws)
		jobs := last.explicitOrganize()
		if len(jobs) == 0 {
			jobs = last.organizeJobs()
		}
		if len(jobs) > 0 {
			if !last.Meta.HasEnqueuedAt || !last.Meta.HasStartedAt || !last.Meta.HasCommittedAt {
				t.Fatalf("F10 timings columns missing (enqueued_at=%v started_at=%v committed_at=%v); table-exists is not the journey",
					last.Meta.HasEnqueuedAt, last.Meta.HasStartedAt, last.Meta.HasCommittedAt)
			}
			inst := jobInstants(jobs[0], time.Now().UTC())
			if strings.Contains(inst.Missing, "enqueue") {
				t.Fatalf("organize job %s has no EnqueuedAt: %+v", jobs[0].ID, jobs[0])
			}
			if !inst.Enqueue.IsZero() && !inst.Start.IsZero() {
				return jobs[0], inst, last
			}
			if time.Now().After(deadline) {
				t.Fatalf("organize job %s did not start within %s (scheduler delay vs five-minute tick). missing=%s job=%+v",
					jobs[0].ID, within, inst.Missing, jobs[0])
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no observe_and_organize job with timings after %s: jobs=%+v meta=%+v reactive=%v",
				within, last.Jobs, last.Meta, reactiveOn(t, home, profile))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func reactiveOn(t *testing.T, dirs ...string) bool {
	t.Helper()
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			continue
		}
		var rows map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			continue
		}
		switch v := rows[foldersReactiveKey].(type) {
		case bool:
			if v {
				return true
			}
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			if s == "on" || s == "true" || s == "1" {
				return true
			}
		}
	}
	return false
}

func assertNoSlashPass(t *testing.T, screen string) {
	t.Helper()
	if strings.Contains(screen, foldersEntryOrganizeSlash) && !strings.Contains(screen, foldersEntryOrganizeWord) {
		t.Fatalf("Organize existing chats is only present as a slash; the pass is the visible action:\n%s", screen)
	}
}

func testFoldersColumnsJ44(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	receiptsID, _ := seedColumnGraph(t, home, ws)
	r, home, ws, _ := startColumnsOn(t, "rx_j44", home, ws, tuiPlain, 40)
	screen := r.enterFoldersPlace()
	assertFiveWordBar(t, screen)
	assertNoSlashPass(t, screen)
	for _, action := range []string{
		say(t, "foldersPlaceNewFolder"),
		say(t, "foldersPlaceNewChat"),
		say(t, "foldersOrganizeExistingWord"),
	} {
		if !strings.Contains(screen, action) {
			t.Errorf("J44 Folders place is missing visible %q:\n%s", action, screen)
		}
	}
	if strings.Contains(screen, say(t, "folderChooserHelp")) {
		t.Fatalf("J44 opened the filesystem /folder chooser, not logical columns:\n%s", screen)
	}
	if !r.clickWord("Billing") {
		t.Fatalf("J44 cannot select Billing by a visible click:\n%s", r.capture())
	}
	r.drillRight()
	if !r.clickWord("Receipts") {
		t.Fatalf("J44 Right on Billing did not reveal Receipts as a child column:\n%s", r.capture())
	}
	r.drillRight()
	wide := r.capture()
	for _, name := range []string{"Billing", "Receipts"} {
		if !strings.Contains(wide, name) {
			t.Errorf("J44 wide columns lost ancestor %q; unlimited columns are windowed, never squashed:\n%s", name, wide)
		}
	}
	graph := readFoldersGraph(t, home, ws)
	if graph.collectionID("Receipts") != receiptsID {
		t.Fatalf("J44 production graph lost Receipts identity %s: %+v", receiptsID, graph.Collections)
	}
	if graph.RootIsARow {
		t.Fatal("J44 persisted Root as a collections row")
	}
}

func testFoldersColumnsJ45(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	receiptsID, _ := seedColumnGraph(t, home, ws)
	r, _, _, _ := startColumnsOn(t, "rx_j45", home, ws, tuiPlain, 40)
	r.enterFoldersPlace()
	if !r.clickWord("Security") {
		t.Fatalf("J45 cannot open the Security path:\n%s", r.capture())
	}
	r.drillRight()
	if !r.clickWord("Receipts") {
		t.Fatalf("J45 Security path did not show Receipts:\n%s", r.capture())
	}
	screen := r.capture()
	if !strings.Contains(screen, say(t, "homeFoldersAlsoIn")) {
		t.Fatalf("J45 shared Receipts is missing %q on the path just walked:\n%s", say(t, "homeFoldersAlsoIn"), screen)
	}
	if strings.Count(screen, "March invoice thread") > 1 {
		t.Fatalf("J45 doubled the chat rollup across two paths:\n%s", screen)
	}
	graph := readFoldersGraph(t, home, ws)
	parents := graph.parentsOf("collection", receiptsID)
	if len(parents) != 2 {
		t.Fatalf("J45 production state needs two parents for one Receipts id, got %v", parents)
	}
}

func testFoldersColumnsJ46(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	_, chatID := seedColumnGraph(t, home, ws)
	r, _, _, _ := startColumnsOn(t, "rx_j46", home, ws, tuiPlain, 40)
	r.enterFoldersPlace()
	r.clickWord("Billing")
	r.drillRight()
	r.clickWord("Receipts")
	r.drillRight()
	if !r.clickWord("March invoice thread") {
		t.Fatalf("J46 cannot select the chat for a details preview:\n%s", r.capture())
	}
	preview := r.capture()
	if !strings.Contains(preview, "March invoice thread") {
		t.Fatalf("J46 details preview is missing the cached title:\n%s", preview)
	}
	if !strings.Contains(preview, say(t, "foldersOpenChatWord")) && !strings.Contains(preview, "invoice for the March") {
		t.Fatalf("J46 preview is not a cached snapshot (title/excerpt or Open chat):\n%s", preview)
	}
	r.keys("C-u")
	r.lit("composer must survive the full open")
	r.keys("Enter")
	time.Sleep(800 * time.Millisecond)
	opened := r.capture()
	if strings.Contains(opened, say(t, "folderChooserHelp")) {
		t.Fatalf("J46 Enter opened /folder, not the existing chat:\n%s", opened)
	}
	r.keys("Escape")
	time.Sleep(400 * time.Millisecond)
	back := r.capture()
	if !strings.Contains(back, "Billing") || !strings.Contains(back, "Receipts") {
		t.Fatalf("J46 return lost the column path:\n%s", back)
	}
	if !strings.Contains(back, "composer must survive the full open") {
		t.Fatalf("J46 return dropped the composer:\n%s", back)
	}
	if !strings.Contains(strings.Join(listConversationIDs(t, home), " "), chatID) &&
		len(listConversationIDs(t, home)) == 0 {
		t.Fatalf("J46 full open must be the existing chat %s, not a new transcript", chatID)
	}
}

func testFoldersColumnsJ47(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	seedColumnGraph(t, home, ws)
	r, _, _, _ := startColumnsOn(t, "rx_j47", home, ws, tuiPlain, 40)
	r.enterFoldersPlace()
	r.clickWord("Billing")
	r.drillRight()
	screen := r.capture()
	if !strings.Contains(screen, "File emailed receipts under Billing.") {
		t.Fatalf("J47 details omitted truthful instructions:\n%s", screen)
	}
	if !strings.Contains(screen, say(t, "foldersManageThisFolderWord")) {
		t.Fatalf("J47 details is missing visible %q:\n%s", say(t, "foldersManageThisFolderWord"), screen)
	}
	if strings.Contains(strings.ToLower(screen), "manager entity") || strings.Contains(screen, "the manager") {
		t.Fatalf("J47 invented a manager entity:\n%s", screen)
	}
	if !r.clickWord(say(t, "foldersManageThisFolderWord")) {
		r.lit("d")
		time.Sleep(300 * time.Millisecond)
	}
	after := r.capture()
	if strings.Contains(after, foldersEntryNoFoldersYet) {
		t.Fatalf("J47 painted banned emptiness:\n%s", after)
	}
}

func testFoldersColumnsJ48(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	seedColumnGraph(t, home, ws)
	r, _, _, _ := startColumnsOn(t, "rx_j48", home, ws, tuiPlain, 40)
	r.enterFoldersPlace()
	r.clickWord("Billing")
	r.drillRight()
	r.clickWord("Receipts")
	r.drillRight()
	r.clickWord("Invoices")
	r.drillRight()
	r.keys("C-u")
	r.lit("path must survive eighty columns")
	wide := r.capture()
	if !strings.Contains(wide, say(t, "foldersShiftRightHint")) {
		r.stripShiftRight()
		strip := r.capture()
		if !strings.Contains(strip, say(t, "foldersPlaceNewFolder")) && !strings.Contains(wide, say(t, "foldersShiftRightHint")) {
			t.Fatalf("J48 help/hint must name %q and shift+→ must open the strip, not drill:\nwide:\n%s\nstrip:\n%s",
				say(t, "foldersShiftRightHint"), wide, strip)
		}
		r.keys("Escape")
	}
	r.resize(80, 24)
	time.Sleep(400 * time.Millisecond)
	narrow := r.capture()
	if !strings.Contains(narrow, say(t, "homePanelFolders")) {
		t.Fatalf("J48 80-col Folders lost its heading:\n%s", narrow)
	}
	if !strings.Contains(narrow, "path must survive eighty columns") {
		t.Fatalf("J48 composer was not retained at 80 columns:\n%s", narrow)
	}
	if !strings.Contains(narrow, "Billing") && !strings.Contains(narrow, "Receipts") && !strings.Contains(narrow, "Invoices") {
		t.Fatalf("J48 80-col lost the breadcrumb/path:\n%s", narrow)
	}
	r.resize(tuiPlain, 40)
	time.Sleep(400 * time.Millisecond)
	back := r.capture()
	if !strings.Contains(back, "Billing") || !strings.Contains(back, "path must survive eighty columns") {
		t.Fatalf("J48 resize back to wide lost path or composer:\n%s", back)
	}
}

func testFoldersColumnsJ49(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	seedColumnGraph(t, home, ws)
	a, _, _, _ := startColumnsOn(t, "rx_j49a", home, ws, tuiPlain, 40)
	a.enterFoldersPlace()
	a.clickWord("Billing")
	a.drillRight()
	a.keys("C-u")
	a.lit("do not teleport this sentence")
	b, _, _, _ := startColumnsOn(t, "rx_j49b", home, ws, tuiPlain, 40)
	b.enterFoldersPlace()
	if !b.clickWord(say(t, "foldersOrganizeExistingWord")) {
		b.stripShiftRight()
		b.lit("o")
	}
	time.Sleep(800 * time.Millisecond)
	held := a.capture()
	if !strings.Contains(held, "do not teleport this sentence") {
		t.Fatalf("J49 dropped the composer while the other window organized:\n%s", held)
	}
	if !strings.Contains(held, "Billing") {
		t.Fatalf("J49 teleported selection off Billing:\n%s", held)
	}
	a.clickWord("Receipts")
	time.Sleep(400 * time.Millisecond)
	after := a.capture()
	if strings.Contains(after, "File emailed receipts under Billing.") && !strings.Contains(after, "Receipts") {
		t.Fatalf("J49 stale Billing details overwrote the Receipts selection:\n%s", after)
	}
}

func testFoldersColumnsF09(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "foldersws", false)
	seedUnfiledBacklog(t, home, ws, foldersEntrySurveyCap+2)
	chat, _, _, _ := startColumnsOn(t, "rx_f09a", home, ws, tuiPlain, 40)
	chat.keys("C-u")
	chat.lit("keep chatting during the survey")
	org, home, ws, profile := startColumnsOn(t, "rx_f09b", home, ws, tuiPlain, 40)
	screen := org.enterFoldersPlace()
	if !strings.Contains(screen, say(t, "foldersOrganizeExistingWord")) {
		t.Fatalf("F09 Organize existing chats is not visible:\n%s", screen)
	}
	if !org.clickWord(say(t, "foldersOrganizeExistingWord")) {
		org.stripShiftRight()
		org.lit("o")
	}
	deadline := time.Now().Add(20 * time.Second)
	var graph foldersGraph
	for {
		graph = readFoldersGraph(t, home, ws)
		if len(graph.explicitOrganize()) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("F09 visible Organize existing chats did not enqueue observe_and_organize: %+v", graph.Jobs)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !graph.Meta.HasCursor {
		t.Fatal("F09 jobs.cursor column is missing; the survey will rescan Unfiled[0..] forever")
	}
	composed := chat.capture()
	if !strings.Contains(composed, "keep chatting during the survey") {
		t.Fatalf("F09 foreground composer was lost:\n%s", composed)
	}
	if strings.Contains(org.capture(), foldersEntryChecked) {
		t.Fatalf("F09 painted banned %q:\n%s", foldersEntryChecked, org.capture())
	}
	_ = profile
}

func testFoldersColumnsF10(t *testing.T) {
	r, home, ws, profile := startColumns(t, "rx_f10", tuiPlain, 40)
	screen := r.enterFoldersPlace()
	assertNoGeneratedFolders(t, home, ws, screen)
	if !r.clickWord(say(t, "foldersPlaceNewChat")) {
		r.lit("n")
	}
	time.Sleep(400 * time.Millisecond)
	r.keys("C-u")
	r.lit("hi")
	r.keys("Enter")
	time.Sleep(2 * time.Second)
	graph := readFoldersGraph(t, home, ws)
	if len(graph.Collections) != 0 {
		t.Fatalf("F10 greeting invented folders %v; tiny/empty/greeting must not CreateFolder", graph.Collections)
	}
	r.enterFoldersPlace()
	if !r.clickWord(say(t, "foldersOrganizeExistingWord")) {
		r.stripShiftRight()
		r.lit("o")
	}
	job, inst, graph := waitJobTimings(t, home, ws, profile, foldersReactiveWakeBudget)
	if inst.SchedulerDelay >= standing.Interval {
		t.Fatalf("F10 scheduler delay %s is the five-minute tick, not an enqueue wakeup. job=%+v", inst.SchedulerDelay, job)
	}
	if !reactiveOn(t, home, profile) {
		t.Fatal("F10 Organize existing chats must persist workspace.reactive=on; do not infer consent from collection count")
	}
	if strings.Contains(r.capture(), foldersEntryChecked) {
		t.Fatalf("F10 painted banned %q:\n%s", foldersEntryChecked, r.capture())
	}
	ids := listConversationIDs(t, home)
	if len(ids) == 0 {
		t.Fatal("F10 greeting chat identity was lost")
	}
	r.clickWord("hi")
	if !r.clickWord(say(t, "foldersOrganizeThisChatWord")) {
		r.lit("t")
	}
	time.Sleep(400 * time.Millisecond)
	r.lit("t")
	time.Sleep(400 * time.Millisecond)
	graph = readFoldersGraph(t, home, ws)
	targeted := 0
	for _, j := range graph.organizeJobs() {
		if strings.HasPrefix(j.CoalesceKey, ids[0]) || j.ChatID == ids[0] {
			targeted++
		}
	}
	if targeted > 1 {
		t.Fatalf("F10 Organize this chat did not coalesce: %+v", graph.Jobs)
	}
	_ = inst
}
