// Package artifactregistry ports src/session/artifact-registry.ts:1-178 from
// swe-pro commit 3b25a1a. It stores prompt artifacts by content hash and emits
// bounded, model-visible references that fail open to the complete body.
//
// Fidelity notes:
//   - Bytes is UTF-8 byte length, while excerpt counts and slices use UTF-16
//     code units, exactly like Buffer.byteLength versus String.length/slice.
//   - Existing content-addressed files are never rewritten, even if a short-id
//     collision or external corruption put different bytes there. RenderRef's
//     integrity check then fails open; this odd-looking split is intentional.
//   - All filesystem failures are swallowed. Prompt losslessness is more
//     important than surfacing registry I/O failures into the run.
package artifactregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const (
	artifactSubdir     = ".codeaf/artifacts"
	defaultExcerptHead = 6000
	defaultExcerptTail = 2000
)

// ArtifactRef is the stored artifact handle. The first four fields are the
// public wire contract; Workspace and Body are carried for rendering and
// fail-open behavior.
type ArtifactRef struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Bytes     int    `json:"bytes"`
	SHA       string `json:"sha"`
	Workspace string `json:"workspace"`
	Body      string `json:"body"`
}

// RenderRefOptions controls the bounded inline excerpt. Nil numeric fields
// mean the TS property was absent and therefore select the default.
type RenderRefOptions struct {
	ExcerptHead *float64 `json:"excerptHead,omitempty"`
	ExcerptTail *float64 `json:"excerptTail,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// ArtifactRefsEnabled is the master CODEAF_ARTIFACT_REFS switch.
func ArtifactRefsEnabled() bool {
	return os.Getenv("CODEAF_ARTIFACT_REFS") == "1"
}

func artifactAbsPath(workspace, id string) string {
	filename := id + ".txt"
	var candidate string
	if filepath.IsAbs(filename) {
		// path.resolve discards every component to the left of its rightmost
		// absolute argument. filepath.Join does not, so handle this explicitly.
		candidate = filename
	} else {
		candidate = filepath.Join(workspace, artifactSubdir, filename)
	}
	if filepath.IsAbs(candidate) {
		return filepath.Clean(candidate)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Clean(candidate)
	}
	return filepath.Clean(filepath.Join(cwd, candidate))
}

// PutArtifact stores body under its sha256 content address. Writes are
// best-effort and an existing short-id file is not rewritten.
func PutArtifact(workspace, kind, body string) ArtifactRef {
	sum := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(sum[:])
	ref := ArtifactRef{
		ID:        sha[:16],
		Kind:      kind,
		Bytes:     len([]byte(body)),
		SHA:       sha,
		Workspace: workspace,
		Body:      body,
	}

	abs := artifactAbsPath(workspace, ref.ID)
	if _, err := os.Stat(abs); err == nil {
		return ref
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
		return ref
	}
	_ = os.WriteFile(abs, []byte(body), 0o666)
	return ref
}

// GetArtifact reads an artifact. Supplying expectedSHA enables the same
// integrity check as the TS optional third argument. The bool is false for
// absent, unreadable, or mismatched content (TS undefined).
func GetArtifact(workspace, id string, expectedSHA ...string) (string, bool) {
	b, err := os.ReadFile(artifactAbsPath(workspace, id))
	if err != nil {
		return "", false
	}
	body := decodeUTF8Lossy(b)
	if len(expectedSHA) > 0 {
		sum := sha256.Sum256([]byte(body))
		if hex.EncodeToString(sum[:]) != expectedSHA[0] {
			return "", false
		}
	}
	return body, true
}

func optionNumber(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func utf16Units(s string) []uint16 {
	return utf16.Encode([]rune(s))
}

func sliceIndex(n float64, length int) int {
	switch {
	case math.IsNaN(n):
		return 0
	case math.IsInf(n, 1):
		return length
	case math.IsInf(n, -1):
		return 0
	}
	n = math.Trunc(n)
	if n < 0 {
		n += float64(length)
	}
	if n < 0 {
		return 0
	}
	if n > float64(length) {
		return length
	}
	return int(n)
}

func decodeUnits(units []uint16) string {
	return string(utf16.Decode(units))
}

func buildExcerpt(body string, head, tail float64) string {
	units := utf16Units(body)
	if float64(len(units)) <= head+tail {
		return body
	}
	elided := float64(len(units)) - head - tail
	headEnd := sliceIndex(head, len(units))
	tailStart := sliceIndex(float64(len(units))-tail, len(units))
	return decodeUnits(units[:headEnd]) +
		"\n… [" + jscompat.FormatNumber(elided) +
		" chars elided — read the full body from the artifact file named above] …\n" +
		decodeUnits(units[tailStart:])
}

// RenderRef renders the durable reference and bounded excerpt. If the durable
// copy is unavailable, corrupt, or differs from Body, the full body is inlined.
func RenderRef(ref ArtifactRef, opts *RenderRefOptions) string {
	if opts == nil {
		opts = &RenderRefOptions{}
	}
	head := math.Max(0, optionNumber(opts.ExcerptHead, defaultExcerptHead))
	tail := math.Max(0, optionNumber(opts.ExcerptTail, defaultExcerptTail))
	absPath := artifactAbsPath(ref.Workspace, ref.ID)

	onDisk, ok := GetArtifact(ref.Workspace, ref.ID, ref.SHA)
	note := ""
	if opts.Note != "" {
		note = " " + opts.Note
	}
	if !ok || onDisk != ref.Body {
		return "[artifact " + ref.ID + " " + ref.Kind + " " +
			jscompat.FormatNumber(float64(ref.Bytes)) +
			"B — durable copy unavailable; full body inlined below]" +
			note + "\n" + ref.Body
	}

	header := "[artifact " + ref.ID + " " + ref.Kind + " " +
		jscompat.FormatNumber(float64(ref.Bytes)) + "B — full body: " +
		absPath + " — read it with your file-read tool if needed]" + note
	return header + "\n" + buildExcerpt(ref.Body, head, tail)
}

// EmbedArtifact stores and renders body in one idempotent step.
func EmbedArtifact(workspace, kind, body string, opts *RenderRefOptions) string {
	return RenderRef(PutArtifact(workspace, kind, body), opts)
}

// decodeUTF8Lossy mirrors Node's UTF-8 decoder: invalid input becomes one
// U+FFFD per maximal subpart.
func decodeUTF8Lossy(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		var need int
		var lo, hi byte
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need, lo, hi = 1, 0x80, 0xBF
		case c == 0xE0:
			need, lo, hi = 2, 0xA0, 0xBF
		case c >= 0xE1 && c <= 0xEC:
			need, lo, hi = 2, 0x80, 0xBF
		case c == 0xED:
			need, lo, hi = 2, 0x80, 0x9F
		case c >= 0xEE && c <= 0xEF:
			need, lo, hi = 2, 0x80, 0xBF
		case c == 0xF0:
			need, lo, hi = 3, 0x90, 0xBF
		case c >= 0xF1 && c <= 0xF3:
			need, lo, hi = 3, 0x80, 0xBF
		case c == 0xF4:
			need, lo, hi = 3, 0x80, 0x8F
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		var cp rune
		switch need {
		case 1:
			cp = rune(c & 0x1F)
		case 2:
			cp = rune(c & 0x0F)
		default:
			cp = rune(c & 0x07)
		}
		j, valid := 1, true
		for ; j <= need; j++ {
			l, h := byte(0x80), byte(0xBF)
			if j == 1 {
				l, h = lo, hi
			}
			if i+j >= len(b) || b[i+j] < l || b[i+j] > h {
				valid = false
				break
			}
			cp = cp<<6 | rune(b[i+j]&0x3F)
		}
		if !valid {
			out.WriteRune(utf8.RuneError)
			i += j
			continue
		}
		out.WriteRune(cp)
		i += need + 1
	}
	return out.String()
}
