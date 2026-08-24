package furrow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// Every test here runs against a FAKE furrow: a shell script on PATH that
// prints canned `--json` answers copied from the shapes furrow's own source
// emits. THE REAL BINARY IS NEVER REQUIRED, which is the only way a seam onto
// an optional program can be tested at all — the machine running these tests is
// exactly the machine this package must behave correctly on when furrow is not
// installed.

// fakeScript is the whole fake, dispatching on the argument layout this package
// actually builds: `--version`, or `--repo <root> --json <subcommand> …`. The
// canned answers are chosen by FURROW_FAKE, which each test sets.
const fakeScript = `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "furrow 0.1.0"
  exit 0
fi
sub="$4"
mode="$FURROW_FAKE"
case "$sub" in
status)
  if [ "$mode" = "unattached" ]; then
    echo "Error: this repository is not watched; run furrow watch first" >&2
    exit 1
  fi
  if [ "$mode" = "badstatus" ]; then
    echo "furrow says hello"
    exit 0
  fi
  echo '{"workspace":"/w","store":"/s","head":"aaaabbbbcccc0001","snapshots":3,"objects":9,"physical_bytes":100,"watcher_running":true,"budget":{}}'
  ;;
timeline)
  if [ "$mode" = "badjson" ]; then
    echo 'not json at all {{{'
    exit 0
  fi
  if [ "$mode" = "empty" ]; then
    echo '[]'
    exit 0
  fi
  echo '[{"id":"aaaabbbbcccc0001","sealed_at":1700000200,"label":"hook turn-end agent=aforge turn=t2","trigger":"agent_run","pinned":false,"materialization":{"grade":"byte-exact","partial_classes":[],"missing_paths":[]},"unknown_field_from_a_newer_furrow":true},{"id":"aaaabbbbcccc0000","sealed_at":1700000100,"label":null,"trigger":"quiet","pinned":true,"materialization":{"grade":"partial"}}]'
  ;;
rewind)
  if [ "$mode" = "nochange" ]; then
    echo '{"target":"aaaabbbbcccc0000","changes":[],"preview_digest":"d"}'
    exit 0
  fi
  echo '{"target":"aaaabbbbcccc0000","changes":[{"path":".env","action":"restore"},{"path":"db.sqlite","action":"restore"}],"preview_digest":"d"}'
  for a in "$@"; do
    if [ "$a" = "--yes" ]; then
      echo '{"restored":"aaaabbbbcccc0000","pre_rewind_snapshot":"ffff000011112222","changes":2}'
    fi
  done
  ;;
exec)
  echo "compiling…" >&2
  echo "tests passed" >&2
  echo '{"driver":{},"materialized_ms":812,"universes":[{"index":1,"fork":"risky","fork_id":"f1","base_snapshot":"aaaabbbbcccc0001","head_snapshot":"aaaabbbbcccc0009","path":"/w.furrow-forks/risky","process_workdir":"/w","port":3000,"exit_code":0}]}'
  ;;
merge)
  if [ "$mode" = "checkfail" ]; then
    echo "Error: merge check failed with exit status: 1" >&2
    echo "FAIL	./pkg  0.2s" >&2
    exit 1
  fi
  if [ "$mode" = "conflict" ]; then
    echo '{"fork":"risky","base_snapshot":"b","ours_snapshot":"o","theirs_snapshot":"t","ours_tree":"ot","theirs_tree":"tt","result_snapshot":null,"changes":0,"conflicts":[{"path":[46,101,110,118],"kind":"modify_modify"}],"check":null,"check_output":null,"preview_digest":"d"}'
    exit 1
  fi
  echo '{"fork":"risky","base_snapshot":"b","ours_snapshot":"o","theirs_snapshot":"t","ours_tree":"ot","theirs_tree":"tt","result_snapshot":"aaaabbbbcccc00ff","changes":4,"conflicts":[],"check":"go test ./...","check_output":"ok  	./...	0.4s","preview_digest":"d"}'
  ;;
forks)
  echo '[{"fork_id":"f1","name":"risky","destination":"/w.furrow-forks/risky","base_snapshot":"aaaabbbbcccc0001","head_snapshot":"aaaabbbbcccc0009","tier":"clone","files":1,"directories":1,"symlinks":0,"fifos":0,"skipped_special":0,"logical_bytes":1,"cloned_bytes":1,"copied_bytes":0,"hardlinked_files":0,"elapsed_ms":9,"created_at":1700000300}]'
  ;;
hook)
  echo '{"event":"turn-end","label":"hook turn-end agent=aforge turn=t9","snapshot":"aaaabbbbcccc00aa"}'
  ;;
*)
  echo "Error: unknown fake subcommand $sub" >&2
  exit 2
  ;;
esac
`

