// Package video is the LOCAL half of aforge's video work: the things done to
// files that already exist, on this machine, with ffmpeg — as against the
// renders bought from a provider a clip at a time
// (internal/session/tools_video.go).
//
// It exists because the two halves are not the same kind of work and were being
// treated as one. A render is minutes of somebody else's GPU, costs real money,
// and produces exactly one short clip because that is what the endpoints do. But
// a *film* is several of those clips joined, with a frame carried from each one
// into the next so the shots connect, and a score laid underneath — and every
// one of those four operations is a local, free, deterministic thing that ffmpeg
// has done for twenty years.
//
// Until this package, aforge did them by writing ffmpeg command lines into the
// shell, and the manual said so in as many words. That has two costs and the
// second one is the expensive one:
//
//   - It asks a language model to write a filter graph, which is a dialect it
//     knows unevenly and cannot test before running.
//   - IT DROPS THE SOUND. The natural way to write a join — `concat` on the
//     video streams, or an `xfade` between them — carries the first input's
//     audio and silently discards every other input's. The result plays, looks
//     right, and goes quiet after the first clip. Nothing errors, nothing warns,
//     and it is not visible in anything but the ear. The manual grew a whole
//     section about the defect ("Why is a stitched video incoherent, or silent
//     after the first clip?") because it kept happening.
//
// So the audio is carried BY CONSTRUCTION here, not by remembering: [Join]
// probes every clip, gives the silent ones a generated silence track of their
// own measured length, and concatenates N video streams with N audio streams. A
// join cannot come out silent, because there is no code path in which the audio
// is not mapped.
//
// The library is deliberately free of the session: it takes paths and returns
// facts and errors, so the belt verb (internal/session/tools_editvideo.go) is a
// thin wire over it and the same operations are available to anything else that
// grows a need for them.
package video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The two binaries this package is. ffprobe answers questions about a file and
// ffmpeg changes it, and a machine with one but not the other can do neither
// honestly — every operation here probes before it encodes, because the audio
// law and the geometry both come out of the probe. So [Missing] demands both.
const (
	ffmpegBinary  = "ffmpeg"
	ffprobeBinary = "ffprobe"
)

// Ceiling is how long one local operation may run before it is given up on, and
// it is THE number: the belt verb interpolates it into its own description
// rather than typing a second copy, on this codebase's one-source-of-truth law.
//
// Five minutes is chosen against the work rather than against the clock. A join
// re-encodes, so it costs roughly the playing time of the material on a modern
// machine and a handful of ten-second clips is well under a minute; five minutes
// is a join of many minutes of footage, which is past the point where a person
// should be watching a tool call spin. What happens at the ceiling matters more
// than where it is: the half-written file never reaches the destination at all
// ([produce] encodes elsewhere and renames), so a timeout leaves nothing that
// could be mistaken for a finished cut and nothing missing that was there
// before.
const Ceiling = 5 * time.Minute

// ErrMissing is the one answer every entry point gives on a machine that has no
// ffmpeg. It is a sentinel rather than a string because the belt uses it for the
// absence law — a verb with no ffmpeg behind it is left OFF the belt entirely
// (design-law: a capability that cannot work is absent, not broken), so this
// error should never reach a model at all.
var ErrMissing = errors.New("ffmpeg and ffprobe are not on this machine")

// Missing names the binary this machine has not got, and "" when it has both.
// It looks the binaries up LIVE rather than caching the answer: the lookup is a
// few stats against PATH, it is asked once per belt build and never in a loop,
// and a cached "no" would outlive an ffmpeg installed while aforge was running.
func Missing() string {
	for _, binary := range []string{ffmpegBinary, ffprobeBinary} {
		if _, err := exec.LookPath(binary); err != nil {
			return binary
		}
	}
	return ""
}

// Available is [Missing] as the belt asks it.
func Available() bool { return Missing() == "" }

// Facts is what one probe learned. Every field obeys the EMPTINESS LAW: a fact
// the file did not state is the zero value, and a caller reporting it says
// nothing rather than guessing. That matters most for Length, because the whole
// point of measuring is that a stitch is timed from it — a length that was
// guessed would be a cut that drifts.
//
// Sound is a DECLARED audio track, not audible samples: a track of pure silence
// answers true, exactly as internal/session's mp4 box reader answers it, because
// the question both are asked is "does the join have an audio stream to carry".
type Facts struct {
	Length time.Duration
	Sound  bool
	Width  int
	Height int
	Rate   float64 // frames per second, 0 when no stream stated one
}

