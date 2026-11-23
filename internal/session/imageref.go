package session

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── pictures are carried by reference and become bytes at the request ───────
//
// A picture in the live transcript is NOT its base64. It is a small immutable
// reference — where the file is, what it hashed to when the message was
// accepted, its type and its size — spelled as an image_url whose URL is a
// private scheme. The conversation, the journal, a replay of two hundred
// pictures and every copy the loop makes of the transcript carry that reference
// and nothing heavier. The BYTES are read, hashed and encoded in exactly one
// place: [materializeImageRefs], at the moment a request is about to leave for a
// model, for the pictures that request will carry.
//
// THREE LAWS FOLLOW FROM THAT.
//
//   - A REQUEST HAS A BUDGET, NOT A HISTORY. The newest pictures are
//     materialized first, until the aggregate decoded bytes or the picture count
//     of ONE request would pass its ceiling. Older pictures are replaced, in the
//     request only, by the same placeholder sentence a lost file gets, naming the
//     path so the person or the model can get it back. The transcript still
//     holds every reference, so the person's history is never edited and a later
//     request with room carries them again.
//
//   - A REFERENCE NEVER LEAVES THE PROCESS. Every part whose URL is in the
//     private scheme is replaced — by the picture or by text — and one that
//     cannot be read back is text, never forwarded. A moved, edited or deleted
//     file is a placeholder, because the digest recorded at accept time no longer
//     matches what is on disk (the same rule the journal's replay always had).
//
//   - THE NEWEST PICTURES ARE NEVER THE ONES DROPPED. One message's pictures are
//     refused at accept above [maxMessageImageBytes], and a request's ceiling is
//     twice that, so the latest message — an original and its marked copy — always
//     fits whatever the transcript before it holds.

const imageRefScheme = "codeaf-image:"

// They are variables only so a test can lower them; nothing else assigns them.
var (
	// maxRequestImageBytes is the most decoded picture bytes ONE request carries.
	// The encoded size is a third more, which is bounded by the same figure.
	maxRequestImageBytes int64 = 2 * maxMessageImageBytes
	// maxRequestImages is the most pictures ONE request carries, so a transcript
	// of thousands of tiny pictures costs a bounded number of file reads.
	maxRequestImages = 64
)

// imageRef is one picture as the transcript carries it.
type imageRef struct {
	Path   string `json:"p"`
	MIME   string `json:"m"`
	Bytes  int64  `json:"b,omitempty"`
	SHA256 string `json:"-"`
}

// part is the reference as a content part. The digest leads the URL so that
// [partKey], which fingerprints a part by its two ends, tells two pictures apart.
func (r imageRef) part() ai.ContentPart {
	body, _ := json.Marshal(r)
	return ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
		URL: imageRefScheme + r.SHA256 + ":" + base64.RawURLEncoding.EncodeToString(body),
	}}
}

// isImageRefPart is whether a part carries the private scheme, whether or not it
// parses.
func isImageRefPart(part ai.ContentPart) bool {
	return part.Type == "image_url" && part.ImageURL != nil && strings.HasPrefix(part.ImageURL.URL, imageRefScheme)
}

// imageRefOf parses a reference part.
func imageRefOf(part ai.ContentPart) (imageRef, bool) {
	if !isImageRefPart(part) {
		return imageRef{}, false
	}
	rest := strings.TrimPrefix(part.ImageURL.URL, imageRefScheme)
	sum, body, found := strings.Cut(rest, ":")
	if !found || len(sum) != 64 {
		return imageRef{}, false
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return imageRef{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return imageRef{}, false
	}
	var ref imageRef
	if json.Unmarshal(raw, &ref) != nil || strings.TrimSpace(ref.Path) == "" || strings.TrimSpace(ref.MIME) == "" || ref.Bytes < 0 {
		return imageRef{}, false
	}
	ref.SHA256 = sum
	return ref, true
}

// imageFiles is the only door to the disk for a request's pictures, so a test
// can count what a request reads.
type imageFiles struct {
	stat func(path string) (int64, error)
	read func(path string, size int64) ([]byte, error)
}

var requestImageFiles = imageFiles{
	stat: func(path string) (int64, error) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return 0, os.ErrNotExist
		}
		return info.Size(), nil
	},
	read: readSubmissionImage,
}

func hasImageRef(messages []ai.Message) bool {
	for _, message := range messages {
		for _, part := range message.Content {
			if isImageRefPart(part) {
				return true
			}
		}
	}
	return false
}

