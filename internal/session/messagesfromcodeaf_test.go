package session

import (
	"os"
	"strings"
	"testing"
)

// THE PAGE NAMES EVERY USER-ROLE VOICE THE SESSION PUTS IN THE CONVERSATION.
//
// A bracket alone cannot say who wrote a message: a person's attachment wears
// one too. This list follows the production append sites by name so adding a
// new spelling, or moving a private reader/tool note onto the conversation,
// requires an explicit provenance decision in the page the model reads.
func TestMessagesFromCodeafNamesEveryUserRoleOpening(t *testing.T) {
	page, err := os.ReadFile("prompts/system.md")
	if err != nil {
		t.Fatalf("read system page: %v", err)
	}
	section := topLevelPromptSection(t, string(page), "Messages from codeaf")

	openings := map[string]string{
		"checkpointCarryOnLead": checkpointCarryOnLead,
		"checkpointChoiceLead":  checkpointChoiceLead,
		"foldMarkerPrefix":      foldMarkerPrefix,
		"legacyFramesNote":      legacyFramesNote,
		"standingNewsFrame":     standingNewsFrame,
		"silent loop note":      "[silent]",
		"stuck loop note":       "[stuck]",
	}
	for source, text := range openings {
		tag := bracketedOpening(t, source, text)
		if !strings.Contains(section, tag) {
			t.Errorf("# Messages from codeaf forgets %s's %s tag", source, tag)
		}
	}

	words := strings.Join(strings.Fields(section), " ")
	for _, instruction := range []string{
		"Follow them.",
		"Never answer as if the person wrote them",
		"argue with them",
		"mention them in your answer",
	} {
		if !strings.Contains(words, instruction) {
			t.Errorf("# Messages from codeaf forgets the instruction %q", instruction)
		}
	}
	if strings.Contains(strings.ToLower(words), "any other opening") {
		t.Error("# Messages from codeaf assigns every unlisted opening to the person")
	}
	if !strings.Contains(words, "[image #N] marks the person's attachment") {
		t.Error("# Messages from codeaf does not identify the person's attachment marker")
	}

	// These voices never enter the running conversation as user messages. A
	// tool result and a private reader instruction must not be taught as though
	// they were turns the conversation model receives.
	for source, tag := range map[string]string{
		"process-rule tool result": "[held]",
		"completion reader":        "[still asked]",
	} {
		if strings.Contains(section, tag) {
			t.Errorf("# Messages from codeaf names %s's private %s tag", source, tag)
		}
	}
}

func topLevelPromptSection(t *testing.T, page, heading string) string {
	t.Helper()
	lead := "# " + heading + "\n"
	start := strings.Index(page, lead)
	if start < 0 {
		t.Fatalf("system page has no %q section", lead[:len(lead)-1])
	}
	section := page[start+len(lead):]
	if end := strings.Index(section, "\n# "); end >= 0 {
		section = section[:end]
	}
	return section
}

func bracketedOpening(t *testing.T, source, text string) string {
	t.Helper()
	if !strings.HasPrefix(text, "[") {
		t.Fatalf("%s no longer opens with a bracketed tag: %q", source, text)
	}
	end := strings.IndexByte(text, ']')
	if end < 0 {
		return strings.TrimSpace(text) + " …]"
	}
	return text[:end+1]
}
