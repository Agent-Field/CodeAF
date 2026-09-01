package video

// The plans are tested as DATA, with no encoder involved.
//
// A filter graph is the part of this package that is easy to get subtly wrong
// and nearly impossible to eyeball in a passing test: a graph that drops one
// clip's audio still encodes, still plays, and is wrong only to the ear. So the
// laws are asserted on the argument list itself — every clip has an audio chain,
// the mix does not normalize, the score is looped and bounded — and the tests in
// video_test.go then prove that a plan these accept is one ffmpeg accepts.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEveryClipInAJoinGetsAnAudioChainWhetherItHasSoundOrNot(t *testing.T) {
	// THE AUDIO LAW, asserted on the graph. Three clips, only the middle one
	// carrying sound: the cut must still be built out of three video chains and
	// three audio chains, because concat with a=1 needs one of each per clip and
	// anything less is the cut that goes quiet.
	clips := []Clip{
		{Path: "one.mp4", Facts: Facts{Length: 2 * time.Second, Width: 1280, Height: 720, Rate: 24}},
		{Path: "two.mp4", Facts: Facts{Length: 3 * time.Second, Width: 1280, Height: 720, Rate: 24, Sound: true}},
		{Path: "three.mp4", Facts: Facts{Length: time.Second, Width: 640, Height: 480, Rate: 30}},
	}
	plan := joinPlan(clips, "cut.mp4")
	graph := graphOf(t, plan)

	for index := range clips {
		if !strings.Contains(graph, fmt.Sprintf("[v%d]", index)) {
			t.Errorf("clip %d has no video chain in the graph", index)
		}
		if !strings.Contains(graph, fmt.Sprintf("[a%d]", index)) {
			t.Errorf("clip %d has no audio chain in the graph — this is the cut that goes silent", index)
		}
		if !strings.Contains(graph, fmt.Sprintf("[v%d][a%d]", index, index)) {
			t.Errorf("clip %d is not fed to concat as a video/audio pair", index)
		}
	}
	if !strings.Contains(graph, "concat=n=3:v=1:a=1") {
		t.Errorf("graph does not concatenate three clips with audio:\n%s", graph)
	}
	if !strings.Contains(strings.Join(plan, " "), "-map [v] -map [a]") {
		t.Errorf("the plan does not map both streams out:\n%s", strings.Join(plan, " "))
	}
}

func TestASilentClipBringsItsOwnSilenceOfItsOwnLength(t *testing.T) {
	// The silence is generated per clip and TIMED TO THAT CLIP, because a
	// silence of the wrong length pushes every later clip's sound out of sync —
	// which is a worse failure than no sound at all, since it sounds like a
	// mistake somebody made rather than a feature that is missing.
	clips := []Clip{
		{Path: "loud.mp4", Facts: Facts{Length: 2 * time.Second, Width: 640, Height: 360, Rate: 25, Sound: true}},
		{Path: "quiet.mp4", Facts: Facts{Length: 1500 * time.Millisecond, Width: 640, Height: 360, Rate: 25}},
	}
	plan := strings.Join(joinPlan(clips, "cut.mp4"), " ")

	if strings.Count(plan, silenceSource) != 1 {
		t.Errorf("want exactly one generated silence for the one silent clip:\n%s", plan)
	}
	if !strings.Contains(plan, "-t 1.5 -i "+silenceSource) {
		t.Errorf("the silence is not 1.5s long, which is the silent clip's own length:\n%s", plan)
	}
	// The loud clip's audio comes from the clip; the quiet clip's comes from the
	// generated input, which sits after every clip in the input list.
	graph := graphOf(t, joinPlan(clips, "cut.mp4"))
	if !strings.Contains(graph, "[0:a]") {
		t.Error("the loud clip's own audio is not used")
	}
	if !strings.Contains(graph, "[2:a]") {
		t.Errorf("the silent clip does not draw on the generated silence at input 2:\n%s", graph)
	}
}

