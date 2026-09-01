package session

// edit_video: the local, free, deterministic half of video work — as against
// generate_video (tools_video.go), which buys one short clip at a time from a
// provider and takes minutes to do it.
//
// ── WHY THIS VERB EXISTS AT ALL ──
//
// Every video longer than one render is an assembly job: several clips laid end
// to end, a frame carried out of each one to open the next so the shots connect,
// and a score laid underneath. Those four operations were, until this file,
// ffmpeg command lines the model wrote into bash — and the manual said so,
// because that was the truth.
//
// It went wrong in a way nobody could see. The obvious join carries the FIRST
// input's audio and silently drops every other clip's, so the cut plays, looks
// right, and goes quiet after the first shot; the obvious mix halves the sound
// already in the clip, because amix normalizes unless told not to; and fitting a
// piece of music to a cut meant measuring both and looping or trimming by hand.
// None of those failures errors. All of them are audible and only audible.
// internal/video carries the fixes structurally — a join there cannot come out
// silent, because there is no branch in which the audio is not mapped — and this
// file is the wire that hands them to the model.
//
// ── ABSENT-NOT-BROKEN, ON A MACHINE FACT RATHER THAN A SETTING ──
//
// The verb is on the belt when ffmpeg and ffprobe are on PATH and off it when
// they are not (design-law: a capability that cannot work is absent, not broken).
// That gate is a fact about the machine and NOT a fact about the person's
// settings, which is the one interesting difference from the four making verbs:
// edit_video needs no model, no key and no money, so it is present on a machine
// that cannot generate a single frame — where it is still the right way to cut
// together footage the person already has.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/video"
)

// The four things this verb does. They are one tool with an action rather than
// four tools for the reason the jobs tool is one tool with an action: they are
// one intention — "work on the video files I already have" — and a belt with
// four hands for one intention makes the model choose, and what it chooses is
// wrong (docs/CHAT-V3.md, Decision 20).
const (
	editVideoMeasure = "measure"
	editVideoFrame   = "frame"
	editVideoJoin    = "join"
	editVideoScore   = "score"
)

// scoreUnderLevel is how loud a score comes in when it is mixed UNDER sound the
// clip already has, and scoreAloneLevel is how loud it is when there is nothing
// to mix it with.
//
// The two differ because the right answer differs and there is no single number
// that is not wrong in one of the cases: a score at full level over dialogue
// buries the dialogue, and a score at a third over silence is a score nobody can
// hear. The model can always name a level; these are what it gets when it does
// not, and the description states both.
const (
	scoreUnderLevel = 0.3
	scoreAloneLevel = 1.0
)

// framePicture is what a saved frame is written as. png because it is lossless —
// a frame is very often handed straight back to generate_video as an opening
// frame, and a jpeg's artefacts would be baked into the next render — and
// because png is one of the formats every picture argument on this belt takes.
const framePicture = ".png"

// frameFormats are the other extensions a model may name for a frame. They are
// the ones this binary already treats as pictures everywhere else, so a frame
// saved as any of them is a valid reference to any other tool here.
var frameFormats = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}

// The description's whole job is to stop the model reaching for bash. A model
// that knows ffmpeg exists and does not know this verb exists will write a
// filter graph, and the filter graph it writes will drop the sound — so the
// first sentence says what this is for and the audio law is stated as a promise
// rather than as advice.
//
// The ceiling is interpolated from [video.Ceiling] and never typed, on this
// codebase's one-source-of-truth law.
var editVideoDescription = "Work on video files that ALREADY EXIST on this machine, with ffmpeg: measure one, save a frame out of one, join several into one longer video, or lay music under one. This is local, free and instant — nothing here is a render and nothing here costs money, so use it rather than writing ffmpeg command lines into bash. join CARRIES EVERY CLIP'S AUDIO: a clip with sound keeps it and a silent clip gets silence of its own length, so a joined cut can never go quiet part-way through, which is what a hand-written concat does. frame with at=closing saves a clip's final frame, which is how you connect two independent renders — hand that picture to generate_video as its opening frame_paths entry and the next shot continues out of this one. score loops or trims the music to the video's own length by itself, so generate_music's piece needs no measuring, and it mixes UNDER existing sound (at level " +
	strconv.FormatFloat(scoreUnderLevel, 'f', -1, 64) + " unless you say otherwise) rather than replacing it. Every action answers with the file's measured facts, read back off the file that now exists. One call is given up on after " +
	strconv.Itoa(int(video.Ceiling/time.Minute)) + " minutes and leaves nothing behind."

