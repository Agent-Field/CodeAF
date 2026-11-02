package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // registered for the header-only dimension probe
	_ "image/jpeg" // registered for the header-only dimension probe
	_ "image/png"  // registered for the header-only dimension probe
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"unsafe"

	_ "golang.org/x/image/webp" // registered for the header-only dimension probe
)

// ── one message: words, pictures and what the person marked on them ─────────
//
// A person who marks up a picture and sends it back sends ONE message. The
// words, the original, the marked copy and the marks are not four things the
// surface sequences; they are the message, and the record has to be able to say
// so. Before this file the two halves had separate doors — SubmitMessage kept a
// description beside text, SubmitImage sent parts with none — so "a picture
// with a note on it" had no way to be both, and an app that showed a chip
// handed the engine a path in a sentence.
//
// THE ENGINE KEEPS WHAT THE DOOR SAID, TYPED. Marks are coordinates and notes
// the editor produced, validated here and recorded on the message's own line;
// NOTHING IS EVER PARSED OUT OF THE WORDS, and nothing in the words changes what
// is recorded. The model reads Text and the pictures; the mark is for the record
// and for whoever draws it.

// MaxSubmissionMedia is how many pictures one message carries. It is the same
// ten an app message may name attachments, so a full tray is never refused here.
const MaxSubmissionMedia = 10

// The bounds of an annotation. Like the rest of a message's mark it is written
// on every line that carries one and read on every page that shows it, so it is
// small and fixed, and a bound is checked on the bytes as given.
const (
	// MaxAnnotationMarks is how many marks one annotation names.
	MaxAnnotationMarks = 64
	// MaxShadePoints is how many points one painted shape may be made of.
	MaxShadePoints = 512
	// MaxMarkNote is the longest one note may be, in bytes.
	MaxMarkNote = 2000
	// maxAnnotationBytes is every string of one annotation together.
	maxAnnotationBytes = 64 << 10
	// MaxPictureCoordinate is the far edge of picture coordinates: they run
	// from 0 to this on both axes whatever the picture's pixel size.
	MaxPictureCoordinate = 100.0
)

// Delivery words: how the pictures reached the model, as the record says it.
const (
	// DeliveryText is a message that carried no picture.
	DeliveryText = "text"
	// DeliveryImage is a message whose pictures the model talking to the person
	// received as image parts.
	DeliveryImage = "image"
	// DeliveryDescription is a message whose pictures a VISION MODEL looked at
	// because the talking model cannot: the talking model read that model's
	// words, not the pictures.
	DeliveryDescription = "description"
)

// Roles a picture can have in an annotated message.
const (
	MediaRoleOriginal = "original"
	MediaRoleMarked   = "marked"
)

// MediaReference says where a picture came from, as the door knew it: the id the
// engine's own index gave the source, its path, and the moment in a clip a frame
// was taken at. Every field is optional; an empty one is "the door did not say".
// Version is the source's canonical recorded version the engine verified when it
// kept the picture; it travels with the message so the record can say WHICH
// version of the source a picture came from, not only which source.
type MediaReference struct {
	SourceID string `json:"sourceId,omitempty"`
	Path     string `json:"path,omitempty"`
	Version  string `json:"version,omitempty"`
	AtMS     *int64 `json:"atMs,omitempty"`
	// FromMS and ToMS are the span of a recorded audio or video a file attachment
	// was kept for ([SnapshotMediaRange]). They are both present or both absent,
	// 0 <= FromMS < ToMS, and never beside AtMS. A picture's source has neither.
	FromMS *int64 `json:"fromMs,omitempty"`
	ToMS   *int64 `json:"toMs,omitempty"`
}

// cloneSource is a source with every pointer its own.
func cloneMediaReference(source *MediaReference) *MediaReference {
	if source == nil {
		return nil
	}
	copied := *source
	for _, field := range []**int64{&copied.AtMS, &copied.FromMS, &copied.ToMS} {
		if *field != nil {
			value := **field
			*field = &value
		}
	}
	return &copied
}

