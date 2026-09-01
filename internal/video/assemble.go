package video

// assemble.go is the three operations that change files: a frame saved out of a
// clip, several clips joined into one, and a score laid under a cut.
//
// Every one of them is built in two halves — a PLAN, which is a pure function
// from measured facts to an argument list, and a run of that plan. The split is
// not tidiness. An ffmpeg filter graph is the part that is easy to get subtly
// wrong and impossible to eyeball in a passing test, so the graph is data that a
// test reads directly: `TestAJoinMapsEveryClipsAudio` asserts on the plan for
// twelve clips without encoding a single frame, and the encode tests then prove
// the plan is one ffmpeg accepts.

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Closing is the moment that means the clip's FINAL frame, for [SaveFrame].
//
// It is its own moment rather than a time the caller works out, because the
// obvious arithmetic is wrong: seeking to the measured length lands one frame
// past the end and decodes nothing, and seeking to length-minus-a-bit needs the
// frame rate to know what "a bit" is. ffmpeg can seek relative to the end, so
// this is the fact and not the estimate.
const Closing = time.Duration(-1)

// closingWindow is how much of the tail is decoded to find the last frame. Each
// frame in the window overwrites the destination, so the final write is the
// final frame; a second is wide enough for any frame rate a video carries and
// short enough that the decode is instant.
const closingWindow = time.Second

// The floor and ceiling on one join. Two is the arithmetic minimum — a join of
// one clip is a copy, and a model asking for it has miscounted rather than asked
// for something. The ceiling is a filter graph's practical limit: every clip
// adds two chains and an input, and past this the command line itself becomes
// the problem. Both are named in the refusals, so a model that hits one knows
// which way to move.
const (
	joinFloor = 2
	joinLimit = 64
)

// joinDefaultRate is the frame rate a join runs at when the first clip does not
// state one. Every clip is resampled to ONE rate because concatenated streams of
// different rates produce a cut whose timestamps drift — the drift is invisible
// in the first clip and accumulates, which is the worst way for it to be.
const joinDefaultRate = 30.0

// audioShape is the one sample format every audio stream in this package is
// converted to before it is concatenated or mixed.
//
// IT IS THE WHOLE REASON A HAND-WRITTEN JOIN GOES WRONG IN THE SECOND-WORST WAY.
// The concat filter demands that its audio inputs agree on format, rate and
// channel layout, and clips from two different renders routinely do not; without
// this, concat either refuses outright or, worse, produces a cut whose later
// clips play at the wrong speed.
const audioShape = "aformat=sample_fmts=fltp:sample_rates=48000:channel_layouts=stereo"

// silenceSource is a generated silent stereo track. A silent clip gets one of
// its own measured length so that the concat filter has N audio inputs for N
// video inputs — this is [Join]'s audio law made mechanical.
const silenceSource = "anullsrc=channel_layout=stereo:sample_rate=48000"

// Clip is one input to a join: where it is and what the probe learned about it.
type Clip struct {
	Path  string
	Facts Facts
}

// Scoring is how a score sits under a cut.
type Scoring struct {
	// Level is how loud the score is, 1 being as recorded. There is no default
	// here on purpose: mixing under dialogue and replacing silence want very
	// different numbers, and the caller knows which it is doing.
	Level float64

	// Replace drops the clip's own audio instead of mixing the score under it.
	Replace bool

	// Fade is how long the score takes to fade out at the end. Zero is an
	// honest hard stop; a looped score cut mid-phrase is what a fade is for.
	Fade time.Duration
}