var editVideoSchemaJSON = `{"type":"object","properties":` +
	`{"action":{"type":"string","description":"The op.","enum":["` +
	editVideoMeasure + `","` + editVideoFrame + `","` + editVideoJoin + `","` + editVideoScore + `"]},` +
	`"video":{"type":"string","description":"The video file to measure, take a frame out of, or lay a score under. A path in the workspace. Not used by join, which takes clips."},` +
	`"clips":{"type":"array","items":{"type":"string"},"description":"For join: the video files to lay end to end, IN THE ORDER THEY SHOULD PLAY. At least two. Every clip is scaled to fit inside the FIRST clip's frame and letterboxed rather than stretched, and every clip's audio is carried."},` +
	`"audio":{"type":"string","description":"For score: the audio file to lay under the video — an mp3 from generate_music or speak, or any file with sound in it. It is looped if it is shorter than the video and trimmed if it is longer, so its own length does not matter."},` +
	`"at":{"type":"string","description":"For frame: which frame to save. 'closing' (the default) is the clip's final frame, which is the one that chains into the next render; 'opening' is its first; a number is that many seconds in."},` +
	`"level":{"type":"number","description":"For score: how loud the music is, 1 being as recorded. Defaults to ` +
	strconv.FormatFloat(scoreUnderLevel, 'f', -1, 64) + ` when the video already has sound to mix under and ` +
	strconv.FormatFloat(scoreAloneLevel, 'f', -1, 64) + ` when it is silent."},` +
	`"replace":{"type":"boolean","description":"For score: drop the video's own sound instead of mixing the music under it (default: false — the music goes under what is already there)."},` +
	`"fade":{"type":"number","description":"For score: seconds to fade the music out over at the end. Worth naming when the music was looped, because a loop that stops dead mid-phrase sounds like a mistake (default: 0, a hard stop)."},` +
	`"path":{"type":"string","description":"Where to save the result, relative to the workspace. Leave it out for a timestamped name where this session keeps its video (or its pictures, for a frame). An existing file there is overwritten, as with the write tool. Not used by measure."}},` +
	`"required":["action"],"additionalProperties":false}`

// videoEditTools is the cutting verb, CONDITIONAL on the machine having the two
// binaries — and on nothing else. Every other conditional verb on this belt asks
// the person's settings; this one asks PATH.
func (a *Agent) videoEditTools() []bare.Tool {
	if !video.Available() {
		return nil
	}
	return []bare.Tool{a.editVideoTool()}
}

type editVideoArguments struct {
	Action  string   `json:"action"`
	Video   string   `json:"video"`
	Clips   []string `json:"clips"`
	Audio   string   `json:"audio"`
	At      string   `json:"at"`
	Level   *float64 `json:"level"`
	Replace bool     `json:"replace"`
	Fade    float64  `json:"fade"`
	Path    string   `json:"path"`
}

func (a *Agent) editVideoTool() bare.Tool {
	return bare.Tool{
		Name:        "edit_video",
		Description: editVideoDescription,
		Schema:      json.RawMessage(editVideoSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed editVideoArguments
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			switch strings.TrimSpace(parsed.Action) {
			case editVideoMeasure:
				return a.measureVideo(ctx, parsed)
			case editVideoFrame:
				return a.frameVideo(ctx, parsed)
			case editVideoJoin:
				return a.joinVideo(ctx, parsed)
			case editVideoScore:
				return a.scoreVideo(ctx, parsed)
			case "":
				return fmt.Sprintf("Invalid arguments: action is required (%s)", editVideoActions()), true, nil
			default:
				return fmt.Sprintf("Unknown action: %s. Use %s.", parsed.Action, editVideoActions()), true, nil
			}
		},
	}
}

