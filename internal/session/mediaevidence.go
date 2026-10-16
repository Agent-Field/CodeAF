package session

// mediaevidence.go records what the agent LOOKED AT as sources of origin
// evidence: the file, and the words the looking model answered with, verbatim.
//
// It is a fact captured where the look happens (view_image, and read of an
// image, a sound or a video), from the values the look itself held — never
// recovered from the tool's prose afterwards. A look answered from the session's
// own memo is not recorded again: nothing was seen anew.
//
// A person's own attachments are not repeated as evidence by THIS file's rule;
// the surface applies it by comparing the canonical path of an evidence source
// with the attachment sources of the same conversation, both structured.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"
)

// recordEvidence leaves one evidence row for a file the agent has just
// perceived. data is the exact bytes that were sent to the looking model, so
// the hash and the size are those bytes' and not a second reading of the disk.
func (a *Agent) recordEvidence(ctx context.Context, absolute, mime, kind string, data []byte, verb, saw, seenBy string, transcribed bool) {
	fact := MediaFact{Origin: MediaEvidence, MIME: mime, Verb: verb, SeenBy: seenBy, Working: a.Model(), Transcribed: transcribed}
	if len(saw) <= mediaSaidMax {
		fact.Saw = saw
	} else {
		fact.SawOmitted = true
	}
	head := data
	if len(head) > mediaProbeBytes {
		head = head[:mediaProbeBytes]
	}
	if kind == "image" {
		if w, h, ok := imageSize(head); ok {
			fact.Width, fact.Height = w, h
		}
	}
	fact.stampFile(absolute)
	sum := sha256.Sum256(data)
	fact.SHA256, fact.Bytes = hex.EncodeToString(sum[:]), int64(len(data))
	fact = fact.WithMade(madeBy(ctx, nil))
	a.recordMedia(Artifact{Path: absolute, Title: filepath.Base(absolute), Kind: kind, Created: time.Now()}, fact)
}
