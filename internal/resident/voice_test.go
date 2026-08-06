package resident

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestVoiceSectionRendersPreferenceAndCountsRead(t *testing.T) {
	graph := openStore(t)
	fact, err := graph.RecordFact("", "user", store.FactPreference,
		"keep answers short; no preamble")
	if err != nil {
		t.Fatal(err)
	}

	section := VoiceSection(graph, "explain the benchmark")
	for _, want := range []string{
		"Use plain speech in the user's terms.",
		"Keep internal plumbing and jargon backstage.",
		"Do not open with an apology or preamble.",
		fact.Body,
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("voice section %q does not contain %q", section, want)
		}
	}
	facts, err := graph.ActiveFacts("user", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Uses != 1 || facts[0].LastUsed.IsZero() {
		t.Fatalf("voice preference read telemetry = %+v", facts)
	}
}

func TestVoiceSectionIsBoundedAndUTF8Safe(t *testing.T) {
	graph := openStore(t)
	for index, marker := range []string{"alpha", "beta", "gamma"} {
		body := "keep answers " + marker + " " + strings.Repeat("é", 180+index)
		if _, err := graph.RecordFact("", "user", store.FactPreference, body); err != nil {
			t.Fatal(err)
		}
	}

	section := VoiceSection(graph, "answer this")
	if len(section) > voiceSectionBytes {
		t.Fatalf("voice section = %d bytes, want at most %d", len(section), voiceSectionBytes)
	}
	if !utf8.ValidString(section) {
		t.Fatalf("voice section is not valid UTF-8: %q", section)
	}
}

func TestQuarantinedVoicePreferenceStopsRendering(t *testing.T) {
	graph := openStore(t)
	fact, err := graph.RecordFact("", "user", store.FactPreference,
		"use bullets and keep answers concise")
	if err != nil {
		t.Fatal(err)
	}
	if section := VoiceSection(graph, "write the result"); !strings.Contains(section, fact.Body) {
		t.Fatalf("active preference did not render: %q", section)
	}
	if err := graph.QuarantineFact(fact.Seq, 0, store.FactOriginCLI); err != nil {
		t.Fatal(err)
	}
	if section := VoiceSection(graph, "write the result"); strings.Contains(section, fact.Body) {
		t.Fatalf("quarantined preference still rendered: %q", section)
	}
}

func TestVoicePromptKeepsEmptyNotebookBytes(t *testing.T) {
	graph := openStore(t)
	const prompt = "first line\n\nExact trailing doctrine."
	if got := VoicePrompt(graph, prompt, "answer this"); got != prompt {
		t.Fatalf("empty-notebook prompt changed:\n got %q\nwant %q", got, prompt)
	}
}
