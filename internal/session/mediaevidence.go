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

// captureWorking: every perception entry reads `working := a.Model()` BEFORE
// its first network call and hands it to recordEvidence. The call can last a
// minute and the person can switch models inside it; reading the model at
// record time would relabel a look the old model supervised with the new one,
// and "seen by" is shown only where the two differ.

// recordEvidence leaves one evidence row for a file the agent has just
// perceived. data is the exact bytes that were sent to the looking model, so
// the hash and the size are those bytes' and not a second reading of the disk.
// working is the session's model as the perception began (see captureWorking).
func (a *Agent) recordEvidence(ctx context.Context, working, absolute, mime, kind string, data []byte, verb, saw, seenBy string, transcribed bool, observed ...MediaFact) {
	fact := MediaFact{}
	if len(observed) > 0 {
		fact = observed[0]
	} else {
		fact.stampFile(absolute)
	}
	fact.Origin, fact.MIME, fact.Verb, fact.SeenBy, fact.Working, fact.Transcribed = MediaEvidence, mime, verb, seenBy, working, transcribed
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
	// The length of a sound and the length and frame of a clip are read off the
	// very bytes the looking model was given, where the container states them.
	fact.measureContainer(data)
	sum := sha256.Sum256(data)
	fact.SHA256, fact.Bytes = hex.EncodeToString(sum[:]), int64(len(data))
	fact = fact.WithMade(madeBy(ctx, nil))
	a.recordMedia(Artifact{Path: absolute, Title: filepath.Base(absolute), Kind: kind, Created: time.Now()}, fact)
}

// recordDocumentEvidence leaves the evidence row of a document the agent has
// just read through read_document: the extraction verbatim (omitted, and said
// so, when too long to keep whole), the model that read it only where a model
// did, and the page count and text layer only where the parser reported them.
// Nothing is guessed: a billed parse that returns no page boundaries leaves the
// page count out.
func (a *Agent) recordDocumentEvidence(ctx context.Context, working, absolute string, answer DocumentAnswer) {
	fact := answer.observed
	fact.Origin, fact.MIME, fact.Verb, fact.SeenBy, fact.Working = MediaEvidence, answer.MediaType, EvidenceRead, answer.Model, working
	fact.PageCount, fact.TextLayer = answer.Pages, answer.TextLayer
	if len(answer.Text) <= mediaSaidMax {
		fact.Saw = answer.Text
	} else {
		fact.SawOmitted = true
	}
	if answer.Digest != "" {
		// The bytes a billed parse was given were in hand, so the hash is theirs.
		fact.SHA256 = answer.Digest
	}
	fact = fact.WithMade(madeBy(ctx, nil))
	a.recordMedia(Artifact{Path: absolute, Title: filepath.Base(absolute), Kind: mediaDocument, Created: time.Now()}, fact)
}