// installFake puts the fake furrow on PATH and clears the detection cache, so
// one test's answer can never be another's.
func installFake(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, Binary)
	if err := os.WriteFile(script, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write the fake furrow: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv(BinaryEnvVar, "")
	t.Setenv("FURROW_FAKE", mode)
	Forget()
	t.Cleanup(Forget)
	return dir
}

// removeFake is the case that matters most: a machine with no furrow at all.
func removeFake(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv(BinaryEnvVar, "")
	Forget()
	t.Cleanup(Forget)
}

func TestNoFurrowMeansNoSeamAtAll(t *testing.T) {
	removeFake(t)
	ctx := context.Background()

	presence := Detect(ctx, t.TempDir())
	if presence.Installed || presence.Attached || presence.Available() {
		t.Fatalf("with no furrow on PATH, presence = %+v; want every half false", presence)
	}
	if workspace := Open(ctx, t.TempDir()); workspace != nil {
		t.Fatalf("Open with no furrow returned %+v; want nil", workspace)
	}
	// THE LAW: the tools are absent, not present and refusing. A single tool
	// here would be a model told it can put files back on a machine that
	// cannot.
	if tools := Tools(ctx, t.TempDir()); len(tools) != 0 {
		t.Fatalf("Tools with no furrow returned %d tools; want none", len(tools))
	}
}

func TestAnUnattachedFolderIsAlsoNoSeam(t *testing.T) {
	installFake(t, "unattached")
	ctx := context.Background()
	root := t.TempDir()

	presence := Detect(ctx, root)
	if !presence.Installed {
		t.Fatalf("the fake furrow was not found: %+v", presence)
	}
	if presence.Attached || presence.Available() {
		t.Fatalf("an unwatched folder reported attached: %+v", presence)
	}
	// furrow's own sentence is carried, because it is the sentence the person
	// will act on.
	if !strings.Contains(presence.Reason, "furrow watch") {
		t.Fatalf("Reason = %q; want furrow's own not-watched line", presence.Reason)
	}
	if tools := Tools(ctx, root); len(tools) != 0 {
		t.Fatalf("Tools on an unwatched folder returned %d tools; want none", len(tools))
	}
}

func TestAnAttachedFolderCarriesTheWholeBelt(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	root := t.TempDir()

	presence := Detect(ctx, root)
	if !presence.Available() {
		t.Fatalf("presence = %+v; want available", presence)
	}
	if presence.Version != "furrow 0.1.0" {
		t.Errorf("Version = %q; want the line furrow --version printed", presence.Version)
	}
	if presence.Head != "aaaabbbbcccc0001" || !presence.Watching {
		t.Errorf("Head = %q, Watching = %v; want the head and watcher status status reported", presence.Head, presence.Watching)
	}

	tools := Tools(ctx, root)
	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.Name)
		if tool.Description == "" || len(tool.Schema) == 0 || tool.Execute == nil {
			t.Errorf("%s is missing a description, a schema or a hand", tool.Name)
		}
		if !json.Valid(tool.Schema) {
			t.Errorf("%s carries a schema that is not JSON", tool.Name)
		}
	}
	want := Names()
	if len(got) != len(want) {
		t.Fatalf("belt = %v; want %v", got, want)
	}
	for index, name := range want {
		if got[index] != name {
			t.Fatalf("belt = %v; want %v", got, want)
		}
	}
}

// The detection answer is reused rather than re-asked. A furrow that vanishes
// from PATH mid-window must not change the answer, which is what proves the
// cache is the thing being read.
func TestDetectionIsCached(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	root := t.TempDir()

	if !Detect(ctx, root).Available() {
		t.Fatal("the first detection did not find the fake furrow")
	}
	t.Setenv("PATH", t.TempDir())
	if !Detect(ctx, root).Available() {
		t.Fatal("the second detection re-asked instead of reading the cache")
	}
	Forget()
	if Detect(ctx, root).Available() {
		t.Fatal("Forget did not drop the cached answer")
	}
}