// editVideoActions is the action list as a refusal spells it, derived from the
// constants so a fifth action cannot be added without appearing here.
func editVideoActions() string {
	return strings.Join([]string{editVideoMeasure, editVideoFrame, editVideoJoin, editVideoScore}, ", ")
}

// ── measure ─────────────────────────────────────────────────────────────────

// measureVideo answers how long a file runs, whether it carries sound, how big
// its frame is and how big the file is. It is the read-only action and the only
// one that writes nothing.
//
// IT IS NOT `read`. read on a video hands the file to a video-input model and
// answers what HAPPENS in it; this answers the facts a cut is planned from, for
// nothing, in milliseconds. The description says which is which because a model
// that reaches for the expensive door to ask a cheap question does it every time.
func (a *Agent) measureVideo(ctx context.Context, parsed editVideoArguments) (string, bool, error) {
	full, refusal := a.videoSource(parsed.Video, "video")
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	facts, err := video.Probe(ctx, full)
	if err != nil {
		return "Could not measure " + namedFile(parsed.Video) + ": " + err.Error(), true, nil
	}
	return a.describeVideoFile(full, facts), false, nil
}

// ── frame ───────────────────────────────────────────────────────────────────

// frameVideo saves one frame as a picture. The default is the CLOSING frame,
// because that is overwhelmingly what a frame is wanted for: it is the only
// thread that makes two independent renders look like one continuous take, and a
// model that has to name the moment will more often name the wrong one than the
// right one.
func (a *Agent) frameVideo(ctx context.Context, parsed editVideoArguments) (string, bool, error) {
	full, refusal := a.videoSource(parsed.Video, "video")
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	at, said, refusal := frameMoment(parsed.At)
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	destination, refusal, err := a.videoDestination(parsed.Path, said, framePicture,
		ImagesDir(a.config.Place, a.config.Workspace), frameFormats)
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	if err != nil {
		return "Could not work out where to save the frame: " + err.Error(), true, nil
	}
	if err := video.SaveFrame(ctx, full, at, destination); err != nil {
		return "Could not save the frame: " + err.Error(), true, nil
	}
	a.recordVideoArtifact(destination, said, "image")

	// The picture's own geometry is read back off the file, for the same reason
	// every other answer here is: a size this claimed rather than measured would
	// be a number the model plans its next render's frame from.
	shape := ""
	if facts, err := video.Probe(ctx, destination); err == nil && facts.Width > 0 {
		shape = fmt.Sprintf("%d×%d ", facts.Width, facts.Height)
	}
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(destination)), ".")
	return fmt.Sprintf("%s — %s%s, %s, %s of %s",
		picturePathInResult(destination), shape, kind, fileWeight(destination), said, namedFile(parsed.Video)), false, nil
}

// frameMoment reads the `at` argument into a moment and the phrase that names
// it. The phrase is used twice — in the file's name and in the answer — so the
// two cannot disagree about which frame was actually saved.
func frameMoment(at string) (time.Duration, string, string) {
	switch word := strings.ToLower(strings.TrimSpace(at)); word {
	case "", "closing", "close", "last", "end", "final":
		return video.Closing, "the closing frame", ""
	case "opening", "open", "first", "start", "beginning":
		return 0, "the opening frame", ""
	default:
		seconds, err := strconv.ParseFloat(strings.TrimSuffix(word, "s"), 64)
		if err != nil || seconds < 0 {
			return 0, "", "at is 'closing', 'opening', or a number of seconds — " + at + " is none of those"
		}
		length := time.Duration(seconds * float64(time.Second))
		return length, fmt.Sprintf("the frame at %ss", strconv.FormatFloat(seconds, 'f', -1, 64)), ""
	}
}

// ── join ────────────────────────────────────────────────────────────────────