func TestAJoinScalesEveryClipIntoTheFirstClipsFrameWithoutStretchingIt(t *testing.T) {
	clips := []Clip{
		{Path: "one.mp4", Facts: Facts{Length: time.Second, Width: 1080, Height: 1920, Rate: 24, Sound: true}},
		{Path: "two.mp4", Facts: Facts{Length: time.Second, Width: 1920, Height: 1080, Rate: 60, Sound: true}},
	}
	graph := graphOf(t, joinPlan(clips, "cut.mp4"))

	if strings.Count(graph, "scale=1080:1920") != 2 {
		t.Errorf("both clips must be scaled into the first clip's 1080x1920 frame:\n%s", graph)
	}
	if !strings.Contains(graph, "force_original_aspect_ratio=decrease") || !strings.Contains(graph, "pad=1080:1920") {
		t.Errorf("a clip of another shape must be letterboxed, never stretched:\n%s", graph)
	}
	if strings.Count(graph, "fps=24") != 2 {
		t.Errorf("both clips must run at the first clip's 24fps — mixed rates drift:\n%s", graph)
	}
}

func TestAJoinFallsBackToADefaultRateWhenTheFirstClipStatesNone(t *testing.T) {
	clips := []Clip{
		{Path: "one.mp4", Facts: Facts{Length: time.Second, Width: 640, Height: 360, Sound: true}},
		{Path: "two.mp4", Facts: Facts{Length: time.Second, Width: 640, Height: 360, Sound: true}},
	}
	if graph := graphOf(t, joinPlan(clips, "cut.mp4")); !strings.Contains(graph, "fps=30") {
		t.Errorf("want the %g fps fallback when nothing states a rate:\n%s", joinDefaultRate, graph)
	}
}

func TestAScoreIsLoopedForeverAndBoundedByThePicture(t *testing.T) {
	// This pair of arguments IS "looped or trimmed to fit", which used to be
	// arithmetic somebody did by hand: an infinite loop on the way in, and the
	// picture's own length on the way out. A short piece repeats, a long one
	// stops, and neither case needs a decision.
	picture := Facts{Length: 8 * time.Second, Width: 1280, Height: 720, Rate: 24}
	plan := strings.Join(scorePlan("cut.mp4", "score.mp3", "scored.mp4", picture, Scoring{Level: 1}), " ")

	if !strings.Contains(plan, "-stream_loop -1 -i score.mp3") {
		t.Errorf("the score is not looped:\n%s", plan)
	}
	if !strings.Contains(plan, "-t 8 ") {
		t.Errorf("the output is not bounded by the picture's 8s:\n%s", plan)
	}
	if !strings.Contains(plan, "-c:v copy") {
		t.Errorf("scoring must not re-encode the picture:\n%s", plan)
	}
}

func TestAScoreUnderExistingSoundMixesWithoutHalvingIt(t *testing.T) {
	// amix DIVIDES every input by the number of inputs unless told not to, so
	// the obvious command quietly halves the dialogue the moment a score is
	// added — and one output is not enough to hear it happen.
	picture := Facts{Length: 4 * time.Second, Width: 640, Height: 360, Rate: 25, Sound: true}
	graph := graphOf(t, scorePlan("cut.mp4", "score.mp3", "scored.mp4", picture, Scoring{Level: 0.3}))

	if !strings.Contains(graph, "amix=inputs=2") {
		t.Errorf("a clip with its own sound must have the score mixed under it:\n%s", graph)
	}
	if !strings.Contains(graph, "normalize=0") {
		t.Errorf("the mix must not normalize, or everything under the score is halved:\n%s", graph)
	}
	if !strings.Contains(graph, "volume=0.3") {
		t.Errorf("the score's level is not applied:\n%s", graph)
	}
}

func TestTheMixOutlastsTheClipsOwnAudioAndStopsWithThePicture(t *testing.T) {
	// amix's duration=first keys the whole mix on the FIRST input, which is the
	// clip's own audio — so a three-second shot carrying one second of sound
	// gets one second of score and then silence, and nothing anywhere says so.
	// A clip whose audio ends before its picture is ordinary, not a broken file.
	// What bounds this command is the -t on the output, which is the picture's
	// own length, so the score being an infinite input is safe.
	picture := Facts{Length: 3 * time.Second, Width: 640, Height: 360, Rate: 25, Sound: true}
	plan := scorePlan("cut.mp4", "score.mp3", "scored.mp4", picture, Scoring{Level: 0.3})
	graph := graphOf(t, plan)

	if strings.Contains(graph, "duration=first") {
		t.Errorf("the mix ends with the clip's own audio, which is the cut that goes quiet part way through:\n%s", graph)
	}
	if !strings.Contains(graph, "amix=inputs=2:duration=longest") {
		t.Errorf("the mix must run as long as the longer of the two:\n%s", graph)
	}
	if !strings.Contains(strings.Join(plan, " "), "-t 3 ") {
		t.Errorf("nothing bounds the infinite score but the picture's 3s:\n%s", strings.Join(plan, " "))
	}
}

