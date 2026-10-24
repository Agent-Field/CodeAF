package session

// mediafact.go is the structured record of a media file the harness made or was
// handed: what the file IS, said once, in typed fields, so no surface has to
// read a tool's prose to learn how big a picture is or what a voice was told
// to say.
//
// It rides on the deliverables index (artifacts.go) as ONE optional field on the
// existing row. There is no second store: a row written before this file existed
// still parses, still lists, and gets an honest, thinner answer (a legacy row is
// a path, a kind and a time — nothing more is claimed for it).
//
// ── A SOURCE ID IS GRANTED BY THE ENGINE, NEVER NAMED BY A CALLER ──
//
// A surface that wants to open, play or copy a file asks the engine for the
// SOURCE it already knows and is handed back a path it recorded itself. The id
// is derived from immutable facts of the record (origin, session, canonical
// path) and never from a path somebody supplies in a request, so a caller that
// can name any string still cannot make the engine vouch for a file it did not
// record. The scope law (see [ResolveMedia]) is the engine's, not any one
// surface's.
//
// ── UNKNOWN STAYS UNKNOWN ──
//
// A width, height, duration or charge that nothing actually reported is zero
// in the record and ABSENT on the wire (design-law §EMPTINESS). Dimensions come
// from the file's own header (image.DecodeConfig reads a header, not the
// picture), a duration from the provider or a probe, a charge from the
// provider's reported usage: positive is charged, an explicit zero is not
// charged, and an absent figure is unknown — never a fabricated $0.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	stdimage "image"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Where a media file came from.
const (
	// MediaAttachment is a file the person explicitly attached to a message.
	MediaAttachment = "attachment"
	// MediaGenerated is a file a tool of the harness made.
	MediaGenerated = "generated"
	// MediaEvidence is a file the agent looked at, listened to, watched or read.
	MediaEvidence = "evidence"
	// MediaShared is a file the agent DELIBERATELY handed over with
	// share_deliverable (share_publish.go). It says who handed the file over and
	// nothing about who made it: it is never generated, never a declared shell
	// output whose change was observed, and never evidence. Its record carries
	// only what the file's own bytes and header said, and the dispatch it was
	// shared under.
	MediaShared = "shared"
)

// What the agent did with a file it looked at.
const (
	EvidenceLookedAt   = "looked_at"
	EvidenceListenedTo = "listened_to"
	EvidenceWatched    = "watched"
	// EvidenceRead is a document the agent read (read_document). It is the only
	// verb on a "document" kind source.
	EvidenceRead = "read"
)

// mediaDocument is the kind of a document the agent read. It exists ONLY as
// evidence: a row of this kind with no structured fact, or one that is not
// evidence, is some other deliverable and is not a media source.
const mediaDocument = "document"

// Whether the provider billed for a generation.
const (
	MediaCharged       = "charged"
	MediaNotCharged    = "not_charged"
	MediaChargeUnknown = "unknown"
)

// What a source is now, against the record.
const (
	MediaAvailable  = "available"
	MediaChanged    = "changed"
	MediaMissing    = "missing"
	MediaOutside    = "outside"
	MediaUnreadable = "unreadable"
)

const (
	mediaIDPrefix     = "src_"
	mediaLegacyPrefix = "srl_"
)

// mediaChunk is one backward read of the index; mediaScanBudget is the most
// bytes one listing call reads before it hands back a resumable cursor. They
// are variables only so a test can make a small index look large.
var (
	mediaChunk      int64 = 256 << 10
	mediaScanBudget int64 = 16 << 20
)

// The bounds of reading sources. Every one is a cap on work, not a promise
// about content.
const (
	// MediaPageDefault and MediaPageMax bound one page of a listing.
	MediaPageDefault = 50
	MediaPageMax     = 100
	// mediaProbeBytes is how much of a file a header probe reads.
	mediaProbeBytes = 64 << 10
	// mediaSaidMax is the longest spoken text kept verbatim; longer is kept
	// as nothing rather than cut, because a cut quotation is a wrong one.
	mediaSaidMax = 32 << 10
	// mediaRowMax is the longest index line read; anything longer is skipped.
	mediaRowMax = 256 << 10
	// mediaReferencesMax bounds the reference paths one row keeps.
	mediaReferencesMax = 32
)