func TestSnapshotsAndPointNear(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil against the fake furrow")
	}

	snapshots, err := workspace.Snapshots(ctx, 0)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("Snapshots returned %d; want 2", len(snapshots))
	}
	newest := snapshots[0]
	if newest.ID != "aaaabbbbcccc0001" || newest.Short() != "aaaabbbbcccc" {
		t.Errorf("newest = %+v; want the full id and a twelve-character short form", newest)
	}
	if !newest.SealedAt.Equal(time.Unix(1700000200, 0)) {
		t.Errorf("SealedAt = %v; want furrow's Unix seconds read as seconds", newest.SealedAt)
	}
	if newest.Label == "" || newest.Grade != "byte-exact" {
		t.Errorf("label/grade = %q/%q; want both carried", newest.Label, newest.Grade)
	}
	// A snapshot furrow labelled nothing carries nothing, and a pin is carried.
	if snapshots[1].Label != "" || !snapshots[1].Pinned {
		t.Errorf("older = %+v; want an empty label and a pin", snapshots[1])
	}

	// The alignment: the newest restore point at or before a moment.
	point, found, err := workspace.PointNear(ctx, time.Unix(1700000150, 0))
	if err != nil || !found {
		t.Fatalf("PointNear = %v, %v, %v", point, found, err)
	}
	if point.ID != "aaaabbbbcccc0000" {
		t.Errorf("PointNear picked %s; want the one sealed before the moment asked about", point.ID)
	}
	// Before the timeline begins there is nothing to offer, and saying so beats
	// offering the oldest there is.
	if _, found, err := workspace.PointNear(ctx, time.Unix(1600000000, 0)); err != nil || found {
		t.Errorf("PointNear before the timeline = %v, %v; want nothing found", found, err)
	}
}

func TestSnapshotsToolSaysSoWhenThereAreNone(t *testing.T) {
	installFake(t, "empty")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	text, isError, err := call(t, workspace.Tools(), ToolSnapshots, `{}`)
	if err != nil || isError {
		t.Fatalf("%s = %q, %v, %v", ToolSnapshots, text, isError, err)
	}
	if text != "This workspace has no restore points yet." {
		t.Errorf("%s said %q", ToolSnapshots, text)
	}
}

// A restore previews unless it is confirmed, and the preview is the ONLY thing
// that happens without confirm. This is furrow's own gate — an explicit id plus
// a yes — kept at aforge's boundary rather than delegated to a model's
// judgement.
func TestRestorePreviewsUnlessConfirmed(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	tools := workspace.Tools()

	text, isError, err := call(t, tools, ToolRestore, `{"snapshot":"aaaabbbbcccc0000"}`)
	if err != nil || isError {
		t.Fatalf("preview = %q, %v, %v", text, isError, err)
	}
	const previewLine = "Nothing applied — this is a preview. Call workspace_restore again with confirm true to apply it."
	if !strings.HasPrefix(text, previewLine) {
		t.Errorf("preview said %q; want it to open with %q", text, previewLine)
	}
	if !strings.Contains(text, ".env") {
		t.Errorf("preview did not name the paths it would touch: %q", text)
	}

	text, isError, err = call(t, tools, ToolRestore, `{"snapshot":"aaaabbbbcccc0000","confirm":true}`)
	if err != nil || isError {
		t.Fatalf("restore = %q, %v, %v", text, isError, err)
	}
	if !strings.HasPrefix(text, "Restored 2 path(s) from aaaabbbbcccc0000.") {
		t.Errorf("restore said %q", text)
	}
	// The undo snapshot is the safest thing about a restore, so it is always
	// offered back.
	if !strings.Contains(text, "ffff000011112222") {
		t.Errorf("restore did not offer the state it sealed first: %q", text)
	}
}

