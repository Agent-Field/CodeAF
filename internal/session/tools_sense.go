package session

// tools_sense.go — a file is a file, so `read` reads it.
//
// Decision 20 settled the shape once, for PDFs: a person drops a file in and
// says "what does it say", and every belt that answers by GROWING A SECOND HAND
// has made the model choose between two verbs for one intention. Models choose
// wrong, reliably. This file is that decision generalized to the other three
// kinds of file whose meaning lives one decode deeper than their bytes.
//
// A screenshot, a voice memo, a song, a screen recording. The model's instinct
// on each of them is the same instinct it has for a source file — "I need to
// look at this / listen to this → read it" — and that instinct must simply
// work. What it must NOT have to do is pick an endpoint. There is no
// `transcribe` verb and no `look_at` verb on the belt for this, because the
// model does not know, before it has heard the file, whether the answer is a
// transcript or "acoustic guitar, slow, melancholy": THE LADDER PICKS THE
// SENSE, in exactly the way read_document's rungs pick the parser.
//
// ONE WRAPPER, ONE SNIFF, ONE ROUTE. The obvious alternative — chain a wrapper
// per kind, as tools_pdf.go's did alone — was rejected: four wrappers each
// parse the same arguments and resolve the same path, so a file two of them
// could claim is decided by the ORDER THEY HAPPEN TO BE APPLIED IN, which is a
// line in another file. Here the kinds are a table, the collisions are visible,
// and the cost of the whole thing on an ordinary source file is one stat and a
// map lookup.
//
// THE SENSES ARE UNCONDITIONAL, which is tools_doc.go's inversion of the
// absence law and lands here for the same reason. `read` is already on the
// belt, always; a sense that is sometimes there would make `read` a tool whose
// meaning depends on settings the model cannot see. So a missing resolver is
// answered with A SENTENCE NAMING WHAT IS MISSING — never a silent fall-through
// to bare, which would hand a model 4MB of base64-looking garbage and let it
// conclude the file was corrupt.
//
// AND read's SENSE AND read_document's LADDER ARE TWO DOORS, deliberately, by
// Decision 20's own precedent. A .png reaches both: bare `read` answers with
// the UNDIRECTED extraction below — transcribe everything, describe everything,
// one fixed prompt — while `read_document` keeps its own rungs and its own
// `question`, and `view_image` (the sight lane) keeps the question-directed
// look. Same file, three intentions, and the one the model reached for is the
// one it gets.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// senseSentence is what the senses add to pi's read description, after
// [pdfSentence]. It is deliberately short and deliberately ends with the
// instruction that pays for itself: the failure this whole file exists to
// prevent is `bash: python3 -c "import whisper"`, and a model told plainly that
// read already does this never writes that line.
const senseSentence = " Images, audio and video are read the same way — as a description: a picture comes back with its text transcribed and its layout described, a recording as its speech transcribed or, when it is not speech, as an account of the sound, and a video as what happens in it. Never write a script to decode any of them."

// ── what kind of file is this ───────────────────────────────────────────────

// senseKind is which sense a file needs. senseNone is every ordinary file and
// is the answer for the overwhelming majority of reads.
type senseKind int

const (
	senseNone senseKind = iota
	sensePDF
	senseImage
	senseAudio
	senseVideo
)

// senseType is one row of the table: what the file is, and the two spellings a
// wire needs for it — the media type for a data URL, and the BARE FORMAT WORD
// ("mp3", not "audio/mpeg") that the input_audio part insists on.
type senseType struct {
	kind      senseKind
	mediaType string
	format    string
}