// SaveFrame writes one frame of source to destination as an image.
//
// at is a time from the start of the clip, or [Closing] for the final frame.
// The final frame is the one that matters most: it is how one generated clip
// connects to the next, handed to generate_video as the opening frame of the
// shot that follows, and it is the only way aforge has of making two independent
// renders look like one continuous take.
func SaveFrame(ctx context.Context, source string, at time.Duration, destination string) error {
	if !Available() {
		return ErrMissing
	}
	// A MOMENT PAST THE END IS REFUSED HERE, WITH THE CLIP'S REAL LENGTH IN THE
	// SENTENCE, because ffmpeg does not refuse it: a seek beyond the last frame
	// decodes nothing, writes nothing and exits 0, so without this the caller
	// reports a frame it has saved and the model hands that empty file to the
	// next render as the shot it is continuing from. One probe costs
	// milliseconds against an encode.
	//
	// A clip whose length cannot be measured is encoded anyway rather than
	// guessed at (the emptiness law), and [produce]'s size check is the backstop
	// for it. [Closing] is a seek from the end and cannot be past one.
	if at >= 0 {
		if facts, err := Probe(ctx, source); err == nil && facts.Length > 0 && at >= facts.Length {
			return fmt.Errorf("%s runs %ss and has no frame at %ss — ask for a moment inside it, or for the closing frame",
				short(source), seconds(facts.Length), seconds(at))
		}
	}
	if err := prepare(destination, source); err != nil {
		return err
	}
	return produce(ctx, destination, func(working string) []string {
		return framePlan(source, at, working)
	})
}

// framePlan is the argument list for one frame.
//
// -update 1 is what makes the closing frame work: with no frame count, every
// decoded frame in the tail window is written to the SAME path, so the last one
// standing is the last frame of the clip. For a frame at a known time the seek
// comes before -i, which makes it a container seek rather than a decode of
// everything up to that point.
func framePlan(source string, at time.Duration, destination string) []string {
	plan := []string{"-y", "-hide_banner", "-nostdin"}
	if at < 0 {
		plan = append(plan, "-sseof", "-"+seconds(closingWindow))
	} else if at > 0 {
		plan = append(plan, "-ss", seconds(at))
	}
	plan = append(plan, "-i", source, "-update", "1")
	if at >= 0 {
		plan = append(plan, "-frames:v", "1")
	}
	return append(plan, destination)
}

// Join concatenates clips into destination and CARRIES EVERY CLIP'S AUDIO.
//
// It probes first, both because the graph is built from the measurements and
// because a clip that cannot be measured is refused BY NAME before anything is
// encoded: a join whose third input has no readable duration would otherwise
// produce a cut that is silently wrong from the third clip on.
//
// The answer is a probe of the RESULT, not a claim about it. What the caller
// says to the person — how long the cut runs, whether it has sound — is read
// back off the file that now exists, so it cannot be a promise the encode
// failed to keep.
func Join(ctx context.Context, paths []string, destination string) (Facts, error) {
	if !Available() {
		return Facts{}, ErrMissing
	}
	if len(paths) < joinFloor {
		return Facts{}, fmt.Errorf("a join needs at least %d clips; one clip is already the video", joinFloor)
	}
	if len(paths) > joinLimit {
		return Facts{}, fmt.Errorf("a join takes at most %d clips at a time and this one names %d — join them in batches and then join the batches",
			joinLimit, len(paths))
	}
	clips := make([]Clip, 0, len(paths))
	for _, path := range paths {
		facts, err := Probe(ctx, path)
		if err != nil {
			return Facts{}, err
		}
		// THE TWO FACTS THE GRAPH CANNOT BE BUILT WITHOUT, refused one at a
		// time so the sentence names the clip and the missing fact. A length is
		// needed to generate a silent clip's silence; a geometry is needed to
		// scale it to the cut's frame.
		if facts.Length == 0 {
			return Facts{}, fmt.Errorf("%s has no readable length, so it cannot be timed into a join", short(path))
		}
		if facts.Width == 0 || facts.Height == 0 {
			return Facts{}, fmt.Errorf("%s has no video in it, so there is nothing to join", short(path))
		}
		clips = append(clips, Clip{Path: path, Facts: facts})
	}
	if err := prepare(destination, paths...); err != nil {
		return Facts{}, err
	}
	if err := produce(ctx, destination, func(working string) []string {
		return joinPlan(clips, working)
	}); err != nil {
		return Facts{}, err
	}
	return Probe(ctx, destination)
}