// joinVideo lays clips end to end. Every clip is resolved and stat'ed HERE,
// before ffmpeg is started, so a path with a typo in it costs nothing and is
// named in the refusal — the same law generate_video's frames follow.
func (a *Agent) joinVideo(ctx context.Context, parsed editVideoArguments) (string, bool, error) {
	if len(parsed.Clips) == 0 {
		return "Invalid arguments: join needs clips — the video files to lay end to end, in order", true, nil
	}
	clips := make([]string, 0, len(parsed.Clips))
	for _, named := range parsed.Clips {
		full, refusal := a.videoSource(named, "clip")
		if refusal != "" {
			return "Invalid arguments: " + refusal, true, nil
		}
		clips = append(clips, full)
	}
	said := fmt.Sprintf("joined cut of %d clips", len(clips))
	destination, refusal, err := a.videoDestination(parsed.Path, said, videoExtension,
		VideoDir(a.config.Place, a.config.Workspace), nil)
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	if err != nil {
		return "Could not work out where to save the cut: " + err.Error(), true, nil
	}
	facts, err := video.Join(ctx, clips, destination)
	if err != nil {
		return "Could not join the clips: " + err.Error(), true, nil
	}
	a.recordVideoArtifact(destination, said, "video")
	return a.describeVideoFile(destination, facts) + fmt.Sprintf(", joined from %d clips", len(clips)), false, nil
}

// ── score ───────────────────────────────────────────────────────────────────

// scoreVideo lays an audio file under a video. The level's default is decided
// HERE rather than in the library, because it depends on what is being done —
// mixing under existing sound or filling a silence — and the library should not
// be guessing at intent.
func (a *Agent) scoreVideo(ctx context.Context, parsed editVideoArguments) (string, bool, error) {
	clip, refusal := a.videoSource(parsed.Video, "video")
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	audio, refusal := a.videoSource(parsed.Audio, "audio")
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	picture, err := video.Probe(ctx, clip)
	if err != nil {
		return "Could not measure " + namedFile(parsed.Video) + ": " + err.Error(), true, nil
	}
	scoring := video.Scoring{
		Level:   scoreAloneLevel,
		Replace: parsed.Replace,
		Fade:    time.Duration(parsed.Fade * float64(time.Second)),
	}
	if picture.Sound && !parsed.Replace {
		scoring.Level = scoreUnderLevel
	}
	if parsed.Level != nil {
		if *parsed.Level <= 0 {
			return "Invalid arguments: level is how loud the music is and must be above zero; use replace to drop the video's own sound", true, nil
		}
		scoring.Level = *parsed.Level
	}
	const said = "scored cut"
	destination, refusal, err := a.videoDestination(parsed.Path, said, videoExtension,
		VideoDir(a.config.Place, a.config.Workspace), nil)
	if refusal != "" {
		return "Invalid arguments: " + refusal, true, nil
	}
	if err != nil {
		return "Could not work out where to save the scored video: " + err.Error(), true, nil
	}
	facts, err := video.Score(ctx, clip, audio, destination, scoring)
	if err != nil {
		return "Could not score the video: " + err.Error(), true, nil
	}
	a.recordVideoArtifact(destination, said, "video")

	// Whether the piece was looped or trimmed is MEASURED, not assumed, and it
	// is worth saying: it is the difference between a score that repeats — where
	// a fade is worth asking for — and one that was cut off.
	fitting := ""
	if music, err := video.Probe(ctx, audio); err == nil && music.Length > 0 && picture.Length > 0 {
		switch {
		case music.Length < picture.Length:
			fitting = ", " + namedFile(parsed.Audio) + " looped to fit"
		case music.Length > picture.Length:
			fitting = ", " + namedFile(parsed.Audio) + " trimmed to fit"
		default:
			fitting = ", " + namedFile(parsed.Audio) + " under it"
		}
	}
	return a.describeVideoFile(destination, facts) + fitting, false, nil
}

// ── the shared middle ───────────────────────────────────────────────────────