// sourceSizes is what a source costs a record that holds it.
func sourceSizes(source *MediaReference) int64 {
	if source == nil {
		return 0
	}
	cost := int64(unsafe.Sizeof(*source)) + int64(len(source.SourceID)+len(source.Path)+len(source.Version))
	for _, field := range []*int64{source.AtMS, source.FromMS, source.ToMS} {
		if field != nil {
			cost += int64(unsafe.Sizeof(*field))
		}
	}
	return cost
}

// SubmissionMedia is one picture riding on a message. ID is the door's own name
// for it, unique within the message — an app's staged attachment id — and is
// what an [Annotation] names. Path is read by the engine, once, at the moment
// the message is accepted.
type SubmissionMedia struct {
	// ExpectedSHA256 binds an engine-owned snapshot to the exact bytes staged.
	ExpectedSHA256 string          `json:"expected_sha256,omitempty"`
	ID             string          `json:"id"`
	Path           string          `json:"path"`
	Source         *MediaReference `json:"source,omitempty"`
}

// Point is a position on a picture in picture coordinates, 0 to 100 on both axes.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Shade is an arbitrary painted shape: the path of the brush and how wide it
// was. AreaPercent, where the door computed it, is a derived fact — the share of
// the picture the shape covers — and never a replacement for the points.
type Shade struct {
	Points      []Point  `json:"points"`
	BrushWidth  float64  `json:"brushWidth"`
	AreaPercent *float64 `json:"areaPercent,omitempty"`
}

// AnnotationMark is one mark. Its place in [Annotation.Marks] is its number.
// Kind is "pin" (At), "box" (From and To, the top-left and bottom-right
// corners) or "shade" (Shade). An empty Note means "change this".
type AnnotationMark struct {
	Kind  string `json:"kind"`
	Note  string `json:"note,omitempty"`
	At    *Point `json:"at,omitempty"`
	From  *Point `json:"from,omitempty"`
	To    *Point `json:"to,omitempty"`
	Shade *Shade `json:"shade,omitempty"`
}

// Annotation is what a person marked on a picture, as structured facts.
// Original and Marked name [SubmissionMedia.ID]s: the picture as it was, and the
// copy with the marks drawn on it. MarksPath is where the marks were also
// written as a file; it is recorded text and the engine never opens it.
type Annotation struct {
	Original         string           `json:"original"`
	Marked           string           `json:"marked,omitempty"`
	MarksPath        string           `json:"marksPath,omitempty"`
	ChangeOnlyMarked bool             `json:"changeOnlyMarked"`
	Marks            []AnnotationMark `json:"marks"`
}

// MessageMedia is one picture of a message as the record keeps it: facts the
// engine measured when it read the file, never the bytes.
type MessageMedia struct {
	ID     string          `json:"id"`
	Path   string          `json:"path"`
	MIME   string          `json:"mime"`
	Bytes  int64           `json:"bytes"`
	SHA256 string          `json:"sha256"`
	Width  int             `json:"width,omitempty"`
	Height int             `json:"height,omitempty"`
	Role   string          `json:"role,omitempty"`
	Source *MediaReference `json:"source,omitempty"`
}

// MessageDelivery is how the pictures reached a model, as it happened. For
// DeliveryDescription Model is the vision model that actually looked.
type MessageDelivery struct {
	Mode  string `json:"mode"`
	Model string `json:"model,omitempty"`
}

// ComposedMark is what a composed submission adds to the person's mark. It is
// one pointer on the mark because nearly every message has none.
type ComposedMark struct {
	// MessageID is the message's durable identity, minted when the engine
	// accepted it.
	MessageID  string           `json:"messageId"`
	Media      []MessageMedia   `json:"media,omitempty"`
	Annotation *Annotation      `json:"annotation,omitempty"`
	Delivery   *MessageDelivery `json:"delivery,omitempty"`
}

// SubmissionReceipt is the engine's answer to an accepted composed submission. Queued says
// a turn was running and the message joined it at the next step boundary.
type SubmissionReceipt struct {
	MessageID string `json:"messageId"`
	Delivery  string `json:"delivery"`
	SeenBy    string `json:"seenBy,omitempty"`
	Queued    bool   `json:"queued,omitempty"`
	// Media are the pictures as the engine measured them and recorded them.
	Media []MessageMedia `json:"media,omitempty"`
}