// senseTypes is the extension table. Audio and video are listed here and the
// image five are folded in by init from image.go's own map, so read's eye and
// the attachment door can never disagree about what a picture is.
//
// The audio five and the video three are the sets the understanding endpoints
// actually accept; a format nobody can hear is better left to bare, which will
// report it as the binary file it is, than sent and answered with a provider
// error about a content part.
var senseTypes = map[string]senseType{
	".pdf": {sensePDF, "application/pdf", ""},

	".mp3":  {senseAudio, "audio/mpeg", "mp3"},
	".wav":  {senseAudio, "audio/wav", "wav"},
	".m4a":  {senseAudio, "audio/mp4", "m4a"},
	".ogg":  {senseAudio, "audio/ogg", "ogg"},
	".flac": {senseAudio, "audio/flac", "flac"},

	".mp4":  {senseVideo, "video/mp4", ""},
	".webm": {senseVideo, "video/webm", ""},
	".mov":  {senseVideo, "video/quicktime", ""},
}

func init() {
	for extension, mediaType := range imageMediaTypes {
		senseTypes[extension] = senseType{kind: senseImage, mediaType: mediaType}
	}
}

// senseHeaderBytes is how much of the file the magic sniff reads: enough for
// the longest signature here, which is the ISO base-media `ftyp` brand at bytes
// 8..12.
const senseHeaderBytes = 16

// sniffSense answers the wrapper's one question. Extension first, because it is
// free and right nearly always; magic bytes second, because a person's file is
// not always named helpfully — a downloaded attachment, a tempfile, a voice
// memo saved with no extension at all — and the bytes are the truth the name
// only claims.
//
// A file that cannot be opened is not claimed: bare owns the wording for that,
// and it is about to say it.
func sniffSense(absolute string) senseType {
	if entry, known := senseTypes[strings.ToLower(filepath.Ext(absolute))]; known {
		return entry
	}
	file, err := os.Open(absolute)
	if err != nil {
		return senseType{}
	}
	defer file.Close()
	header := make([]byte, senseHeaderBytes)
	read, _ := file.Read(header)
	return senseFromMagic(header[:max(read, 0)])
}

// senseFromMagic reads the first bytes of a file as its own claim about itself.
//
// ONE SIGNATURE IS DELIBERATELY MISSING: a bare MPEG frame sync (0xFF 0xEx) is
// the standard way to spot an extension-less MP3, and it is eleven bits, which
// a great many binary files begin with by accident. Every other rung here is
// free to be wrong; this one would spend money sending a random blob to a
// transcriber. So an MP3 with no extension is recognized by its ID3 tag or not
// at all, and the cost of being wrong is one fall-through to bare.
func senseFromMagic(header []byte) senseType {
	switch {
	case bytes.HasPrefix(header, []byte("%PDF-")):
		return senseTypes[".pdf"]
	case bytes.HasPrefix(header, []byte("\x89PNG\r\n\x1a\n")):
		return senseType{senseImage, "image/png", ""}
	case bytes.HasPrefix(header, []byte{0xff, 0xd8, 0xff}):
		return senseType{senseImage, "image/jpeg", ""}
	case bytes.HasPrefix(header, []byte("GIF87a")), bytes.HasPrefix(header, []byte("GIF89a")):
		return senseType{senseImage, "image/gif", ""}
	case bytes.HasPrefix(header, []byte("OggS")):
		return senseType{senseAudio, "audio/ogg", "ogg"}
	case bytes.HasPrefix(header, []byte("fLaC")):
		return senseType{senseAudio, "audio/flac", "flac"}
	case bytes.HasPrefix(header, []byte("ID3")):
		return senseType{senseAudio, "audio/mpeg", "mp3"}
	case bytes.HasPrefix(header, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		// Matroska's signature, which WebM shares. A .mka audio file wears it
		// too and will be watched rather than listened to; the watching rung
		// reads a soundtrack, so the answer is right either way.
		return senseType{senseVideo, "video/webm", ""}
	}
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		// The ISO base-media family: one container, three meanings, told apart
		// by the brand that follows.
		switch brand := string(header[8:12]); {
		case strings.HasPrefix(brand, "M4A"):
			return senseType{senseAudio, "audio/mp4", "m4a"}
		case strings.HasPrefix(brand, "qt"):
			return senseType{senseVideo, "video/quicktime", ""}
		default:
			return senseType{senseVideo, "video/mp4", ""}
		}
	}
	if len(header) >= 12 && string(header[0:4]) == "RIFF" {
		switch string(header[8:12]) {
		case "WEBP":
			return senseType{senseImage, "image/webp", ""}
		case "WAVE":
			return senseType{senseAudio, "audio/wav", "wav"}
		}
	}
	return senseType{}
}

