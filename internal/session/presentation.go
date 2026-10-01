package session

import (
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
// every word. It is memory only: the journal keeps the typed words, so a
// restored message has no block and nothing to strip.
type messagePresentation struct {
	Audience    string  `json:"audience"`
	Text        *string `json:"text,omitempty"`
	Interrupted bool    `json:"interrupted,omitempty"`
	SkillsBlock string  `json:"-"`
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