// MediaFact is what the index remembers about one media file, beyond the
// columns every artifact row has. Every field is optional on disk.
type MediaFact struct {
	// ID is the source id, persisted for new records; legacy rows have the
	// same id computed from the same function ([MediaSourceID]).
	ID     string `json:"id,omitempty"`
	Origin string `json:"origin,omitempty"`
	// Canonical is the symlink-resolved absolute path at the moment of
	// recording. The id is derived from it.
	Canonical string `json:"canonical,omitempty"`
	MIME      string `json:"mime,omitempty"`
	// Bytes, ModNS, CtimeNS, Dev and Ino are the file as it was recorded.
	Bytes   int64  `json:"bytes,omitempty"`
	ModNS   int64  `json:"mod_ns,omitempty"`
	CtimeNS int64  `json:"ctime_ns,omitempty"`
	Dev     uint64 `json:"dev,omitempty"`
	Ino     uint64 `json:"ino,omitempty"`
	// SHA256 is the content hash, set only where the engine held the bytes
	// when it wrote them.
	SHA256     string `json:"sha256,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	// Model is the model the call ASKED for (the resolved request), not a
	// claim about which machine served it: no provider response reports that
	// today, so there is no served model to record.
	Model string `json:"model,omitempty"`
	// Requested is how many pictures the request asked for in one call (the
	// request's own N), where the request has one. Files with the same Tool id
	// were asked for together; nothing is inferred beyond that.
	Requested int `json:"requested_count,omitempty"`
	// Batch, VariantIndex and Returned describe ONE provider request that asked
	// for several pictures (generate_image's n > 1); see [MediaFact.WithBatch].
	// Files with the same Batch came from the same request. VariantIndex is the
	// 1-based position in the provider's reply (a gap means that output was not
	// saved) and Returned is how many pictures the provider's reply held. None of
	// the three says anything about quality: variants are never ranked.
	Batch        string `json:"batch,omitempty"`
	VariantIndex int    `json:"variant_index,omitempty"`
	Returned     int    `json:"returned_count,omitempty"`
	// Declared and Change are set only on a file a shell command declared as an
	// output (origin published): the path as the model spelled it, and what the
	// engine observed around that one command (see shell_outputs.go).
	Declared string `json:"declared,omitempty"`
	Change   string `json:"change,omitempty"`
	// Run, Message and Tool are immutable ids of the turn that made it, where
	// the tool site knew them.
	Run     string `json:"run,omitempty"`
	Message string `json:"message,omitempty"`
	Tool    string `json:"tool,omitempty"`
	// References are the exact reference_paths the generator was given.
	References           []string `json:"references,omitempty"`
	InputMessages        []string `json:"input_messages,omitempty"`
	InputMessagesOmitted bool     `json:"input_messages_omitted,omitempty"`
	// Said is the exact text given to speak.
	Said string `json:"said,omitempty"`
	// The rest is set only on evidence — a file the agent LOOKED AT (origin
	// evidence). Verb is what it did, Saw the looking model's words verbatim
	// (omitted, with SawOmitted, when too long to keep whole), SeenBy the model
	// that looked and Working the model the session was running when it did.
	// Transcribed says the words are a transcript from a speech endpoint.
	Verb        string `json:"verb,omitempty"`
	Saw         string `json:"saw,omitempty"`
	SawOmitted  bool   `json:"saw_omitted,omitempty"`
	SeenBy      string `json:"seen_by,omitempty"`
	Working     string `json:"working_model,omitempty"`
	Transcribed bool   `json:"transcribed,omitempty"`
	// PageCount and TextLayer are what the PARSER reported about a document it
	// read, and are absent when it reported nothing: a parse that bills per page
	// and returns no boundaries has no page count, and "no text layer" is only
	// known when the local extractor found a scanned file. TextLayer is a pointer
	// so a known false stays distinct from an unknown.
	PageCount int   `json:"page_count,omitempty"`
	TextLayer *bool `json:"text_layer,omitempty"`
	// Charge and CostUSD are the provider's reported usage, see
	// [ChargeFromUsage].
	Charge  string   `json:"charge,omitempty"`
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// CostScope is "call" when the charge covers the whole provider request and
	// not this file alone; CallCostUSD is then that request's TOTAL, repeated on
	// every file of the Batch, and CostUSD is left unset because no provider
	// reports a per-picture price. Sum CallCostUSD once per Batch, never per file.
	CostScope   string   `json:"cost_scope,omitempty"`
	CallCostUSD *float64 `json:"call_cost_usd,omitempty"`
}

// NewMediaID mints the id of a new source: random, unique per record, written
// once with the row and never recomputed. TWO FILES WRITTEN AT THE SAME PATH ARE
// TWO SOURCES: an id derived from the path would make a regenerated draft the
// same source as the one it replaced, and break every version chain built on it.
func NewMediaID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand does not fail on a working machine; a clock-derived id
		// is still unique enough for an index row that would otherwise be lost.
		sum := sha256.Sum256([]byte(time.Now().String()))
		copy(raw[:], sum[:])
	}
	return mediaIDPrefix + hex.EncodeToString(raw[:])
}

// LegacyMediaID is the id of a row recorded before ids were persisted: a
// function of the row's own immutable columns — session, origin, recorded
// canonical path and creation time — so it is the same on every read and never
// derived from a path a caller supplies. The prefix differs from [NewMediaID]'s
// so the two can never collide.
func LegacyMediaID(sessionID, origin, canonical string, created time.Time) string {
	sum := sha256.Sum256([]byte("media-source/legacy/v1\x00" + sessionID + "\x00" + origin + "\x00" + canonical +
		"\x00" + strconv.FormatInt(created.UnixNano(), 10)))
	return mediaLegacyPrefix + hex.EncodeToString(sum[:12])
}

// validMediaID says whether id has the shape this engine issues.
func validMediaID(id string) bool {
	switch {
	case strings.HasPrefix(id, mediaIDPrefix):
		return len(id) == len(mediaIDPrefix)+32 && isHex(id[len(mediaIDPrefix):])
	case strings.HasPrefix(id, mediaLegacyPrefix):
		return len(id) == len(mediaLegacyPrefix)+24 && isHex(id[len(mediaLegacyPrefix):])
	}
	return false
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// canonicalFile is path made absolute with every link followed, or cleaned
// when it cannot be followed (a file already gone still has a spelling).
func canonicalFile(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// mediaKindOf is the kind a sniffed content type belongs to, or "" for a file
// that is not media.
func mediaKindOf(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	}
	return ""
}

// sniffMedia is the media content type of the head of a file, or "".
func sniffMedia(head []byte) string {
	if mp3Frames(head) {
		return "audio/mpeg"
	}
	if len(head) > 512 {
		head = head[:512]
	}
	mime := http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	if mediaKindOf(mime) == "" {
		return ""
	}
	return mime
}

// imageSize is the header size of an image, and false when the standard
// library does not know the format (webp reports no size rather than a guess).
func imageSize(head []byte) (width, height int, ok bool) {
	config, _, err := stdimage.DecodeConfig(bytes.NewReader(head))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// ChargeFromUsage reads what the provider said it charged: a positive cost is
// charged, an explicit zero is not charged, and no figure at all is unknown.
func ChargeFromUsage(usage *ai.Usage) (string, *float64) {
	if usage == nil || usage.Cost == nil {
		return MediaChargeUnknown, nil
	}
	cost := *usage.Cost
	if math.IsNaN(cost) || math.IsInf(cost, 0) {
		// Not representable as JSON and not a figure: unknown, never charged.
		return MediaChargeUnknown, nil
	}
	switch {
	case cost > 0:
		return MediaCharged, &cost
	case cost == 0:
		return MediaNotCharged, nil
	}
	return MediaChargeUnknown, nil
}

// GeneratedMedia is the fact for bytes a tool has just written at path. The
// bytes are in hand, so the hash, the size and the picture's header are read
// from them and nothing is read back from disk but the file's identity.
func GeneratedMedia(path string, data []byte, model string) MediaFact {
	fact := MediaFact{Origin: MediaGenerated, Model: strings.TrimSpace(model), Bytes: int64(len(data))}
	sum := sha256.Sum256(data)
	fact.SHA256 = hex.EncodeToString(sum[:])
	head := data
	if len(head) > mediaProbeBytes {
		head = head[:mediaProbeBytes]
	}
	fact.MIME = sniffMedia(head)
	if mediaKindOf(fact.MIME) == "image" {
		if w, h, ok := imageSize(head); ok {
			fact.Width, fact.Height = w, h
		}
	}
	fact.measureContainer(data)
	fact.stampFile(path)
	return fact
}

// measureContainer reads the length of a WAV and the length and frame size of
// an mp4 from bytes in hand, and leaves anything it cannot state as unknown.
// It goes by the container's own signature and not by the sniffed content type:
// http.DetectContentType does not know the "isom" brand real renders carry.
// Nothing here looks at a request: a duration the caller ASKED for is not one
// the file has.
func (f *MediaFact) measureContainer(data []byte) {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		if length, ok := wavLength(data); ok {
			f.DurationMS = length.Milliseconds()
		}
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		if length, _, ok := mp4Facts(data); ok {
			f.DurationMS = length.Milliseconds()
		}
		if w, h, ok := mp4FrameSize(data); ok {
			f.Width, f.Height = w, h
		}
	}
}

// DerivedMedia is the fact for a file a local tool made (ffmpeg): the bytes are
// not in hand, so only the head of the file is read and no hash is taken. The
// origin is generated, and nothing is claimed about a charge.
func DerivedMedia(path string) MediaFact {
	fact := MediaFact{Origin: MediaGenerated}
	canonical := canonicalFile(path)
	if file, err := os.Open(canonical); err == nil {
		head, _ := io.ReadAll(io.LimitReader(file, mediaProbeBytes))
		_ = file.Close()
		fact.MIME = sniffMedia(head)
		if mediaKindOf(fact.MIME) == "image" {
			if w, h, ok := imageSize(head); ok {
				fact.Width, fact.Height = w, h
			}
		}
	}
	fact.stampFile(canonical)
	return fact
}

// stampFile records the file's identity as it is now.
func (f *MediaFact) stampFile(path string) {
	f.Canonical = canonicalFile(path)
	info, err := os.Stat(f.Canonical)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	f.Bytes = info.Size()
	f.ModNS = info.ModTime().UnixNano()
	f.CtimeNS, f.Dev, f.Ino = fileIdentity(info)
}

// WithUsage sets the charge from the provider's reported usage.
func (f MediaFact) WithUsage(usage *ai.Usage) MediaFact {
	f.Charge, f.CostUSD = ChargeFromUsage(usage)
	return f
}

// MediaCostScopeCall is the [MediaFact.CostScope] of a charge that covers a
// whole provider request.
const MediaCostScopeCall = "call"

// WithBatch marks a file as one output of a single multi-picture provider
// request: the batch id minted once for that request, the file's 1-based
// position in the reply, and how many pictures the reply held.
func (f MediaFact) WithBatch(batch string, variant, returned int) MediaFact {
	f.Batch, f.VariantIndex, f.Returned = batch, variant, returned
	return f
}

// WithCallUsage records the provider's usage as the cost of the WHOLE request:
// the total is kept as CallCostUSD and never as this file's CostUSD, because
// dividing it, or repeating it as each file's price, would invent a figure.
func (f MediaFact) WithCallUsage(usage *ai.Usage) MediaFact {
	var total *float64
	f.Charge, total = ChargeFromUsage(usage)
	f.CostScope, f.CallCostUSD, f.CostUSD = MediaCostScopeCall, total, nil
	return f
}

// mediaMade is what the call that is making a file knew when it started: the
// inputs it was handed and the immutable ids of the turn and the tool call. It
// is captured where the tool runs, on the call's own context, because a job
// that lands the file minutes later runs on a background context that has
// neither.
type mediaMade struct {
	Inputs    []string
	Messages  mediaInputMessages
	Run, Tool string
	// Asked is the authored message the call was made under ([askedMessageID]);
	// empty when the call carries no such causality.
	Asked string
}

// madeBy reads the call's identity off its context: the run is the turn's
// immutable execution id, the tool is the provider's id for this very call.
// Either may be absent (a test, a door with no turn) and is then left unset.
func madeBy(ctx context.Context, inputs []string) mediaMade {
	return mediaMade{Inputs: inputs, Messages: mediaInputsFrom(ctx), Run: runIdentity(ctx), Tool: callIDFrom(ctx), Asked: askedMessageID(ctx)}
}

// WithMade stamps the call's inputs and identity onto a fact.
func (f MediaFact) WithMade(made mediaMade) MediaFact {
	f.Run, f.Tool, f.Message = made.Run, made.Tool, made.Asked
	f.InputMessages = append([]string(nil), made.Messages.IDs...)
	f.InputMessagesOmitted = made.Messages.Omitted
	return f.WithReferences(made.Inputs)
}

// WithRequested keeps how many files the request asked for.
func (f MediaFact) WithRequested(n int) MediaFact {
	if n > 0 {
		f.Requested = n
	}
	return f
}

// WithReferences keeps the exact reference paths, bounded.
func (f MediaFact) WithReferences(paths []string) MediaFact {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" || len(f.References) >= mediaReferencesMax {
			continue
		}
		f.References = append(f.References, path)
	}
	return f
}

// WithSaid keeps the exact text a voice was given, or nothing when it is too
// long to keep whole.
func (f MediaFact) WithSaid(text string) MediaFact {
	if text != "" && len(text) <= mediaSaidMax {
		f.Said = text
	}
	return f
}

// WithDuration keeps a duration the provider or a probe genuinely knew.
func (f MediaFact) WithDuration(length time.Duration) MediaFact {
	if length > 0 {
		f.DurationMS = length.Milliseconds()
	}
	return f
}

// WithSize keeps a frame size a probe genuinely read.
func (f MediaFact) WithSize(width, height int) MediaFact {
	if width > 0 && height > 0 {
		f.Width, f.Height = width, height
	}
	return f
}

// RecordMedia writes a media row: the existing artifact columns plus the fact,
// under a freshly minted source id, which it returns ("" when nothing was
// written). The id is minted here so no tool site can forget it.
func RecordMedia(index string, artifact Artifact, fact MediaFact) string {
	id, _ := recordMediaRow(index, artifact, fact)
	return id
}

// recordMediaRow is [RecordMedia] that also answers the row as WRITTEN, which
// can differ from the one offered: an over-size deliverable fact is dropped
// ([RecordArtifact]), and an event may name only a deliverable that is there.
func recordMediaRow(index string, artifact Artifact, fact MediaFact) (string, Artifact) {
	if fact.Canonical == "" {
		fact.Canonical = canonicalFile(artifact.Path)
	}
	if fact.Origin == "" {
		fact.Origin = MediaGenerated
	}
	fact.ID = NewMediaID()
	artifact.Media = &fact
	written, ok := recordArtifactRow(index, artifact)
	if !ok {
		return "", Artifact{}
	}
	return fact.ID, written
}

// RecordAttachment records a file the person attached to a message in the
// session, when it is a regular file that looks like an image, a sound or a
// video. A file that is not media, or cannot be read, records nothing (an
// attachment is not a deliverable, and a row nobody could open is noise). Only
// the head of the file is read.
func RecordAttachment(index, sessionID, path string) string {
	if sessionID == "" {
		return ""
	}
	canonical := canonicalFile(path)
	info, err := os.Stat(canonical)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	file, err := os.Open(canonical)
	if err != nil {
		return ""
	}
	head, _ := io.ReadAll(io.LimitReader(file, mediaProbeBytes))
	_ = file.Close()
	mime := sniffMedia(head)
	if mime == "" {
		return ""
	}
	fact := MediaFact{Origin: MediaAttachment, MIME: mime}
	if mediaKindOf(mime) == "image" {
		if w, h, ok := imageSize(head); ok {
			fact.Width, fact.Height = w, h
		}
	}
	// Reuse only the bytes already held by the bounded header probe. A container
	// whose metadata or declared payload is not present stays unknown.
	fact.measureContainer(head)
	fact.stampFile(canonical)
	return RecordMedia(index, Artifact{
		Path:    canonical,
		Session: sessionID,
		Title:   filepath.Base(canonical),
		Kind:    mediaKindOf(mime),
		Created: time.Now(),
	}, fact)
}

// recordMedia is the agent's door: the session id and the index are the
// agent's own.
func (a *Agent) recordMedia(artifact Artifact, fact MediaFact) {
	artifact.Session = a.journalID()
	// The sealed copy a place keeps beside the index (artifactcell.go).
	RecordSealedArtifact(a.config.Place, artifact)
	if fact.Origin == "" || fact.Origin == MediaGenerated {
		artifact.Deliver = a.generatedDeliverable(artifact.Path, fact)
	}
	if id, written := recordMediaRow(a.config.ArtifactsIndex, artifact, fact); id != "" {
		a.emitMediaChange(id, fact)
		a.emitArtifactChange(written)
	}
}

// fileVersion is the identity form of a version: size, modification time,
// change time, device and inode, as stat reports them.
func fileVersion(size, modNS, ctimeNS int64, dev, ino uint64) string {
	return "file:" + strconv.FormatInt(size, 10) + ":" + strconv.FormatInt(modNS, 10) + ":" +
		strconv.FormatInt(ctimeNS, 10) + ":" + strconv.FormatUint(dev, 10) + ":" + strconv.FormatUint(ino, 10)
}

// MediaRow is one source as the index remembers it, the legacy shape
// included, with its id settled.
type MediaRow struct {
	Artifact Artifact
	Fact     MediaFact
	Kind     string
	// Recorded says a structured fact was recorded with the row; false is a
	// legacy row whose only facts are the artifact's own columns.
	Recorded bool
	// Start and Len are where the row's line sits in the index: the offset of
	// its first byte and its length without the newline. They are what a page
	// cursor resumes from.
	Start int64
	Len   int
}

// Version is the recorded version: the content hash where the engine held the
// bytes, else the file's recorded identity. A legacy row has none.
func (r MediaRow) Version() string {
	switch {
	case r.Fact.SHA256 != "":
		return "sha256:" + r.Fact.SHA256
	case r.Recorded && r.Fact.ModNS != 0:
		return fileVersion(r.Fact.Bytes, r.Fact.ModNS, r.Fact.CtimeNS, r.Fact.Dev, r.Fact.Ino)
	}
	return ""
}

// mediaRowOf settles one index row into a source, or reports that it is not
// one (an export, a document, a row with no media kind).
func mediaRowOf(row Artifact) (MediaRow, bool) {
	out := MediaRow{Artifact: row}
	if row.Deliver != nil && row.Deliver.Update {
		// An appended update restates a deliverable; the media source it
		// restates is the base row, and listing both would be two sources.
		return MediaRow{}, false
	}
	if row.Media != nil {
		out.Fact = *row.Media
		out.Recorded = true
	}
	switch row.Kind {
	case "image", "audio", "video":
		out.Kind = row.Kind
	case mediaDocument:
		// Other deliverables are called documents too; only the engine's own
		// evidence row is a media source.
		if out.Recorded && out.Fact.Origin == MediaEvidence {
			out.Kind = mediaDocument
		}
	default:
		out.Kind = mediaKindOf(out.Fact.MIME)
	}
	if out.Kind == "" {
		return MediaRow{}, false
	}
	if out.Fact.Origin == "" {
		// Only generators wrote these kinds before facts existed.
		out.Fact.Origin = MediaGenerated
	}
	if out.Fact.Canonical == "" {
		out.Fact.Canonical = canonicalFile(row.Path)
	}
	if !validMediaID(out.Fact.ID) {
		out.Fact.ID = LegacyMediaID(row.Session, out.Fact.Origin, out.Fact.Canonical, row.Created)
	}
	return out, true
}

// Why a listing or a lookup did not answer in full.
var (
	// ErrMediaCursor is a cursor this engine did not issue, or one whose index
	// has since been replaced or shortened: list again from the start.
	ErrMediaCursor = errors.New("media cursor is not valid for the index as it is now")
	// ErrMediaNotFound is a lookup that read the WHOLE index and found nothing.
	ErrMediaNotFound = errors.New("no such media source in this conversation")
	// ErrMediaBusy is a lookup that ran out of time before it had read the
	// whole index. It says nothing about whether the source exists.
	ErrMediaBusy = errors.New("the media index could not be searched in time")
	// ErrMediaTooLarge is a single record that cannot be sent within the reply
	// bound. It is reported, not skipped: a list that silently lost a row would
	// be wrong in a way nobody could see.
	ErrMediaTooLarge = errors.New("a media record is too large to send")
)

// mediaCursorBody is the position a listing resumes from: the index file's
// identity and the byte offset below which older rows remain. The file is
// append-only, so an offset stays valid as rows are appended; a replaced or
// shortened file does not, and the cursor then says so rather than guessing.
type mediaCursorBody struct {
	Dev uint64 `json:"d"`
	Ino uint64 `json:"i"`
	End int64  `json:"e"`
}

func encodeMediaCursor(body mediaCursorBody) string {
	raw, _ := json.Marshal(body)
	return "m2." + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeMediaCursor(cursor string) (mediaCursorBody, error) {
	var body mediaCursorBody
	text, ok := strings.CutPrefix(cursor, "m2.")
	if !ok {
		return body, ErrMediaCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || json.Unmarshal(raw, &body) != nil || body.End < 0 {
		return mediaCursorBody{}, ErrMediaCursor
	}
	return body, nil
}

// scanBackward visits the lines of file below end, newest (last) first, reading
// at most budget bytes and stopping at deadline. It keeps one chunk and one
// partial line in memory, whatever the file's size. visit answers false to stop
// after the line it was given.
//
// The boundary it returns is the start offset of the earliest line it finished
// with: everything at or after it has been visited, and a caller resumes by
// scanning below it. Zero means the start of the file was reached. A line
// longer than mediaRowMax is not visited at all.
func scanBackward(file *os.File, end, budget int64, deadline time.Time, visit func(start int64, line []byte) bool) (boundary int64, err error) {
	pos, boundary := end, end
	var carry []byte
	overlong := false
	for pos > 0 {
		if budget <= 0 || (!deadline.IsZero() && time.Now().After(deadline)) {
			if overlong {
				// What was read of the oversize line is garbage; resume below it.
				boundary = pos
			}
			return boundary, nil
		}
		size := mediaChunk
		if size > pos {
			size = pos
		}
		buf := make([]byte, size+int64(len(carry)))
		if _, err := file.ReadAt(buf[:size], pos-size); err != nil && !errors.Is(err, io.EOF) {
			return boundary, err
		}
		copy(buf[size:], carry)
		budget -= size
		pos -= size
		// Walk the segments from the last, so lines arrive newest first.
		hi := len(buf)
		for {
			nl := bytes.LastIndexByte(buf[:hi], '\n')
			if nl < 0 {
				break
			}
			if !overlong && hi-(nl+1) > 0 {
				start := pos + int64(nl) + 1
				if !visit(start, buf[nl+1:hi]) {
					return start, nil
				}
			}
			overlong = false
			boundary = pos + int64(nl) + 1
			hi = nl
		}
		// buf[:hi] holds no newline: the tail of a line that starts earlier.
		switch {
		case pos == 0:
			if !overlong && hi > 0 && !visit(0, buf[:hi]) {
				return 0, nil
			}
			return 0, nil
		case overlong || hi > mediaRowMax:
			overlong, carry = true, nil
		default:
			carry = append(carry[:0], buf[:hi]...)
		}
	}
	return 0, nil
}

// MediaScan is one page of a listing.
type MediaScan struct {
	Rows []MediaRow
	// Next resumes the listing below this page; empty means the whole index
	// has been read.
	Next string
	// Incomplete says the page is short NOT because the history ended but
	// because the call spent its read budget: Next is set and the caller keeps
	// asking. An empty incomplete page is normal for a sparse session in a big
	// index.
	Incomplete bool
}

// mediaNeedle is the byte pattern every row of a session contains, so a line of
// another conversation is dropped without being parsed. The id is spelled as
// the row's writer spells it (json.Marshal, which escapes a quote, a
// backslash, a control character, &, <, > and U+2028/U+2029): searched for raw,
// an id holding any of those never met its own rows. Every caller still
// decodes a hit and compares the session exactly.
func mediaNeedle(sessionID string) []byte {
	return []byte(`"session":"` + mediaSessionValue(sessionID) + `"`)
}

// mediaSessionValue is a session id exactly as a written row holds it between
// the quotes of its "session" value.
func mediaSessionValue(sessionID string) string {
	raw, _ := json.Marshal(sessionID)
	return string(raw[1 : len(raw)-1])
}

// openIndex opens the index and reports its identity and size.
func openIndex(index string) (*os.File, mediaCursorBody, error) {
	file, err := os.Open(index)
	if err != nil {
		return nil, mediaCursorBody{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, mediaCursorBody{}, err
	}
	_, dev, ino := fileIdentity(info)
	return file, mediaCursorBody{Dev: dev, Ino: ino, End: info.Size()}, nil
}

// ListMedia is one page of the session's media sources, newest first, read
// backward through the WHOLE index from the cursor. Each call reads at most
// mediaScanBudget bytes and keeps only the page; a session with few rows in a
// large index takes several calls, each handing back where to resume. The first
// call fixes the end of the index it reads to, so a page boundary is not moved
// by rows appended while the person scrolls.
func ListMedia(index, sessionID, cursor string, limit int, deadline time.Time) (MediaScan, error) {
	return ListMediaFit(index, sessionID, cursor, limit, deadline, 0, nil)
}

// ListMediaFit is ListMedia with a reply budget: fit answers what a row will
// cost to send, and the page ends before the row that would take it past
// budget, with a cursor that resumes at that row. A single row that alone does
// not fit is [ErrMediaTooLarge], never a silent drop.
func ListMediaFit(index, sessionID, cursor string, limit int, deadline time.Time, budget int, fit func(MediaRow) (int, error)) (MediaScan, error) {
	spent, tooLarge := 0, false
	if strings.TrimSpace(index) == "" || sessionID == "" {
		return MediaScan{}, nil
	}
	if limit <= 0 {
		limit = MediaPageDefault
	}
	limit = min(limit, MediaPageMax)
	file, now, err := openIndex(index)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MediaScan{}, nil
		}
		return MediaScan{}, err
	}
	defer file.Close()
	end := now.End
	if cursor != "" {
		held, err := decodeMediaCursor(cursor)
		if err != nil || held.Dev != now.Dev || held.Ino != now.Ino || held.End > now.End {
			return MediaScan{}, ErrMediaCursor
		}
		end = held.End
	}
	needle := mediaNeedle(sessionID)
	var scan MediaScan
	rejected := false
	boundary, err := scanBackward(file, end, mediaScanBudget, deadline, func(start int64, line []byte) bool {
		if !bytes.Contains(line, needle) {
			return true
		}
		var row Artifact
		if json.Unmarshal(line, &row) != nil || row.Session != sessionID ||
			strings.TrimSpace(row.Path) == "" || strings.TrimSpace(row.Title) == "" {
			return true
		}
		media, ok := mediaRowOf(row)
		if !ok {
			return true
		}
		media.Start, media.Len = start, len(line)
		if fit != nil {
			cost, err := fit(media)
			if err != nil || cost > budget-spent {
				if len(scan.Rows) == 0 {
					tooLarge = true
				}
				rejected = true
				return false
			}
			spent += cost
		}
		scan.Rows = append(scan.Rows, media)
		cacheMediaRow(index, mediaCacheKey{dev: now.Dev, ino: now.Ino}, media, line)
		return len(scan.Rows) < limit
	})
	if err != nil {
		return MediaScan{}, err
	}
	if tooLarge {
		return MediaScan{}, ErrMediaTooLarge
	}
	if rejected {
		// The page ends BEFORE the row that did not fit; the next page starts
		// with it. Nothing is dropped and the cursor says exactly where.
		boundary = scan.Rows[len(scan.Rows)-1].Start
	}
	if boundary > 0 {
		scan.Next = encodeMediaCursor(mediaCursorBody{Dev: now.Dev, Ino: now.Ino, End: boundary})
		scan.Incomplete = len(scan.Rows) < limit && !rejected
	}
	return scan, nil
}

// FindMedia is the session's source with id. A record a listing or an earlier
// lookup already read is answered from the bounded cache after one validating
// read of its line (mediacache.go); otherwise the whole index is read backward
// in bounded memory, at most mediaScanSlots such reads at a time, and askers of
// the same record share one. It never answers not-found for a source that was
// only out of reach: a deadline, or every slot taken, answers [ErrMediaBusy].
func FindMedia(index, sessionID, id string, deadline time.Time) (MediaRow, error) {
	if !validMediaID(id) {
		return MediaRow{}, ErrMediaNotFound
	}
	// A warm cache still validates a record on disk. Do not start that read
	// after the caller's budget expired.
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return MediaRow{}, ErrMediaBusy
	}
	if row, ok := cachedMediaRow(index, sessionID, id); ok {
		return row, nil
	}
	file, now, err := openIndex(index)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MediaRow{}, ErrMediaNotFound
		}
		return MediaRow{}, err
	}
	key := mediaCacheKey{index: index, session: sessionID, id: id, dev: now.Dev, ino: now.Ino}
	flight, run, err := joinMediaFlight(key)
	if err != nil {
		_ = file.Close()
		return MediaRow{}, err
	}
	if !run {
		_ = file.Close()
		return waitMediaFlight(flight, deadline)
	}
	row, err := scanForMedia(file, now, index, sessionID, id, deadline)
	_ = file.Close()
	finishMediaFlight(key, flight, row, err)
	if err != nil {
		return MediaRow{}, err
	}
	return cloneMediaRow(row), nil
}

// scanForMedia is the full-index lookup. Every record of the session it passes
// on the way is left in the cache: the records newer than the one asked for are
// the likeliest next questions.
func scanForMedia(file *os.File, now mediaCursorBody, index, sessionID, id string, deadline time.Time) (MediaRow, error) {
	mediaIndexScans.Add(1)
	active := mediaScansActive.Add(1)
	defer mediaScansActive.Add(-1)
	for peak := mediaScansPeak.Load(); active > peak && !mediaScansPeak.CompareAndSwap(peak, active); peak = mediaScansPeak.Load() {
	}
	needle := mediaNeedle(sessionID)
	var found MediaRow
	hit := false
	boundary, err := scanBackward(file, now.End, math.MaxInt64, deadline, func(start int64, line []byte) bool {
		if !bytes.Contains(line, needle) {
			return true
		}
		var row Artifact
		if json.Unmarshal(line, &row) != nil || row.Session != sessionID ||
			strings.TrimSpace(row.Path) == "" || strings.TrimSpace(row.Title) == "" {
			return true
		}
		media, ok := mediaRowOf(row)
		if !ok {
			return true
		}
		media.Start, media.Len = start, len(line)
		cacheMediaRow(index, mediaCacheKey{dev: now.Dev, ino: now.Ino}, media, line)
		if media.Fact.ID == id {
			found, hit = media, true
			return false
		}
		return true
	})
	switch {
	case err != nil:
		return MediaRow{}, err
	case hit:
		return found, nil
	case boundary > 0:
		return MediaRow{}, ErrMediaBusy
	}
	return MediaRow{}, ErrMediaNotFound
}

// MediaLive is a source checked against the disk now.
type MediaLive struct {
	State string
	// Version is the file's identity right now ("file:" form), "" when it
	// cannot be read.
	Version string
	// Bytes is the file's size now, valid when HasBytes.
	Bytes    int64
	HasBytes bool
	// Path is the resolved path to open, set only when the state allows
	// opening (available or changed).
	Path string
	// Copy says Path is a kept copy because the original is gone
	// ([ResolveDeliverable]).
	Copy bool
}

// ResolveMedia checks one source against the disk and the engine's scope law.
//
// THE LAW. The live canonical path must equal the recorded one: a link that
// has been retargeted since is not the file that was recorded. A generated or
// evidence file must also lie inside one of roots (the session's workspace and
// artifacts folder, resolved by the caller); the exact file a person attached
// is allowed wherever they put it. Nothing here follows a path the caller
// named — there is none — and nothing larger than a stat is read.
func ResolveMedia(row MediaRow, roots []string) MediaLive {
	recorded := row.Fact.Canonical
	resolved, err := filepath.EvalSymlinks(recorded)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MediaLive{State: MediaMissing}
		}
		return MediaLive{State: MediaUnreadable}
	}
	if resolved != recorded {
		return MediaLive{State: MediaOutside}
	}
	if row.Fact.Origin != MediaAttachment && !withinAny(roots, resolved) {
		return MediaLive{State: MediaOutside}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MediaLive{State: MediaMissing}
		}
		return MediaLive{State: MediaUnreadable}
	}
	if !info.Mode().IsRegular() {
		return MediaLive{State: MediaUnreadable}
	}
	ctime, dev, ino := fileIdentity(info)
	live := MediaLive{
		State:    MediaAvailable,
		Version:  fileVersion(info.Size(), info.ModTime().UnixNano(), ctime, dev, ino),
		Bytes:    info.Size(),
		HasBytes: true,
		Path:     resolved,
	}
	if recordedIdentityDiffers(row.Fact, info.Size(), info.ModTime().UnixNano(), ctime, dev, ino) {
		live.State = MediaChanged
	}
	return live
}

// recordedIdentityDiffers says the file is no longer the one that was recorded.
// Size and modification time alone are not an identity: a same-size rewrite can
// put the old mtime back (os.Chtimes, an archive extract, a tool that preserves
// times). The change time cannot be set by a caller, so it is compared whenever
// both the record and the platform have one, and the inode and device catch an
// atomic replace that also reset the times. A record that has no identity (an
// older row, or a platform that reports none) is never called changed on a
// guess.
func recordedIdentityDiffers(fact MediaFact, size, modNS, ctimeNS int64, dev, ino uint64) bool {
	if fact.ModNS == 0 {
		return false
	}
	switch {
	case size != fact.Bytes, modNS != fact.ModNS:
		return true
	case fact.CtimeNS != 0 && ctimeNS != 0 && ctimeNS != fact.CtimeNS:
		return true
	case fact.Ino != 0 && ino != 0 && (ino != fact.Ino || dev != fact.Dev):
		return true
	}
	return false
}

// withinAny says whether path is inside one of the roots (canonicalized here).
func withinAny(roots []string, path string) bool {
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		root = canonicalFile(root)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// ProbeImage reads the header of the image at an already-resolved path, at
// most mediaProbeBytes of it. It exists for a legacy row recorded before sizes
// were; an unknown format answers false.
func ProbeImage(path string) (width, height int, ok bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	head, _ := io.ReadAll(io.LimitReader(file, mediaProbeBytes))
	if mediaKindOf(sniffMedia(head)) != "image" {
		return 0, 0, false
	}
	return imageSize(head)
}

// ProbeMIME is the media content type of the head of the file at path, or "".
func ProbeMIME(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	head, _ := io.ReadAll(io.LimitReader(file, 512))
	return sniffMedia(head)
}
