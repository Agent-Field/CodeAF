package main

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ResolveMediaReference preserves the existing source ID, owner, version and
// provenance. A reference to evidence remains evidence, never a made-by claim.
func (b *appBackend) ResolveMediaReference(agent, path, expectVersion string) (appwire.MediaSource, error) {
	if !session.ValidMediaReferencePath(path) || len(expectVersion) > 256 {
		return appwire.MediaSource{}, appwire.Errorf(appwire.CodeBadParams, "the media reference needs a canonical absolute path")
	}
	owner, roots, err := b.mediaScope(agent)
	if err != nil {
		return appwire.MediaSource{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appMediaGetTime)
	defer cancel()
	row, err := session.FindMediaByPath(ctx, appMediaIndex(), owner, path, time.Now().Add(appMediaGetTime))
	if errors.Is(err, session.ErrMediaReferencePath) {
		return appwire.MediaSource{}, appwire.Errorf(appwire.CodeBadParams, "the media reference is not valid")
	}
	if err != nil {
		return appwire.MediaSource{}, appMediaError(err)
	}
	got := appMediaSource(row, session.ResolveMedia(row, roots), true)
	if expectVersion != "" && expectVersion != got.Version && expectVersion != got.LiveVersion {
		return appwire.MediaSource{}, &appwire.Error{Code: appwire.CodeConflict, Message: "the referenced file changed since you looked", Detail: appwire.MediaReferenceConflict{Reason: "changed"}}
	}
	return got, nil
}
