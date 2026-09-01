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
// than where it is: the half-written file is REMOVED, so a timeout leaves
// nothing that could be mistaken for a finished cut.
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
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
		FrameRate string `json:"r_frame_rate"`
	} `json:"streams"`
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
				facts.Rate = readRate(stream.FrameRate)
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

// abandon removes a half-written output. Every operation here calls it on
// failure, because a truncated mp4 is the worst possible artefact: it exists, it
// has a plausible size, some players open it, and nothing about it says it is
// the wreck of a command that failed.
func abandon(destination string) {
	_ = os.Remove(destination)
}