// An applied restore prints its plan AND then its outcome — two JSON documents
// on one stream. A decoder that demanded one would fail on exactly this call.
func TestAnAppliedRestoreReadsTheLastOfTwoDocuments(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	restore, err := workspace.ApplyRestore(ctx, "aaaabbbbcccc0000", nil)
	if err != nil {
		t.Fatalf("ApplyRestore: %v", err)
	}
	if !restore.Applied || restore.Undo != "ffff000011112222" || len(restore.Changes) != 2 {
		t.Fatalf("ApplyRestore = %+v; want applied, with an undo snapshot and both paths", restore)
	}
}

func TestARestoreWithNothingToDoSaysSo(t *testing.T) {
	installFake(t, "nochange")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	text, isError, err := call(t, workspace.Tools(), ToolRestore, `{"snapshot":"aaaabbbbcccc0000","confirm":true}`)
	if err != nil || isError {
		t.Fatalf("restore = %q, %v, %v", text, isError, err)
	}
	if text != "Nothing to restore: the workspace already matches that restore point." {
		t.Errorf("restore said %q", text)
	}
}

func TestRestoreRefusesAnAbsolutePath(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	if _, err := workspace.PreviewRestore(ctx, "aaaabbbbcccc0000", []string{"/etc/passwd"}); err == nil {
		t.Fatal("an absolute path was accepted; want a refusal")
	}
}

func TestForkRunsSomethingRiskySomewhereElse(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}

	fork, err := workspace.RunInFork(ctx, "risky", "go test ./...")
	if err != nil {
		t.Fatalf("RunInFork: %v", err)
	}
	if fork.Name != "risky" || fork.ExitCode != 0 || fork.Head != "aaaabbbbcccc0009" {
		t.Fatalf("RunInFork = %+v", fork)
	}
	// Under --json furrow sends the command's own output to stderr so the JSON
	// on stdout stays clean; the point of the fork is seeing that output.
	if !strings.Contains(fork.Output, "tests passed") {
		t.Errorf("the command's output was lost: %q", fork.Output)
	}

	text, isError, err := call(t, workspace.Tools(), ToolFork, `{"command":"go test ./...","name":"risky"}`)
	if err != nil || isError {
		t.Fatalf("%s = %q, %v, %v", ToolFork, text, isError, err)
	}
	if !strings.Contains(text, "The real workspace was not modified.") {
		t.Errorf("%s did not say the workspace was untouched: %q", ToolFork, text)
	}

	forks, err := workspace.Forks(ctx)
	if err != nil || len(forks) != 1 || forks[0].Name != "risky" {
		t.Fatalf("Forks = %+v, %v", forks, err)
	}
}

func TestAMergeThatPassesItsCheckLands(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	text, isError, err := call(t, workspace.Tools(), ToolMerge, `{"fork":"risky","check":"go test ./..."}`)
	if err != nil || isError {
		t.Fatalf("%s = %q, %v, %v", ToolMerge, text, isError, err)
	}
	if !strings.HasPrefix(text, "Merged risky: 4 path(s) changed.") {
		t.Errorf("%s said %q", ToolMerge, text)
	}
	if !strings.Contains(text, "aaaabbbbcccc00ff") {
		t.Errorf("%s did not name the snapshot it sealed: %q", ToolMerge, text)
	}
}

// A check that says no is an ANSWER, not a breakage: furrow aborts before
// printing any JSON, with the check's own output on stderr, and this must read
// as a merge that did not land rather than as a furrow that failed.
func TestAMergeWhoseCheckFailsLandsNothingAndIsNotAnError(t *testing.T) {
	installFake(t, "checkfail")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}

	merge, err := workspace.MergeFork(ctx, "risky", "go test ./...", false)
	if err != nil {
		t.Fatalf("MergeFork on a failing check returned an error: %v", err)
	}
	if merge.Landed || !merge.CheckFailed {
		t.Fatalf("MergeFork = %+v; want nothing landed and the check named as the reason", merge)
	}
	if !strings.Contains(merge.CheckOutput, "FAIL") {
		t.Errorf("the check's own output was lost: %q", merge.CheckOutput)
	}

	text, isError, err := call(t, workspace.Tools(), ToolMerge, `{"fork":"risky","check":"go test ./..."}`)
	if err != nil || isError {
		t.Fatalf("%s = %q, %v, %v", ToolMerge, text, isError, err)
	}
	if !strings.HasPrefix(text, "The check failed, so nothing was merged.") {
		t.Errorf("%s said %q", ToolMerge, text)
	}
}

