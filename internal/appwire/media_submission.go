package appwire

import (
	"math"
	"strings"
	"unicode/utf8"
)

// ── a message that carries pictures, and what the person marked on them ──────
//
// agents.send gains one optional field, `submission`. It names which of the
// message's staged attachments are PICTURES the model must be sent as pictures
// (every other attachment stays the words it always was), and carries what the
// person marked on them as structured facts. It never carries a path: a picture
// is named by its staged attachment id, so the engine reads only a file the
// person staged, and nothing in a message can point it at another.
//
// THE SHAPE IS CHECKED HERE, THE MEANING IS THE ENGINE'S. Counts, lengths and
// numbers are refused as bad_params before the engine hears of the message, so a
// refusal leaves every staged chip where it was. Whether a file is a picture the
// engine can carry is the engine's to say, for the same reason.

// FeatureMessageMedia is hello's word for an engine that is a [ComposedSender]:
// agents.send keeps a submission's pictures and annotation on the person's
// message.
const FeatureMessageMedia = "messages.media"

// The bounds of a submission's shape. The engine's own bounds are the same
// numbers, spelled again there because the engine is also a library.
const (
	MaxSubmissionMarks = 64
	MaxSubmissionPoint = 512
	MaxSubmissionNote  = 2000
	maxSubmissionPath  = 4096
	// MaxPictureCoordinate is the far edge of picture coordinates: 0 to this on
	// both axes whatever the picture's size in pixels.
	MaxPictureCoordinate = 100.0
)

// MediaReference says where a picture came from, as the app knew it. Every field
// is optional.
type MediaReference struct {
	SourceID string `json:"source_id,omitempty"`
	Path     string `json:"path,omitempty"`
	AtMS     *int64 `json:"at_ms,omitempty"`
}

// SubmissionMedia names one staged attachment to be sent as a picture.
type SubmissionMedia struct {
	ID     string          `json:"id"`
	Source *MediaReference `json:"source,omitempty"`
}

// Point is a position on a picture, 0 to 100 on both axes.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Shade is a painted shape: the path of the brush and its width. AreaPercent is
// a derived fact, never a replacement for the points.
type Shade struct {
	Points      []Point  `json:"points"`
	BrushWidth  float64  `json:"brush_width"`
	AreaPercent *float64 `json:"area_percent,omitempty"`
}

// AnnotationMark is one mark; its place in the list is its number. Kind is pin
// (At), box (From and To) or shade (Shade). An empty Note means "change this".
type AnnotationMark struct {
	Kind  string `json:"kind" enum:"pin,box,shade"`
	Note  string `json:"note,omitempty"`
	At    *Point `json:"at,omitempty"`
	From  *Point `json:"from,omitempty"`
	To    *Point `json:"to,omitempty"`
	Shade *Shade `json:"shade,omitempty"`
}

// Annotation is what a person marked on a picture. Original and Marked name
// attachments of the same message. MarksPath is recorded text the engine never
// opens.
type Annotation struct {
	Original         string           `json:"original"`
	Marked           string           `json:"marked,omitempty"`
	MarksPath        string           `json:"marks_path,omitempty"`
	ChangeOnlyMarked bool             `json:"change_only_marked"`
	Marks            []AnnotationMark `json:"marks"`
}

// MessageSubmission is the pictures of a message and what was marked on them.
type MessageSubmission struct {
	Media      []SubmissionMedia `json:"media"`
	Annotation *Annotation       `json:"annotation,omitempty"`
}

// AttachmentMedia is what the engine measured of a picture it was sent, on the
// attachment of the entry that carried it. Every field is omitted when unknown.
type AttachmentMedia struct {
	Mime   string          `json:"mime,omitempty"`
	Bytes  *int64          `json:"bytes,omitempty"`
	Width  *int            `json:"width,omitempty"`
	Height *int            `json:"height,omitempty"`
	SHA256 string          `json:"sha256,omitempty"`
	Role   string          `json:"role,omitempty" enum:"original,marked"`
	Source *MediaReference `json:"source,omitempty"`
}

// The delivery words, as the engine records them.
const (
	DeliveryText        = "text"
	DeliveryImage       = "image"
	DeliveryDescription = "description"
)

// TurnReceipt is what a composed send answers: the turn, the durable message id
// minted when the engine accepted the message, how its pictures reached a model,
// and — for a description — the vision model that actually looked.
type TurnReceipt struct {
	Turn      uint64
	MessageID string
	Delivery  string
	SeenBy    string
	Queued    bool
}

// ComposedSender is the optional engine half of agents.send carrying a
// submission. An engine that is not one answers such a send unknown_method
// before anything is read, and every staged attachment stays staged.
type ComposedSender interface {
	SendComposed(agent string, message AgentMessage) (TurnReceipt, error)
}

