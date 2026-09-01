package video

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ── the measurements, which everything else is built out of ──────────────────

func TestASecondsFieldIsBelievedOnlyWhenItIsABelievableLength(t *testing.T) {
	// ffprobe states every number as a string and states "N/A" for the ones it
	// does not know, so the parse is where the emptiness law is enforced: a
	// length nobody measured must read as zero and never as a guess.
	for _, spoken := range []struct {
		text string
		want time.Duration
	}{
		{"2.000000", 2 * time.Second},
		{"0.5", 500 * time.Millisecond},
		{" 1.25 ", 1250 * time.Millisecond},
		{"N/A", 0},
		{"", 0},
		{"-3", 0},
		{"0", 0},
		{"inf", 0},
		{"nonsense", 0},
	} {
		if got := readSeconds(spoken.text); got != spoken.want {
			t.Errorf("readSeconds(%q) = %v, want %v", spoken.text, got, spoken.want)
		}
	}
}

func TestAFrameRateIsAFractionUntilItCannotBe(t *testing.T) {
	for _, spoken := range []struct {
		text string
		want float64
	}{
		{"30/1", 30},
		{"15/1", 15},
		{"24", 24},
		{"0/0", 0},
		{"", 0},
		{"30/0", 0},
		{"N/A", 0},
	} {
		got := readRate(spoken.text)
		if spoken.want == 0 && got != 0 {
			t.Errorf("readRate(%q) = %v, want no rate at all", spoken.text, got)
		}
		if spoken.want != 0 && (got < spoken.want-0.01 || got > spoken.want+0.01) {
			t.Errorf("readRate(%q) = %v, want %v", spoken.text, got, spoken.want)
		}
	}
	// The one that is a fraction on purpose: NTSC's 29.97.
	if got := readRate("30000/1001"); got < 29.96 || got > 29.98 {
		t.Errorf("readRate(30000/1001) = %v, want about 29.97", got)
	}
}

func TestTheContainersLengthWinsAndTheStreamsIsTheFallback(t *testing.T) {
	both := probeAnswer{}
	both.Format.Duration = "4.0"
	both.Streams = []probeStream{
		{CodecType: "video", Width: 640, Height: 360, Duration: "9.0", FrameRate: "25/1"},
		{CodecType: "audio"},
	}
	facts := both.facts()
	if facts.Length != 4*time.Second {
		t.Errorf("length = %v, want the container's 4s and not the stream's 9s", facts.Length)
	}
	if !facts.Sound {
		t.Error("a file with an audio stream must answer sound")
	}
	if facts.Width != 640 || facts.Height != 360 {
		t.Errorf("geometry = %dx%d, want 640x360", facts.Width, facts.Height)
	}

	// A container with no duration falls to the video stream's own.
	both.Format.Duration = "N/A"
	if facts := both.facts(); facts.Length != 9*time.Second {
		t.Errorf("length = %v, want the video stream's 9s when the container states none", facts.Length)
	}
}