// Conflicts come back with the path spelled as a sequence of BYTES, because a
// path on Unix need not be valid UTF-8. And furrow exits non-zero while having
// printed a complete account, which is the case the decode-before-the-exit-code
// rule exists for.
func TestAMergeWithConflictsIsReadDespiteTheExitCode(t *testing.T) {
	installFake(t, "conflict")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	merge, err := workspace.MergeFork(ctx, "risky", "", false)
	if err != nil {
		t.Fatalf("MergeFork with conflicts returned an error: %v", err)
	}
	if merge.Landed {
		t.Fatal("a merge with conflicts reported that it landed")
	}
	if len(merge.Conflicts) != 1 || merge.Conflicts[0].Path != ".env" || merge.Conflicts[0].Kind != "modify_modify" {
		t.Fatalf("conflicts = %+v; want one .env modify_modify decoded from its bytes", merge.Conflicts)
	}
	text := mergeReport(merge, false)
	if !strings.HasPrefix(text, "Nothing was merged: 1 path(s) changed on both sides.") {
		t.Errorf("mergeReport said %q", text)
	}
}

// furrow's JSON is furrow's to change. A shape this package cannot read must
// read as "I could not read that" — never as a panic, and never as a confident
// misreading.
func TestUnreadableOutputIsAnHonestFailureAndNotAGuess(t *testing.T) {
	installFake(t, "badjson")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	if _, err := workspace.Snapshots(ctx, 0); err == nil {
		t.Fatal("malformed JSON was accepted")
	}

	text, isError, err := call(t, workspace.Tools(), ToolSnapshots, `{}`)
	if err != nil {
		t.Fatalf("%s returned a hard error: %v", ToolSnapshots, err)
	}
	if !isError || text == "" {
		t.Errorf("%s = %q, %v; want a readable refusal marked as an error", ToolSnapshots, text, isError)
	}
}

// A status furrow answered happily and unreadably is NOT an attached workspace:
// a furrow whose status cannot be read is a furrow whose other answers cannot
// be trusted, and a belt built on that fails in a way nobody can explain.
func TestAnUnreadableStatusIsNotAttached(t *testing.T) {
	installFake(t, "badstatus")
	ctx := context.Background()
	root := t.TempDir()

	presence := Detect(ctx, root)
	if !presence.Installed {
		t.Fatal("the fake furrow was not found")
	}
	if presence.Attached {
		t.Fatalf("an unreadable status reported attached: %+v", presence)
	}
	if tools := Tools(ctx, root); len(tools) != 0 {
		t.Fatalf("Tools returned %d tools against an unreadable status", len(tools))
	}
}

// A newer furrow with fields this package has never heard of must cost it
// nothing. The fake's timeline carries one such field on purpose.
func TestUnknownFieldsFromANewerFurrowAreIgnored(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	if _, err := workspace.Snapshots(ctx, 0); err != nil {
		t.Fatalf("a field this package does not know broke the decode: %v", err)
	}
}

func TestMarkSealsAnAttributedRestorePoint(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	workspace := Open(ctx, t.TempDir())
	if workspace == nil {
		t.Fatal("Open returned nil")
	}
	snapshot, err := workspace.Mark(ctx, "aforge", "t9")
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if snapshot != "aaaabbbbcccc00aa" {
		t.Errorf("Mark = %q; want the snapshot furrow sealed", snapshot)
	}
}