// ── the wrapper ─────────────────────────────────────────────────────────────

// senseRead wraps bare's read: the same tool, with four more kinds of file it
// can answer for.
//
// The memo is built HERE, in the closure, rather than as a field on the Agent.
// belt() runs exactly once per agent (agent.go), so a closure variable is
// per-session state with the same lifetime a struct field would have, and it
// keeps a lane that owns no line of session.go from adding one. If a second
// caller ever builds a second belt for the same agent, this becomes a field and
// the comment goes away.
func (a *Agent) senseRead(inner bare.Tool) bare.Tool {
	memo := &senseMemo{}
	return bare.Tool{
		Name:        inner.Name,
		Description: inner.Description + pdfSentence + senseSentence,
		Schema:      inner.Schema,
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Path   string `json:"path"`
				Offset *int   `json:"offset"`
				Limit  *int   `json:"limit"`
			}
			// Arguments that do not parse belong to bare: it owns the wording of
			// every other read error, and a second parser reporting the same
			// fault in different words helps nobody.
			if err := json.Unmarshal(args, &parsed); err != nil {
				return inner.Execute(ctx, args)
			}
			absolute := resolveInWorkspace(parsed.Path, a.config.Workspace)
			// A file that is not there, or is a directory, is bare's sentence
			// and not one of ours. A name ending in .mp3 is a claim about a file
			// that does not exist yet, and "Error reading file: no such file" is
			// the answer to the question the model actually asked.
			info, err := os.Stat(absolute)
			if err != nil || info.IsDir() {
				return inner.Execute(ctx, args)
			}

			shown := filepath.ToSlash(parsed.Path)
			switch entry := sniffSense(absolute); entry.kind {
			case sensePDF:
				return a.pdfSense(shown, absolute, parsed.Offset, parsed.Limit)
			case senseImage:
				return a.imageSense(ctx, memo, shown, absolute, entry, parsed.Offset, parsed.Limit)
			case senseAudio:
				return a.audioSense(ctx, memo, shown, absolute, entry, parsed.Offset, parsed.Limit)
			case senseVideo:
				return a.videoSense(ctx, memo, shown, absolute, entry, parsed.Offset, parsed.Limit)
			}
			return inner.Execute(ctx, args)
		},
	}
}

// ── the size caps ───────────────────────────────────────────────────────────
//
// Every one of these files rides a single JSON request as base64, which inflates
// it by a third, so the number that matters is the payload and not the file.
//
//   - Images reuse image.go's maxImageBytes (10MB), so a photograph is accepted
//     or refused identically whether it is read or attached.
//   - Audio is 25MB, pinned to internal/voice's MaxAudioBytes and to the
//     transcription endpoint's own published ceiling — about half an hour of
//     ordinary speech, which is longer than anything a conversation drops in.
//   - Video is 64MB, which is ~85MB on the wire: generous enough for a few
//     minutes of screen recording at sane settings, bounded enough that a
//     mis-typed path to a Blu-ray rip is refused in one stat instead of read
//     into memory.
const (
	senseAudioMaxBytes = 25 << 20
	senseVideoMaxBytes = 64 << 20
)