// joinPlan is the whole ffmpeg invocation for a join, and the audio law lives
// here in one readable loop.
//
// Every clip contributes exactly two chains — one video, one audio — and the
// concat filter is handed all 2N of them with a=1. A clip that HAS sound gets
// its own stream conformed to [audioShape]; a clip that has none gets a
// generated silence input of its own measured length. There is deliberately no
// third case, because a third case is where the silence bug lives: any branch
// that produces fewer audio chains than video chains produces a cut that goes
// quiet, and the graph below cannot express one.
//
// The geometry is the FIRST clip's, and everything else is scaled to fit inside
// it and padded — letterboxed, never stretched, because a stretched face is a
// worse answer than a black bar and the first clip is the one whose framing the
// person chose.
func joinPlan(clips []Clip, destination string) []string {
	width, height := clips[0].Facts.Width, clips[0].Facts.Height
	rate := clips[0].Facts.Rate
	if rate <= 0 {
		rate = joinDefaultRate
	}
	frame := fmt.Sprintf("%d:%d", width, height)
	pace := strconv.FormatFloat(math.Round(rate*1000)/1000, 'f', -1, 64)

	plan := []string{"-y", "-hide_banner", "-nostdin"}
	for _, clip := range clips {
		plan = append(plan, "-i", clip.Path)
	}
	// The silence inputs come after every clip, so a clip's own index is its
	// position in the list and stays readable in the graph.
	silence := len(clips)
	var chains, feed strings.Builder
	for index, clip := range clips {
		fmt.Fprintf(&chains,
			"[%d:v]scale=%s:force_original_aspect_ratio=decrease,pad=%s:-1:-1:color=black,setsar=1,fps=%s,format=yuv420p[v%d];",
			index, frame, frame, pace, index)
		source := index
		if !clip.Facts.Sound {
			plan = append(plan, "-f", "lavfi", "-t", seconds(clip.Facts.Length), "-i", silenceSource)
			source = silence
			silence++
		}
		fmt.Fprintf(&chains, "[%d:a]%s[a%d];", source, audioShape, index)
		fmt.Fprintf(&feed, "[v%d][a%d]", index, index)
	}
	graph := chains.String() + feed.String() +
		fmt.Sprintf("concat=n=%d:v=1:a=1[v][a]", len(clips))

	plan = append(plan, "-filter_complex", graph, "-map", "[v]", "-map", "[a]")
	plan = append(plan, encodeVideo()...)
	plan = append(plan, encodeAudio()...)
	return append(plan, destination)
}

// Score lays an audio file under a video, LOOPED OR TRIMMED to the video's own
// length — whichever the two lengths call for, without the caller working out
// which.
//
// That is the whole of what used to be manual arithmetic. generate_music has no
// length argument (the model writes a piece of its own choosing), so a score
// almost never matches the cut it is for, and putting one under the other meant
// measuring both and then looping or trimming by hand. Here the loop is infinite
// and the output is bounded by the video, so a short piece repeats and a long one
// stops, and both come out exactly as long as the picture.
//
// The picture is not re-encoded — it is copied stream-for-stream — so scoring a
// finished cut costs seconds and loses no quality.
func Score(ctx context.Context, clip, audio, destination string, scoring Scoring) (Facts, error) {
	if !Available() {
		return Facts{}, ErrMissing
	}
	if scoring.Level <= 0 {
		return Facts{}, fmt.Errorf("a score's level is how loud it is and must be above zero")
	}
	if scoring.Fade < 0 {
		return Facts{}, fmt.Errorf("a fade is a length of time and cannot be negative")
	}
	picture, err := Probe(ctx, clip)
	if err != nil {
		return Facts{}, err
	}
	if picture.Length == 0 {
		return Facts{}, fmt.Errorf("%s has no readable length, so there is nothing to fit a score to", short(clip))
	}
	if picture.Width == 0 {
		return Facts{}, fmt.Errorf("%s has no video in it — a score goes under a video", short(clip))
	}
	sound, err := Probe(ctx, audio)
	if err != nil {
		return Facts{}, err
	}
	if !sound.Sound {
		return Facts{}, fmt.Errorf("%s has no audio in it, so there is nothing to lay under the video", short(audio))
	}
	if scoring.Fade >= picture.Length {
		return Facts{}, fmt.Errorf("a %s fade is longer than the %s video it would fade out of",
			seconds(scoring.Fade), seconds(picture.Length))
	}
	if err := prepare(destination, clip, audio); err != nil {
		return Facts{}, err
	}
	if err := produce(ctx, destination, func(working string) []string {
		return scorePlan(clip, audio, working, picture, scoring)
	}); err != nil {
		return Facts{}, err
	}
	return Probe(ctx, destination)
}

