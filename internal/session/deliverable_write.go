package session

// deliverable_write.go records the bytes observed at file mutation seams.
//
// Successful write and edit mutations inside the deliverable roots are
// deliverables. Rewriting a recorded path updates its identity and bytes.
//
// The write, edit and media tools observe their own writes. `bash` observes
// only a picture the model declared in its
// `outputs` argument (shell_outputs.go): the engine looked at that path before
// and after the one command. Other shell-created files are not recorded.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// askedMessageID is the authored message a tool call was made under, in the
// form TaskSource.ID uses, or "" when the call carries no such causality. It is
// read off the call's own episode and never from the session's latest words.
func askedMessageID(ctx context.Context) string {
	asked := askedFrom(ctx)
	if !asked.id.live() {
		return ""
	}
	return asked.id.scope + "#" + strconv.FormatUint(asked.id.seq, 10)
}

// deliverableRoots are the places a session's own files may live.
func (a *Agent) deliverableRoots() []string {
	return []string{a.config.Workspace, a.config.Place.Work(), a.config.Place.Artifacts()}
}

// deliverableStateAt is the state a file has at the moment it is created, and
// how: in the session's own folder it is a draft; created inside a borrowed
// repository it is kept from the start; with no folder to judge by the state is
// unknown. This is a fact of creation and is never recomputed later.
func (a *Agent) deliverableStateAt(canonical string) (state string, kept *DeliverableKept) {
	place := a.config.Place
	switch {
	case strings.TrimSpace(place.Dir) == "":
		return "", nil
	case withinAny([]string{place.Work(), place.Artifacts()}, canonical):
		return DeliverDraft, nil
	case withinAny([]string{a.config.Workspace}, canonical):
		return DeliverKept, &DeliverableKept{Path: canonical, At: time.Now().UTC(), How: KeptCreatedInRepo}
	}
	return "", nil
}

// identityOf is a file's identity as its own descriptor reported it.
func identityOf(canonical string, info os.FileInfo) *DeliverableFile {
	out := &DeliverableFile{Canonical: canonical, Bytes: info.Size(), ModNS: info.ModTime().UnixNano()}
	out.CtimeNS, out.Dev, out.Ino = fileIdentity(info)
	return out
}

// authoredFact is the deliverable fact of a file this session just made.
func (a *Agent) authoredFact(ctx context.Context, file *DeliverableFile, from []string) DeliverableFact {
	state, kept := a.deliverableStateAt(file.Canonical)
	fact := DeliverableFact{
		ID: NewDeliverableID(), At: time.Now().UTC(),
		Turn: askedMessageID(ctx), Run: runIdentity(ctx),
		Call: callIDFrom(ctx),
		From: boundedFrom(from), State: state, Origin: DeliverOriginAgent,
		Authored: true, File: file, Kept: kept,
	}
	if file.SHA256 != "" {
		fact.Version = "sha256:" + file.SHA256
	}
	return fact
}

// recordWritten records a file the write or edit tool just wrote. It is called by
// the tool itself, inside the file's mutation queue, with the exact bytes it
// wrote and the descriptor's own view of the file, so authorship, hash and
// identity are one fact of one write and nothing is read back from a disk
// someone else could have changed. An overwrite updates the recorded path.
// Every failure is silence, for RecordArtifact's reason.
func (a *Agent) recordWritten(ctx context.Context, event bare.WriteEvent) {
	if event.Info == nil {
		return
	}
	if a.config.InTask {
		if !event.Created {
			return
		}
		// A worker has no session of its own: only the sink its owning
		// conversation gave it can record, and as that conversation.
		a.recordTaskWritten(ctx, writtenFile(event))
		return
	}
	index, session := a.config.ArtifactsIndex, a.journalID()
	if index == "" || session == "" {
		return
	}
	canonical := canonicalFile(event.Path)
	kind := deliverableKindExt(canonical)
	if !withinAny(a.deliverableRoots(), canonical) {
		return
	}
	if kind == "" {
		kind = shareKindFile
	}
	file := identityOf(canonical, event.Info)
	sum := sha256.Sum256(event.Content)
	file.SHA256 = hex.EncodeToString(sum[:])
	fact := a.authoredFact(ctx, file, nil)
	artifact := Artifact{Path: canonical, Session: session, Title: filepath.Base(canonical), Kind: kind, Created: time.Now()}
	attachWrittenMedia(ctx, &artifact, file, event.Content)
	artifact.Deliver = &fact
	if written, ok := recordWrittenArtifact(index, artifact); ok {
		a.emitArtifactChange(written)
	}
}

// deliverableWrite observes successful mutations inside the tool's queue.
func (a *Agent) deliverableWrite(inner bare.Tool) bare.Tool {
	wrapped := inner
	wrapped.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		observed := bare.WithWriteObserver(ctx, func(event bare.WriteEvent) { a.recordWritten(ctx, event) })
		return inner.Execute(observed, args)
	}
	return wrapped
}

// generatedDeliverable is the deliverable fact of a file a media tool made. The
// turn is the message the call was made under (fact.Message) and the run is
// the producing RunID, each only where the tool site knew it. The identity and
// hash are the media fact's own, taken from the bytes the tool held; the file is
// not read again.
func (a *Agent) generatedDeliverable(path string, fact MediaFact) *DeliverableFact {
	canonical := canonicalFile(path)
	out := DeliverableFact{
		ID: NewDeliverableID(), At: time.Now().UTC(),
		Turn: fact.Message, Run: fact.Run, From: boundedFrom(fact.References),
		Origin: DeliverOriginAgent, Authored: true,
	}
	out.State, out.Kept = a.deliverableStateAt(canonical)
	if fact.ModNS != 0 {
		out.File = &DeliverableFile{Canonical: canonical, Bytes: fact.Bytes, ModNS: fact.ModNS, CtimeNS: fact.CtimeNS, Dev: fact.Dev, Ino: fact.Ino, SHA256: fact.SHA256, MIME: fact.MIME}
	}
	if fact.SHA256 != "" {
		out.Version = "sha256:" + fact.SHA256
	}
	return &out
}

// writtenFile is the write tool's event as the task recorder takes it.
type writtenFile = bare.WriteEvent

// attachWrittenMedia publishes facts from the exact authored bytes and descriptor.
func attachWrittenMedia(ctx context.Context, artifact *Artifact, file *DeliverableFile, content []byte) {
	file.MIME = http.DetectContentType(content[:min(len(content), 512)])
	switch artifact.Kind {
	case "image", "audio", "video":
		// A media file is also a media source, from the SAME row: its facts come
		// from the bytes in hand, and an extension alone is only a name.
		media := GeneratedMedia(artifact.Path, content, "")
		if mediaKindOf(media.MIME) != artifact.Kind {
			artifact.Kind = shareKindFile
			break
		}
		media.Canonical, media.Bytes, media.ModNS = artifact.Path, file.Bytes, file.ModNS
		media.CtimeNS, media.Dev, media.Ino = file.CtimeNS, file.Dev, file.Ino
		media.Run, media.Tool, media.Message = runIdentity(ctx), callIDFrom(ctx), askedMessageID(ctx)
		file.MIME = media.MIME
		media.ID = NewMediaID()
		artifact.Media = &media
	}
}
