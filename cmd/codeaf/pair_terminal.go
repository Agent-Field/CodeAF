package main

// The terminal's half of a pairing: the questions a person answers with y or n,
// and the lines both doors of `codeaf pair` print. The sentences themselves are
// internal/pair's (lines.go), so the terminal and the chat screen cannot drift.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// lineSource hands out the lines of one input, one at a time, without ever
// blocking a caller past its context.
//
// A READ CANNOT BE CANCELLED, so a question that runs out of time leaves its read
// waiting. That read is kept and given to the next question rather than started
// again: a second reader on the same input would race the first for the line
// and one of the two answers would be lost.
type lineSource struct {
	mu      sync.Mutex
	reader  *bufio.Reader
	waiting chan string
}

func newLineSource(in io.Reader) *lineSource { return &lineSource{reader: bufio.NewReader(in)} }

// next is the next line typed, or false when ctx ends first. The end of the
// input is an empty line, which every question here reads as a no.
func (s *lineSource) next(ctx context.Context) (string, bool) {
	line := s.pending()
	select {
	case text := <-line:
		s.finish(line)
		return text, true
	case <-ctx.Done():
		return "", false
	}
}

// pending is the read in flight, started if there is none.
func (s *lineSource) pending() chan string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting == nil {
		s.waiting = make(chan string, 1)
		out := s.waiting
		guard.Go("pair/read a line", func() {
			text, _ := s.reader.ReadString('\n')
			out <- text
		})
	}
	return s.waiting
}

// finish forgets a read that has been answered, so the next question starts its own.
func (s *lineSource) finish(line chan string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting == line {
		s.waiting = nil
	}
}

// terminal is a person at a plain terminal: what they read and what they type.
type terminal struct {
	out   io.Writer
	lines *lineSource
}

func newTerminal(in io.Reader, out io.Writer) *terminal {
	return &terminal{out: out, lines: newLineSource(in)}
}

// say prints one line.
func (t *terminal) say(line string) { fmt.Fprintln(t.out, line) }

// confirm puts a question with the pair package's own choice under it and
// answers whether the person said yes. THERE IS NO DEFAULT: Enter, the end of
// the input, silence and every word but y and yes are a no, because a stray key
// must never let a device in.
func (t *terminal) confirm(ctx context.Context, question string) bool {
	fmt.Fprint(t.out, question+"  "+pair.AskChoice+" ")
	typed, ok := t.lines.next(ctx)
	if !ok {
		// Nothing was typed, so the line the prompt is on is left open: close it.
		fmt.Fprintln(t.out)
		return false
	}
	return saidYes(typed)
}

func saidYes(typed string) bool {
	switch strings.ToLower(strings.TrimSpace(typed)) {
	case "y", "yes":
		return true
	}
	return false
}

// offerScreen is the device that shows the code, as a terminal.
type offerScreen struct{ *terminal }

func (s offerScreen) Show(_ *pair.Code, lines string) { fmt.Fprint(s.out, lines) }

func (s offerScreen) Burned() { s.say(pair.BurnLine) }

func (s offerScreen) Ask(ctx context.Context, label, words string) bool {
	return s.confirm(ctx, pair.AskChatsLine(label, words))
}

// joinScreen is the device that types the code, as a terminal.
type joinScreen struct{ *terminal }

func (s joinScreen) Waiting(words string) { s.say(pair.WaitingChatsLine(words)) }

// serveApprover is how this machine asks the person at it before a device may
// use it: the pair package's question on the terminal, answered y or n.
//
// NIL WHEN NOBODY IS AT A TERMINAL, because a question nobody can read is a no
// and [pair.Host] already says so for a nil approver. Each question is given
// [pair.ConfirmWithin] and no more, the same as the device on the other side.
func serveApprover(ctx context.Context, in *os.File, out io.Writer) func(label, words string) bool {
	if !stdinIsTerminal(in) {
		return nil
	}
	person := newTerminal(in, out)
	return func(label, words string) bool {
		asked, stop := context.WithTimeout(ctx, pair.ConfirmWithin)
		defer stop()
		return person.confirm(asked, pair.AskMachineLine(label, words))
	}
}