// ── checking ─────────────────────────────────────────────────────────────────

// Check is the pure, filesystem-free half of validating a submission: every
// bound and every shape, answered as an error wrapping [ErrMessageMark] so a door
// can tell a description it got wrong from a session that would not take a turn.
// [Agent.SubmitComposed] runs it first, so a refusal here leaves nothing behind.
func (m Submission) Check() error {
	if _, err := m.mark(); err != nil {
		return err
	}
	return m.checkComposed()
}

func (m Submission) checkComposed() error {
	if len(m.Media) > MaxSubmissionMedia {
		return markError("%d pictures, and one message carries at most %d", len(m.Media), MaxSubmissionMedia)
	}
	ids := make(map[string]bool, len(m.Media))
	for index, media := range m.Media {
		n := index + 1
		if media.ID == "" || len(media.ID) > maxAttachmentField || !utf8.ValidString(media.ID) {
			return markError("picture %d has no id of at most %d bytes", n, maxAttachmentField)
		}
		if ids[media.ID] {
			return markError("two pictures are both called %s", media.ID)
		}
		ids[media.ID] = true
		if err := checkPath(media.Path); err != nil || strings.TrimSpace(media.Path) == "" {
			return markError("picture %d has no path of at most %d bytes", n, maxAttachmentField)
		}
		if media.ExpectedSHA256 != "" {
			decoded, err := hex.DecodeString(media.ExpectedSHA256)
			if err != nil || len(decoded) != sha256.Size {
				return markError("picture %d has an invalid expected image digest", n)
			}
		}
		if err := checkReference(media.Source); err != nil {
			return markError("picture %d's source %v", n, err)
		}
	}
	if m.Annotation == nil {
		return nil
	}
	return checkAnnotation(m.Annotation, ids)
}

func markError(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrMessageMark}, args...)...)
}

func checkPath(path string) error {
	if len(path) > maxAttachmentField || !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
		return fmt.Errorf("is not a path of at most %d bytes", maxAttachmentField)
	}
	return nil
}

func checkReference(ref *MediaReference) error {
	if ref == nil {
		return nil
	}
	if len(ref.SourceID) > maxAttachmentField || !utf8.ValidString(ref.SourceID) {
		return fmt.Errorf("has an id that is not text of at most %d bytes", maxAttachmentField)
	}
	if err := checkPath(ref.Path); err != nil {
		return err
	}
	if len(ref.Version) > maxAttachmentField || !utf8.ValidString(ref.Version) || strings.ContainsRune(ref.Version, 0) {
		return fmt.Errorf("has a version that is not text of at most %d bytes", maxAttachmentField)
	}
	if ref.AtMS != nil && *ref.AtMS < 0 {
		return errors.New("has a negative moment")
	}
	if ref.FromMS != nil || ref.ToMS != nil {
		return errors.New("names a time range, and only a recorded audio or video attachment has one")
	}
	return nil
}

// checkRangeSource is the source of a file attachment kept from a span of a
// recorded audio or video: the id and verified version it was kept from, and
// the span, with nothing a picture's source says.
func checkRangeSource(ref *MediaReference) error {
	if err := checkReference(&MediaReference{SourceID: ref.SourceID, Path: ref.Path, Version: ref.Version, AtMS: ref.AtMS}); err != nil {
		return err
	}
	if ref.SourceID == "" || ref.Version == "" {
		return errors.New("names no recorded source and version")
	}
	if ref.AtMS != nil {
		return errors.New("names a moment as well as a range")
	}
	if ref.FromMS == nil || ref.ToMS == nil || *ref.FromMS < 0 || *ref.ToMS <= *ref.FromMS || *ref.ToMS > MaxMediaRangeMS {
		return fmt.Errorf("does not name a range of 0 <= from < to <= %d ms", MaxMediaRangeMS)
	}
	return nil
}

func checkPoint(p *Point) bool {
	return p != nil && finiteWithin(p.X, MaxPictureCoordinate) && finiteWithin(p.Y, MaxPictureCoordinate)
}

