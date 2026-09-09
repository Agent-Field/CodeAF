package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/video"
)

// ── the belt's answer, which is a fact about the machine ─────────────────────

func TestEditVideoIsOnTheBeltOnlyWhenFfmpegIsOnTheMachine(t *testing.T) {
	// The absence law, on the one gate this verb has. PATH is emptied rather
	// than a seam being injected because that is what "the machine has no
	// ffmpeg" actually is, and a test of the real lookup is a test of the real
	// gate.
	t.Setenv("PATH", "")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if hasTool(agent, "edit_video") {
		t.Fatal("edit_video is on the belt on a machine with no ffmpeg — a capability that cannot work is absent, not broken")
	}
}

func TestEditVideoNeedsNoModelNoKeyAndNoMoney(t *testing.T) {
	// THE INTERESTING DIFFERENCE from the five making verbs. They are gated on
	// the person's settings resolving a model for their modality; this one buys
	// nothing, so it must be present on a machine that cannot generate a single
	// frame — where cutting together footage somebody already has is the only
	// video work there is.
	requireFfmpeg(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if hasTool(agent, "generate_video") {
		t.Fatal("this agent has no media client, so generate_video should be absent")
	}
	if !hasTool(agent, "edit_video") {
		t.Fatal("edit_video needs nothing from settings and must be on the belt anyway")
	}
}

// ── the refusals, all of which happen before ffmpeg is started ───────────────

func TestEveryActionRefusesAFileItCannotReadAndSaysWhichArgumentItWas(t *testing.T) {
	// A path with a typo in it must cost nothing and must be named. The noun
	// matters as much as the name: this verb takes three different kinds of path
	// and "could not read x" would leave the model guessing which one it meant.
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	real := madeVideo(t, workspace, "real.mp4", 1, true)

	for _, testCase := range []struct {
		name  string
		args  string
		wants []string
	}{
		{"measure", `{"action":"measure","video":"nope.mp4"}`, []string{"video", "nope.mp4"}},
		{"frame", `{"action":"frame","video":"nope.mp4"}`, []string{"video", "nope.mp4"}},
		{"join", `{"action":"join","clips":["` + real + `","nope.mp4"]}`, []string{"clip", "nope.mp4"}},
		{"score", `{"action":"score","video":"` + real + `","audio":"nope.mp3"}`, []string{"audio", "nope.mp3"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, isError := runTool(t, agent, "edit_video", testCase.args)
			if !isError {
				t.Fatalf("a missing file must be refused, got %q", result)
			}
			for _, want := range testCase.wants {
				if !strings.Contains(result, want) {
					t.Errorf("refusal %q does not name %q", result, want)
				}
			}
		})
	}
}

func TestAnUnknownOrMissingActionNamesTheFourThatExist(t *testing.T) {
	requireFfmpeg(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, args := range []string{`{"action":"transcode"}`, `{}`} {
		result, isError := runTool(t, agent, "edit_video", args)
		if !isError {
			t.Fatalf("%s must be refused, got %q", args, result)
		}
		for _, action := range []string{editVideoMeasure, editVideoFrame, editVideoJoin, editVideoScore} {
			if !strings.Contains(result, action) {
				t.Errorf("refusal %q for %s does not name the %s action", result, args, action)
			}
		}
	}
}

func TestAJoinOfOneClipOrNoneIsRefusedRatherThanCopied(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 1, true)

	result, isError := runTool(t, agent, "edit_video", `{"action":"join","clips":[]}`)
	if !isError || !strings.Contains(result, "clips") {
		t.Errorf("an empty join must be refused naming the argument, got %q", result)
	}
	result, isError = runTool(t, agent, "edit_video", `{"action":"join","clips":["`+clip+`"]}`)
	if !isError {
		t.Errorf("a join of one clip must be refused, got %q", result)
	}
}

// THE NAME IS CLAIMED BEFORE THE WORK IS ATTEMPTED, so a refusal has to give it
// back. mediaDestination creates the default timestamped file empty and holds it
// with O_EXCL — which is how two calls of one batch cannot be handed one name —
// and every refusal the library makes on its own facts happens after that. Each
// one of them used to leave a nought-byte mp4 in the person's folder that
// `/files` and the person then had to make sense of.
func TestARefusedActionLeavesNothingWhereItWouldHaveWritten(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 1, false)
	silent := madeVideo(t, workspace, "quiet.mp4", 1, false)

	// A join of one clip is the cheapest of them: the library refuses on the
	// count alone, before ffmpeg is started and after the name has been taken.
	result, isError := runTool(t, agent, "edit_video", fmt.Sprintf(`{"action":"join","clips":[%q]}`, clip))
	if !isError {
		t.Fatalf("a join of one clip must be refused, got %q", result)
	}
	if left := filesIn(t, VideoDir(agent.config.Place, workspace)); len(left) != 0 {
		t.Errorf("a refused join left %v behind — a claimed name a refusal did not fill is litter, and /files offers it", left)
	}

	// The same for a score the library refuses on the audio it was given: a
	// video file with no sound in it is not something to lay under a picture.
	result, isError = runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"score","video":%q,"audio":%q}`, clip, silent))
	if !isError {
		t.Fatalf("a score with a silent audio file must be refused, got %q", result)
	}
	if left := filesIn(t, VideoDir(agent.config.Place, workspace)); len(left) != 0 {
		t.Errorf("a refused score left %v behind", left)
	}
}

// AND IT NEVER DELETES SOMEBODY'S FILE, which is the other half of the same
// rule. A path the model NAMED is not claimed by this belt at all — it may
// already hold work a person or a provider was paid for — so a refusal on that
// road leaves it exactly as it found it.
func TestARefusalNeverRemovesAFileTheModelNamedItself(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 1, false)

	kept := filepath.Join(workspace, "keep.mp4")
	if err := os.WriteFile(kept, []byte("somebody's film"), 0o644); err != nil {
		t.Fatalf("could not write the fixture: %v", err)
	}
	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"join","clips":[%q],"path":"keep.mp4"}`, clip))
	if !isError {
		t.Fatalf("a join of one clip must be refused, got %q", result)
	}
	held, err := os.ReadFile(kept)
	if err != nil || string(held) != "somebody's film" {
		t.Errorf("the refusal took keep.mp4 (%q, %v) — a file with bytes in it is somebody's", held, err)
	}
}

