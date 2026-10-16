package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shellPNG is a real, tiny picture; the seed makes two of them differ.
func shellPNG(t *testing.T, seed uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{R: seed, G: 80, B: 160, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeCommand is an existing shell command that writes data to name.
func writeCommand(data []byte, name string) string {
	return "printf %s '" + base64.StdEncoding.EncodeToString(data) + "' | base64 -d > " + name
}

func shellAgent(t *testing.T) (*Agent, string, string) {
	t.Helper()
	agent, workspace, index := deliverableAgent(t, false)
	return agent, workspace, index
}

func bashArgs(t *testing.T, command string, outputs ...string) json.RawMessage {
	t.Helper()
	fields := map[string]any{"command": command}
	if outputs != nil {
		fields["outputs"] = outputs
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func callCtx() context.Context {
	return withCallID(withRunIdentity(context.Background()), "call_shell_1")
}

func publishedRows(t *testing.T, agent *Agent, index string) []MediaRow {
	t.Helper()
	scan, err := ListMedia(index, agent.journalID(), "", 100, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return scan.Rows
}

func TestShellOutputsAPictureMadeByTheShellIsAPublishedSourceOfThisCall(t *testing.T) {
	agent, workspace, index := shellAgent(t)
	ctx := callCtx()
	text, isError, err := beltTool(t, agent, "bash").Execute(ctx, bashArgs(t, writeCommand(shellPNG(t, 1), "chart.png"), "chart.png"))
	if err != nil || isError {
		t.Fatalf("bash: %v %v %s", err, isError, text)
	}
	if !strings.Contains(text, "Output chart.png registered (created).") {
		t.Fatalf("the result does not say it registered:\n%s", text)
	}
	rows := publishedRows(t, agent, index)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	fact := rows[0].Fact
	canonical := canonicalFile(filepath.Join(workspace, "chart.png"))
	if fact.Origin != MediaPublished || fact.Change != ShellCreated || fact.Declared != "chart.png" || fact.Canonical != canonical {
		t.Fatalf("fact = %+v", fact)
	}
	if fact.Run != runIdentity(ctx) || fact.Run == "" || fact.Tool != "call_shell_1" {
		t.Fatalf("run/tool = %q/%q", fact.Run, fact.Tool)
	}
	if fact.Message != "" {
		t.Fatalf("a message was invented: %q", fact.Message)
	}
	if fact.Width != 3 || fact.Height != 2 || fact.SHA256 == "" || fact.MIME != "image/png" || fact.Charge != "" || fact.CostUSD != nil {
		t.Fatalf("fact = %+v", fact)
	}
	if rows[0].Artifact.Deliver == nil || !rows[0].Artifact.Deliver.Authored || rows[0].Artifact.Deliver.Run != fact.Run {
		t.Fatalf("a created picture carries no authored deliverable: %+v", rows[0].Artifact.Deliver)
	}
	// RESTART: a fresh read of the index alone finds the same source by id, with
	// the same run and call, and the file is still the recorded version.
	found, err := FindMedia(index, agent.journalID(), fact.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if found.Fact.Run != fact.Run || found.Fact.Tool != fact.Tool || found.Version() != "sha256:"+fact.SHA256 {
		t.Fatalf("found = %+v", found.Fact)
	}
	if live := ResolveMedia(found, agent.deliverableRoots()); live.State != MediaAvailable {
		t.Fatalf("live = %+v", live)
	}
}

func TestShellOutputsAPythonChartIsPublished(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 on this machine")
	}
	agent, _, index := shellAgent(t)
	script := `python3 -c "import zlib,struct
def ch(t,d):
    c=struct.pack('>I',len(d))+t+d
    return c+struct.pack('>I',zlib.crc32(t+d)&0xffffffff)
raw=b'\x00\xff\x00\x00'
open('plot.png','wb').write(b'\x89PNG\r\n\x1a\n'+ch(b'IHDR',struct.pack('>IIBBBBB',1,1,8,2,0,0,0))+ch(b'IDAT',zlib.compress(b'\x00\xff\x00\x00'))+ch(b'IEND',b''))"`
	text, isError, err := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, script, "plot.png"))
	if err != nil || isError || !strings.Contains(text, "registered (created)") {
		t.Fatalf("%v %v %s", err, isError, text)
	}
	if rows := publishedRows(t, agent, index); len(rows) != 1 || rows[0].Fact.Width != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestShellOutputsAnExistingFileIsNeverInventedCreated(t *testing.T) {
	agent, workspace, index := shellAgent(t)
	if err := os.WriteFile(filepath.Join(workspace, "old.png"), shellPNG(t, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "edit.png"), shellPNG(t, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	command := writeCommand(shellPNG(t, 9), "edit.png")
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, command, "old.png", "edit.png"))
	if !strings.Contains(text, "Output old.png registered (unchanged).") || !strings.Contains(text, "Output edit.png registered (modified).") {
		t.Fatalf("%s", text)
	}
	for _, row := range publishedRows(t, agent, index) {
		if row.Artifact.Deliver != nil {
			t.Fatalf("%s is a Created deliverable: %+v", row.Artifact.Title, row.Artifact.Deliver)
		}
		if row.Fact.Change == ShellCreated {
			t.Fatalf("%s was invented created", row.Artifact.Title)
		}
	}
}

func TestShellOutputsOnlyDeclaredPathsAreLookedAt(t *testing.T) {
	agent, workspace, index := shellAgent(t)
	// A picture the agent only LOOKED AT is evidence, and stays that.
	seen := filepath.Join(workspace, "seen.png")
	if err := os.WriteFile(seen, shellPNG(t, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	agent.recordEvidence(callCtx(), "m", seen, "image/png", "image", shellPNG(t, 4), EvidenceLookedAt, "a picture", "m", false)
	command := writeCommand(shellPNG(t, 5), "stray.png") + " && " + writeCommand(shellPNG(t, 6), "chart.png")
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, command, "chart.png"))
	if !strings.Contains(text, "chart.png registered") || strings.Contains(text, "stray") {
		t.Fatalf("%s", text)
	}
	origins := map[string]int{}
	for _, row := range publishedRows(t, agent, index) {
		origins[row.Fact.Origin]++
		if strings.HasSuffix(row.Fact.Canonical, "stray.png") {
			t.Fatal("an undeclared file was registered")
		}
		if row.Fact.Origin == MediaEvidence && row.Fact.Change != "" {
			t.Fatal("evidence was promoted")
		}
	}
	if origins[MediaPublished] != 1 || origins[MediaEvidence] != 1 {
		t.Fatalf("origins = %v", origins)
	}
}

func TestShellOutputsRefuseWhatTheyCannotVouchFor(t *testing.T) {
	agent, workspace, index := shellAgent(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "away.png"), shellPNG(t, 7), 0o644); err != nil {
		t.Fatal(err)
	}
	// A large binary, a link to a real picture, a link to a folder elsewhere, a
	// text file, a directory, and an absolute path outside every root.
	big := make([]byte, maxImageBytes+1)
	copy(big, shellPNG(t, 8))
	os.WriteFile(filepath.Join(workspace, "big.png"), big, 0o644)
	os.WriteFile(filepath.Join(workspace, "real.png"), shellPNG(t, 8), 0o644)
	os.Symlink("real.png", filepath.Join(workspace, "link.png"))
	os.Symlink(outside, filepath.Join(workspace, "elsewhere"))
	os.WriteFile(filepath.Join(workspace, "notes.png"), []byte("not a picture at all"), 0o644)
	os.Mkdir(filepath.Join(workspace, "dir.png"), 0o755)
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, "true",
		"big.png", "link.png", "elsewhere/away.png", "notes.png", "dir.png", filepath.Join(outside, "away.png"), "missing.png"))
	for _, want := range []string{
		"Output big.png not registered: it is over the 10MB image limit.",
		"Output link.png not registered: it is not a regular file.",
		"Output elsewhere/away.png not registered: it is outside the working folder.",
		"Output notes.png not registered: it is not a picture it can vouch for.",
		"Output dir.png not registered: it is not a regular file.",
		"Output missing.png not registered: the command did not leave a file there.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, away(outside)+" not registered: it is outside the working folder.") {
		t.Errorf("the absolute outside path was not refused:\n%s", text)
	}
	if rows := publishedRows(t, agent, index); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func away(outside string) string { return filepath.Join(outside, "away.png") }

func TestShellOutputsABadDeclarationRunsNothing(t *testing.T) {
	agent, workspace, _ := shellAgent(t)
	nine := []string{"1.png", "2.png", "3.png", "4.png", "5.png", "6.png", "7.png", "8.png", "9.png"}
	for name, outputs := range map[string][]string{"nine": nine, "empty": {" "}, "twice": {"a.png", "./a.png"}, "directory": {"."}} {
		text, isError, err := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, "touch ran", outputs...))
		if err != nil || !isError || !strings.Contains(text, "Invalid arguments") {
			t.Errorf("%s: %v %v %s", name, err, isError, text)
		}
		if _, err := os.Stat(filepath.Join(workspace, "ran")); err == nil {
			t.Fatalf("%s: the command ran", name)
		}
	}
}