// finiteWithin is whether v is a number from 0 to limit: NaN and the infinities
// fail every comparison a bound could make, so they are named here.
func finiteWithin(v, limit float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= limit
}

func checkAnnotation(note *Annotation, media map[string]bool) error {
	if note.Original == "" || !media[note.Original] {
		return markError("the annotation's original is not one of the message's pictures")
	}
	if note.Marked != "" {
		if !media[note.Marked] {
			return markError("the annotation's marked copy is not one of the message's pictures")
		}
		if note.Marked == note.Original {
			return markError("the marked copy and the original are the same picture")
		}
	}
	if err := checkPath(note.MarksPath); err != nil {
		return markError("the annotation's marks file %v", err)
	}
	if len(note.Marks) == 0 || len(note.Marks) > MaxAnnotationMarks {
		return markError("%d marks, and an annotation names 1 to %d", len(note.Marks), MaxAnnotationMarks)
	}
	total := len(note.MarksPath)
	for index, mark := range note.Marks {
		n := index + 1
		if len(mark.Note) > MaxMarkNote || !utf8.ValidString(mark.Note) || strings.ContainsRune(mark.Note, 0) {
			return markError("mark %d's note is not text of at most %d bytes", n, MaxMarkNote)
		}
		total += len(mark.Note)
		switch mark.Kind {
		case "pin":
			if !checkPoint(mark.At) || mark.From != nil || mark.To != nil || mark.Shade != nil {
				return markError("mark %d is not a pin at a point from 0 to %.0f", n, MaxPictureCoordinate)
			}
		case "box":
			if !checkPoint(mark.From) || !checkPoint(mark.To) || mark.At != nil || mark.Shade != nil ||
				!(mark.From.X < mark.To.X && mark.From.Y < mark.To.Y) {
				return markError("mark %d is not a box with its top-left corner above and left of its bottom-right, inside 0 to %.0f", n, MaxPictureCoordinate)
			}
		case "shade":
			shade := mark.Shade
			if shade == nil || mark.At != nil || mark.From != nil || mark.To != nil {
				return markError("mark %d is not a shaded area", n)
			}
			if len(shade.Points) == 0 || len(shade.Points) > MaxShadePoints {
				return markError("mark %d's shaded area has %d points, and one has 1 to %d", n, len(shade.Points), MaxShadePoints)
			}
			for _, point := range shade.Points {
				if !checkPoint(&point) {
					return markError("mark %d's shaded area has a point outside 0 to %.0f", n, MaxPictureCoordinate)
				}
			}
			if !finiteWithin(shade.BrushWidth, MaxPictureCoordinate) || shade.BrushWidth == 0 {
				return markError("mark %d's brush is not wider than 0 and at most %.0f", n, MaxPictureCoordinate)
			}
			if shade.AreaPercent != nil && !finiteWithin(*shade.AreaPercent, 100) {
				return markError("mark %d's area is not a share from 0 to 100", n)
			}
		default:
			return markError("mark %d is a %q, and a mark is a pin, a box or a shade", n, mark.Kind)
		}
	}
	if total > maxAnnotationBytes {
		return markError("the annotation's text comes to more than %d bytes", maxAnnotationBytes)
	}
	return nil
}

// keptAnnotation is the record's own deep copy of a checked annotation.
func keptAnnotation(given *Annotation) *Annotation {
	if given == nil {
		return nil
	}
	kept := *given
	kept.Original, kept.Marked, kept.MarksPath = strings.Clone(given.Original), strings.Clone(given.Marked), strings.Clone(given.MarksPath)
	kept.Marks = make([]AnnotationMark, len(given.Marks))
	for index, mark := range given.Marks {
		copied := AnnotationMark{Kind: mark.Kind, Note: strings.Clone(mark.Note)}
		if mark.At != nil {
			at := *mark.At
			copied.At = &at
		}
		if mark.From != nil {
			from := *mark.From
			copied.From = &from
		}
		if mark.To != nil {
			to := *mark.To
			copied.To = &to
		}
		if mark.Shade != nil {
			shade := *mark.Shade
			shade.Points = append([]Point(nil), mark.Shade.Points...)
			if shade.AreaPercent != nil {
				area := *shade.AreaPercent
				shade.AreaPercent = &area
			}
			copied.Shade = &shade
		}
		kept.Marks[index] = copied
	}
	return &kept
}

