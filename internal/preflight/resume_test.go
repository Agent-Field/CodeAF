package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

// here is a machine that has the folders and answers the ports it is given.
type here struct {
	folders []string
	ports   []int
}

func (h here) Has(p string) bool {
	for _, f := range h.folders {
		if f == p {
			return true
		}
	}
	return false
}

func (h here) Answers(port int) bool {
	for _, p := range h.ports {
		if p == port {
			return true
		}
	}
	return false
}

func webRecord() inventory.Inventory {
	return inventory.Inventory{
		Workspace: "/Users/me/app",
		Withheld: []inventory.Withheld{
			{Path: "web/node_modules", Lock: "web/package-lock.json", MadeBy: "npm ci", Cwd: "web"},
			{Path: ".venv", Lock: "uv.lock"},
		},
		Running:  []inventory.Running{{Command: "npm run dev", Cwd: "web", Ports: []int{3000}}},
		Detached: []inventory.Detached{{Command: "docker compose up -d", Cwd: "."}},
	}
}

func needsNode() Report {
	return Report{Items: []Item{{Bucket: Installable, Name: "node", Want: "v22.4.0"}}}
}

func TestBriefTellsWhatWasLeftOutWhatWasRunningAndWhatIsNeeded(t *testing.T) {
	r := Compare("blackmac", webRecord(), here{}, needsNode())
	r.Now = "/home/me/app"
	want := strings.Join([]string{
		"This chat moved here from blackmac. This machine has none of what it left out or left running. Bring back only what is listed, then stop; do not explore the machine.",
		"Not brought along (rebuild from the file named):",
		"- web/node_modules: from web/package-lock.json; it was made with `npm ci` in web/",
		"- .venv: from uv.lock; usually `uv sync`",
		"Was running there, not running here:",
		"- `npm run dev` in web/, port 3000",
		"Started there outside this chat:",
		"- `docker compose up -d`",
		"Also needed here:",
		"- needs node 22.4 — agent can set it up",
		"",
	}, "\n")
	if got := r.Brief(); got != want {
		t.Fatalf("brief:\n%s\nwant:\n%s", got, want)
	}
}