func TestAFrameSavedAsSomethingThatIsNotAPictureIsRefusedByExtension(t *testing.T) {
	// ffmpeg's own complaint about an unknown muxer is not a sentence anybody
	// can act on, so the extension is checked here where the reason is known.
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 1, false)

	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"frame","video":%q,"path":"still.mp4"}`, clip))
	if !isError {
		t.Fatalf("a frame saved as an mp4 must be refused, got %q", result)
	}
	if !strings.Contains(result, "png") {
		t.Errorf("refusal %q does not say what a frame can be saved as", result)
	}
}

func TestAScoreLevelOfZeroIsRefusedAndPointsAtReplace(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 1, true)
	music := madeAudio(t, workspace, "score.mp3", 1)

	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"score","video":%q,"audio":%q,"level":0}`, clip, music))
	if !isError {
		t.Fatalf("a level of zero must be refused, got %q", result)
	}
	if !strings.Contains(result, "replace") {
		t.Errorf("refusal %q does not name the argument that does what a zero level was reaching for", result)
	}
}

// ── which frame `at` actually means ─────────────────────────────────────────

func TestTheDefaultFrameIsTheClosingOneBecauseThatIsTheOneThatChains(t *testing.T) {
	// The closing frame is the only thread that makes two independent renders
	// look like one continuous take, so it is the default: a model made to name
	// the moment will more often name the wrong one than the right one.
	at, said, refusal := frameMoment("")
	if refusal != "" {
		t.Fatalf("an absent at must be accepted: %s", refusal)
	}
	if at != video.Closing {
		t.Errorf("at = %v, want the closing frame", at)
	}
	if said != "the closing frame" {
		t.Errorf("said = %q, want it to name the closing frame", said)
	}
}

func TestFrameMomentReadsTheWordsPeopleActuallyUse(t *testing.T) {
	for _, spoken := range []struct {
		word  string
		want  string
		valid bool
	}{
		{"closing", "the closing frame", true},
		{"last", "the closing frame", true},
		{"final", "the closing frame", true},
		{"end", "the closing frame", true},
		{"opening", "the opening frame", true},
		{"first", "the opening frame", true},
		{"start", "the opening frame", true},
		{"3", "the frame at 3s", true},
		{"2.5s", "the frame at 2.5s", true},
		{"halfway", "", false},
		{"-1", "", false},
	} {
		_, said, refusal := frameMoment(spoken.word)
		if spoken.valid && refusal != "" {
			t.Errorf("at=%q was refused: %s", spoken.word, refusal)
			continue
		}
		if !spoken.valid {
			if refusal == "" {
				t.Errorf("at=%q should be refused", spoken.word)
			} else if !strings.Contains(refusal, spoken.word) {
				t.Errorf("refusal %q does not name the word it refused", refusal)
			}
			continue
		}
		if said != spoken.want {
			t.Errorf("at=%q said %q, want %q", spoken.word, said, spoken.want)
		}
	}
}

