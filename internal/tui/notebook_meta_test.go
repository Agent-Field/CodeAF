package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestNotebookRendersTraitAndDimChannelConfidence(t *testing.T) {
	model := Model{notebookOpen: true, notebookFacts: []store.Fact{{
		Seq: 42, Time: time.Now(), Scope: "trait:proposal-appetite", Kind: store.FactTrait,
		Channel: store.FactChannelDistilled, Confidence: .2, Status: store.FactActive,
		Body: `{"value":{"acceptance":0.5},"n":8,"updated":"2026-08-06T12:00:00Z"}`,
	}}}
	view := ansi.Strip(model.renderNotebookSurface(120, 0, false))
	if !strings.Contains(view, "tentative") || !strings.Contains(view, "acceptance") {
		t.Fatalf("notebook trait confidence missing:\n%s", view)
	}
}