func TestShellOutputsNothingIsClaimedWhenTheCommandDidNotEndHere(t *testing.T) {
	agent, workspace, index := shellAgent(t)
	bash := beltTool(t, agent, "bash")
	// A non-zero exit.
	text, _, _ := bash.Execute(callCtx(), bashArgs(t, writeCommand(shellPNG(t, 1), "late.png")+"; exit 3", "late.png"))
	if !strings.Contains(text, "Outputs not registered") {
		t.Fatalf("%s", text)
	}
	// A background job: the job ends later, so nothing is promised.
	text, _, _ = bash.Execute(callCtx(), json.RawMessage(`{"command":"sleep 0.3; printf x > bg.png","background":true,"outputs":["bg.png"]}`))
	if !strings.Contains(text, "job ") || !strings.Contains(text, "Outputs not registered: a background job ends after this call.") {
		t.Fatalf("%s", text)
	}
	// A call cancelled mid-run.
	ctx, cancel := context.WithCancel(callCtx())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	text, _, _ = bash.Execute(ctx, bashArgs(t, writeCommand(shellPNG(t, 1), "cut.png")+"; sleep 5", "cut.png"))
	if !strings.Contains(text, "Outputs not registered") {
		t.Fatalf("%s", text)
	}
	time.Sleep(400 * time.Millisecond)
	_ = workspace
	if rows := publishedRows(t, agent, index); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestShellOutputsAPromotedCallClaimsNothing(t *testing.T) {
	agent, _, index := deliverableAgentWith(t, func(c *Config) { c.BashBackgroundAfterSeconds = 1 })
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, "sleep 3; "+writeCommand(shellPNG(t, 1), "slow.png"), "slow.png"))
	if !strings.HasPrefix(text, BashPromotedLead) && !strings.Contains(text, BashPromotedLead) {
		t.Skipf("the call was not promoted on this machine: %s", text)
	}
	if !strings.Contains(text, "Outputs not registered") {
		t.Fatalf("%s", text)
	}
	if rows := publishedRows(t, agent, index); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	agent.jobs.shutdown(time.Second)
}