// scorePlan is the whole invocation for a score.
//
// -stream_loop -1 on the audio input is the loop, and -t on the output is the
// trim; together they are "fit the score to the picture" with no arithmetic. The
// mix carries normalize=0 DELIBERATELY: amix's default divides every input by
// the number of inputs, so the obvious command halves the dialogue the moment a
// score is added and the fix is not discoverable by listening to one output.
func scorePlan(clip, audio, destination string, picture Facts, scoring Scoring) []string {
	score := fmt.Sprintf("[1:a]%s,volume=%s", audioShape,
		strconv.FormatFloat(scoring.Level, 'f', -1, 64))
	if scoring.Fade > 0 {
		score += fmt.Sprintf(",afade=t=out:st=%s:d=%s",
			seconds(picture.Length-scoring.Fade), seconds(scoring.Fade))
	}

	var graph string
	if scoring.Replace || !picture.Sound {
		graph = score + "[a]"
	} else {
		// duration=longest, and NOT duration=first, which keys the mix on the
		// clip's own audio and ends the score the moment that track does. A
		// clip whose audio stops before its picture is ordinary — a recording
		// muxed under a longer shot, a render whose sound was trimmed — and
		// under `first` its score dies at that point with nothing said about
		// it. `longest` is safe here precisely because the score is the
		// infinite input: what stops this command is the -t on the output,
		// which is the picture's own length, exactly as it is in the branch
		// above where the score plays alone.
		graph = score + "[score];" +
			fmt.Sprintf("[0:a]%s[own];", audioShape) +
			"[own][score]amix=inputs=2:duration=longest:normalize=0[a]"
	}

	plan := []string{"-y", "-hide_banner", "-nostdin",
		"-i", clip,
		"-stream_loop", "-1", "-i", audio,
		"-filter_complex", graph,
		"-map", "0:v", "-map", "[a]",
		"-c:v", "copy",
	}
	plan = append(plan, encodeAudio()...)
	return append(plan, "-t", seconds(picture.Length), destination)
}

// encodeVideo is how a re-encoded picture is written: the one widely playable
// codec, a preset that finishes in about the material's own playing time, and
// yuv420p because a pixel format some players refuse is not a saved file.
// faststart moves the index to the front, which is what lets a player start
// before the whole file has loaded.
func encodeVideo() []string {
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-pix_fmt", "yuv420p", "-movflags", "+faststart"}
}

// encodeAudio is how every audio stream this package writes is written.
func encodeAudio() []string {
	return []string{"-c:a", "aac", "-b:a", "192k"}
}

// seconds is a duration as ffmpeg spells one on a command line: plain decimal
// seconds, with the trailing zeros trimmed off so a plan reads the way a person
// would have typed it.
func seconds(length time.Duration) string {
	return strconv.FormatFloat(math.Round(length.Seconds()*1000)/1000, 'f', -1, 64)
}

// short is a path as a refusal should name it: the file's own name, because the
// caller gave the path and a refusal that reads the person's own word back is
// the one they can act on. The full path is the caller's to add if it has one
// worth adding.
func short(path string) string {
	if index := strings.LastIndexAny(path, `/\`); index >= 0 && index+1 < len(path) {
		return path[index+1:]
	}
	return path
}