// materializeImageRefs is the one place a picture becomes bytes. It returns
// messages untouched — not one allocation — when none carries a reference, and
// otherwise a copy in which only the messages that carried references are new;
// the transcript it was handed is never written to.
func materializeImageRefs(messages []ai.Message) []ai.Message {
	if !hasImageRef(messages) {
		return messages
	}
	type slot struct {
		message, part int
	}
	var slots []slot
	for m, message := range messages {
		for p, part := range message.Content {
			if isImageRefPart(part) {
				slots = append(slots, slot{m, p})
			}
		}
	}
	replaced := make(map[slot]ai.ContentPart, len(slots))
	// THE BUDGET IS FOR ATTEMPTS, NOT FOR SUCCESSES. A picture is charged — one
	// slot of the count and its recorded size of the bytes — BEFORE the disk is
	// touched, and the charge stays when the file turns out gone, edited or
	// unreadable. A budget that only counted pictures that loaded would let a
	// transcript of ten thousand changed files cost ten thousand reads and hashes
	// per request, with every failure invisible to the ceiling. What the walk
	// does for a reference it has no room for is read its metadata and write a
	// placeholder: no stat, no open.
	var reserved int64
	attempts, exhausted := 0, false
	// Newest first, so the budget is spent on the pictures the model is being
	// asked about now.
	for i := len(slots) - 1; i >= 0; i-- {
		at := slots[i]
		part := messages[at.message].Content[at.part]
		ref, ok := imageRefOf(part)
		if !ok {
			replaced[at] = ai.ContentPart{Type: "text", Text: "[image — its reference could not be read]"}
			continue
		}
		lost := journalPart{Type: journalPartImage, Path: ref.Path, SHA256: ref.SHA256, MIME: ref.MIME}
		if ref.Bytes > maxImageBytes {
			// Larger than a send could ever have accepted: not a picture this
			// transcript's own door wrote, and not one to open.
			replaced[at] = lost.placeholderBecause("it is over the size one picture may be")
			continue
		}
		if exhausted || attempts >= maxRequestImages {
			// Once one picture does not fit, every older one is left out too: a
			// request that skipped a big one to keep a small older one would be a
			// different request every time the transcript grew.
			exhausted = true
			replaced[at] = lost.placeholderBecause("it is older than the pictures one request carries")
			continue
		}
		// This reference takes a slot now; the disk is touched only after.
		attempts++
		size := ref.Bytes
		if size <= 0 {
			// A line written before sizes were recorded: one stat finds it, and
			// it has already been counted.
			var err error
			if size, err = requestImageFiles.stat(ref.Path); err != nil {
				replaced[at] = lost.placeholder()
				continue
			}
			if size > maxImageBytes {
				replaced[at] = lost.placeholderBecause("it is over the size one picture may be")
				continue
			}
		}
		// Compared as what is left, never as a sum, so no size can overflow.
		if size > maxRequestImageBytes-reserved {
			exhausted = true
			replaced[at] = lost.placeholderBecause("it is older than the pictures one request carries")
			continue
		}
		reserved += size
		data, err := requestImageFiles.read(ref.Path, size)
		if err != nil {
			replaced[at] = lost.placeholder()
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != ref.SHA256 {
			replaced[at] = lost.placeholder()
			continue
		}
		replaced[at] = ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{URL: imageDataURL(ref.MIME, data)}}
	}
	out := make([]ai.Message, len(messages))
	copy(out, messages)
	for _, at := range slots {
		if out[at.message].Content == nil || sameBacking(out[at.message].Content, messages[at.message].Content) {
			// A NEW slice, never a write into the old one: a request already in
			// flight holds a shallow copy of this message (see blindswap.go).
			out[at.message].Content = append([]ai.ContentPart(nil), messages[at.message].Content...)
		}
		out[at.message].Content[at.part] = replaced[at]
	}
	return out
}

// sameBacking is whether two content slices are the same array.
func sameBacking(a, b []ai.ContentPart) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

// imageRefFromFile is a picture accepted into the conversation: its digest and
// size are the ones its bytes had, now.
func imageRefFromFile(path, mediaType string, data []byte) (imageRef, journalPart) {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	path = filepath.Clean(path)
	return imageRef{Path: path, MIME: mediaType, Bytes: int64(len(data)), SHA256: digest},
		journalPart{Type: journalPartImage, Path: path, SHA256: digest, MIME: mediaType, Bytes: int64(len(data))}
}
