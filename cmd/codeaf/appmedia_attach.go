package main

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

const appReferenceMax = 64
const appReferenceWorkers = 2

type appReference struct {
	copy   session.MediaReferenceCopy
	source session.MediaReference
	// ranged says the snapshot is a recorded audio or video kept for a span
	// (media.attach_range). It is an ordinary FILE attachment with typed source
	// metadata, and is never a picture: no image path may see it.
	ranged bool
	// Once delivery was attempted, a lost reply may still mean accepted. Removal
	// forgets the staging ID but conservatively retains the message's image file.
	attempted bool
}

func (r *appReference) submission(id string) session.SubmissionMedia {
	source := r.source
	return session.SubmissionMedia{ID: id, Path: r.copy.Path, ExpectedSHA256: r.copy.SHA256, Source: &source}
}

// AttachMedia creates one engine-owned image attachment from the exact recorded
// version. No path is accepted from a caller, and no model is called. Copy work
// is admitted before I/O: two active copies and 64 unsent/in-flight snapshots
// for the process, at most ten per conversation. No background worker is spawned.
func (b *appBackend) AttachMedia(agent, source, version string) (appwire.Attachment, error) {
	return b.attachReference(agent, source, appSpan{}, func(ctx context.Context, row session.MediaRow, roots []string, destination string) (session.MediaReferenceCopy, error) {
		return session.CopyMediaReference(ctx, row, roots, version, destination)
	})
}

// AttachMediaFrame keeps one frame of an exact recorded video, supplied as base64
// PNG, as an engine-owned image whose provenance names the clip, its recorded
// version and the moment. The encoded text is decoded ONLY inside the admitted
// snapshot, so a refused or busy request never allocates the decoded bytes. The
// same admission as AttachMedia applies; the bytes are validated before any file
// exists and the video's recorded version must still be live.
func (b *appBackend) AttachMediaFrame(agent, source, version string, atMS int64, pngBase64 string) (appwire.Attachment, error) {
	at := atMS
	return b.attachReference(agent, source, appSpan{at: &at}, func(ctx context.Context, row session.MediaRow, roots []string, destination string) (session.MediaReferenceCopy, error) {
		if len(pngBase64) > base64.StdEncoding.EncodedLen(session.MaxFrameBytes) {
			return session.MediaReferenceCopy{}, session.ErrMediaFrame
		}
		data, err := base64.StdEncoding.Strict().DecodeString(pngBase64)
		if err != nil {
			return session.MediaReferenceCopy{}, session.ErrMediaFrame
		}
		return session.SnapshotMediaFrame(ctx, row, roots, version, at, data, destination)
	})
}

// AttachMediaRange keeps an exact copy of a recorded audio or video as an
// engine-owned FILE attachment whose source names the recording, its verified
// version and the span. The span is provenance only: nothing is clipped,
// rendered or decoded, and the attachment is never a picture. Admission is
// attachReference's; no raw path is accepted and no model is called.
func (b *appBackend) AttachMediaRange(agent, source, version string, fromMS, toMS int64) (appwire.Attachment, error) {
	from, to := fromMS, toMS
	return b.attachReference(agent, source, appSpan{from: &from, to: &to}, func(ctx context.Context, row session.MediaRow, roots []string, destination string) (session.MediaReferenceCopy, error) {
		return session.SnapshotMediaRange(ctx, row, roots, version, from, to, destination)
	})
}

// appSpan is the part of a recorded source a snapshot is about: a moment for a
// frame, a range for audio or video, nothing for a whole picture.
type appSpan struct{ at, from, to *int64 }

