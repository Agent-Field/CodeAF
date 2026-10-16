package main

import (
	"context"
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
	destination := (session.Place{Dir: filepath.Dir(a.transcript), Owned: true}).Artifacts()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return appwire.Attachment{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appMediaGetTime)
	defer cancel()
	snapshot, err := session.CopyMediaReference(ctx, row, roots, version, destination)
	if err != nil {
		return appwire.Attachment{}, appwire.Errorf(appwire.CodeBadParams, "could not attach recorded image: %v", err)
	}
	ref := &appReference{copy: snapshot, source: session.MediaReference{SourceID: row.Fact.ID, Path: row.Fact.Canonical}}
	var view appwire.Attachment
	closing := false
	b.locked(func() {
		if b.closing {
			closing = true
			return
		}
		a.attachN++
		view = appwire.Attachment{ID: "at" + strconv.Itoa(a.attachN), Kind: appwire.AttachFile, Path: snapshot.Path, Title: filepath.Base(row.Fact.Canonical), Media: &appwire.AttachmentMedia{SHA256: snapshot.SHA256, Source: appWireReference(&ref.source)}}
		if a.attach == nil {
			a.attach = map[string]appAttach{}
		}
		a.attach[view.ID] = appAttach{view: view, reference: ref}
		kept = true
	})
	if closing {
		_ = snapshot.Remove()
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
		a.gate.Lock()
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
		a.gate.Unlock()
		for _, copy := range copies {
			_ = copy.Remove()
		}
	}
}

// The surface may repeat a staged reference in submission.media, but its source
// and digest always come from the engine-owned snapshot, never the request.
func appReferenceImages(kept []appAttach, media []session.SubmissionMedia) []session.SubmissionMedia {
	positions := make(map[string]int, len(media))
	for i, image := range media {
		positions[image.ID] = i
	}
	for _, attach := range kept {
		if attach.reference == nil {
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
