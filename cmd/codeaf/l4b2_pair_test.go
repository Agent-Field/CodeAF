package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
)

type l4B2OfferScreen struct{ lines string }

func (s *l4B2OfferScreen) Show(_ *pair.Code, lines string)        { s.lines = lines }
func (*l4B2OfferScreen) Burned()                                  {}
func (*l4B2OfferScreen) Ask(context.Context, string, string) bool { return false }

// L4-M5a: The chat offer adapter supplies both directions through the selected service; the terminal stays a code road.
func TestL4B2ChatOfferDirections(t *testing.T) {
	code, err := pair.NewCode(time.Unix(1_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	screen := &l4B2OfferScreen{}
	ui := chatOfferUI{OfferUI: screen, service: "http://127.0.0.1:8901"}
	terminal := pair.ChatLines(code, ui.service)
	ui.Show(code, terminal)
	want := "paste the link the new device shows, or on it run codeaf pair " + code.Shown() + " --via http://127.0.0.1:8901"
	if !strings.Contains(screen.lines, want) {
		t.Fatal(screen.lines)
	}
	if strings.Contains(terminal, "paste the link") {
		t.Fatal("terminal offered an absent field")
	}
}
