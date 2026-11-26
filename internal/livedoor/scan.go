package livedoor

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/remote"
)

var (
	errLineTooLong = errors.New("livedoor: line exceeds maximum length")
	errNoNewline   = errors.New("livedoor: line ended without a newline")
)

const initialScanBuffer = 4096

// lineScanner reads newline-delimited EventWire JSON lines from r while never
// allocating a buffer larger than maxCap + 1 bytes.
type lineScanner struct {
	r      io.Reader
	maxCap int
	buf    []byte
	rpos   int
	wpos   int
}

func newLineScanner(r io.Reader, maxCap int) *lineScanner {
	if maxCap <= 0 {
		maxCap = MaxLine
	}
	initCap := initialScanBuffer
	if initCap > maxCap+1 {
		initCap = maxCap + 1
	}
	return &lineScanner{
		r:      r,
		maxCap: maxCap,
		buf:    make([]byte, initCap),
	}
}

// ReadLine returns the next newline-terminated line (including its trailing '\n')
// whose content before '\n' is at most limit bytes. If more than limit bytes
// arrive before '\n', ReadLine returns errLineTooLong without allocating beyond
// maxCap + 1 bytes.
func (s *lineScanner) ReadLine(limit int) ([]byte, error) {
	if limit <= 0 || limit > s.maxCap {
		limit = s.maxCap
	}
	for {
		unconsumed := s.buf[s.rpos:s.wpos]
		if idx := bytes.IndexByte(unconsumed, '\n'); idx >= 0 {
			if idx > limit {
				return nil, errLineTooLong
			}
			line := unconsumed[:idx+1]
			s.rpos += idx + 1
			return line, nil
		}

		have := s.wpos - s.rpos
		if have > limit {
			return nil, errLineTooLong
		}

		if s.rpos > 0 {
			copy(s.buf, s.buf[s.rpos:s.wpos])
			s.wpos = have
			s.rpos = 0
		}

		maxBuf := s.maxCap + 1
		if len(s.buf) == s.wpos {
			if len(s.buf) >= maxBuf {
				return nil, errLineTooLong
			}
			newCap := len(s.buf) * 2
			if newCap < initialScanBuffer {
				newCap = initialScanBuffer
			}
			if newCap > maxBuf {
				newCap = maxBuf
			}
			next := make([]byte, newCap)
			copy(next, s.buf[:s.wpos])
			s.buf = next
		}

		readCap := limit + 1
		if readCap > len(s.buf) {
			readCap = len(s.buf)
		}
		n, err := s.r.Read(s.buf[s.wpos:readCap])
		s.wpos += n
		if n > 0 {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if s.wpos-s.rpos == 0 {
					return nil, io.EOF
				}
				return nil, errNoNewline
			}
			return nil, err
		}
	}
}

type viewerFrameHeader struct {
	Kind   string `json:"kind"`
	ID     uint64 `json:"id,omitempty"`
	Method string `json:"method,omitempty"`
}

type viewerLineAction int

const (
	viewerLineForward viewerLineAction = iota
	viewerLineRefuse
	viewerLineDrop
)

// classifyViewerLine inspects a single post-handshake line from a remote viewer
// and applies the default-deny frame-kind and method policy. Only well-formed
// "call" frames are eligible; calls prohibited by remote.GuestMay produce a
// synthesized result frame with the same ID and the host's refusal sentence.
func classifyViewerLine(line []byte, host string, drive bool) (viewerLineAction, []byte) {
	trimmed := bytes.TrimRight(line, "\r\n")
	var hdr viewerFrameHeader
	if err := json.Unmarshal(trimmed, &hdr); err != nil {
		return viewerLineDrop, nil
	}
	if hdr.Kind != "call" {
		return viewerLineDrop, nil
	}
	method := strings.TrimSpace(hdr.Method)
	if method == "" {
		return viewerLineDrop, nil
	}
	if remote.GuestMay(method, drive) {
		return viewerLineForward, nil
	}
	reply, err := json.Marshal(remote.Frame{
		Kind:  "result",
		ID:    hdr.ID,
		Error: remote.GuestRefusal(host, method, drive),
	})
	if err != nil {
		return viewerLineDrop, nil
	}
	return viewerLineRefuse, append(reply, '\n')
}