func TestATimebaseIsNotAFrameRateAndIsNotBelievedAsOne(t *testing.T) {
	// The file this is written against is a webm out of a browser's
	// MediaRecorder, which is what a screen recording arrives as: it states
	// r_frame_rate 1000/1, because a millisecond is the finest timestamp its
	// container can spell, and avg_frame_rate 0/0, because it holds no constant
	// rate at all. Believed, that 1000 becomes the rate EVERY clip in a join is
	// resampled to.
	for _, spoken := range []struct {
		what    string
		average string
		stated  string
		want    float64
	}{
		{"a variable-rate webm out of a browser", "0/0", "1000/1", 0},
		{"ordinary NTSC material", "30000/1001", "30000/1001", 29.97},
		{"a believable average against a timebase", "25/1", "1000/1", 25},
		{"a stream that states only r_frame_rate", "0/0", "30/1", 30},
		{"a stream that states neither", "", "N/A", 0},
		{"a rate slower than one frame a second", "1/4", "1/4", 0},
		{"the fast end of the band, which real material reaches", "120/1", "120/1", fastestRate},
	} {
		var answer probeAnswer
		answer.Format.Duration = "5.0"
		answer.Streams = []probeStream{{
			CodecType: "video", Width: 640, Height: 360,
			AverageRate: spoken.average, FrameRate: spoken.stated,
		}}
		if got := answer.facts().Rate; got < spoken.want-0.01 || got > spoken.want+0.01 {
			t.Errorf("%s: rate = %v, want %v", spoken.what, got, spoken.want)
		}
	}

	// And the whole point of answering nothing: the join runs at its default
	// rather than at the timebase, which is the encode that never finishes.
	var webm probeAnswer
	webm.Format.Duration = "5.0"
	webm.Streams = []probeStream{{CodecType: "video", Width: 640, Height: 360, AverageRate: "0/0", FrameRate: "1000/1"}}
	clips := []Clip{{Path: "one.webm", Facts: webm.facts()}, {Path: "two.webm", Facts: webm.facts()}}
	if plan := strings.Join(joinPlan(clips, "cut.mp4"), " "); !contains(plan, fmt.Sprintf("fps=%g", joinDefaultRate)) {
		t.Errorf("a join of variable-rate clips must run at the %g fps default:\n%s", joinDefaultRate, plan)
	}
}

// ── the refusals that protect a file somebody paid for ───────────────────────

func TestWritingOverOneOfTheClipsBeingReadIsRefusedByName(t *testing.T) {
	// ffmpeg opens its output before it finishes reading its inputs, so this
	// would destroy the input and then fail to read it — losing a clip a
	// provider was paid for.
	home := t.TempDir()
	clip := filepath.Join(home, "first.mp4")
	if err := os.WriteFile(clip, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := prepare(clip, clip, filepath.Join(home, "second.mp4"))
	if err == nil {
		t.Fatal("writing a join over one of its own inputs must be refused")
	}
	if got := err.Error(); !contains(got, "first.mp4") || !contains(got, "destroy") {
		t.Errorf("refusal = %q, want it to name first.mp4 and say what would happen", got)
	}
}

func TestAJoinOfFewerThanTwoClipsIsRefusedWithTheNumber(t *testing.T) {
	_, err := Join(context.Background(), []string{"only.mp4"}, filepath.Join(t.TempDir(), "out.mp4"))
	if err == nil {
		t.Fatal("a join of one clip must be refused")
	}
	if !Available() {
		return // the refusal above was ErrMissing, which is its own answer
	}
	if got := err.Error(); !contains(got, "at least 2") {
		t.Errorf("refusal = %q, want it to name the floor", got)
	}
}

func TestAJoinOfTooManyClipsSaysHowManyAndWhatToDo(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg is not on PATH")
	}
	many := make([]string, joinLimit+1)
	for index := range many {
		many[index] = "clip.mp4"
	}
	_, err := Join(context.Background(), many, filepath.Join(t.TempDir(), "out.mp4"))
	if err == nil {
		t.Fatal("a join over the ceiling must be refused")
	}
	if got := err.Error(); !contains(got, "batches") {
		t.Errorf("refusal = %q, want it to name the way out", got)
	}
}

func TestAScoreWithNoLevelIsRefusedBeforeAnythingIsOpened(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg is not on PATH")
	}
	_, err := Score(context.Background(), "clip.mp4", "music.mp3",
		filepath.Join(t.TempDir(), "out.mp4"), Scoring{})
	if err == nil {
		t.Fatal("a score with no level must be refused")
	}
	if got := err.Error(); !contains(got, "how loud") {
		t.Errorf("refusal = %q, want it to say what a level is", got)
	}
}

// ── the whole thing, against the real encoder ───────────────────────────────

