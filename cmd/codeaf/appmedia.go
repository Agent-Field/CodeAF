package main

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appMediaIndex is where the media sources are read from: the global
// deliverables index. It is a variable so a test can point it at a fixture.
var appMediaIndex = artifactsIndexPath

// Time a media call may spend reading the index before it answers busy. A
// listing also stops at its byte budget (session.ListMedia) and hands back a
// cursor; only a lookup, which must read the whole history to say not-found,
// is bounded by time alone.
const (
	appMediaListTime = 2 * time.Second
	appMediaGetTime  = 8 * time.Second
)

// mediaScope is what the engine knows of one conversation that media needs: its
// id (the folder's name), and the roots a generated file may live under.
func (b *appBackend) mediaScope(agent string) (sessionID string, roots []string, err error) {
	a, err := b.find(agent)
	if err != nil {
		return "", nil, err
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	dir := filepath.Dir(a.transcript)
	place := session.Place{Dir: dir, Owned: true}
	roots = []string{workspace, place.Work(), place.Artifacts()}
	return filepath.Base(dir), roots, nil
}

// ListMedia answers media.list; see appwire.MediaReader.
func (b *appBackend) ListMedia(agent, cursor string, limit int) (appwire.MediaPage, error) {
	id, roots, err := b.mediaScope(agent)
	if err != nil {
		return appwire.MediaPage{}, err
	}
	// Each source is built, and its size as JSON measured, BEFORE it is admitted:
	// the page ends before the row that would pass the reply bound, and the
	// cursor resumes at that row.
	built := map[string]appwire.MediaSource{}
	scan, err := session.ListMediaFit(appMediaIndex(), id, cursor, limit, time.Now().Add(appMediaListTime),
		appwire.MediaReplyBudget-8192, func(row session.MediaRow) (int, error) {
			source := appMediaSource(row, session.ResolveMedia(row, roots), false)
			size, err := appwire.MediaSourceSize(source)
			if err != nil {
				return 0, err
			}
			built[row.Fact.ID] = source
			return size + 1, nil
		})
	if err != nil {
		return appwire.MediaPage{}, appMediaError(err)
	}
	page := appwire.MediaPage{Sources: make([]appwire.MediaSource, 0, len(scan.Rows)), Next: scan.Next, Incomplete: scan.Incomplete}
	for _, row := range scan.Rows {
		page.Sources = append(page.Sources, built[row.Fact.ID])
	}
	return page, nil
}

// GetMedia answers media.get; see appwire.MediaReader.
func (b *appBackend) GetMedia(agent, source string) (appwire.MediaSource, error) {
	id, roots, err := b.mediaScope(agent)
	if err != nil {
		return appwire.MediaSource{}, err
	}
	row, err := session.FindMedia(appMediaIndex(), id, source, time.Now().Add(appMediaGetTime))
	if err != nil {
		return appwire.MediaSource{}, appMediaError(err)
	}
	got := appMediaSource(row, session.ResolveMedia(row, roots), true)
	if _, err := appwire.MediaSourceSize(got); err != nil {
		return appwire.MediaSource{}, appMediaError(session.ErrMediaTooLarge)
	}
	return got, nil
}

func appMediaError(err error) error {
	switch {
	case errors.Is(err, session.ErrMediaCursor):
		return appwire.Errorf(appwire.CodeBadParams, "that cursor is no longer valid; list again from the start")
	case errors.Is(err, session.ErrMediaNotFound):
		return appwire.Errorf(appwire.CodeNotFound, "there is no such media source in this conversation")
	case errors.Is(err, session.ErrMediaTooLarge):
		return appwire.Errorf(appwire.CodeEngine, "a media record is too large to send within the reply bound")
	case errors.Is(err, session.ErrMediaBusy):
		return appwire.Errorf(appwire.CodeBusy, "the media history is still being searched; try again")
	}
	return err
}

// appMediaSource is a row and its live check as the wire carries them. The
// record is passed through as recorded; only a legacy image whose record has no
// size is probed, and only on a get, and the answer says it was probed.
func appMediaSource(row session.MediaRow, live session.MediaLive, probe bool) appwire.MediaSource {
	fact := row.Fact
	out := appwire.MediaSource{
		ID: fact.ID, Kind: row.Kind, Origin: fact.Origin, Path: fact.Canonical,
		State: live.State, Recorded: row.Recorded, Version: row.Version(), LiveVersion: live.Version,
		MIME: fact.MIME, Created: row.Artifact.Created.UTC().Format(time.RFC3339Nano),
		Title: row.Artifact.Title, Model: fact.Model, RequestedModel: fact.Model, Run: fact.Run, Message: fact.Message, Tool: fact.Tool,
		References: fact.References, Said: fact.Said,
		InputMessages: fact.InputMessages, InputMessagesOmitted: fact.InputMessagesOmitted,
		Batch: fact.Batch, VariantIndex: fact.VariantIndex, ReturnedCount: fact.Returned, CostScope: fact.CostScope,
		Verb: fact.Verb, Saw: fact.Saw, SawOmitted: fact.SawOmitted, SeenBy: fact.SeenBy, WorkingModel: fact.Working, Transcribed: fact.Transcribed, Charge: fact.Charge,
	}
	if out.Charge == "" {
		out.Charge = session.MediaChargeUnknown
	}
	if out.Charge == session.MediaCharged {
		out.CostUSD = fact.CostUSD
		out.CallCostUSD = fact.CallCostUSD
	}
	if row.Recorded && fact.Bytes > 0 {
		bytes := fact.Bytes
		out.Bytes = &bytes
	}
	if live.HasBytes {
		bytes := live.Bytes
		out.LiveBytes = &bytes
	}
	if fact.Width > 0 && fact.Height > 0 {
		w, h := fact.Width, fact.Height
		out.Width, out.Height = &w, &h
	}
	if fact.Requested > 0 {
		n := fact.Requested
		out.RequestedCount = &n
	}
	if fact.PageCount > 0 {
		n := fact.PageCount
		out.PageCount = &n
	}
	if fact.TextLayer != nil {
		layer := *fact.TextLayer
		out.TextLayer = &layer
	}
	if fact.DurationMS > 0 {
		ms := fact.DurationMS
		out.DurationMS = &ms
	}
	if probe && live.Path != "" {
		if out.MIME == "" {
			out.MIME = session.ProbeMIME(live.Path)
		}
		if out.Width == nil && row.Kind == "image" {
			if w, h, ok := session.ProbeImage(live.Path); ok {
				out.Width, out.Height, out.Probed = &w, &h, true
			}
		}
	}
	return out
}

var _ appwire.MediaReader = (*appBackend)(nil)
