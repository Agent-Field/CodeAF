package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── a message with pictures, from the app to the engine and back ────────────
//
// The app stages files as attachments and, on send, names which of them are
// pictures and what was marked on them (appwire's MessageSubmission). This file
// turns that into the engine's one composed submission and the engine's answer
// back into the facts an entry shows. It holds no policy of its own: a path is
// resolved only from an attachment the person staged, and every bound belongs to
// the engine.

// appComposer is a conversation that takes a message carrying pictures. Only a
// conversation whose engine said so is one; the rest are refused before the
// turn starts, never sent the words alone.
type appComposer interface {
	ComposedKnown() bool
	SubmitComposed(ctx context.Context, message session.Submission) (session.SubmissionReceipt, <-chan session.Event, error)
}

var (
	_ appwire.ComposedSender = (*appBackend)(nil)
	_ appComposer            = (*remote.Agent)(nil)
)

// appComposed is the pictures and annotation of one message as the engine is
// asked to take them, and — once the turn has started — what it answered.
type appComposed struct {
	media      []session.SubmissionMedia
	annotation *session.Annotation
	receipt    *session.SubmissionReceipt
}

// SendComposed is agents.send for a message that carries pictures. It is
// [appBackend.sendMessage] with the submission attached, and it answers what the
// engine said about the message it kept.
func (b *appBackend) SendComposed(agent string, message appwire.AgentMessage) (appwire.TurnReceipt, error) {
	if message.Submission == nil {
		turn, _, err := b.sendMessage(agent, message)
		return appwire.TurnReceipt{Turn: turn}, err
	}
	turn, shown, err := b.sendMessage(agent, message)
	if err != nil {
		return appwire.TurnReceipt{}, err
	}
	receipt := appwire.TurnReceipt{Turn: turn}
	if shown != nil && shown.composed != nil && shown.composed.receipt != nil {
		got := shown.composed.receipt
		receipt.MessageID, receipt.Delivery, receipt.SeenBy, receipt.Queued = got.MessageID, got.Delivery, got.SeenBy, got.Queued
	}
	return receipt, nil
}

// appComposedOf resolves a send's submission against the attachments the person
// staged. A picture is named by its attachment id and found in kept, so the
// engine is never handed a path the person did not stage. The whole submission
// is checked here, before the conversation is opened or a turn is asked for, so
// a refusal leaves every chip staged.
func appComposedOf(workspace string, kept []appAttach, given *appwire.MessageSubmission) (*appComposed, error) {
	explicit := given != nil
	if given == nil {
		given = &appwire.MessageSubmission{}
	}
	staged := make(map[string]appAttach, len(kept))
	for _, attach := range kept {
		staged[attach.view.ID] = attach
	}
	composed := &appComposed{media: make([]session.SubmissionMedia, 0, len(given.Media))}
	for _, media := range given.Media {
		attach, ok := staged[media.ID]
		if !ok {
			return nil, appwire.Errorf(appwire.CodeBadParams, "picture %q is not one of the message's attachments", media.ID)
		}
		if attach.view.Kind != appwire.AttachFile {
			return nil, appwire.Errorf(appwire.CodeBadParams, "%s is not a file, and only a file can be sent as a picture", media.ID)
		}
		composed.media = append(composed.media, session.SubmissionMedia{
			ID: media.ID, Path: appAttachFull(workspace, attach.view.Path), Source: appSessionReference(media.Source),
		})
	}
	composed.media = appReferenceImages(kept, composed.media)
	if !explicit && len(composed.media) == 0 && given.Annotation == nil {
		return nil, nil
	}
	composed.annotation = appSessionAnnotation(given.Annotation)
	if err := (session.Submission{Media: composed.media, Annotation: composed.annotation}).Check(); err != nil {
		return nil, appEngineError(err)
	}
	return composed, nil
}

func appSessionReference(ref *appwire.MediaReference) *session.MediaReference {
	if ref == nil {
		return nil
	}
	out := session.MediaReference{SourceID: ref.SourceID, Path: ref.Path, Version: ref.Version}
	if ref.AtMS != nil {
		at := *ref.AtMS
		out.AtMS = &at
	}
	return &out
}

func appWireReference(ref *session.MediaReference) *appwire.MediaReference {
	if ref == nil {
		return nil
	}
	out := appwire.MediaReference{SourceID: ref.SourceID, Path: ref.Path, Version: ref.Version}
	if ref.AtMS != nil {
		at := *ref.AtMS
		out.AtMS = &at
	}
	return &out
}

func appSessionPoint(p *appwire.Point) *session.Point {
	if p == nil {
		return nil
	}
	return &session.Point{X: p.X, Y: p.Y}
}