// readSenseFile reads a file the sense is about to spend money on, refusing an
// oversized one BEFORE the read rather than after it — image.go's law, for
// image.go's reason: a limit enforced after the read has already pulled a
// multi-gigabyte file into memory to discover it was too big.
//
// A non-empty second return is the whole answer, worded like tools_doc.go's.
func readSenseFile(absolute, shown, kindWord string, ceiling int) ([]byte, string) {
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Sprintf("could not read %s", shown)
	}
	if info.Size() > int64(ceiling) {
		return nil, fmt.Sprintf("%s is over the %dMB %s limit", shown, ceiling>>20, kindWord)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fmt.Sprintf("could not read %s", shown)
	}
	// Checked again: the file could have grown between the stat and the read.
	if len(data) > ceiling {
		return nil, fmt.Sprintf("%s is over the %dMB %s limit", shown, ceiling>>20, kindWord)
	}
	return data, ""
}

// ── the memo ────────────────────────────────────────────────────────────────

// senseReading is one file, perceived once: the provenance note and the text.
// They are kept apart because the note sits OUTSIDE the paged body — the
// footer's line numbers are the description's, so "use offset=451 to continue"
// means line 451 of the description and keeps meaning that whether or not the
// note is above it (tools_doc.go's documentNote states the same rule).
type senseReading struct {
	note string
	text string
}

// senseMemoLimit bounds what one session holds. The memo exists to make PAGING
// free, not to be a cache: a model that reads a long description, then asks for
// lines 200-400 of it, must not be charged for a second look at the same file.
// Four is enough for a conversation working through a couple of files at once;
// the fifth clears the map rather than evicting cleverly, for tools_doc.go's
// reason — the cost of being wrong is one re-read and the cost of a heap policy
// is a heap policy.
const senseMemoLimit = 4

type senseMemo struct {
	// mu guards entries, whose writers are tool calls running in parallel
	// inside one batch (loop.go): two reads of the same recording race here by
	// design, and the loser simply re-stores what the winner stored.
	mu      sync.Mutex
	entries map[string]senseReading
}

func (m *senseMemo) get(key string) (senseReading, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	return entry, ok
}

func (m *senseMemo) put(key string, entry senseReading) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]senseReading, senseMemoLimit)
	}
	if len(m.entries) >= senseMemoLimit {
		m.entries = make(map[string]senseReading, senseMemoLimit)
	}
	m.entries[key] = entry
}

// senseKey is the memo's key: the CONTENT digest and every model that shaped
// the answer. Content and not path, so a file copied or renamed is not looked at
// twice; the models too, because a /model switch mid-session must not serve one
// model's description under another's name.
func senseKey(data []byte, parts ...string) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) + "|" + strings.Join(parts, "|")
}

// ── who answers ─────────────────────────────────────────────────────────────

// senseModel asks the one use-time resolver which model serves a modality
// (Config.MediaModel, docs/MULTIMODAL.md Decision 5). Nil is a surface that
// wired nothing, and "" is a modality with no capable model behind it — the two
// are the same absence here, and both are answered out loud.
//
// STUB(media/knob): the resolver words are "vision", "transcribe", "listen" and
// "watch", and the settings slots behind them are that lane's. These sentences
// name the slot in the sheet's own vocabulary and follow whatever it lands on.
func (a *Agent) senseModel(modality string) string {
	if a.config.MediaModel == nil {
		return ""
	}
	return strings.TrimSpace(a.config.MediaModel(modality))
}

// The three fixed prompts. They are FIXED, and not a `question` argument, and
// that is the point of the whole file: `read` takes a path and nothing else, so
// there is no second thing for the model to get right, and the answer is the
// UNDIRECTED extraction — everything that is in the file, in text — which is
// exactly what read has always returned for a source file. A question about a
// picture is view_image's door and read_document's; this one is "what does this
// file say".
const (
	senseImagePrompt  = "Describe this image completely: transcribe every piece of text exactly, then describe the layout and content."
	senseListenPrompt = "Transcribe any speech exactly; if this is not speech, describe the audio: genre, mood, instruments, structure."
	senseWatchPrompt  = "Describe this video: what happens, on-screen text, speech content, style."
)