// Check is the shape of a submission against the attachments the send names. The
// zero check is a nil submission.
func (s *MessageSubmission) Check(attachments []string) error {
	if s == nil {
		return nil
	}
	if len(s.Media) == 0 {
		return Errorf(CodeBadParams, "a submission names at least one picture")
	}
	if len(s.Media) > MaxAttachments {
		return Errorf(CodeBadParams, "a message carries at most %d pictures, not %d", MaxAttachments, len(s.Media))
	}
	staged := make(map[string]bool, len(attachments))
	for _, id := range attachments {
		staged[id] = true
	}
	named := make(map[string]bool, len(s.Media))
	for _, media := range s.Media {
		if media.ID == "" || !staged[media.ID] {
			return Errorf(CodeBadParams, "picture %q is not one of the message's attachments", media.ID)
		}
		if named[media.ID] {
			return Errorf(CodeBadParams, "picture %s is named twice", media.ID)
		}
		named[media.ID] = true
		if err := checkMediaReference(media.Source); err != nil {
			return err
		}
	}
	if s.Annotation == nil {
		return nil
	}
	return checkAnnotationShape(s.Annotation, named)
}

func checkMediaReference(ref *MediaReference) error {
	if ref == nil {
		return nil
	}
	if len(ref.SourceID) > maxSubmissionPath || len(ref.Path) > maxSubmissionPath || !utf8.ValidString(ref.SourceID+ref.Path) {
		return Errorf(CodeBadParams, "a picture's source is not text of at most %d bytes", maxSubmissionPath)
	}
	if ref.AtMS != nil && *ref.AtMS < 0 {
		return Errorf(CodeBadParams, "a picture's source names a negative moment")
	}
	return nil
}

func okCoordinate(v, limit float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= limit
}

func okPoint(p *Point) bool {
	return p != nil && okCoordinate(p.X, MaxPictureCoordinate) && okCoordinate(p.Y, MaxPictureCoordinate)
}

func checkAnnotationShape(a *Annotation, named map[string]bool) error {
	if !named[a.Original] {
		return Errorf(CodeBadParams, "the annotation's original is not one of the message's pictures")
	}
	if a.Marked != "" && (!named[a.Marked] || a.Marked == a.Original) {
		return Errorf(CodeBadParams, "the annotation's marked copy is not a second picture of the message")
	}
	if len(a.MarksPath) > maxSubmissionPath || !utf8.ValidString(a.MarksPath) || strings.ContainsRune(a.MarksPath, 0) {
		return Errorf(CodeBadParams, "the annotation's marks file is not a path of at most %d bytes", maxSubmissionPath)
	}
	if len(a.Marks) == 0 || len(a.Marks) > MaxSubmissionMarks {
		return Errorf(CodeBadParams, "an annotation names 1 to %d marks, not %d", MaxSubmissionMarks, len(a.Marks))
	}
	for index, mark := range a.Marks {
		n := index + 1
		if len(mark.Note) > MaxSubmissionNote || !utf8.ValidString(mark.Note) || strings.ContainsRune(mark.Note, 0) {
			return Errorf(CodeBadParams, "mark %d's note is not text of at most %d bytes", n, MaxSubmissionNote)
		}
		switch mark.Kind {
		case "pin":
			if !okPoint(mark.At) || mark.From != nil || mark.To != nil || mark.Shade != nil {
				return Errorf(CodeBadParams, "mark %d is not a pin at a point from 0 to %.0f", n, MaxPictureCoordinate)
			}
		case "box":
			if !okPoint(mark.From) || !okPoint(mark.To) || mark.At != nil || mark.Shade != nil || !(mark.From.X < mark.To.X && mark.From.Y < mark.To.Y) {
				return Errorf(CodeBadParams, "mark %d is not a box inside 0 to %.0f with its top-left corner above and left of its bottom-right", n, MaxPictureCoordinate)
			}
		case "shade":
			shade := mark.Shade
			if shade == nil || mark.At != nil || mark.From != nil || mark.To != nil ||
				len(shade.Points) == 0 || len(shade.Points) > MaxSubmissionPoint ||
				!okCoordinate(shade.BrushWidth, MaxPictureCoordinate) || shade.BrushWidth == 0 ||
				(shade.AreaPercent != nil && !okCoordinate(*shade.AreaPercent, 100)) {
				return Errorf(CodeBadParams, "mark %d is not a shaded area of 1 to %d points and a brush from 0 to %.0f", n, MaxSubmissionPoint, MaxPictureCoordinate)
			}
			for i := range shade.Points {
				if !okPoint(&shade.Points[i]) {
					return Errorf(CodeBadParams, "mark %d has a point outside 0 to %.0f", n, MaxPictureCoordinate)
				}
			}
		default:
			return Errorf(CodeBadParams, "mark %d is a %q, and a mark is a pin, a box or a shade", n, mark.Kind)
		}
	}
	return nil
}

// refuseSubmission is a correction, a queued message or a crew message carrying
// a submission, refused rather than run without the pictures it names.
func refuseSubmission(s *MessageSubmission, name string) error {
	if s != nil {
		return Errorf(CodeBadParams, "%s does not carry pictures; send them with agents.send", name)
	}
	return nil
}
