package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── A JOB'S PAGE IS A CARD, NOT A ROOM ──────────────────────────────────────

func jobNotice(id int, state session.JobState, command, log string) session.JobNotice {
	return session.JobNotice{
		ID:      id,
		Name:    "video",
		Command: command,
		State:   state,
		LogPath: log,
		Started: taskFixtureNow,
		Elapsed: 32 * time.Second,
	}
}

func jobLogFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "video.log")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the job log: %v", err)
	}
	return path
}

func appendJobLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening the job log: %v", err)
	}
	defer file.Close()
	for _, line := range lines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("appending to the job log: %v", err)
		}
	}
}

func jobPageApp(t *testing.T, job session.JobNotice) *app {
	t.Helper()
	a, _, _ := taskApp(t)
	a.jobUpdate(job)
	if !a.showJobPage(job.ID) {
		t.Fatal("showJobPage refused a job the window just filed")
	}
	if cmd := a.jobPageArm(); cmd != nil {
		takeJobLog(t, a, cmd)
	}
	return a
}

func takeJobLog(t *testing.T, a *app, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no log reading was armed")
	}
	msg := cmd()
	logMsg, ok := msg.(jobLogMsg)
	if !ok {
		t.Fatalf("got %T, want a log reading", msg)
	}
	return a.jobPageRead(logMsg)
}

func jobPageText(a *app) string {
	width, height := a.size()
	lines, _, _, _ := a.jobPageFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = ansi.Strip(line)
	}
	return strings.Join(out, "\n")
}

func jobPagePainted(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.jobPageFrame(width, height)
	return lines
}

