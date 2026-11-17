package serve

import (
	"bytes"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/pdfx"
)

// SEARCH FINDS AN UPLOAD BY WHAT IT SAYS, NOT ONLY BY ITS NAME (live2 E3
// F5). A text upload (a CSV, a Markdown or plain text file) is searched as
// its words, and a PDF by its text layer; a picture or anything else without
// words is found by its name alone. The words are read once per kept bytes
// and remembered by their sha256, so a search does not parse every PDF again.

// uploadWordsMost is the most of one upload's words a search reads.
const uploadWordsMost = 2 << 20

// uploadWordCacheMost is how many uploads' words are remembered at once.
const uploadWordCacheMost = 512

// uploadWordCache is each upload's words, lowered, by the sha256 of its bytes.
type uploadWordCache struct {
	mu    sync.Mutex
	words map[string]string
}

// uploadSays is whether f is an upload whose words contain q (already lowered).
func (h *Handler) uploadSays(f factory.WorkstreamFile, q string) bool {
	if f.Kind != factory.FileUpload || f.Upload == nil || f.Upload.Blob == "" {
		return false
	}
	return strings.Contains(h.uploadText(f.Upload), q)
}

// uploadText is an upload's words, lowered, or "" when it has none.
func (h *Handler) uploadText(u *factory.UploadBlob) string {
	c := &h.uploadWords
	c.mu.Lock()
	words, ok := c.words[u.Blob]
	c.mu.Unlock()
	if ok {
		return words
	}
	words = strings.ToLower(h.readUploadWords(u))
	c.mu.Lock()
	if c.words == nil || len(c.words) >= uploadWordCacheMost {
		c.words = map[string]string{}
	}
	c.words[u.Blob] = words
	c.mu.Unlock()
	return words
}

// readUploadWords reads an upload's words from its kept bytes: the bytes
// themselves when they are text, a PDF's text layer, and nothing otherwise
// (or when they cannot be read: a search is never refused over one file).
func (h *Handler) readUploadWords(u *factory.UploadBlob) string {
	data, err := h.config.Store.Blob(u.Blob)
	if err != nil {
		return ""
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) || u.Mime == "application/pdf" {
		path, err := h.config.Store.BlobPath(u.Blob)
		if err != nil {
			return ""
		}
		text, err := pdfx.Extract(path)
		if err != nil {
			return ""
		}
		return cutWords(text)
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return ""
	}
	return cutWords(string(data))
}

// cutWords is s cut to uploadWordsMost bytes, on a character's start.
func cutWords(s string) string {
	if len(s) <= uploadWordsMost {
		return s
	}
	end := uploadWordsMost
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}