// videoSource resolves one path the model named and refuses, by name, anything
// that is not a readable file. The noun is in the refusal because this verb
// takes three different kinds of path and "could not read x" leaves the model
// guessing which argument it belongs to.
func (a *Agent) videoSource(named, noun string) (string, string) {
	if strings.TrimSpace(named) == "" {
		return "", noun + " is required"
	}
	full := resolveInWorkspace(named, a.config.Workspace)
	info, err := os.Stat(full)
	if err != nil {
		return "", "could not read the " + noun + " " + filepath.ToSlash(strings.TrimSpace(named))
	}
	if info.IsDir() {
		return "", filepath.ToSlash(strings.TrimSpace(named)) + " is a directory, not a " + noun
	}
	return full, ""
}

// videoDestination is [Agent.mediaDestination] with one extra rule: when the
// action can only write certain formats, an extension outside them is refused
// rather than handed to ffmpeg, whose own complaint about an unknown muxer is
// not a sentence anybody can act on.
func (a *Agent) videoDestination(asked, said, extension, directory string, allowed map[string]bool) (string, string, error) {
	if allowed != nil && strings.TrimSpace(asked) != "" {
		if suffix := strings.ToLower(filepath.Ext(asked)); suffix != "" && !allowed[suffix] {
			return "", fmt.Sprintf("a frame is saved as a picture — %s is not one of %s", suffix, formatList(allowed)), nil
		}
	}
	path, err := a.mediaDestination(asked, said, extension, directory)
	return path, "", err
}

// formatList is the allowed extensions in a stable order, for a refusal.
func formatList(allowed map[string]bool) string {
	kinds := make([]string, 0, len(allowed))
	for suffix := range allowed {
		kinds = append(kinds, strings.TrimPrefix(suffix, "."))
	}
	sortStrings(kinds)
	return strings.Join(kinds, ", ")
}

// recordVideoArtifact gives everything this verb writes a row in the
// deliverables index, so `/files` finds a cut or a frame again by name and date
// exactly as it finds a generated one. A file the person may want back is a
// deliverable whether a provider made it or ffmpeg did.
func (a *Agent) recordVideoArtifact(path, said, kind string) {
	RecordArtifact(a.config.ArtifactsIndex, Artifact{
		Path:    path,
		Session: a.journalID(),
		Title:   mediaTitle(said, path),
		Kind:    kind,
		Created: time.Now(),
	})
}

// describeVideoFile is the one sentence every action that touches a video
// answers with: where it is, how long it runs, whether it carries sound, how big
// its frame is, how big the file is.
//
// EVERY FACT IS MEASURED OFF THE FILE THAT NOW EXISTS, and a fact the file does
// not state is simply absent (design-law §EMPTINESS) rather than zero or
// guessed. That matters because the model plans from this line — a join is timed
// from its clips' lengths and a score is fitted to the cut's — and one guessed
// number here is a cut that drifts.
func (a *Agent) describeVideoFile(path string, facts video.Facts) string {
	var said []string
	if facts.Length > 0 {
		carrying := " with sound"
		if !facts.Sound {
			carrying = " without sound"
		}
		said = append(said, mediaLength(facts.Length)+carrying)
	}
	if facts.Width > 0 && facts.Height > 0 {
		frame := fmt.Sprintf("%d×%d", facts.Width, facts.Height)
		if facts.Rate > 0 {
			frame += " at " + strconv.FormatFloat(math.Round(facts.Rate*100)/100, 'f', -1, 64) + "fps"
		}
		said = append(said, frame)
	}
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if kind == "" {
		kind = "video"
	} else {
		kind += " video"
	}
	said = append(said, fileWeight(path)+" of "+kind)
	return displayMediaPath(a.config.Workspace, path) + " — " + strings.Join(said, ", ")
}

// fileWeight is how big a file on disk is, in the words the making verbs use
// for the bytes they still hold in memory. A file that cannot be stat'ed
// answers nothing about its size rather than "0B".
func fileWeight(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "an unknown size"
	}
	return mediaByteSize(int(info.Size()))
}

// namedFile is a path as an answer names it: the file's own name, because the
// model gave the path and the sentence is about which of its files this is.
func namedFile(named string) string {
	return filepath.Base(filepath.ToSlash(strings.TrimSpace(named)))
}