func cloneMedia(media []MessageMedia) []MessageMedia {
	if len(media) == 0 {
		return nil
	}
	out := append([]MessageMedia(nil), media...)
	for index := range out {
		out[index].Source = cloneMediaReference(out[index].Source)
	}
	return out
}

// cloneComposed is a reading's own copy of a composed mark, sharing nothing
// mutable with the index behind it.
func cloneComposed(mark *ComposedMark) *ComposedMark {
	if mark == nil {
		return nil
	}
	out := *mark
	out.Media = cloneMedia(mark.Media)
	out.Annotation = keptAnnotation(mark.Annotation)
	if mark.Delivery != nil {
		delivery := *mark.Delivery
		out.Delivery = &delivery
	}
	return &out
}

// composedSizes is what a composed mark costs a record that holds it.
func composedSizes(mark *ComposedMark) int64 {
	if mark == nil {
		return 0
	}
	cost := int64(unsafe.Sizeof(*mark)) + int64(len(mark.MessageID))
	cost += int64(cap(mark.Media)) * int64(unsafe.Sizeof(MessageMedia{}))
	for _, media := range mark.Media {
		cost += int64(len(media.ID) + len(media.Path) + len(media.MIME) + len(media.SHA256) + len(media.Role))
		cost += sourceSizes(media.Source)
	}
	if note := mark.Annotation; note != nil {
		cost += int64(unsafe.Sizeof(*note)) + int64(len(note.Original)+len(note.Marked)+len(note.MarksPath))
		cost += int64(cap(note.Marks)) * int64(unsafe.Sizeof(AnnotationMark{}))
		for _, m := range note.Marks {
			cost += int64(len(m.Kind) + len(m.Note))
			for _, point := range []*Point{m.At, m.From, m.To} {
				if point != nil {
					cost += int64(unsafe.Sizeof(*point))
				}
			}
			if shade := m.Shade; shade != nil {
				cost += int64(unsafe.Sizeof(*shade)) + int64(cap(shade.Points))*int64(unsafe.Sizeof(Point{}))
				if shade.AreaPercent != nil {
					cost += int64(unsafe.Sizeof(*shade.AreaPercent))
				}
			}
		}
	}
	if delivery := mark.Delivery; delivery != nil {
		cost += int64(unsafe.Sizeof(*delivery)) + int64(len(delivery.Mode)+len(delivery.Model))
	}
	return cost
}

// ── reading the pictures ─────────────────────────────────────────────────────

// newMessageID mints a message's durable identity.
func newMessageID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("session: could not mint a message id: %w", err)
	}
	return "msg_" + hex.EncodeToString(raw[:]), nil
}

// sniffedImageTypes is what the bytes can say they are. The extension and any
// word a caller gave are not consulted: a path is a name, and a name is what a
// person or a link can make say anything.
var sniffedImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