func appWirePoint(p *session.Point) *appwire.Point {
	if p == nil {
		return nil
	}
	return &appwire.Point{X: p.X, Y: p.Y}
}

// appSessionAnnotation is an annotation as the engine takes it.
func appSessionAnnotation(given *appwire.Annotation) *session.Annotation {
	if given == nil {
		return nil
	}
	out := &session.Annotation{
		Original: given.Original, Marked: given.Marked, MarksPath: given.MarksPath, ChangeOnlyMarked: given.ChangeOnlyMarked,
		Marks: make([]session.AnnotationMark, 0, len(given.Marks)),
	}
	for _, mark := range given.Marks {
		kept := session.AnnotationMark{Kind: mark.Kind, Note: mark.Note, At: appSessionPoint(mark.At), From: appSessionPoint(mark.From), To: appSessionPoint(mark.To)}
		if mark.Shade != nil {
			shade := session.Shade{BrushWidth: mark.Shade.BrushWidth, Points: make([]session.Point, 0, len(mark.Shade.Points))}
			for _, p := range mark.Shade.Points {
				shade.Points = append(shade.Points, session.Point{X: p.X, Y: p.Y})
			}
			if mark.Shade.AreaPercent != nil {
				area := *mark.Shade.AreaPercent
				shade.AreaPercent = &area
			}
			kept.Shade = &shade
		}
		out.Marks = append(out.Marks, kept)
	}
	return out
}

// appWireAnnotation is a recorded annotation as an entry shows it.
func appWireAnnotation(given *session.Annotation) *appwire.Annotation {
	if given == nil {
		return nil
	}
	out := &appwire.Annotation{
		Original: given.Original, Marked: given.Marked, MarksPath: given.MarksPath, ChangeOnlyMarked: given.ChangeOnlyMarked,
		Marks: make([]appwire.AnnotationMark, 0, len(given.Marks)),
	}
	for _, mark := range given.Marks {
		kept := appwire.AnnotationMark{Kind: mark.Kind, Note: mark.Note, At: appWirePoint(mark.At), From: appWirePoint(mark.From), To: appWirePoint(mark.To)}
		if mark.Shade != nil {
			shade := appwire.Shade{BrushWidth: mark.Shade.BrushWidth, Points: make([]appwire.Point, 0, len(mark.Shade.Points))}
			for _, p := range mark.Shade.Points {
				shade.Points = append(shade.Points, appwire.Point{X: p.X, Y: p.Y})
			}
			if mark.Shade.AreaPercent != nil {
				area := *mark.Shade.AreaPercent
				shade.AreaPercent = &area
			}
			kept.Shade = &shade
		}
		out.Marks = append(out.Marks, kept)
	}
	return out
}

// appAttachmentMedia is what the engine measured of one picture, as an
// attachment shows it. Unknown stays absent.
func appAttachmentMedia(media session.MessageMedia) *appwire.AttachmentMedia {
	out := &appwire.AttachmentMedia{Mime: media.MIME, SHA256: media.SHA256, Role: media.Role, Source: appWireReference(media.Source)}
	bytes := media.Bytes
	out.Bytes = &bytes
	if media.Width > 0 && media.Height > 0 {
		width, height := media.Width, media.Height
		out.Width, out.Height = &width, &height
	}
	return out
}

// appWithMedia is attachments with the engine's measurements put on the ones
// that went as pictures. The views are copied: the slice belongs to a message
// that may be shown again.
func appWithMedia(views []appwire.Attachment, media []session.MessageMedia) []appwire.Attachment {
	if len(media) == 0 || len(views) == 0 {
		return views
	}
	out := append([]appwire.Attachment(nil), views...)
	for index := range out {
		for _, m := range media {
			if m.ID == out[index].ID {
				out[index].Media = appAttachmentMedia(m)
			}
		}
	}
	return out
}

// appComposedFields puts a message's engine-kept facts on its entry: the
// durable id, how its pictures reached a model, the vision model that looked
// when it was not the talking one, and what was marked.
func appComposedFields(entry *appwire.Entry, mark *session.ComposedMark) {
	if mark == nil {
		return
	}
	entry.MessageID = mark.MessageID
	if mark.Delivery != nil {
		entry.Delivery = mark.Delivery.Mode
		if mark.Delivery.Mode == session.DeliveryDescription {
			entry.SeenBy = mark.Delivery.Model
		}
	}
	entry.Annotation = appWireAnnotation(mark.Annotation)
	entry.Attachments = appWithMedia(entry.Attachments, mark.Media)
}