func TestAJoinKeepsTheSoundOfEveryClipAndTheLengthOfAllOfThem(t *testing.T) {
	// THE REGRESSION THIS PACKAGE EXISTS FOR. The silent clip is FIRST on
	// purpose: the natural hand-written join carries the first input's audio and
	// discards the rest, so a cut that begins with a silent clip comes out silent
	// throughout — and the file plays, looks right, and says nothing about it.
	home := requireEncoder(t)
	silent := madeClip(t, home, "silent.mp4", 1.5, false)
	loud := madeClip(t, home, "loud.mp4", 2, true)
	cut := filepath.Join(home, "cut.mp4")

	facts, err := Join(context.Background(), []string{silent, loud}, cut)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if !facts.Sound {
		t.Error("the joined cut has no audio stream at all")
	}
	if !about(facts.Length, 3500*time.Millisecond, 400*time.Millisecond) {
		t.Errorf("cut runs %v, want about 3.5s — the two clips end to end", facts.Length)
	}
	// The stream has to SPAN the cut, not merely exist: an audio stream that
	// stops when the first clip does is exactly the defect, and it is invisible
	// in everything except the ear and this assertion.
	if audio := audioLength(t, cut); !about(audio, facts.Length, 400*time.Millisecond) {
		t.Errorf("audio runs %v under a %v cut — it must run the whole way", audio, facts.Length)
	}
}

func TestTheJoinedCutsSoundSitsWhereItsOwnClipDid(t *testing.T) {
	// One rung above the test before it. That one proves the cut HAS an audio
	// stream spanning the whole thing, which a stream of silence would satisfy
	// just as well — and a join whose generated silence came out the wrong
	// length would produce exactly that: a full-length stream with the real
	// audio slid off the picture it belongs to. Desync is not visible in a
	// duration, so it is measured here in the one place it shows, which is how
	// loud each half actually is.
	home := requireEncoder(t)
	silent := madeClip(t, home, "silent.mp4", 2, false)
	loud := madeClip(t, home, "loud.mp4", 2, true)
	cut := filepath.Join(home, "cut.mp4")

	if _, err := Join(context.Background(), []string{silent, loud}, cut); err != nil {
		t.Fatalf("join: %v", err)
	}
	opening := loudness(t, cut, 0, 1.8)
	closing := loudness(t, cut, 2.2, 1.8)
	t.Logf("the cut measures %.1f dB over its silent clip and %.1f dB over its loud one", opening, closing)
	if opening > -40 {
		t.Errorf("the first two seconds measure %.1f dB — the silent clip's half of the cut is not silent, so the audio has slid", opening)
	}
	if closing < -40 {
		t.Errorf("the last two seconds measure %.1f dB — the loud clip's own sound is not under its own picture", closing)
	}
}

func TestAJoinLetterboxesEveryClipIntoTheFirstClipsFrame(t *testing.T) {
	home := requireEncoder(t)
	first := madeSized(t, home, "first.mp4", 320, 240, 1, true)
	second := madeSized(t, home, "second.mp4", 480, 270, 1, true)
	cut := filepath.Join(home, "cut.mp4")

	facts, err := Join(context.Background(), []string{first, second}, cut)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if facts.Width != 320 || facts.Height != 240 {
		t.Errorf("cut is %dx%d, want the first clip's 320x240", facts.Width, facts.Height)
	}
}

func TestAJoinRefusesAFileWithNoVideoInItAndNamesIt(t *testing.T) {
	home := requireEncoder(t)
	clip := madeClip(t, home, "clip.mp4", 1, true)
	music := madeMusic(t, home, "score.mp3", 1)

	_, err := Join(context.Background(), []string{clip, music}, filepath.Join(home, "cut.mp4"))
	if err == nil {
		t.Fatal("joining an mp3 as though it were a clip must be refused")
	}
	if got := err.Error(); !contains(got, "score.mp3") {
		t.Errorf("refusal = %q, want it to name the file that is not a video", got)
	}
}

