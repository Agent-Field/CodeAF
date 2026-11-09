// Package textfile preserves a text file's encoding and line endings.
package textfile

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Encoding records only BOM-marked UTF-16. All other bytes retain their
// existing interpretation, including UTF-8 BOMs and unmarked binary data.
type Encoding uint8

const (
	unchanged Encoding = iota
	littleEndian
	bigEndian
)

// Decode recognizes UTF-16 only when its byte order mark is present.
func Decode(raw []byte) (string, Encoding, error) {
	encoding := unchanged
	if len(raw) >= 2 {
		switch string(raw[:2]) {
		case "\xff\xfe":
			encoding = littleEndian
		case "\xfe\xff":
			encoding = bigEndian
		}
	}
	if encoding == unchanged {
		return string(raw), encoding, nil
	}
	if len(raw)%2 != 0 {
		return "", encoding, fmt.Errorf("UTF-16 file has an incomplete code unit")
	}
	order := encoding.order()
	units := make([]uint16, (len(raw)-2)/2)
	for i := range units {
		units[i] = order.Uint16(raw[2+i*2:])
	}
	for i := 0; i < len(units); i++ {
		unit := units[i]
		if unit >= 0xd800 && unit <= 0xdbff && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
			i++
		} else if unit >= 0xd800 && unit <= 0xdfff {
			return "", encoding, fmt.Errorf("UTF-16 file has an unpaired surrogate")
		}
	}
	return string(utf16.Decode(units)), encoding, nil
}

func (e Encoding) order() binary.ByteOrder {
	if e == bigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// Encode restores the original UTF-16 byte order and BOM, or writes bytes as
// before when no UTF-16 BOM was present.
func (e Encoding) Encode(content string) []byte {
	if e == unchanged {
		return []byte(content)
	}
	units := utf16.Encode([]rune(content))
	raw := make([]byte, 2+len(units)*2)
	order := e.order()
	order.PutUint16(raw, 0xfeff)
	for i, unit := range units {
		order.PutUint16(raw[2+i*2:], unit)
	}
	return raw
}

// WriteContent preserves an existing file's encoding and line-ending style.
// A file with no line endings supplies no style to impose on new content.
func WriteContent(original []byte, content string) ([]byte, error) {
	before, encoding, err := Decode(original)
	if err != nil {
		return nil, err
	}
	if strings.Contains(before, "\n") {
		content = RestoreLineEndings(NormalizeToLF(content), DetectLineEnding(before))
	}
	return encoding.Encode(content), nil
}

// DetectLineEnding uses the chat edit rule: the first newline decides the style.
func DetectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if crlfIdx >= 0 && crlfIdx < lfIdx {
		return "\r\n"
	}
	return "\n"
}

// NormalizeToLF is the chat edit's normalization, including lone CR lines.
func NormalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// RestoreLineEndings restores normalized text to the target's style.
func RestoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}