// senseOneShot is runVision's shape as a tool result: ONE CALL, ONE ANSWER, and
// nothing of this conversation in it.
//
// No system prompt, no transcript, no tools. The model is being asked what is in
// a file, not being made a second agent with a second memory of this session —
// and the file's bytes never enter the transcript, so the chat model does not
// carry a megabyte of base64 on every step of every turn after this one.
//
// The usage folds into the SESSION total and never the turn's
// ([Agent.addAuxiliaryUsage]), the same treatment the title, the compaction
// summary and read_document's rungs get, and for the same reason: the person
// pays for it, but no turn of theirs ran on that model.
func (a *Agent) senseOneShot(ctx context.Context, model, prompt string, attachment ai.ContentPart) (string, error) {
	message := ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: prompt},
		attachment,
	}}
	response, err := a.client.CompleteWithMessages(ctx, []ai.Message{message}, ai.WithModel(model))
	if response != nil {
		a.addAuxiliaryUsage(response, model, 1)
	}
	if err != nil {
		return "", err
	}
	answer := ""
	if response != nil {
		answer = strings.TrimSpace(response.Text())
	}
	if answer == "" {
		return "", fmt.Errorf("no answer")
	}
	return answer, nil
}

func dataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// senseNote is the one dim line above the description, in the wording
// [Agent.runVision] established for the attachment fallback: a bracketed label
// and the model that spoke. A second model answering in read's voice, silently,
// would be the tool lying about who is talking — and a model that later cites
// "the file says X" ought to be able to see who said so.
func senseNote(label, model string) string { return "[" + label + ": " + model + "]\n" }

// ── the image sense ─────────────────────────────────────────────────────────

// imageSense is the undirected look: one shot on the looking model, one fixed
// prompt, the answer paged by pi's law.
func (a *Agent) imageSense(ctx context.Context, memo *senseMemo, shown, absolute string, entry senseType, offset, limit *int) (string, bool, error) {
	seer := a.senseModel(roleSenseVision)
	if seer == "" {
		return fmt.Sprintf("%s is an image, and this session has no model that can look at one — set the looking model in settings, or attach the picture to a message.", shown), true, nil
	}
	data, refusal := readSenseFile(absolute, shown, "image", maxImageBytes)
	if refusal != "" {
		return refusal, true, nil
	}
	key := senseKey(data, "image", seer)
	if cached, ok := memo.get(key); ok {
		return cached.note + piReadLaw(cached.text, offset, limit), false, nil
	}

	answer, err := a.senseOneShot(ctx, seer, senseImagePrompt, ai.ContentPart{
		Type: "image_url", ImageURL: &ai.ImageURLData{URL: dataURL(entry.mediaType, data)},
	})
	if err != nil {
		return fmt.Sprintf("could not look at %s — %s: %s", shown, seer, oneLineReason(err.Error())), true, nil
	}
	reading := senseReading{note: senseNote("vision", seer), text: answer}
	memo.put(key, reading)
	return reading.note + piReadLaw(reading.text, offset, limit), false, nil
}

// ── the audio ladder ────────────────────────────────────────────────────────