func (b *appBackend) attachReference(agent, source string, span appSpan, snapshot func(context.Context, session.MediaRow, []string, string) (session.MediaReferenceCopy, error)) (appwire.Attachment, error) {
	a, err := b.find(agent)
	if err != nil {
		return appwire.Attachment{}, err
	}
	if err := b.reserveReference(a); err != nil {
		return appwire.Attachment{}, err
	}
	kept := false
	defer func() {
		b.locked(func() {
			b.referenceWorkers--
			if !kept {
				b.referenceN--
				a.referenceN--
			}
		})
	}()
	id, roots, err := b.mediaScope(agent)
	if err != nil {
		return appwire.Attachment{}, err
	}
	row, err := session.FindMedia(appMediaIndex(), id, source, time.Now().Add(appMediaGetTime))
	if err != nil {
		return appwire.Attachment{}, appMediaError(err)
	}
	// Owned session storage, never the original's directory or the workspace.
	destination := (session.Place{Dir: session.DirOf(a.transcript), Owned: true}).Artifacts()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return appwire.Attachment{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appMediaGetTime)
	defer cancel()
	snap, err := snapshot(ctx, row, roots, destination)
	if err != nil {
		return appwire.Attachment{}, appwire.Errorf(appwire.CodeBadParams, "could not attach recorded media: %v", err)
	}
	ref := &appReference{copy: snap, ranged: span.from != nil, source: session.MediaReference{SourceID: row.Fact.ID, Path: row.Fact.Canonical, Version: row.Version(), AtMS: span.at, FromMS: span.from, ToMS: span.to}}
	var view appwire.Attachment
	closing := false
	b.locked(func() {
		if b.closing {
			closing = true
			return
		}
		a.attachN++
		title := filepath.Base(row.Fact.Canonical)
		if span.at != nil || span.from != nil {
			title = filepath.Base(snap.Path)
		}
		view = appwire.Attachment{ID: "at" + strconv.Itoa(a.attachN), Kind: appwire.AttachFile, Path: snap.Path, Title: title, Media: &appwire.AttachmentMedia{SHA256: snap.SHA256, Source: appWireReference(&ref.source)}}
		if a.attach == nil {
			a.attach = map[string]appAttach{}
		}
		a.attach[view.ID] = appAttach{view: view, reference: ref}
		kept = true
	})
	if closing {
		_ = snap.Remove()
		return appwire.Attachment{}, appwire.Errorf(appwire.CodeBusy, "the engine is closing")
	}
	return view, nil
}
func (b *appBackend) reserveReference(a *appAgent) error {
	var err error
	b.locked(func() {
		if b.closing || b.referenceWorkers >= appReferenceWorkers || b.referenceN >= appReferenceMax || a.referenceN >= appwire.MaxAttachments {
			err = appwire.Errorf(appwire.CodeBusy, "reference staging is full; send or remove an attachment first")
			return
		}
		b.referenceWorkers++
		b.referenceN++
		a.referenceN++
	})
	return err
}

// Shutdown drops only definitely-unsent snapshots. An attempted delivery may
// already be durable even if its receipt was lost, so those files are retained.
func (b *appBackend) closeReferences(agents []*appAgent) {
	for _, a := range agents {
		for _, copy := range b.detachReferences(a) {
			_ = copy.Remove()
		}
	}
}

// Detach under the same gate a send holds, but release it before disk cleanup.
// Attempted copies are forgotten here and retained on disk exactly as before.
func (b *appBackend) detachReferences(a *appAgent) []session.MediaReferenceCopy {
	a.gate.Lock()
	defer a.gate.Unlock()
	var copies []session.MediaReferenceCopy
	b.locked(func() {
		for id, attach := range a.attach {
			if attach.reference == nil {
				continue
			}
			if !attach.reference.attempted {
				copies = append(copies, attach.reference.copy)
			}
			delete(a.attach, id)
			a.referenceN--
			b.referenceN--
		}
	})
	return copies
}

// The surface may repeat a staged reference in submission.media, but its source
// and digest always come from the engine-owned snapshot, never the request.
func appReferenceImages(kept []appAttach, media []session.SubmissionMedia) []session.SubmissionMedia {
	positions := make(map[string]int, len(media))
	for i, image := range media {
		positions[image.ID] = i
	}
	for _, attach := range kept {
		// A recorded audio or video range is a file, never a picture.
		if attach.reference == nil || attach.reference.ranged {
			continue
		}
		image := attach.reference.submission(attach.view.ID)
		if i, exists := positions[image.ID]; exists {
			media[i] = image
		} else {
			positions[image.ID] = len(media)
			media = append(media, image)
		}
	}
	return media
}

func (b *appBackend) referenceAttempt(kept []appAttach) []*appReference {
	var fresh []*appReference
	b.locked(func() {
		for _, attach := range kept {
			if ref := attach.reference; ref != nil && !ref.attempted {
				ref.attempted = true
				fresh = append(fresh, ref)
			}
		}
	})
	return fresh
}
func (b *appBackend) referenceRefused(fresh []*appReference, err error) {
	var wire *appwire.Error
	if !errors.As(err, &wire) {
		return
	}
	switch wire.Code {
	case appwire.CodeBadParams, appwire.CodeUnknownMethod, appwire.CodeBusy, appwire.CodeNotFound:
		// These are explicit pre-admission refusals. Never clear an uncertainty
		// recorded by an earlier attempt with the same attachment.
		b.locked(func() {
			for _, ref := range fresh {
				ref.attempted = false
			}
		})
	}
}
