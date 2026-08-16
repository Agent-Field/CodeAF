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
	if learned := len(section) - len(voiceDoctrine); learned > voiceSectionBytes {
		t.Fatalf("learned voice = %d bytes, want at most %d", learned, voiceSectionBytes)
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

// TestVoiceRegisterInstallsOnAnEmptyNotebook pins the thing that was wrong: the
// anti-jargon doctrine used to be conditional on the notebook already carrying a
// learned voice preference, so a fresh machine — the one install whose user has
// the least vocabulary for any of this — got no register instruction at all.
// The register now rides every prompt, and the exact byte difference from the
// bare prompt is that register and nothing else.
func TestVoiceRegisterInstallsOnAnEmptyNotebook(t *testing.T) {
	graph := openStore(t)
	const prompt = "first line\n\nExact trailing doctrine."
	got := VoicePrompt(graph, prompt, "answer this")
	if want := prompt + "\n\n" + VoiceRegister; got != want {
		t.Fatalf("empty-notebook prompt:\n got %q\nwant %q", got, want)
	}
	for _, word := range []string{"node", "leaf", "graph", "splice", "worker",
		"charter", "craft", "rail", "firing"} {
		if !strings.Contains(got, word) {
			t.Fatalf("register does not name the backstage word %q: %q", word, got)
		}
	}
}

// TestVoiceRegisterIsAStablePrefixOfLearnedVoice is the cache-shape guard.
// Learning a first voice preference must APPEND to what a cached prompt already
// paid for rather than rewriting the segment: the register is a byte-exact
// prefix of the full section in both directions.
func TestVoiceRegisterIsAStablePrefixOfLearnedVoice(t *testing.T) {
	graph := openStore(t)
	const prompt = "first line\n\nExact trailing doctrine."
	before := VoicePrompt(graph, prompt, "answer this")
	if _, err := graph.RecordFact("", "user", store.FactPreference,
		"keep answers short; no preamble"); err != nil {
		t.Fatal(err)
	}
	after := VoicePrompt(graph, prompt, "answer this")
	if after == before {
		t.Fatal("a learned preference did not reach the prompt")
	}
	if !strings.HasPrefix(after, before) {
		t.Fatalf("learned voice rewrote the cached prefix:\nbefore %q\n after %q", before, after)
	}
}