func TestNewsIsOneParagraphThatAsksForNothing(t *testing.T) {
	r := Compare("blackmac", webRecord(), here{}, Report{})
	got := r.News()
	for _, part := range []string{
		"This chat moved here from blackmac.",
		"Not running here: `npm run dev` in web/ (was on port 3000).",
		"Not brought along, so absent: web/node_modules (npm ci), .venv (uv sync).",
		"This is news and asks for nothing",
		"never assume the server is up",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("news lacks %q:\n%s", part, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("news is more than a paragraph:\n%s", got)
	}
}

func TestNoOfferWhenNothingToRestore(t *testing.T) {
	inv := webRecord()
	r := Compare("blackmac", inv, here{folders: []string{"web/node_modules", ".venv"}, ports: []int{3000}}, Report{})
	r.Detached = nil
	if !r.Empty() || r.Brief() != "" || r.News() != "" {
		t.Fatalf("a takeover with nothing to say said something: %+v", r)
	}
	if !Compare("", inventory.Inventory{}, here{}, Report{}).Empty() {
		t.Fatal("an empty record is not an empty resume")
	}
}

func TestBriefOmitsAnsweringPort(t *testing.T) {
	inv := inventory.Inventory{Running: []inventory.Running{
		{Command: "npm run dev", Ports: []int{3000}},
		{Command: "worker"}, // no port to ask: listed
		{Command: "api", Ports: []int{8000, 8001}},
	}}
	r := Compare("", inv, here{ports: []int{3000, 8001}}, Report{})
	if len(r.Stopped) != 1 || r.Stopped[0].Command != "worker" {
		t.Fatalf("stopped = %+v: a command whose port answers here is running here", r.Stopped)
	}
}

func TestBriefSkipsFolderPresentHere(t *testing.T) {
	r := Compare("blackmac", webRecord(), here{folders: []string{"web/node_modules"}}, Report{})
	if len(r.Missing) != 1 || r.Missing[0].Path != ".venv" {
		t.Fatalf("missing = %+v: the machine that kept its own folder is not told to rebuild it", r.Missing)
	}
	if strings.Contains(r.Brief(), "web/node_modules") {
		t.Fatalf("brief names a folder that is here:\n%s", r.Brief())
	}
}

func TestBriefNamesFolderChange(t *testing.T) {
	inv := inventory.Inventory{Workspace: "/Users/me/app", Running: []inventory.Running{{Command: "python /Users/me/app/serve.py"}}}
	r := Compare("blackmac", inv, here{}, Report{})
	r.Now = "/home/me/app"
	for _, got := range []string{r.Brief(), r.News()} {
		if !strings.Contains(got, "The folder was /Users/me/app; here it is /home/me/app.") || !strings.Contains(got, "python /Users/me/app/serve.py") {
			t.Fatalf("the change is not said once beside the command as recorded:\n%s", got)
		}
	}
	quiet := Compare("blackmac", inventory.Inventory{Workspace: "/Users/me/app", Running: []inventory.Running{{Command: "npm run dev"}}}, here{}, Report{})
	quiet.Now = "/home/me/app"
	if strings.Contains(quiet.Brief(), "The folder was") {
		t.Fatalf("a folder change is said although no command names the old one:\n%s", quiet.Brief())
	}
}

func TestBriefAndRecordBounded(t *testing.T) {
	var inv inventory.Inventory
	for i := 0; i < 100; i++ {
		inv.Withheld = append(inv.Withheld, inventory.Withheld{Path: fmt.Sprintf("pkg%02d/node_modules", i), Lock: fmt.Sprintf("pkg%02d/package-lock.json", i), MadeBy: "npm ci", Cwd: fmt.Sprintf("pkg%02d", i)})
		inv.Running = append(inv.Running, inventory.Running{Command: strings.Repeat("serve ", 30) + fmt.Sprint(i), Cwd: "web", Ports: []int{3000 + i}})
	}
	inv.SetWithheld(func(string) bool { return true }, inv.Withheld)
	inv.SetRunning(inv.Running)
	r := Compare("blackmac", inv, here{}, Report{})
	for name, text := range map[string]string{"brief": r.Brief(), "news": r.News()} {
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		if len(lines) > maxLines || len(text) > maxChars {
			t.Fatalf("%s is %d lines and %d characters:\n%s", name, len(lines), len(text), text)
		}
		if !strings.Contains(text, "and ") || !strings.Contains(text, " more") {
			t.Fatalf("%s does not say how many more:\n%s", name, text)
		}
	}
}

func TestPlatformAloneIsNotSomethingToSetUp(t *testing.T) {
	report := Report{Items: []Item{{Bucket: Present, Severity: Advisory, Name: "platform", Want: "darwin/arm64", Have: "linux/amd64"}}}
	if !Compare("blackmac", inventory.Inventory{}, here{}, report).Empty() {
		t.Fatal("a different operating system raised an offer")
	}
	mismatch := Report{Items: []Item{{Bucket: Present, Severity: Advisory, Name: "node", Want: "22.4.0", Have: "20.1.0"}}}
	if got := Compare("", inventory.Inventory{}, here{}, mismatch).Lacks; len(got) != 1 {
		t.Fatalf("a version that differs is not listed: %+v", got)
	}
}

// ── the machine keeps what a takeover found until the agent is told ─────────

func arrived(t *testing.T) (*Machine, Resume, string) {
	t.Helper()
	root, workspace := t.TempDir(), t.TempDir()
	if err := inventory.Record(root, func(inv *inventory.Inventory) { *inv = webRecord(); inv.V = 1 }); err != nil {
		t.Fatal(err)
	}
	r, err := Arrive(root, workspace, "blackmac")
	if err != nil {
		t.Fatal(err)
	}
	m, err := OpenMachine(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	return m, r, root
}

func TestBriefAsNewsOnce(t *testing.T) {
	m, r, _ := arrived(t)
	if r.Empty() {
		t.Fatal("the takeover found nothing")
	}
	first := m.News()
	if !strings.Contains(first, "npm run dev") || !strings.Contains(first, "web/node_modules") {
		t.Fatalf("news:\n%s", first)
	}
	if again := m.News(); again != "" {
		t.Fatalf("the same news came twice:\n%s", again)
	}
}

func TestBriefSaidOncePerTakeover(t *testing.T) {
	m, _, root := arrived(t)
	_ = m.News()
	reopened, err := OpenMachine(root, m.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.News() != "" || reopened.Plan().Resume.Stopped != nil {
		t.Fatal("a chat closed and reopened after one takeover said it again")
	}
	// A takeover with nothing to say clears what an earlier one left.
	if _, err := Arrive(root, m.workspace, "blackmac"); err != nil {
		t.Fatal(err)
	}
	if err := inventory.Record(root, func(inv *inventory.Inventory) { *inv = inventory.Inventory{V: 1} }); err != nil {
		t.Fatal(err)
	}
	if _, err := Arrive(root, m.workspace, "blackmac"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, stashName)); err == nil {
		t.Fatal("an empty takeover left a stash behind")
	}
}

func TestPlanCarriesWhatOnlyTheTakeoverSawUntilSetupSettles(t *testing.T) {
	m, _, _ := arrived(t)
	plan := m.Plan()
	if len(plan.Resume.Stopped) != 1 || plan.Resume.From != "blackmac" || len(plan.Resume.Missing) != 2 {
		t.Fatalf("plan.Resume = %+v", plan.Resume)
	}
	if plan.Idle() {
		t.Fatal("a plan with folders to bring back is idle")
	}
	brief := plan.SetupBrief()
	if !strings.Contains(brief, "web/node_modules") || !strings.Contains(brief, "Only this folder is writable") {
		t.Fatalf("the setup turn's brief:\n%s", brief)
	}
	m.Settle(plan)
	if again := m.Plan().Resume; again.Stopped != nil || again.From != "" {
		t.Fatalf("the setup turn was not the last to be told: %+v", again)
	}
	if m.News() != "" {
		t.Fatal("news after a setup turn that already said it")
	}
}

func TestFoldersThatCameBackAreNotMissing(t *testing.T) {
	m, _, _ := arrived(t)
	for _, dir := range []string{"web/node_modules", ".venv"} {
		if err := os.MkdirAll(filepath.Join(m.workspace, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if plan := m.Plan(); len(plan.Resume.Missing) != 0 {
		t.Fatalf("missing = %+v after the setup turn brought them back", plan.Resume.Missing)
	}
}

func TestSetupBriefWithoutFactsIsTheToolBriefAsBefore(t *testing.T) {
	r := Report{Items: []Item{{Bucket: Installable, Name: "jq"}}}
	if got := r.SetupBrief(); !strings.HasPrefix(got, "This chat needs the following on this machine") || !strings.Contains(got, "- needs jq") {
		t.Fatalf("brief:\n%s", got)
	}
}

// The transcript names a job log by the folder the chat had on the other
// machine. When the chat's files were carried to another folder, the next step
// is told where they are, though nothing was left out or left running: it is
// news and not an offer.
func TestCarriedChatFilesAreToldWhereTheyAreWithoutRaisingAnOffer(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := inventory.Record(root, func(inv *inventory.Inventory) { inv.V = 1; inv.ChatDir = "/home/me/a/chat1" }); err != nil {
		t.Fatal(err)
	}
	r, err := Arrive(root, workspace, "box")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Empty() {
		t.Fatal("moved chat files alone raised an offer")
	}
	m, err := OpenMachine(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	news := m.News()
	if !strings.Contains(news, "name under /home/me/a/chat1 were carried: they are under "+root+" here") {
		t.Fatalf("news:\n%s", news)
	}
	if again := m.News(); again != "" {
		t.Fatalf("the same news came twice:\n%s", again)
	}
}

func TestAChatFileThatStayedBehindIsNamedWithWhy(t *testing.T) {
	inv := inventory.Inventory{Withheld: []inventory.Withheld{{Path: "logs/jobs/2.log", Reason: "over the 1 MiB carry limit for one file; it stayed on the machine that made it"}}}
	r := Compare("box", inv, here{}, Report{})
	for _, text := range []string{r.Brief(), r.News()} {
		if !strings.Contains(text, "logs/jobs/2.log") || !strings.Contains(text, "stayed on the machine that made it") {
			t.Errorf("text does not say the log stayed:\n%s", text)
		}
	}
}

func TestAChatFileIsLookedForInTheChatFolderNotTheWorkspace(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "logs/jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logs/jobs/2.log"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !(LocalHere{Root: root, Workspace: workspace}).Has("logs/jobs/2.log") {
		t.Error("a log in the chat folder was reported missing")
	}
}

func TestAnEntryWithAReasonIsToldWithItsReasonAndNoCommand(t *testing.T) {
	r := Resume{Missing: []inventory.Withheld{{Path: "odd.bin", Reason: "cannot be read on this machine"}}}
	if got := missingBrief(r.Missing[0]); got != "odd.bin: not brought along; cannot be read on this machine" {
		t.Fatalf("brief %q", got)
	}
	if got := missingNews(r.Missing[0]); got != "odd.bin (cannot be read on this machine)" {
		t.Fatalf("news %q", got)
	}
	if got := r.Grants(); len(got) != 0 {
		t.Fatalf("a path with no way to rebuild it was granted %v", got)
	}
	if !strings.Contains(r.Brief(), "odd.bin: not brought along; cannot be read") {
		t.Fatalf("brief lacks the entry:\n%s", r.Brief())
	}
}