// readComposedImages reads the message's pictures, refusing before it allocates
// for anything it will refuse. Every file is stat'ed first — a regular file, not
// empty, not over the per-image limit — and the sizes are summed against the
// per-message limit before one byte of any is read, so a refused message costs
// the box a handful of syscalls and not the file.
func readComposedImages(ctx context.Context, media []SubmissionMedia, roles map[string]string) ([]Image, []MessageMedia, error) {
	sizes := make([]int64, len(media))
	var total int64
	for index, item := range media {
		path := strings.TrimSpace(item.Path)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, nil, markError("%s is not a file this message can carry", filepath.Base(path))
		}
		if info.Size() == 0 {
			return nil, nil, markError("%s is empty", filepath.Base(path))
		}
		if info.Size() > maxImageBytes {
			return nil, nil, markError("%s is over the %s image limit", filepath.ToSlash(path), byteLimit(maxImageBytes))
		}
		sizes[index] = info.Size()
		total += info.Size()
		if total > maxMessageImageBytes {
			return nil, nil, markError("these pictures total more than the %s a single message may carry — send them across a few messages", byteLimit(maxMessageImageBytes))
		}
	}
	images := make([]Image, 0, len(media))
	kept := make([]MessageMedia, 0, len(media))
	for index, item := range media {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		path := strings.TrimSpace(item.Path)
		data, err := readSubmissionImage(path, sizes[index])
		if err != nil {
			return nil, nil, markError("could not read %s", filepath.ToSlash(path))
		}
		mediaType := http.DetectContentType(data[:min(len(data), 512)])
		if !sniffedImageTypes[mediaType] {
			return nil, nil, markError("%s is not an image this surface can send — png, jpeg, webp and gif are", filepath.Base(path))
		}
		sum := sha256.Sum256(data)
		if item.ExpectedSHA256 != "" && hex.EncodeToString(sum[:]) != item.ExpectedSHA256 {
			return nil, nil, markError("the staged reference image changed; attach it again")
		}
		fact := MessageMedia{
			ID: strings.Clone(item.ID), Path: strings.Clone(path), MIME: mediaType, Bytes: int64(len(data)),
			SHA256: hex.EncodeToString(sum[:]), Role: roles[item.ID],
		}
		if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && config.Width > 0 && config.Height > 0 {
			fact.Width, fact.Height = config.Width, config.Height
		}
		fact.Source = cloneMediaReference(item.Source)
		images = append(images, Image{Path: path, MIME: mediaType, Bytes: data})
		kept = append(kept, fact)
	}
	return images, kept, nil
}