func TestAJobsPageDrawsTheNameTheHandleAndTheCommand(t *testing.T) {
	const command = "ffmpeg -i in.mp4 -c:v libx264 out.mp4"
	a := jobPageApp(t, jobNotice(3, session.JobRunning, command, jobLogFile(t)))
	text := jobPageText(a)
	for _, want := range []string{"video", "job 3", command} {
		if !strings.Contains(text, want) {
			t.Fatalf("the page does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, prompt) {
		t.Fatalf("a job's page drew a composer:\n%s", text)
	}
}

// AN UNNAMED JOB'S TITLE IS THE HANDLE, NEVER THE COMMAND. The body already
// draws the command once; repeating it in the head hid the number and made the
// page say the same string twice.
func TestAnUnnamedJobsPageTitleIsTheHandle(t *testing.T) {
	const command = `sleep 50 && echo "job 4 done"`
	job := session.JobNotice{
		ID:      8,
		Command: command,
		State:   session.JobRunning,
		LogPath: jobLogFile(t),
		Started: taskFixtureNow,
		Elapsed: 33 * time.Second,
	}
	a := jobPageApp(t, job)
	width, _ := a.size()
	head, _ := a.jobPageTitleLine(width, job)
	title := ansi.Strip(head)
	if !strings.Contains(title, "job 8") {
		t.Fatalf("an unnamed job's title is not the handle: %q", title)
	}
	if strings.Contains(title, "sleep") || strings.Contains(title, command) {
		t.Fatalf("an unnamed job's title repeated the command: %q", title)
	}
	text := jobPageText(a)
	if !strings.Contains(text, command) {
		t.Fatalf("the body dropped the command:\n%s", text)
	}
	if strings.Count(text, "sleep 50") != 1 {
		t.Fatalf("the command was drawn more than once:\n%s", text)
	}
}

func TestARunningJobsFootOffersAStopAndASettledOneDoesNot(t *testing.T) {
	log := jobLogFile(t)
	running := jobPageApp(t, jobNotice(3, session.JobRunning, "ffmpeg -i in.mp4 out.mp4", log))
	// The sheet the foot DRAWS is the one without the way out, because the head
	// is on screen naming it (jobpage.go's [jobPageFootKeys]).
	if !strings.Contains(jobPageText(running), jobPageKeysRunHeld) {
		t.Fatalf("a running job's foot does not offer a stop:\n%s", jobPageText(running))
	}
	if !strings.Contains(jobPageText(running), "x stop it") {
		t.Fatalf("a running job's foot does not name x stop it:\n%s", jobPageText(running))
	}

	settled := jobPageApp(t, jobNotice(3, session.JobFailed, "ffmpeg -i in.mp4 out.mp4", log))
	text := jobPageText(settled)
	if !strings.Contains(text, jobPageKeysOverHeld) {
		t.Fatalf("a settled job's foot is not the ended legend:\n%s", text)
	}
	if strings.Contains(text, "x stop it") || strings.Contains(text, stopActWord) {
		t.Fatalf("a settled job still offers a stop:\n%s", text)
	}
}

func TestAJobsLogTailIsDimNewestAtTheBottomAndCutRatherThanWrapped(t *testing.T) {
	long := strings.Repeat("x", 80)
	log := jobLogFile(t, "ffmpeg started · 1200 frames", long)
	a := jobPageApp(t, jobNotice(3, session.JobRunning, "ffmpeg -i in.mp4 out.mp4", log))
	a.width = 40
	text := jobPageText(a)
	started := strings.Index(text, "ffmpeg started")
	later := strings.Index(text, "xx")
	if started < 0 || later < 0 {
		t.Fatalf("the tail is missing from the page:\n%s", text)
	}
	if started > later {
		t.Fatalf("the log is drawn newest-first:\n%s", text)
	}
	if strings.Contains(text, long) {
		t.Fatalf("a log line wrapped instead of cutting:\n%s", text)
	}
	if !strings.Contains(text, glyphMore) {
		t.Fatalf("a long log line was not cut:\n%s", text)
	}

	painted := jobPagePainted(a)
	want := a.pal.dim(fit("ffmpeg started · 1200 frames", a.width-2))
	found := false
	for _, line := range painted {
		if strings.Contains(line, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the log tail is not drawn dim; wanted %q in the painted frame", want)
	}
}

func TestAnEmptyOrMissingJobLogDrawsThePageRatherThanAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-written.log")
	a := jobPageApp(t, jobNotice(3, session.JobRunning, "ffmpeg -i in.mp4 out.mp4", missing))
	text := jobPageText(a)
	for _, want := range []string{"video", "job 3", "ffmpeg -i in.mp4 out.mp4"} {
		if !strings.Contains(text, want) {
			t.Fatalf("a job with no log yet dropped %q:\n%s", want, text)
		}
	}
	// THE PATH IS ON THE FOOT and names a file that is not there; that is a fact,
	// not an error. What must never appear is a sentence about the miss.
	for _, banned := range []string{"no such file", "cannot read", "failed to"} {
		if strings.Contains(strings.ToLower(text), banned) {
			t.Fatalf("the page shouted %q about a job that is running fine:\n%s", banned, text)
		}
	}
}

func TestAHostedJobPageNeverReadsThisDisk(t *testing.T) {
	log := jobLogFile(t, "THIS LAPTOP'S FILE")
	job := jobNotice(3, session.JobRunning, "ffmpeg -i in.mp4 out.mp4", log)
	a, _, _ := taskApp(t)
	a.host = "box"
	a.jobUpdate(job)
	a.showJobPage(job.ID)
	if cmd := a.jobPageArm(); cmd != nil {
		takeJobLog(t, a, cmd)
	}
	text := jobPageText(a)
	if strings.Contains(text, "THIS LAPTOP'S FILE") {
		t.Fatalf("a hosted job's page read a log on this disk:\n%s", text)
	}
	if !strings.Contains(text, jobPageHostedWord+"box") {
		t.Fatalf("a hosted job's page does not say the log is on the engine:\n%s", text)
	}
	if !strings.Contains(text, "box:"+log) {
		t.Fatalf("a hosted job's foot dropped the far path:\n%s", text)
	}
}

func TestAJobsLogReaderTakesOneLastReadingAfterTheJobEnds(t *testing.T) {
	log := jobLogFile(t, "ffmpeg started · 1200 frames")
	job := jobNotice(3, session.JobRunning, "ffmpeg -i in.mp4 out.mp4", log)
	a, _, _ := taskApp(t)
	a.jobUpdate(job)
	a.showJobPage(job.ID)
	next := takeJobLog(t, a, a.jobPageArm())
	if next == nil {
		t.Fatal("a running job's reader did not re-arm")
	}

	appendJobLog(t, log, "done · wrote out.mp4")
	job.State = session.JobDone
	a.jobUpdate(job)

	msg := next()
	tick, ok := msg.(farRoomTickMsg)
	if !ok {
		t.Fatalf("the running beat produced %T, want a tick", msg)
	}
	poll := a.farRoomPoll(tick.gen)
	next = takeJobLog(t, a, poll)
	if next == nil {
		t.Fatal("the page did not take one last reading after the job ended")
	}
	if text := jobPageText(a); !strings.Contains(text, "done · wrote out.mp4") {
		t.Fatalf("the last lines the process wrote never reached the page:\n%s", text)
	}

	msg = next()
	tick, ok = msg.(farRoomTickMsg)
	if !ok {
		t.Fatalf("the last reading's beat produced %T, want a tick", msg)
	}
	poll = a.farRoomPoll(tick.gen)
	next = takeJobLog(t, a, poll)
	if next != nil {
		t.Fatal("the reader kept going after its last reading")
	}
}

func TestAJobsLogLineIsStrippedOfWhatWouldRepaintTheFrame(t *testing.T) {
	got := jobLogLine("\x1b[31mbuild\x1b[0m\tfailed\x07 ")
	if got != "build    failed" {
		t.Fatalf("a raw log line drew as %q", got)
	}
}