func deliverableAgentWith(t *testing.T, mutate func(*Config)) (*Agent, string, string) {
	t.Helper()
	place := newPlace(t, false)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ArtifactsIndex = index
		config.Place = place
		config.SessionFile = filepath.Join(place.Dir, "transcript.jsonl")
		mutate(config)
	})
	return agent, workspace, index
}

func TestShellOutputsAReplacedFileIsNotVouchedFor(t *testing.T) {
	agent, _, index := shellAgent(t)
	var workspace = agent.config.Workspace
	shellOutputsAfterHash = func() {
		replacement := filepath.Join(workspace, "other.tmp")
		os.WriteFile(replacement, shellPNG(t, 99), 0o644)
		os.Rename(replacement, filepath.Join(workspace, "swap.png"))
	}
	t.Cleanup(func() { shellOutputsAfterHash = func() {} })
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, writeCommand(shellPNG(t, 1), "swap.png"), "swap.png"))
	if !strings.Contains(text, "Output swap.png not registered: it changed while it was being read.") {
		t.Fatalf("%s", text)
	}
	if rows := publishedRows(t, agent, index); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestShellOutputsAnotherRunningProcessMakesProvenanceUnknown(t *testing.T) {
	agent, _, index := shellAgent(t)
	if _, err := agent.jobs.start("sleep 30"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.jobs.shutdown(time.Second) })
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, writeCommand(shellPNG(t, 1), "who.png"), "who.png"))
	if !strings.Contains(text, "registered (unknown)") {
		t.Fatalf("%s", text)
	}
	rows := publishedRows(t, agent, index)
	if len(rows) != 1 || rows[0].Artifact.Deliver != nil {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestShellOutputsTheTotalReadBudgetAndDeadlineAreOne(t *testing.T) {
	agent, workspace, index := shellAgent(t)
	for _, name := range []string{"a.png", "b.png"} {
		os.WriteFile(filepath.Join(workspace, name), shellPNG(t, 1), 0o644)
	}
	old := shellOutputsHashBudget
	shellOutputsHashBudget = int64(len(shellPNG(t, 1))) + 1
	t.Cleanup(func() { shellOutputsHashBudget = old })
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, "true", "a.png", "b.png"))
	if !strings.Contains(text, "Output a.png registered") || !strings.Contains(text, "Output b.png not registered: the read budget for this call is spent.") {
		t.Fatalf("%s", text)
	}
	if rows := publishedRows(t, agent, index); len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	shellOutputsHashBudget = old
	oldDeadline := shellOutputsDeadline
	shellOutputsDeadline = time.Nanosecond
	t.Cleanup(func() { shellOutputsDeadline = oldDeadline })
	text, _, _ = beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, "true", "a.png"))
	if !strings.Contains(text, "ran out of time") {
		t.Fatalf("%s", text)
	}
}

