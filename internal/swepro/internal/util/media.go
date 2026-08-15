// Attachment media helpers — port of src/util/media.ts:1-25
// (swe-pro 3b25a1a).
package util

import (
	"bytes"
	"strings"
)

func IsPDFAttachment(mime string) bool { return mime == "application/pdf" }

func IsMedia(mime string) bool {
	return strings.HasPrefix(mime, "image/") || IsPDFAttachment(mime)
}

func IsImageAttachment(mime string) bool {
	return strings.HasPrefix(mime, "image/") &&
		mime != "image/svg+xml" &&
		mime != "image/vnd.fastbidsheet"
}

func SniffAttachmentMime(data []byte, fallback string) string {
	for _, item := range []struct {
		prefix []byte
		mime   string
	}{
		{[]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, "image/png"},
		{[]byte{0xff, 0xd8, 0xff}, "image/jpeg"},
		{[]byte{0x47, 0x49, 0x46, 0x38}, "image/gif"},
		{[]byte{0x42, 0x4d}, "image/bmp"},
		{[]byte{0x25, 0x50, 0x44, 0x46, 0x2d}, "application/pdf"},
	} {
		if bytes.HasPrefix(data, item.prefix) {
			return item.mime
		}
	}
	if len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.HasPrefix(data[8:], []byte("WEBP")) {
		return "image/webp"
	}
	return fallback
}