func TestAScoreOverASilentClipOrAReplacedOneDoesNotMixAtAll(t *testing.T) {
	silent := Facts{Length: 4 * time.Second, Width: 640, Height: 360, Rate: 25}
	if graph := graphOf(t, scorePlan("cut.mp4", "s.mp3", "out.mp4", silent, Scoring{Level: 1})); strings.Contains(graph, "amix") {
		t.Errorf("there is nothing to mix a score with on a silent clip:\n%s", graph)
	}
	loud := silent
	loud.Sound = true
	if graph := graphOf(t, scorePlan("cut.mp4", "s.mp3", "out.mp4", loud, Scoring{Level: 1, Replace: true})); strings.Contains(graph, "amix") {
		t.Errorf("replace means the clip's own sound is dropped, not mixed:\n%s", graph)
	}
}

func TestAFadeStartsItsOwnLengthBeforeTheEnd(t *testing.T) {
	picture := Facts{Length: 10 * time.Second, Width: 640, Height: 360, Rate: 25}
	graph := graphOf(t, scorePlan("cut.mp4", "s.mp3", "out.mp4", picture,
		Scoring{Level: 1, Fade: 2 * time.Second}))
	if !strings.Contains(graph, "afade=t=out:st=8:d=2") {
		t.Errorf("a 2s fade out of a 10s cut starts at 8s:\n%s", graph)
	}
	// And no fade means no filter, rather than a zero-length one.
	plain := graphOf(t, scorePlan("cut.mp4", "s.mp3", "out.mp4", picture, Scoring{Level: 1}))
	if strings.Contains(plain, "afade") {
		t.Errorf("no fade was asked for and none should be applied:\n%s", plain)
	}
}

func TestTheClosingFramePlanSeeksFromTheEndAndTakesTheLastWrite(t *testing.T) {
	closing := strings.Join(framePlan("clip.mp4", Closing, "last.png"), " ")
	if !strings.Contains(closing, "-sseof -1") {
		t.Errorf("the closing frame must seek relative to the end:\n%s", closing)
	}
	if !strings.Contains(closing, "-update 1") {
		t.Errorf("the closing frame relies on each frame overwriting the last:\n%s", closing)
	}
	if strings.Contains(closing, "-frames:v") {
		t.Errorf("capping the frame count would keep the FIRST frame of the tail, not the last:\n%s", closing)
	}

	// A frame at a known time is the opposite shape: seek in, take one frame.
	timed := strings.Join(framePlan("clip.mp4", 3*time.Second, "at.png"), " ")
	if !strings.Contains(timed, "-ss 3 -i clip.mp4") {
		t.Errorf("a timed frame seeks before the input, so nothing is decoded up to it:\n%s", timed)
	}
	if !strings.Contains(timed, "-frames:v 1") {
		t.Errorf("a timed frame takes exactly one:\n%s", timed)
	}

	// And the opening frame needs no seek at all.
	if opening := strings.Join(framePlan("clip.mp4", 0, "first.png"), " "); strings.Contains(opening, "-ss") {
		t.Errorf("the opening frame needs no seek:\n%s", opening)
	}
}

// graphOf pulls the filter graph out of a plan, so an assertion reads the graph
// and not the whole command line around it.
func graphOf(t *testing.T, plan []string) string {
	t.Helper()
	for index, word := range plan {
		if word == "-filter_complex" && index+1 < len(plan) {
			return plan[index+1]
		}
	}
	t.Fatalf("the plan has no filter graph in it: %s", strings.Join(plan, " "))
	return ""
}