// audioSense is the ladder, and the ladder is the whole idea.
//
// RUNG 1 IS TRANSCRIPTION, because a voice memo is the common case and
// /audio/transcriptions is the cheap, purpose-built endpoint for it — cents
// against a chat model's dollars for the same sentences.
//
// RUNG 2 IS LISTENING: the file itself, as an input_audio part, to a model that
// can hear. It runs when rung 1 failed, when rung 1 came back THIN by
// tools_doc.go's own law, or when what came back is NOISE — the "[Music]",
// "you", "Thank you." that every speech recognizer emits when handed something
// that is not speech.
//
// That last case is why the person who asks "what style is this song" gets an
// answer without the model ever choosing an endpoint. It could not have chosen
// well: it has not heard the file yet, so it does not know whether the answer is
// a transcript or "slow acoustic guitar, melancholy". The ladder finds out.
//
// AND A THIN ANSWER FROM THE LAST RUNG IS THE ANSWER. If rung 2 is out of reach
// or fails, the thin transcript rung 1 produced is returned rather than
// discarded: a four-second recording really does transcribe to three words, and
// a refusal invented by a threshold is worse than a short truth
// ([documentMinimumRunes] says this at length).
func (a *Agent) audioSense(ctx context.Context, memo *senseMemo, shown, absolute string, entry senseType, offset, limit *int) (string, bool, error) {
	scribe := a.senseModel(roleSenseTranscribe)
	listener := a.senseModel(roleSenseListen)
	if a.config.Media == nil && scribe != "" {
		// Rung 1 needs the media client; rung 2 rides the session's own chat
		// client, so an unwired client removes one rung and not the sense. When
		// it removes the ONLY rung, the sentence says which half is missing —
		// "no model is set" would send the person to a settings row that
		// already names a model.
		if listener == "" {
			return fmt.Sprintf("%s is audio, and %s is set to transcribe it but this session has no media client to reach — set the listening model in settings, which rides the session's own model instead.", shown, scribe), true, nil
		}
		scribe = ""
	}
	if scribe == "" && listener == "" {
		return fmt.Sprintf("%s is audio, and this session has no model that can listen to one — set the listening model in settings.", shown), true, nil
	}
	data, refusal := readSenseFile(absolute, shown, "audio", senseAudioMaxBytes)
	if refusal != "" {
		return refusal, true, nil
	}
	key := senseKey(data, "audio", scribe, listener)
	if cached, ok := memo.get(key); ok {
		return cached.note + piReadLaw(cached.text, offset, limit), false, nil
	}

	var failures []string
	// thin is rung 1's answer when it was too little to be believed on its own.
	// It is held rather than dropped so it can become the answer if there turns
	// out to be no rung above it.
	var thin senseReading

	if scribe != "" {
		response, err := a.config.Media.Transcribe(ctx, provider.TranscriptionRequest{
			Model:    scribe,
			Data:     data,
			MIME:     entry.mediaType,
			Filename: filepath.Base(absolute),
		})
		// Accounted BEFORE the answer is judged, by tools_doc.go's law: a rung
		// that billed for an unusable answer still billed.
		if response != nil {
			a.addAuxiliaryUsage(&ai.Response{Usage: response.Usage}, scribe, 1)
		}
		switch {
		case err != nil:
			failures = append(failures, scribe+": "+oneLineReason(err.Error()))
		case response == nil || strings.TrimSpace(response.Text) == "":
			failures = append(failures, scribe+": no speech transcribed")
		default:
			text := strings.TrimSpace(response.Text)
			switch {
			case audioNoise(text):
				failures = append(failures, scribe+": nothing but "+oneLineReason(text))
				thin = senseReading{note: senseNote("transcript", scribe), text: text}
			case documentThin(text):
				failures = append(failures, scribe+": only "+oneLineReason(text))
				thin = senseReading{note: senseNote("transcript", scribe), text: text}
			default:
				reading := senseReading{note: senseNote("transcript", scribe), text: text}
				memo.put(key, reading)
				return reading.note + piReadLaw(reading.text, offset, limit), false, nil
			}
		}
	}

	if listener != "" {
		answer, err := a.senseOneShot(ctx, listener, senseListenPrompt, ai.ContentPart{
			Type: "input_audio", InputAudio: &ai.InputAudioData{
				Data: base64.StdEncoding.EncodeToString(data), Format: entry.format,
			},
		})
		if err == nil {
			reading := senseReading{note: senseNote("audio", listener), text: answer}
			memo.put(key, reading)
			return reading.note + piReadLaw(reading.text, offset, limit), false, nil
		}
		failures = append(failures, listener+": "+oneLineReason(err.Error()))
	}

	if thin.text != "" {
		memo.put(key, thin)
		return thin.note + piReadLaw(thin.text, offset, limit), false, nil
	}
	// Every rung named, in the order they were tried, because "which one broke"
	// is the difference between a model that retries forever and a person who
	// knows whether to add credit or change a setting.
	return fmt.Sprintf("could not read %s — %s", shown, strings.Join(failures, "; ")), true, nil
}

