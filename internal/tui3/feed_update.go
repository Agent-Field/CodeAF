package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
)

// The probe belongs to a response, so reasoning fragments and queued user
// messages cannot split a control marker or let part of it reach the screen.
type userUpdateStream struct {
	turn                        int
	pending                     string
	resolved, marked, trimSpace bool
}

func (f *feed) updateText(text string) string {
	p := &f.updateDelivery
	if p.turn != f.turn {
		*p = userUpdateStream{turn: f.turn}
	}
	if !p.resolved {
		p.pending += text
		prefix := strings.TrimLeft(p.pending, " \t\r\n")
		if strings.HasPrefix(session.UserUpdatePrefix, prefix) && prefix != session.UserUpdatePrefix {
			return ""
		}
		p.resolved = true
		if body, marked := session.UserFacingUpdate(p.pending); marked {
			text, p.marked, p.trimSpace = body, true, true
		} else {
			text = p.pending
		}
		p.pending = ""
	}
	if p.trimSpace {
		text = strings.TrimLeft(text, " \t\r\n")
		if text != "" {
			p.trimSpace = false
		}
	}
	return text
}

// An incomplete marker that ends as ordinary text must not lose characters.
func (f *feed) flushUpdatePrefix() {
	if text := f.updateDelivery.pending; text != "" {
		f.updateDelivery.pending = ""
		f.sayVisibleStream(text, false)
	}
}

// A marked preamble is a delivered message, even though this response also
// calls tools. Ordinary preambles keep their original operational classification.
func (f *feed) confirmUserUpdate() {
	f.flushUpdatePrefix()
	if f.updateDelivery.marked {
		f.confirmResponse()
	}
	f.updateDelivery = userUpdateStream{}
}
