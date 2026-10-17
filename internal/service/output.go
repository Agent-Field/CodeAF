package service

import (
	"bufio"
	"errors"
	"io"
	"os"
)

// A service's output is read in bounded pages and never whole.
//
// THE STREAM IS THE OLDER CHUNK THEN THE LIVE ONE, at most outputWindow bytes
// of it: <id>.log.1 followed by <id>.log, which is what the log keeps while a
// codeaf runs. A log that grew past that while no codeaf was open is read from
// its last outputWindow bytes, and the page says so ([OutputPage.Truncated]),
// rather than a protocol answer or a screen being handed gigabytes. Lines are
// numbered from 0 within the stream, and a line longer than maxOutputLine is
// cut there and marked.
const (
	outputWindow    = 2 * logChunk
	maxOutputLine   = 4 << 10
	maxOutputLines  = 1000
	maxOutputBytes  = 256 << 10
	cutLineMark     = " …[line cut]"
	defaultOutLines = 200
)

// OutputPage is one bounded read of a service's output.
type OutputPage struct {
	Lines []string `json:"lines"`
	// FirstLine is the number of Lines[0]; NextLine the one after the last.
	FirstLine int `json:"first_line"`
	NextLine  int `json:"next_line"`
	// TotalLines is how many lines the stream holds now.
	TotalLines int `json:"total_lines"`
	// Truncated says earlier output is no longer kept: it was cut back while
	// codeaf ran, or the log is larger than the window read.
	Truncated bool `json:"truncated,omitempty"`
	// Partial says the page stopped before the end of the stream because the
	// line cap or the byte cap was reached, so a line at NextLine exists and was
	// not returned. It is NOT Truncated: nothing was lost, read on from NextLine.
	Partial bool `json:"partial,omitempty"`
}

// OutputPath is where one of owner's services — kept, or removed with its
// output kept — writes.
func (s *Supervisor) OutputPath(owner, id string) (string, error) {
	s.mu.Lock()
	_, err := s.findLocked(owner, id)
	s.mu.Unlock()
	if err == nil {
		return s.logPath(id), nil
	}
	if _, archErr := s.readArchive(owner, id); archErr != nil {
		return "", ErrNotFound
	}
	return s.logPath(id), nil
}

// Output is up to max lines of one of owner's services from line from on, and
// never more than maxOutputBytes of them.
func (s *Supervisor) Output(owner, id string, from, max int) (OutputPage, error) {
	if from < 0 {
		return OutputPage{}, errors.New("from is a line number")
	}
	path, truncated, err := s.outputOf(owner, id)
	if err != nil {
		return OutputPage{}, err
	}
	max = clampLines(max)
	page := OutputPage{Lines: []string{}, FirstLine: from, NextLine: from, Truncated: truncated}
	bytes := 0
	err = scanOutput(path, &page.Truncated, func(n int, line string) {
		page.TotalLines = n + 1
		// THE PAGE IS A CONTIGUOUS PREFIX from `from`. Once a line has not been
		// returned, no later line may be: a client resumes at NextLine, so a
		// skipped line followed by an accepted one would be a gap it must refuse.
		// The scan goes on only to count TotalLines.
		if n < from || page.Partial {
			return
		}
		if len(page.Lines) >= max || bytes+len(line) > maxOutputBytes {
			page.Partial = true
			return
		}
		page.Lines = append(page.Lines, line)
		bytes += len(line)
		page.NextLine = n + 1
	})
	return page, err
}

// Tail is the last n lines of one of owner's services.
func (s *Supervisor) Tail(owner, id string, n int) (OutputPage, error) {
	path, truncated, err := s.outputOf(owner, id)
	if err != nil {
		return OutputPage{}, err
	}
	n = clampLines(n)
	ring := make([]string, n)
	used, next := 0, 0
	page := OutputPage{Lines: []string{}, Truncated: truncated}
	err = scanOutput(path, &page.Truncated, func(i int, line string) {
		page.TotalLines = i + 1
		ring[next] = line
		next = (next + 1) % n
		if used < n {
			used++
		}
	})
	// Flatten once after the scan. Shifting on each line multiplies work by
	// the requested tail size for logs containing millions of short lines.
	ordered := make([]string, used)
	start := 0
	if used == n {
		start = next
	}
	for i := range ordered {
		ordered[i] = ring[(start+i)%n]
	}
	ring = ordered
	bytes := 0
	for i := len(ring) - 1; i >= 0; i-- {
		if bytes+len(ring[i]) > maxOutputBytes {
			ring = ring[i+1:]
			break
		}
		bytes += len(ring[i])
	}
	page.Lines = append(page.Lines, ring...)
	page.FirstLine = page.TotalLines - len(page.Lines)
	page.NextLine = page.TotalLines
	return page, err
}

func clampLines(n int) int {
	if n <= 0 {
		return defaultOutLines
	}
	if n > maxOutputLines {
		return maxOutputLines
	}
	return n
}

func (s *Supervisor) outputOf(owner, id string) (string, bool, error) {
	path, err := s.OutputPath(owner, id)
	if err != nil {
		return "", false, err
	}
	s.mu.Lock()
	truncated := false
	live := s.entries[id] != nil
	if e := s.entries[id]; e != nil {
		truncated = e.rec.Truncated
	}
	s.mu.Unlock()
	if !live {
		if archived, err := s.readArchive(owner, id); err == nil {
			truncated = archived.OutputTruncated
		}
	}
	return path, truncated, nil
}

// scanOutput walks the stream line by line, holding one line at a time.
func scanOutput(path string, truncated *bool, line func(int, string)) error {
	var readers []io.Reader
	var budget int64 = outputWindow
	var files []*os.File
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	// The live chunk is the newest, so it is taken first from the budget and
	// the older one gets what is left.
	for _, name := range []string{path, path + ".1"} {
		f, err := os.Open(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		files = append(files, f)
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("the output is not a plain file")
		}
		size := info.Size()
		start := int64(0)
		if size > budget {
			start, *truncated = size-budget, true
		}
		budget -= size - start
		readers = append([]io.Reader{io.NewSectionReader(f, start, size-start)}, readers...)
		if budget <= 0 {
			if name == path {
				if _, err := os.Stat(path + ".1"); err == nil {
					*truncated = true
				}
			}
			break
		}
	}
	reader := bufio.NewReaderSize(io.MultiReader(readers...), maxOutputLine)
	n := 0
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			text, cut := string(chunk), false
			for errors.Is(err, bufio.ErrBufferFull) {
				cut = true
				_, err = reader.ReadSlice('\n')
			}
			if cut {
				text += cutLineMark
			}
			line(n, trimNewline(text))
			n++
		}
		if err == io.EOF {
			return nil
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
}

func trimNewline(text string) string {
	if n := len(text); n > 0 && text[n-1] == '\n' {
		text = text[:n-1]
		if n := len(text); n > 0 && text[n-1] == '\r' {
			text = text[:n-1]
		}
	}
	return text
}