// audioNoiseMarkers are the phrases a speech recognizer emits when it was handed
// something that is not speech. They are the reason the ladder has a second
// rung at all: a song transcribes to a page of "[Music]", which is a SUCCESSFUL
// call returning nothing anybody asked for.
//
// The set is small and literal on purpose. Anything cleverer would eventually
// throw away a real transcript, and the cost of missing one of these is one
// answer that says "[Music]" instead of describing a song.
var audioNoiseMarkers = map[string]bool{
	"":                                true,
	"you":                             true,
	"thankyou":                        true,
	"thanksforwatching":               true,
	"music":                           true,
	"blankaudio":                      true,
	"silence":                         true,
	"applause":                        true,
	"inaudible":                       true,
	"noaudio":                         true,
	"soundeffects":                    true,
	"foreign":                         true,
	"subtitlesbytheamaraorgcommunity": true,
}

// audioNoise answers whether a transcript is a recognizer describing its own
// failure rather than words anybody said.
//
// Bracketed and parenthesized cues are stripped first — a page of "[Music]
// [Music] [Music]" is long enough to pass every length test there is — and what
// is left is reduced to bare letters before it is looked up, so "Thank you." and
// "thank you" and "THANK YOU!" are one entry rather than three.
func audioNoise(text string) bool {
	var bare strings.Builder
	depth := 0
	for _, character := range text {
		switch character {
		case '[', '(':
			depth++
			continue
		case ']', ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			continue
		}
		if unicode.IsLetter(character) {
			bare.WriteRune(unicode.ToLower(character))
		}
	}
	return audioNoiseMarkers[bare.String()]
}

// ── the video sense ─────────────────────────────────────────────────────────

// videoSense is one shot on the watching model, with the file as a video_url
// data part.
//
// There is no ladder here and that is honest rather than unfinished: nothing
// cheaper than a model that can watch exists to fall back to, exactly as
// read_document has one rung for an image ([documentRungs]).
func (a *Agent) videoSense(ctx context.Context, memo *senseMemo, shown, absolute string, entry senseType, offset, limit *int) (string, bool, error) {
	watcher := a.senseModel(roleSenseWatch)
	if watcher == "" {
		return fmt.Sprintf("%s is video, and this session has no model that can watch one — set the watching model in settings.", shown), true, nil
	}
	data, refusal := readSenseFile(absolute, shown, "video", senseVideoMaxBytes)
	if refusal != "" {
		return refusal, true, nil
	}
	key := senseKey(data, "video", watcher)
	if cached, ok := memo.get(key); ok {
		return cached.note + piReadLaw(cached.text, offset, limit), false, nil
	}

	answer, err := a.senseOneShot(ctx, watcher, senseWatchPrompt, ai.ContentPart{
		Type: "video_url", VideoURL: &ai.VideoURLData{URL: dataURL(entry.mediaType, data)},
	})
	if err != nil {
		return fmt.Sprintf("could not watch %s — %s: %s", shown, watcher, oneLineReason(err.Error())), true, nil
	}
	reading := senseReading{note: senseNote("video", watcher), text: answer}
	memo.put(key, reading)
	return reading.note + piReadLaw(reading.text, offset, limit), false, nil
}

// The four words this file hands Config.MediaModel. They are constants because
// a typo in one of them is a modality that silently resolves to nothing, which
// reads on the surface as "this session has no model that can look at one" on a
// machine that has four.
//
// STUB(media/knob): these are the resolver's own vocabulary; that lane owns the
// ladder behind each word.
const (
	roleSenseVision     = "vision"
	roleSenseTranscribe = "transcribe"
	roleSenseListen     = "listen"
	roleSenseWatch      = "watch"
)