// ── the whole verb, against real ffmpeg ─────────────────────────────────────

func TestMeasureAnswersTheFactsACutIsPlannedFrom(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 2, true)

	result, isError := runTool(t, agent, "edit_video", fmt.Sprintf(`{"action":"measure","video":%q}`, clip))
	if isError {
		t.Fatalf("measure failed: %s", result)
	}
	for _, want := range []string{"clip.mp4", "with sound", "320×240", "mp4 video"} {
		if !strings.Contains(result, want) {
			t.Errorf("measure said %q, want it to carry %q", result, want)
		}
	}
	if !strings.Contains(result, "2.0s") {
		t.Errorf("measure said %q, want the measured length", result)
	}
}

func TestASilentClipIsMeasuredAsWithoutSoundAndNotAsNothing(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "quiet.mp4", 1, false)

	result, isError := runTool(t, agent, "edit_video", fmt.Sprintf(`{"action":"measure","video":%q}`, clip))
	if isError {
		t.Fatalf("measure failed: %s", result)
	}
	if !strings.Contains(result, "without sound") {
		t.Errorf("measure said %q — a silent clip must say so, because it decides whether a join needs silence", result)
	}
}

func TestAJoinedCutIsMeasuredBackOffTheFileAndKeepsItsSound(t *testing.T) {
	// The end-to-end shape of the regression internal/video exists for: a cut
	// that opens on a silent clip must still carry the later clips' sound, and
	// the answer must be measured off the file rather than claimed.
	requireFfmpeg(t)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ArtifactsIndex = index
	})
	silent := madeVideo(t, workspace, "one.mp4", 1.5, false)
	loud := madeVideo(t, workspace, "two.mp4", 2, true)

	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"join","clips":[%q,%q]}`, silent, loud))
	if isError {
		t.Fatalf("join failed: %s", result)
	}
	if !strings.Contains(result, "with sound") {
		t.Errorf("the joined cut said %q — a cut whose second clip has sound must carry it", result)
	}
	if !strings.Contains(result, "joined from 2 clips") {
		t.Errorf("the answer %q does not say what it was made of", result)
	}
	// Both clips end to end, and not one of them: the exact tenth is the
	// encoder's to decide once every clip has been resampled to one frame rate,
	// so the assertion is on the second — three-and-something rather than one or
	// two.
	if !regexp.MustCompile(`\b3\.\ds with sound\b`).MatchString(result) {
		t.Errorf("the answer %q does not carry the measured length of both clips", result)
	}
	// And it landed where this session keeps its video, with a row in the index.
	cut := onlyFileIn(t, filepath.Join(workspace, ".aforge-v3", "video"))
	if !strings.HasSuffix(cut, ".mp4") {
		t.Errorf("the cut landed as %s, want an mp4", cut)
	}
	if rows := ReadArtifacts(index); len(rows) != 1 || rows[0].Kind != "video" {
		t.Errorf("artifact rows = %+v, want one video row so /files can find the cut again", rows)
	}
}

func TestASavedFrameLandsWithThePicturesAndNamesWhichFrameItIs(t *testing.T) {
	requireFfmpeg(t)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ArtifactsIndex = index
	})
	clip := madeVideo(t, workspace, "clip.mp4", 2, false)

	result, isError := runTool(t, agent, "edit_video", fmt.Sprintf(`{"action":"frame","video":%q}`, clip))
	if isError {
		t.Fatalf("frame failed: %s", result)
	}
	if !strings.Contains(result, "the closing frame of clip.mp4") {
		t.Errorf("the answer %q does not say which frame of which clip it is", result)
	}
	if !strings.Contains(result, "320×240 png") {
		t.Errorf("the answer %q does not carry the picture's measured shape", result)
	}
	// A frame is a PICTURE: it lands with the pictures, because the next thing
	// that happens to it is being handed to generate_video as a frame.
	saved := onlyFileIn(t, filepath.Join(workspace, ".aforge-v3", "images"))
	if filepath.Ext(saved) != ".png" {
		t.Errorf("the frame landed as %s, want a png", saved)
	}
	if rows := ReadArtifacts(index); len(rows) != 1 || rows[0].Kind != "image" {
		t.Errorf("artifact rows = %+v, want one image row", rows)
	}
	// The result names the path ABSOLUTELY, as every other picture tool does:
	// the line is what a person is shown in place of the picture.
	if !strings.HasPrefix(result, "/") {
		t.Errorf("the answer %q does not start with the whole path", result)
	}
}

func TestAScoreFitsTheMusicToThePictureAndSaysWhichWay(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 3, false)
	short := madeAudio(t, workspace, "short.mp3", 1)
	long := madeAudio(t, workspace, "long.mp3", 6)

	looped, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"score","video":%q,"audio":%q,"fade":0.5}`, clip, short))
	if isError {
		t.Fatalf("score failed: %s", looped)
	}
	if !strings.Contains(looped, "short.mp3 looped to fit") {
		t.Errorf("the answer %q does not say the piece was looped — which is when a fade is worth asking for", looped)
	}
	if !strings.Contains(looped, "with sound") || !strings.Contains(looped, "3.0s") {
		t.Errorf("the answer %q does not carry the scored cut's measured facts", looped)
	}

	trimmed, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"score","video":%q,"audio":%q}`, clip, long))
	if isError {
		t.Fatalf("score failed: %s", trimmed)
	}
	if !strings.Contains(trimmed, "long.mp3 trimmed to fit") {
		t.Errorf("the answer %q does not say the piece was trimmed", trimmed)
	}
}

func TestAScoreCanBeLaidUnderAClipThatAlreadyHasSound(t *testing.T) {
	requireFfmpeg(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	clip := madeVideo(t, workspace, "clip.mp4", 2, true)
	music := madeAudio(t, workspace, "score.mp3", 1)

	result, isError := runTool(t, agent, "edit_video",
		fmt.Sprintf(`{"action":"score","video":%q,"audio":%q}`, clip, music))
	if isError {
		t.Fatalf("score failed: %s", result)
	}
	if !strings.Contains(result, "with sound") {
		t.Errorf("the answer %q does not report sound on a cut that has both its own and a score", result)
	}
}

func TestTheSchemaIsOneWellFormedObjectTheModelCanRead(t *testing.T) {
	requireFfmpeg(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if text, failed := runTool(t, agent, loadCapabilityToolName, `{"group":"media"}`); failed {
		t.Fatalf("loading media: %s", text)
	}
	for _, tool := range agent.beltTools() {
		if tool.Name != "edit_video" {
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.Schema, &schema); err != nil {
			t.Fatalf("edit_video's schema does not parse: %v", err)
		}
		if _, has := schema["properties"]; !has {
			t.Fatal("edit_video's schema has no properties")
		}
		// The ceiling is interpolated from the library's own constant, never
		// typed twice: a figure in a description is read by the model as a fact
		// about the machine and a stale one is a lie it reasons from.
		if !strings.Contains(tool.Description, "5 minutes") {
			t.Errorf("the description does not state the ceiling: %s", tool.Description)
		}
		return
	}
	t.Fatal("edit_video is not on the belt")
}

// ── fixtures ────────────────────────────────────────────────────────────────

// requireFfmpeg skips rather than fails, which is the posture every other
// shelling-out test in this repository takes.
func requireFfmpeg(t *testing.T) {
	t.Helper()
	if !video.Available() {
		t.Skipf("%s is not on PATH; edit_video is absent from the belt without it", video.Missing())
	}
}

func madeVideo(t *testing.T, home, name string, length float64, sound bool) string {
	t.Helper()
	path := filepath.Join(home, name)
	args := []string{"-v", "error", "-y", "-f", "lavfi",
		"-i", fmt.Sprintf("testsrc=size=320x240:rate=15:duration=%g", length)}
	if sound {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%g", length))
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
	if sound {
		args = append(args, "-c:a", "aac", "-shortest")
	}
	if out, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput(); err != nil {
		t.Fatalf("could not make the fixture %s: %v\n%s", name, err, out)
	}
	return path
}

func madeAudio(t *testing.T, home, name string, length float64) string {
	t.Helper()
	path := filepath.Join(home, name)
	out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=330:duration=%g", length), path).CombinedOutput()
	if err != nil {
		t.Fatalf("could not make the fixture %s: %v\n%s", name, err, out)
	}
	return path
}

// filesIn is what a directory holds, and a directory that was never made holds
// nothing — which is the answer a test of litter wants, because "no folder at
// all" and "an empty folder" are the same clean workspace.
func filesIn(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func onlyFileIn(t *testing.T, directory string) string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("nothing landed in %s: %v", directory, err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files landed in %s, want exactly one", len(entries), directory)
	}
	return entries[0].Name()
}