// The pairing is an OFFER and never a call. Nothing here runs `furrow remote
// add`, because its output carries the workspace's recovery key and a secret
// that reaches a tool result has been written into a transcript.
func TestSyncIsOfferedAndNeverPerformed(t *testing.T) {
	installFake(t, "ok")
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "my-project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("make the workspace: %v", err)
	}
	workspace := Open(ctx, root)
	if workspace == nil {
		t.Fatal("Open returned nil")
	}

	offer := workspace.OfferSync("ssh://dev@machine-a.tailnet")
	if offer.Name != "my-project" {
		t.Errorf("Name = %q; want the folder's own name", offer.Name)
	}
	if len(offer.Here) != 2 || !strings.HasPrefix(offer.Here[0], "furrow remote add ssh://dev@machine-a.tailnet --name my-project") {
		t.Errorf("Here = %v", offer.Here)
	}
	if offer.Here[1] != "furrow sync --follow" {
		t.Errorf("Here[1] = %q", offer.Here[1])
	}
	if len(offer.There) != 1 || !strings.Contains(offer.There[0], "FURROW_RECOVERY_KEY=<key> furrow clone") {
		t.Errorf("There = %v", offer.There)
	}
	// The recovery key is a placeholder and never a value: this package does
	// not have one and must never go looking.
	if offer.There[0] != "FURROW_RECOVERY_KEY=<key> furrow clone ssh://dev@machine-a.tailnet/my-project" {
		t.Errorf("There = %v; want the clone URL furrow's own README spells", offer.There)
	}
	if offer.Note != syncNote {
		t.Errorf("Note = %q; want the divergence sentence", offer.Note)
	}
}

// The number in the schema is the number the code applies. A schema that
// advertises a figure the code does not honour is the drift this repository has
// been bitten by before.
func TestTheSchemaAndTheCodeShareTheirNumbers(t *testing.T) {
	if !strings.Contains(snapshotsSchemaJSON, "default: 20") || !strings.Contains(snapshotsSchemaJSON, "maximum: 100") {
		t.Fatalf("the snapshots schema does not read its own constants: %s", snapshotsSchemaJSON)
	}
	if !strings.Contains(snapshotsDescription, "Default 20 restore points, at most 100.") {
		t.Fatalf("the snapshots description does not read its own constants: %s", snapshotsDescription)
	}
	for name, schema := range Schemas() {
		if !json.Valid([]byte(schema)) {
			t.Errorf("%s carries a schema that is not JSON", name)
		}
	}
	if len(Descriptions()) != len(Names()) || len(Schemas()) != len(Names()) {
		t.Error("the exported metadata does not cover every name")
	}
}

// capped says how much it left behind, because a truncation nobody is told
// about is a result that reads as complete and is not.
func TestCappedOutputSaysWhatItLeftBehind(t *testing.T) {
	long := strings.Repeat("x", outputCap+500)
	out := capped(long)
	if len(out) >= len(long) {
		t.Fatalf("capped returned %d bytes for %d", len(out), len(long))
	}
	if !strings.Contains(out, "more bytes not shown") {
		t.Errorf("capped said nothing about the truncation: %q", out[len(out)-60:])
	}
	if short := capped("two\nlines"); short != "two\nlines" {
		t.Errorf("capped changed a short string: %q", short)
	}
}

func TestDocumentsReadsAStreamAndSurvivesATrailingFragment(t *testing.T) {
	found := documents([]byte(`{"a":1}` + "\n" + `{"b":2}` + "\n" + `{"c":`))
	if len(found) != 2 {
		t.Fatalf("documents found %d; want the two that parsed", len(found))
	}
	var last struct {
		B int `json:"b"`
	}
	if err := decodeLast([]byte(`{"a":1}`+"\n"+`{"b":2}`), &last); err != nil || last.B != 2 {
		t.Fatalf("decodeLast = %+v, %v; want the last document", last, err)
	}
	if err := decodeLast([]byte("not json"), &last); err == nil {
		t.Fatal("decodeLast accepted a stream with no JSON on it")
	}
}

// An AFORGE_FURROW that names something missing is an error and never a quiet
// fall back to PATH: somebody who set that variable meant that binary.
func TestAConfiguredBinaryThatIsNotThereIsNotSilentlyReplaced(t *testing.T) {
	installFake(t, "ok")
	t.Setenv(BinaryEnvVar, filepath.Join(t.TempDir(), "nowhere"))
	Forget()
	if presence := Detect(context.Background(), t.TempDir()); presence.Installed {
		t.Fatalf("a missing AFORGE_FURROW fell back to PATH: %+v", presence)
	}
}

// call runs one tool off a belt by name, which is how a session would.
func call(t *testing.T, tools []bare.Tool, name, args string) (string, bool, error) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Execute(context.Background(), json.RawMessage(args))
		}
	}
	t.Fatalf("%s is not on this belt", name)
	return "", false, nil
}