func TestTheClosingFrameIsTheLastFrameAndNotTheFirstOne(t *testing.T) {
	// The closing frame is how one generated clip connects to the next, so
	// "which frame is it really" is load-bearing: a closing frame that was
	// silently the opening one would chain every shot back to its own start.
	home := requireEncoder(t)
	clip := madeClip(t, home, "clip.mp4", 2, false)
	opening := filepath.Join(home, "opening.png")
	closing := filepath.Join(home, "closing.png")

	if err := SaveFrame(context.Background(), clip, 0, opening); err != nil {
		t.Fatalf("opening frame: %v", err)
	}
	if err := SaveFrame(context.Background(), clip, Closing, closing); err != nil {
		t.Fatalf("closing frame: %v", err)
	}
	first, last := read(t, opening), read(t, closing)
	if len(first) == 0 || len(last) == 0 {
		t.Fatal("a frame was written empty")
	}
	if string(first) == string(last) {
		t.Error("the closing frame is byte-identical to the opening one — the seek from the end did nothing")
	}
	if string(first[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Errorf("a saved frame is not a png: % x", first[:8])
	}
}

func TestAScoredCutIsAsLongAsThePictureWithTheMusicLoopedToFit(t *testing.T) {
	// generate_music has no length argument, so a score is almost never the
	// length of the cut it is for. A one-second piece under a three-second clip
	// must loop, and the result must be the picture's length exactly.
	home := requireEncoder(t)
	clip := madeClip(t, home, "clip.mp4", 3, false)
	music := madeMusic(t, home, "score.mp3", 1)
	scored := filepath.Join(home, "scored.mp4")

	facts, err := Score(context.Background(), clip, music, scored, Scoring{Level: 1})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if !facts.Sound {
		t.Error("the scored cut has no audio stream")
	}
	if !about(facts.Length, 3*time.Second, 400*time.Millisecond) {
		t.Errorf("scored cut runs %v, want the picture's 3s", facts.Length)
	}
	if audio := audioLength(t, scored); !about(audio, 3*time.Second, 400*time.Millisecond) {
		t.Errorf("score runs %v under a 3s picture — the loop did not fill it", audio)
	}
}

func TestAScoreTrimsAPieceThatIsLongerThanTheCut(t *testing.T) {
	home := requireEncoder(t)
	clip := madeClip(t, home, "clip.mp4", 1, false)
	music := madeMusic(t, home, "score.mp3", 4)
	scored := filepath.Join(home, "scored.mp4")

	facts, err := Score(context.Background(), clip, music, scored, Scoring{Level: 0.4, Fade: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if !about(facts.Length, time.Second, 300*time.Millisecond) {
		t.Errorf("scored cut runs %v, want the picture's 1s — a longer piece is trimmed", facts.Length)
	}
}

func TestAFailedRunLeavesNoHalfWrittenFileBehind(t *testing.T) {
	// A truncated mp4 is the worst artefact of all: it exists, it has a
	// plausible size, and nothing about it says it is the wreck of a command
	// that failed.
	home := requireEncoder(t)
	broken := filepath.Join(home, "broken.mp4")
	if err := os.WriteFile(broken, []byte("this is not an mp4 at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(home, "out.png")
	if err := SaveFrame(context.Background(), broken, 0, out); err == nil {
		t.Fatal("a frame out of a file that is not a video must fail")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("the failed frame left a file behind")
	}
}

func TestAFailedRunCannotDeleteAFileItNeverWrote(t *testing.T) {
	// THE OTHER HALF OF THE TEST ABOVE, AND THE MORE EXPENSIVE ONE. ffmpeg
	// opens its output before it knows whether the job is possible, so an
	// operation that removes its destination on failure destroys whatever was
	// already at that path on every failure that happens early — an unreadable
	// input, an unknown muxer, a graph that will not initialise. None of them
	// wrote a byte, and the file they took with them may be one a provider was
	// paid for.
	home := requireEncoder(t)
	broken := filepath.Join(home, "broken.mp4")
	if err := os.WriteFile(broken, []byte("this is not an mp4 at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	standing := filepath.Join(home, "standing.png")
	was := []byte("a picture the person already had, worth more than this run")
	if err := os.WriteFile(standing, was, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveFrame(context.Background(), broken, 0, standing); err == nil {
		t.Fatal("a frame out of a file that is not a video must fail")
	}
	now, err := os.ReadFile(standing)
	if err != nil {
		t.Fatalf("the failed run deleted a file it never wrote: %v", err)
	}
	if string(now) != string(was) {
		t.Errorf("the file at the destination is %q, want the %q it was before the run", now, was)
	}
	// And nothing of the wreck is left beside it either.
	if entries, err := os.ReadDir(home); err == nil && len(entries) != 2 {
		t.Errorf("%d files in the directory, want only the two that were there: %v", len(entries), entries)
	}
}

func TestAFrameAskedForPastTheEndIsRefusedWithTheClipsOwnLength(t *testing.T) {
	// ffmpeg does not refuse this: a seek past the last frame decodes nothing,
	// writes nothing and exits 0, so the caller used to answer "saved the frame"
	// over an empty png — which the model then hands to the next render as the
	// shot it is continuing from.
	home := requireEncoder(t)
	clip := madeClip(t, home, "clip.mp4", 2, false)
	out := filepath.Join(home, "past.png")

	err := SaveFrame(context.Background(), clip, 9*time.Second, out)
	if err == nil {
		t.Fatal("a frame nine seconds into a two-second clip must be refused")
	}
	if got := err.Error(); !contains(got, "clip.mp4") || !contains(got, "runs 2") {
		t.Errorf("refusal = %q, want it to name the clip and how long it actually runs", got)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("the refused frame left a file behind")
	}
	// The closing frame of the same clip is a seek from the END and is not
	// touched by any of this.
	if err := SaveFrame(context.Background(), clip, Closing, filepath.Join(home, "closing.png")); err != nil {
		t.Errorf("the closing frame must still be saved: %v", err)
	}
}

func TestAScoreRunsTheWholePictureWhenTheClipsOwnSoundStopsFirst(t *testing.T) {
	// A clip whose audio ends before its picture does is ordinary — a recording
	// muxed under a longer shot, a render whose sound was trimmed — and mixing
	// keyed on that track ends the score where it ends, leaving a cut that goes
	// quiet part way through with nothing said about it. It is the join's
	// silence bug wearing the score's clothes, and the ear is the only place it
	// shows.
	home := requireEncoder(t)
	clip := madeShortSoundClip(t, home, "brief.mp4", 3, 1)
	music := madeMusic(t, home, "score.mp3", 1)
	scored := filepath.Join(home, "scored.mp4")

	facts, err := Score(context.Background(), clip, music, scored, Scoring{Level: 0.3})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if !about(facts.Length, 3*time.Second, 400*time.Millisecond) {
		t.Errorf("scored cut runs %v, want the picture's 3s", facts.Length)
	}
	if audio := audioLength(t, scored); !about(audio, 3*time.Second, 400*time.Millisecond) {
		t.Errorf("the sound runs %v under a 3s picture — it stopped with the clip's own second of audio", audio)
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

// requireEncoder skips rather than fails on a machine with no ffmpeg, which is
// the same posture every other shelling-out test in this repository takes.
func requireEncoder(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skipf("%s is not on PATH; this package is a wrapper around it", Missing())
	}
	return t.TempDir()
}

// madeClip is a generated clip: a moving test pattern so consecutive frames
// actually differ, with or without a tone under it.
func madeClip(t *testing.T, home, name string, length float64, sound bool) string {
	t.Helper()
	return madeSizedSound(t, home, name, 320, 240, length, sound)
}

func madeSized(t *testing.T, home, name string, width, height int, length float64, sound bool) string {
	t.Helper()
	return madeSizedSound(t, home, name, width, height, length, sound)
}

func madeSizedSound(t *testing.T, home, name string, width, height int, length float64, sound bool) string {
	t.Helper()
	path := filepath.Join(home, name)
	args := []string{"-v", "error", "-y",
		"-f", "lavfi", "-i", pattern(width, height, length)}
	if sound {
		args = append(args, "-f", "lavfi", "-i", tone(length))
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
	if sound {
		args = append(args, "-c:a", "aac", "-shortest")
	}
	args = append(args, path)
	if out, err := exec.Command(ffmpegBinary, args...).CombinedOutput(); err != nil {
		t.Fatalf("could not make the fixture %s: %v\n%s", name, err, out)
	}
	return path
}

func madeMusic(t *testing.T, home, name string, length float64) string {
	t.Helper()
	path := filepath.Join(home, name)
	out, err := exec.Command(ffmpegBinary, "-v", "error", "-y",
		"-f", "lavfi", "-i", tone(length), path).CombinedOutput()
	if err != nil {
		t.Fatalf("could not make the fixture %s: %v\n%s", name, err, out)
	}
	return path
}

// madeShortSoundClip is the one shape madeSizedSound cannot make: a picture
// whose own audio track stops before the picture does. Every other fixture here
// passes -shortest so that the two come out the same length, and this one
// leaves it off on purpose — the tone runs out and the pattern carries on,
// which is what a recording muxed under a longer shot looks like.
func madeShortSoundClip(t *testing.T, home, name string, picture, sound float64) string {
	t.Helper()
	path := filepath.Join(home, name)
	out, err := exec.Command(ffmpegBinary, "-v", "error", "-y",
		"-f", "lavfi", "-i", pattern(320, 240, picture),
		"-f", "lavfi", "-i", tone(sound),
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", path).CombinedOutput()
	if err != nil {
		t.Fatalf("could not make the fixture %s: %v\n%s", name, err, out)
	}
	return path
}

func pattern(width, height int, length float64) string {
	return fmt.Sprintf("testsrc=size=%dx%d:rate=15:duration=%g", width, height, length)
}

func tone(length float64) string {
	return fmt.Sprintf("sine=frequency=440:duration=%g", length)
}

// audioLength is the first audio stream's own duration, which is the fact that
// catches a cut whose sound stops early.
func audioLength(t *testing.T, path string) time.Duration {
	t.Helper()
	spoken, err := exec.Command(ffprobeBinary, "-v", "error",
		"-select_streams", "a:0", "-show_entries", "stream=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		t.Fatalf("could not measure the audio of %s: %v", path, err)
	}
	return readSeconds(string(spoken))
}

// loudness is the mean volume in dB of one stretch of a file, read out of
// ffmpeg's own volumedetect filter. Digital silence reports -91 dB or lower; a
// tone at full scale reports around -3.
func loudness(t *testing.T, path string, from, length float64) float64 {
	t.Helper()
	said, err := exec.Command(ffmpegBinary, "-hide_banner", "-nostdin",
		"-ss", fmt.Sprintf("%g", from), "-t", fmt.Sprintf("%g", length),
		"-i", path, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("could not measure the loudness of %s: %v\n%s", path, err, said)
	}
	const marker = "mean_volume:"
	at := strings.Index(string(said), marker)
	if at < 0 {
		t.Fatalf("volumedetect said nothing about the mean volume of %s:\n%s", path, said)
	}
	spoken := strings.Fields(string(said)[at+len(marker):])
	if len(spoken) == 0 {
		t.Fatalf("volumedetect gave no figure for %s", path)
	}
	if spoken[0] == "-inf" {
		return -200
	}
	measured, err := strconv.ParseFloat(spoken[0], 64)
	if err != nil {
		t.Fatalf("volumedetect said %q, which is not a number of decibels", spoken[0])
	}
	return measured
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	return data
}

func about(got, want, slack time.Duration) bool {
	if got < want {
		got, want = want, got
	}
	return got-want <= slack
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