// readSubmissionImage reads a file the caller already stat'ed into ONE buffer of exactly
// the size it was told, reading one byte past it to learn the file grew. A reader
// that appended its way up would allocate several times the picture to hold it.
func readSubmissionImage(path string, size int64) ([]byte, error) {
	if size < 0 || size > maxImageBytes {
		return nil, errors.New("outside the image limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, size+1)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if int64(n) != size {
		return nil, errors.New("changed size while being read")
	}
	return buf[:n], nil
}

// imageDataURL is a picture as the content part carries it. The encoder writes into
// the builder it grew once, so the string is the only copy beside the file's own
// bytes: concatenating a prefix onto a separately encoded string would hold the
// picture three times at its peak.
func imageDataURL(mediaType string, data []byte) string {
	prefix := "data:" + mediaType + ";base64,"
	var out strings.Builder
	out.Grow(len(prefix) + base64.StdEncoding.EncodedLen(len(data)))
	out.WriteString(prefix)
	encoder := base64.NewEncoder(base64.StdEncoding, &out)
	_, _ = encoder.Write(data)
	_ = encoder.Close()
	return out.String()
}

// ── the door ─────────────────────────────────────────────────────────────────

// SubmitComposed is [Agent.SubmitMessage] for a message that carries pictures and
// what the person marked on them: ONE user message, in the record once, with its
// words, its descriptors, its real image parts and its annotation together, and a
// durable id minted when it was accepted.
//
// IT REFUSES BEFORE IT RECORDS OR READS, in this order: a cancelled context, a
// description that does not fit, a spend rail that would refuse the turn, a
// picture that cannot be carried. A refusal leaves nothing behind, so a door that
// keeps its attachments staged until a message was accepted keeps them on every
// one of these.
//
// A MODEL THAT CANNOT SEE DOES NOT SILENTLY GET WORDS ALONE. The pictures go to
// the vision model the surface resolves ([Agent.visionSeer]), exactly as
// [Agent.SubmitImage] sends them, and the record says so: Delivery is
// [DeliveryDescription] and names the model that looked. A surface with no
// vision model gets SubmitImage's refusal and nothing is recorded.
//
// A message with neither pictures nor annotation is a plain message that still
// gets an id.
func (a *Agent) SubmitComposed(ctx context.Context, message Submission) (SubmissionReceipt, <-chan Event, error) {
	if err := a.CheckWork(); err != nil {
		return SubmissionReceipt{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return SubmissionReceipt{}, nil, err
	}
	message.proven = a.provenCreation(message.Creation)
	if err := message.Check(); err != nil {
		return SubmissionReceipt{}, nil, err
	}
	id, err := newMessageID()
	if err != nil {
		return SubmissionReceipt{}, nil, err
	}
	if err := a.admissible(); err != nil {
		return SubmissionReceipt{}, nil, err
	}

	if len(message.Media) == 0 {
		mark, err := message.composedMark(&ComposedMark{MessageID: id, Delivery: &MessageDelivery{Mode: DeliveryText}})
		if err != nil {
			return SubmissionReceipt{}, nil, err
		}
		events, err := a.submitText(ctx, message.Text, mark)
		if err != nil {
			return SubmissionReceipt{}, nil, err
		}
		return SubmissionReceipt{MessageID: id, Delivery: DeliveryText}, events, nil
	}

	roles := map[string]string{}
	if note := message.Annotation; note != nil {
		roles[note.Original] = MediaRoleOriginal
		if note.Marked != "" {
			roles[note.Marked] = MediaRoleMarked
		}
	}
	// The model is read before the files: nothing is opened for a surface that
	// has no one to show the pictures to.
	model := a.Model()
	sees := a.config.SupportsImages != nil && a.config.SupportsImages(model)
	seer := ""
	if !sees {
		seer = a.visionSeer()
		if seer == "" || strings.EqualFold(seer, model) {
			return SubmissionReceipt{}, nil, blindRefusal(model)
		}
	}
	images, facts, err := readComposedImages(ctx, message.Media, roles)
	if err != nil {
		return SubmissionReceipt{}, nil, err
	}

	words := strings.TrimSpace(message.Text)
	said := words
	if note := a.mentionNote(words); note != "" && words != "" {
		words += "\n\n" + note
	}
	user, err := imageUserMessage(words, images)
	if err != nil {
		return SubmissionReceipt{}, nil, markError("%s", strings.TrimPrefix(err.Error(), "session: "))
	}
	if words != said {
		user.said = said
	}

	if sees {
		mark, err := message.composedMark(&ComposedMark{MessageID: id, Media: facts, Delivery: &MessageDelivery{Mode: DeliveryImage, Model: model}})
		if err != nil {
			return SubmissionReceipt{}, nil, err
		}
		user.person = mark
		if digest := a.planDigest(); digest != "" {
			user = planDigestedParts(digest, user)
		}
		events, queued, err := a.admitImageMessage(ctx, user)
		if err != nil {
			return SubmissionReceipt{}, nil, err
		}
		return SubmissionReceipt{MessageID: id, Delivery: DeliveryImage, Queued: queued, Media: cloneMedia(facts)}, events, nil
	}

	mark, err := message.composedMark(&ComposedMark{MessageID: id, Media: facts, Delivery: &MessageDelivery{Mode: DeliveryDescription, Model: seer}})
	if err != nil {
		return SubmissionReceipt{}, nil, err
	}
	events, err := a.visionTurnMarked(ctx, words, said, user, model, seer, mark)
	if err != nil {
		return SubmissionReceipt{}, nil, err
	}
	return SubmissionReceipt{MessageID: id, Delivery: DeliveryDescription, SeenBy: seer, Media: cloneMedia(facts)}, events, nil
}

// composedMark is the submission's presentation mark with the composed half
// set: the same mark [Submission.mark] makes, or a bare person's mark when the
// submission said nothing beyond its pictures.
func (m Submission) composedMark(composed *ComposedMark) (*messagePresentation, error) {
	mark, err := m.mark()
	if err != nil {
		return nil, err
	}
	if mark == nil {
		mark = &messagePresentation{Audience: audiencePerson}
	}
	composed.Annotation = keptAnnotation(m.Annotation)
	mark.Composed = composed
	return mark, nil
}

// admissible refuses, before anything is read or recorded, a message the agent
// could not take: a closed one, or — when no turn is running to steer into — one
// the spend rail would refuse. The rail's own check happens again where the turn
// starts; this one exists so a refused message is an error a door can leave its
// staged attachments alone on, rather than a stream that reports it.
func (a *Agent) admissible() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("session: agent is closed")
	}
	if a.running {
		return nil
	}
	return a.railBlockLocked()
}