func TestShellOutputsAFailedRecordIsUnknownNotComplete(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	agent, _, _ := deliverableAgentWith(t, func(c *Config) { c.ArtifactsIndex = filepath.Join(blocker, "artifacts.jsonl") })
	text, _, _ := beltTool(t, agent, "bash").Execute(callCtx(), bashArgs(t, writeCommand(shellPNG(t, 1), "x.png"), "x.png"))
	if !strings.Contains(text, "Output x.png: whether it was recorded is unknown.") || strings.Contains(text, "registered") {
		t.Fatalf("%s", text)
	}
}

func TestShellOutputsAbsentWhereTheyCannotWorkAndUntouchedWithout(t *testing.T) {
	agent, _, _ := shellAgent(t)
	bash := beltTool(t, agent, "bash")
	if !strings.Contains(string(bash.Schema), `"outputs"`) || !strings.Contains(bash.Description, "list its path in outputs") {
		t.Fatal("the model is not told about outputs")
	}
	text, _, _ := bash.Execute(callCtx(), bashArgs(t, "echo hi"))
	if strings.Contains(text, "Output") || strings.Contains(text, "registered") {
		t.Fatalf("a call without outputs was changed: %s", text)
	}
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.InTask = true; c.ArtifactsIndex = filepath.Join(t.TempDir(), "a.jsonl") })
	if strings.Contains(string(beltTool(t, worker, "bash").Schema), `"outputs"`) {
		t.Fatal("a task worker is offered outputs")
	}
	bare, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if strings.Contains(string(beltTool(t, bare, "bash").Schema), `"outputs"`) {
		t.Fatal("an agent with no index is offered outputs")
	}
}
