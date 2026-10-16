package session

import (
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// messagePresentation records who a producer meant to address. The provider's
// content remains untouched; Text, when supplied, is the exact human portion of
// a mixed message. Operational records remain available behind disclosure.
//
// SkillsBlock is provenance of another kind: the exact bytes
// [Agent.attachTurnSkillsLocked] spliced onto the copy of THIS message the
// model reads, so shapeEntries can take them off the display by record rather
// than by pattern — a block the person typed themselves has no mark and keeps
// every word. It is memory only: new journal writes keep the typed words, so
// those restored messages need no mark. An older compacted journal can still
// carry the saved block without a mark.
//
// On a PERSON'S line (Audience [audiencePerson]) Text is their own words where
// the door that sent the message said them apart from what the model reads,
// Attachments are what rode on it, and Reads are the reads of references its
// words were written from ([Agent.SubmitMessage]). None of them changes the
// content; all are absent from every line no such door wrote.
type messagePresentation struct {
	Audience    string              `json:"audience"`
	Text        *string             `json:"text,omitempty"`
	Interrupted bool                `json:"interrupted,omitempty"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
	Reads       []MessageRead       `json:"reads,omitempty"`
	SkillsBlock string              `json:"-"`
}

// person is the part of a mark a person's message carries into the record: the
// mark without the memory-only skills provenance, or nil when there is nothing
// of the person's to keep. It is what a line is written with and what a
// re-journaled copy of the line is marked with again.
func (m *messagePresentation) person() *messagePresentation {
	if m == nil || m.Audience != audiencePerson {
		return nil
	}
	kept := *m
	kept.SkillsBlock = ""
	return &kept
}

// Message identity follows its immutable content allocation, as reasoning repair
// already does. Equal words from different speakers or later responses cannot
// inherit one another's audience. Shallow rewind, repair and compaction copies
// retain identity; synthesized summaries deliberately start with their own.
type presentationIndex struct {
	mu      sync.Mutex
	entries map[*ai.ContentPart]messagePresentation
}

func (p *presentationIndex) remember(message ai.Message, mark *messagePresentation) {
	if p == nil || mark == nil || len(message.Content) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = make(map[*ai.ContentPart]messagePresentation)
	}
	p.entries[&message.Content[0]] = *mark
}

// forget drops one message's mark. It is for a copy whose only reader has
// already read it — the record's own copy of a person's line, once the line is
// written ([Agent.recordUserLocked]).
func (p *presentationIndex) forget(message ai.Message) {
	if p == nil || len(message.Content) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, &message.Content[0])
}

// retain keeps the marks of the messages given and drops every other one.
//
// A MARK IS KEYED BY ITS MESSAGE'S FIRST PART, AND THE KEY KEEPS THE WHOLE PART
// ARRAY ALIVE. A message a compaction folded away or a rewind cut is gone from
// the conversation, but its mark would hold its content — a person's message
// can carry hundreds of kilobytes of paste the model was sent — for as long as
// the session lives. So at each boundary that lets messages go, the index is
// cut back to the messages something still holds. The record is untouched: a
// line's mark is on the line, and a reopen reads it from there.
//
// It is one walk of what is live and one of the index, at a boundary that has
// already walked the transcript; nothing per token or per message pays for it.
func (p *presentationIndex) retain(live ...[]ai.Message) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return
	}
	kept := make(map[*ai.ContentPart]messagePresentation)
	for _, messages := range live {
		for index := range messages {
			if len(messages[index].Content) == 0 {
				continue
			}
			key := &messages[index].Content[0]
			if mark, ok := p.entries[key]; ok {
				kept[key] = mark
			}
		}
	}
	p.entries = kept
}

// size is how many marks the index holds.
func (p *presentationIndex) size() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

func (p *presentationIndex) of(message ai.Message) *messagePresentation {
	if p == nil || len(message.Content) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	mark, ok := p.entries[&message.Content[0]]
	if !ok {
		return nil
	}
	return &mark
}

// personWords keeps the person's words separate from the model's skills context.
// THE MODEL'S SKILLS CONTEXT IS NOT CONVERSATION. It rides the copy in a.messages
// the provider reads, and quoting it as the person's message would put words in
// their mouth. The strip is by PROVENANCE, never by pattern: only a message
// attachTurnSkillsLocked marked, and only the exact suffix it appended, comes
// off. A skills block the person typed or pasted themselves keeps every word.
// New journal writes keep these words too; an older compacted journal has no
// injection mark, so its saved bytes remain untouched. A nil index has no marks.
func (p *presentationIndex) personWords(message ai.Message) string {
	text := messageContentText(message)
	if mark := p.of(message); mark != nil && mark.SkillsBlock != "" {
		return strings.TrimSuffix(text, mark.SkillsBlock)
	}
	return text
}

func humanPresentation(text string) *messagePresentation {
	return &messagePresentation{Audience: "human", Text: &text}
}

func (a *Agent) recordPresentedAssistant(message ai.Message, reasoning provider.MessageReasoning, mark *messagePresentation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presentation == nil {
		a.presentation = &presentationIndex{}
	}
	// A caller may reuse an ai.Message value. Recording creates a new
	// occurrence, while later transcript snapshots remain shallow copies.
	message.Content = append([]ai.ContentPart(nil), message.Content...)
	a.presentation.remember(message, mark)
	a.alignReasoningLocked()
	a.messages = append(a.messages, message)
	a.messageReasoning = append(a.messageReasoning, reasoning)
	if a.file != nil {
		a.file.appendReasonedMessage(message, reasoning)
	}
	a.chatlog.post(message)
}

// An interrupted response has not completed. Only its explicit address to
// the person can keep it in conversation; ordinary partial narration remains
// operational. The exact streamed words are retained in either case.
func interruptedPresentation(text string) *messagePresentation {
	if _, addressed := UserFacingUpdate(text); addressed {
		mark := humanPresentation(text)
		mark.Interrupted = true
		return mark
	}
	return &messagePresentation{Audience: "operational", Interrupted: true}
}

// Older records cannot prove the audience of mixed prose. Only the two exact,
// standalone reserved interruption records are treated as operational. Quoted
// text and mixed replies remain untouched. New records always write an explicit
// audience, so even these exact words can be deliberately addressed to a person.
func legacyPresentation(entry sessionEntry) *messagePresentation {
	if entry.Role != "assistant" || len(entry.ToolCalls) != 0 {
		return nil
	}
	if entry.Content == cutDroppedCallNote(nil) || entry.Content == cutDroppedCallNote(errMarkCut) {
		return &messagePresentation{Audience: "operational"}
	}
	return nil
}