// Probe measures one file. It is ffprobe's json, read for the five facts the
// rest of this package and the belt verb need, and nothing else.
func Probe(ctx context.Context, path string) (Facts, error) {
	if !Available() {
		return Facts{}, ErrMissing
	}
	spoken, err := run(ctx, ffprobeBinary,
		"-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	if err != nil {
		return Facts{}, err
	}
	var answer probeAnswer
	if err := json.Unmarshal(spoken, &answer); err != nil {
		return Facts{}, fmt.Errorf("ffprobe said something about %s that could not be read", filepath.Base(path))
	}
	return answer.facts(), nil
}

// probeAnswer is ffprobe's json, cut down to the fields read. Every number
// arrives as a STRING in this format, which is why nothing here is a numeric
// field: a json.Number or a float would fail to decode the whole document
// because one stream stated a duration as "N/A".
type probeAnswer struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []probeStream `json:"streams"`
}

// probeStream is one stream out of that answer. It is a named type rather than
// an anonymous one so that a test can fabricate the awkward shapes — the ones a
// real file states and a fixture cannot easily be made to — and drive them
// through [probeAnswer.facts] with no encoder anywhere near it.
type probeStream struct {
	CodecType string `json:"codec_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  string `json:"duration"`

	// The TWO rates ffprobe states, which disagree on exactly the files that
	// matter. FrameRate is r_frame_rate, the smallest tick the container can
	// express a timestamp in; AverageRate is avg_frame_rate, the frames it
	// actually holds divided by how long it runs. [believableRate] says which
	// one is believed and why.
	FrameRate   string `json:"r_frame_rate"`
	AverageRate string `json:"avg_frame_rate"`
}

// facts folds the json into [Facts]. The container's duration is preferred over
// the video stream's because it is the one a player honours; the stream's is the
// fallback for a container that does not state one.
func (a probeAnswer) facts() Facts {
	var facts Facts
	facts.Length = readSeconds(a.Format.Duration)
	for _, stream := range a.Streams {
		switch stream.CodecType {
		case "audio":
			facts.Sound = true
		case "video":
			// The FIRST video stream decides the geometry, because it is the
			// one that plays; a file with two is a file with a thumbnail in it.
			if facts.Width == 0 {
				facts.Width, facts.Height = stream.Width, stream.Height
				facts.Rate = believableRate(stream.AverageRate, stream.FrameRate)
				if facts.Length == 0 {
					facts.Length = readSeconds(stream.Duration)
				}
			}
		}
	}
	return facts
}

// readSeconds turns ffprobe's decimal-seconds string into a Duration, answering
// zero for everything that is not a believable positive length — "N/A", "", a
// negative, and the infinities a stream copy of a live source can state.
func readSeconds(text string) time.Duration {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// The band a stated frame rate has to fall inside to be believed as one.
//
// A NUMBER OUTSIDE IT IS NOT A FRAME RATE, IT IS A TIMEBASE. A variable-rate
// recording — a webm out of a browser's MediaRecorder is the everyday one —
// states r_frame_rate as 1000/1, because a millisecond is the finest timestamp
// its container can spell, while stating avg_frame_rate as 0/0 because it holds
// no constant rate at all. Believed, that 1000 becomes the rate every clip in a
// join is resampled to: thirty-three times the frames, an encode that takes
// minutes instead of seconds, and a file no player is happy with. The floor is
// the other end of the same argument — under a frame a second is a slideshow's
// timebase rather than a rate anything was shot at. Outside the band the answer
// is 0, "no rate stated", which [joinPlan] already turns into [joinDefaultRate].
const (
	slowestRate = 1.0
	fastestRate = 120.0
)

// believableRate is the frame rate a join should run at, out of the two ffprobe
// states. The average is preferred because it is measured from the frames that
// are actually in the file; r_frame_rate is the fallback for a stream that
// states no average, which constant-rate material routinely does not.
func believableRate(average, stated string) float64 {
	for _, spoken := range []string{average, stated} {
		if rate := readRate(spoken); rate >= slowestRate && rate <= fastestRate {
			return rate
		}
	}
	return 0
}

// readRate turns ffprobe's "30000/1001" into 29.97. A zero denominator is what
// a stream with no rate states ("0/0"), and it answers 0 — no rate, rather than
// a division nobody can use.
func readRate(text string) float64 {
	numerator, denominator, split := strings.Cut(strings.TrimSpace(text), "/")
	if !split {
		rate, err := strconv.ParseFloat(numerator, 64)
		if err != nil || rate <= 0 {
			return 0
		}
		return rate
	}
	top, topErr := strconv.ParseFloat(numerator, 64)
	bottom, bottomErr := strconv.ParseFloat(denominator, 64)
	if topErr != nil || bottomErr != nil || bottom == 0 || top <= 0 {
		return 0
	}
	return top / bottom
}

// run executes one of the two binaries under [Ceiling] and returns its stdout.
//
// The error it builds is written for a MODEL to read and act on, which means
// ffmpeg's own last words and not a Go wrapper's: an encoder refuses for
// specific, fixable reasons ("Invalid data found", "No such file", "Unknown
// encoder") and the fix is in the sentence. A timeout says so as itself, because
// that is the one failure whose remedy is different in kind — less material,
// not a different command.
func run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	bounded, done := context.WithTimeout(ctx, Ceiling)
	defer done()

	command := exec.CommandContext(bounded, binary, args...)
	var out, complaint strings.Builder
	command.Stdout = &out
	command.Stderr = &complaint
	err := command.Run()
	if err == nil {
		return []byte(out.String()), nil
	}
	if bounded.Err() != nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%s gave up after %s", binary, Ceiling)
	}
	if said := lastWords(complaint.String()); said != "" {
		return nil, fmt.Errorf("%s: %s", binary, said)
	}
	return nil, fmt.Errorf("%s failed: %w", binary, err)
}

// lastWordsLimit is how much of a failure's tail is quoted. ffmpeg's stderr is
// a banner, a stream dump and then the cause; the cause is the last line or two
// and everything above it is noise a model would have to read past.
const lastWordsLimit = 2

// lastWords is the tail of ffmpeg's complaint: the last couple of non-blank
// lines, joined, which is where the reason is.
func lastWords(complaint string) string {
	var kept []string
	for _, line := range strings.Split(complaint, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) > lastWordsLimit {
		kept = kept[len(kept)-lastWordsLimit:]
	}
	return strings.Join(kept, "; ")
}

// prepare makes the destination's directory and answers the refusal for a
// destination that is one of the sources.
//
// THE SELF-OVERWRITE CHECK IS NOT PEDANTRY. ffmpeg opens its output for writing
// before it has finished reading its inputs, so `join a.mp4 b.mp4 -> a.mp4`
// destroys a.mp4 and then fails to read it, which loses a file the person may
// have paid a provider for. It is refused by name, before anything is opened.
func prepare(destination string, sources ...string) error {
	full, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("could not work out where %s is", destination)
	}
	for _, source := range sources {
		other, err := filepath.Abs(source)
		if err == nil && other == full {
			return fmt.Errorf("%s is one of the files being read, and writing it would destroy it — name a different destination",
				filepath.Base(destination))
		}
	}
	return os.MkdirAll(filepath.Dir(full), 0o755)
}

// produce runs one operation's plan and puts the result at destination ONLY if
// ffmpeg both succeeded and wrote something. It is the one place all three
// operations encode, so the two laws below are stated once rather than three
// times with two of the copies eventually wrong.
//
// A FAILED RUN MAY NOT TOUCH A FILE THE OPERATION DID NOT CREATE. ffmpeg opens
// its output long before it has read enough of its input to know whether the
// job is possible, so the obvious shape — encode straight to the destination,
// remove it when the command fails — deletes whatever the person already had at
// that path on every failure that happens early: an unreadable input, an
// unknown muxer, a filter graph that will not initialise. None of those wrote a
// byte, and all of them used to take the file with them. Here the encode
// happens somewhere else entirely and the destination is written by nothing but
// the rename at the end.
//
// The temporary sits IN THE DESTINATION'S OWN DIRECTORY, so the two are on one
// filesystem and the rename is atomic: a destination that exists is a finished
// file and never a half-copied one. It also keeps the destination's own name,
// and with it the extension WITHOUT WHICH FFMPEG CANNOT INFER THE MUXER — a
// temporary called something ending in nothing is refused before it encodes.
func produce(ctx context.Context, destination string, plan func(working string) []string) error {
	room, err := os.MkdirTemp(filepath.Dir(destination), ".video-*")
	if err != nil {
		return fmt.Errorf("could not make room to write %s: %w", short(destination), err)
	}
	// The whole directory goes on the way out, on success and on failure alike,
	// so a wrecked encode leaves nothing anywhere rather than nothing at the
	// destination and a mess beside it.
	defer func() { _ = os.RemoveAll(room) }()

	working := filepath.Join(room, filepath.Base(destination))
	if _, err := run(ctx, ffmpegBinary, plan(working)...); err != nil {
		return err
	}
	// BELT AND BRACES ON A RUN THAT CLAIMED IT WORKED. ffmpeg exits 0 having
	// encoded nothing more readily than it ought to — a frame seeked past the
	// end of a clip is the case this was found on — and an empty file reported
	// as a saved one is worse than any refusal, because the model hands it
	// straight to the next tool as though it were a picture.
	if written, err := os.Stat(working); err != nil || written.Size() == 0 {
		return fmt.Errorf("%s finished without writing anything into %s", ffmpegBinary, short(destination))
	}
	if err := os.Rename(working, destination); err != nil {
		return fmt.Errorf("could not put the finished %s in place: %w", short(destination), err)
	}
	return nil
}
