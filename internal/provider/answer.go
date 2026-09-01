package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHAT IS THE ANSWER AND WHAT IS THE WORKING ──────────────────────────────
//
// THIS FILE IS THE ONE PLACE THAT DECIDES, and it decides with the wire in
// hand. Everything above it — the session's transcript, the surface's answer
// block, the thought window that collapses to `thought for 6s` — reads the
// decision and never re-takes it, because a second reader is a second opinion
// and the two disagreeing is the whole of #225: a reply drawn as thinking, a
// turn that looks like it never answered, a `<think>` tag typed into the
// person's answer as though the model had meant to write it.
//
// THREE FACTS ABOUT THE WIRE FORCE THE SHAPE, and none of them is about any
// particular model:
//
//  1. AN ENDPOINT MAY SPLIT THE CHANNELS. `content` is the answer and
//     `reasoning` / `reasoning_content` / `reasoning_text` is the working
//     (sse.go's [streamDelta]). This is the ordinary case and nothing here
//     disturbs it.
//
//  2. AN ENDPOINT MAY NOT SPLIT THEM, and then the working arrives INSIDE
//     `content`, fenced in a `<think>` tag the endpoint never stripped. It is
//     the shape every OpenAI-compatible gateway in front of a raw open model
//     produces — vLLM, llama.cpp, Ollama, LM Studio, anything reached through
//     AFORGE_BASE_URL — and until this file existed the tag and the model's
//     private working were typed into the answer and recorded in the
//     transcript as words the model had said out loud.
//
//  3. AN ENDPOINT MAY PUT THE WHOLE REPLY ON THE REASONING CHANNEL AND SEND NO
//     CONTENT AT ALL. Measured on 2026-09-01 against the real router:
//     `qwen/qwen3.5-9b` answered a two-sentence question with zero content
//     deltas and 3,066 characters of `reasoning`, and one call inside a live
//     `deepseek/deepseek-v4-flash` conversation did the same with 76 reasoning
//     deltas and no content. A response like that is not a model that declined
//     to answer — the answer is sitting in the working — and reading it as an
//     empty reply is how a turn ends up costing money, retrying, and telling
//     the person nothing (internal/session's [turnBroke]).
//
// THE DECISION IS MADE ONCE AND SPENT TWICE, which is the property that makes
// the live drawing and the recorded message agree by construction: the same
// [answerSplit] feeds the observer that the surface draws from AND the
// accumulated `content` that becomes the assistant message. A replay cannot
// show something streaming did not, because there is only one account of it.
//
// NOTHING HERE KNOWS A MODEL'S NAME. The rules are about the SHAPE of what
// arrived — a fenced region at the head of the answer, an answer that is empty
// when the stream ends — for the reason every other law in this package is
// shape-based: a table of model names is a table that is wrong the week after
// it is written.

// thinkTags are the wire spellings of a working region fenced inside the answer
// channel. It is a list of WIRE SPELLINGS and not a list of models, in exactly
// the sense [streamDelta]'s three reasoning field names are: an endpoint uses
// one of them consistently, and holding all of them is what makes an unfamiliar
// gateway lossless rather than mangled.
var thinkTags = []struct{ open, close string }{
	{"<think>", "</think>"},
	{"<thinking>", "</thinking>"},
}

// splitPhase is where in one response's answer channel the reader has got to.
type splitPhase int

const (
	// splitLeading is the head of the answer, where a `<think>` fence may still
	// open. Nothing has been given to the person yet.
	splitLeading splitPhase = iota
	// splitFenced is inside a working region: every byte is the model's own
	// working until the fence closes.
	splitFenced
	// splitOpen is the ordinary state — every byte of content is answer, and a
	// `<think>` written from here on is a model TALKING about the tag rather
	// than opening one.
	splitOpen
)

// answerSplit reads one response's two channels in wire order and answers, for
// every piece of it, whether it is the ANSWER or the WORKING.
//
// A FENCE IS ONLY RECOGNISED AT THE HEAD OF THE ANSWER, and the narrowness is
// deliberate. A model that opens with `<think>` is an endpoint that did not
// strip its own template; a model that writes `<think>` in the fourth paragraph
// is answering a question about the tag, and an answer that had its own words
// eaten because it quoted a tag would be a worse defect than the one this
// closes. So the fence may open while nothing has been said, and never after.
type answerSplit struct {
	answer  strings.Builder
	working strings.Builder
	// held is the tail of the answer channel that is withheld because it may yet
	// turn out to be half of a fence. It is at most one tag long and it is
	// flushed by [answerSplit.flush] when the stream ends, so nothing the model
	// wrote can be lost to a tag that never arrived.
	held  string
	phase splitPhase
	// tools records that this response asked for a call. A turn that called a
	// tool and said nothing is a turn that behaved perfectly, so it is never a
	// candidate for the promotion below.
	tools bool
}

// content reads one delta of the ANSWER channel and returns the two things it
// turned out to contain, in that order. Either may be empty; both are empty
// while a fence is still being spelled out across two deltas.
func (s *answerSplit) content(delta string) (answer, working string) {
	if delta == "" {
		return "", ""
	}
	s.held += delta
	var out, thought strings.Builder
	// THE TWO BUILDERS ARE WRITTEN ONCE, AT THE ONE EXIT. Every road out of the
	// loop below owes the response's running totals the same update — the whole
	// promotion rests on `answer` and `working` being complete — and a loop with
	// four returns in it is four chances to owe it and not pay.
	defer func() {
		s.answer.WriteString(out.String())
		s.working.WriteString(thought.String())
	}()
	for s.held != "" {
		switch s.phase {
		case splitLeading:
			// Leading space is held rather than emitted: a fence that opens after
			// a newline is still a fence at the head of the answer, and a blank
			// row given to the person before the answer starts is a blank row
			// nobody asked for.
			trimmed := strings.TrimLeft(s.held, " \t\r\n")
			if trimmed == "" {
				return out.String(), thought.String()
			}
			if tag, ok := openingTag(trimmed); ok {
				s.phase = splitFenced
				s.held = trimmed[len(tag.open):]
				continue
			}
			if partialTag(trimmed) {
				return out.String(), thought.String()
			}
			// Nothing here can become a fence, so this response has an ordinary
			// answer and will never have a fenced region.
			s.phase = splitOpen

		case splitFenced:
			closer, at := closingTag(s.held)
			if at < 0 {
				// Everything except a possible half-written closer is working.
				keep := tagTail(s.held)
				thought.WriteString(s.held[:len(s.held)-keep])
				s.held = s.held[len(s.held)-keep:]
				return out.String(), thought.String()
			}
			thought.WriteString(s.held[:at])
			// The break the endpoint's template puts between the fence and the
			// reply is the template's and not the model's, so the answer starts
			// at its first real word.
			s.held = strings.TrimLeft(s.held[at+len(closer):], " \t\r\n")
			s.phase = splitOpen

		case splitOpen:
			out.WriteString(s.held)
			s.held = ""
		}
	}
	return out.String(), thought.String()
}

// reasoning records a delta that arrived on the reasoning channel. It is
// already the working by the endpoint's own account; this only has to remember
// it, in case the answer channel turns out to be empty.
func (s *answerSplit) reasoning(delta string) { s.working.WriteString(delta) }

// sawTools records that this response asked for a tool call.
func (s *answerSplit) sawTools() { s.tools = true }

// flush ends the response's answer channel and hands back whatever was being
// withheld against a fence that never completed. A tag half-spelled when the
// stream stopped is text the model wrote, and the person gets it.
func (s *answerSplit) flush() (answer, working string) {
	held := s.held
	s.held = ""
	if held == "" {
		return "", ""
	}
	if s.phase == splitFenced {
		s.working.WriteString(held)
		return "", held
	}
	s.phase = splitOpen
	s.answer.WriteString(held)
	return held, ""
}

// promote is the last question the split asks, and it is asked once, when the
// stream has ended: WAS THE WORKING THE ANSWER?
//
// It says yes only on the shape that has no other honest reading — a response
// that asked for nothing, said nothing, and thought at length. Every other
// response keeps the channels the endpoint put its words on.
//
// AN UNCLOSED FENCE COMES OUT HERE TOO, and by the same rule rather than by a
// second one: a model that opened `<think>` and never closed it produced an
// empty answer and a long working, which is this shape exactly.
func (s *answerSplit) promote(prose bool) (string, bool) {
	if !prose || s.tools || strings.TrimSpace(s.answer.String()) != "" {
		return "", false
	}
	working := s.working.String()
	if strings.TrimSpace(working) == "" {
		return "", false
	}
	s.answer.WriteString(working)
	return working, true
}

// splitWhole is the same decision taken over an answer that arrived in ONE
// PIECE rather than a delta at a time, which is what every non-streamed call
// gets back (client.go's [Client.completionInOnePiece]).
//
// It is this file's function and not a second reading of the same rules, for
// the reason the two transports share [Client.completionInOnePiece] at all: a
// law that had one spelling for streams and another for whole bodies is a law
// that has already started to drift.
func splitWhole(text string) (answer, working string) {
	var split answerSplit
	gotAnswer, gotWorking := split.content(text)
	heldAnswer, heldWorking := split.flush()
	return gotAnswer + heldAnswer, gotWorking + heldWorking
}

// openingTag reports the fence that text opens with, if any.
func openingTag(text string) (struct{ open, close string }, bool) {
	for _, tag := range thinkTags {
		if strings.HasPrefix(text, tag.open) {
			return tag, true
		}
	}
	return struct{ open, close string }{}, false
}

// closingTag finds the earliest closing fence in text, and which one it was.
func closingTag(text string) (string, int) {
	closer, at := "", -1
	for _, tag := range thinkTags {
		if i := strings.Index(text, tag.close); i >= 0 && (at < 0 || i < at) {
			closer, at = tag.close, i
		}
	}
	return closer, at
}

// partialTag reports that text is a proper prefix of some opening fence, and so
// may still become one when the next delta arrives.
func partialTag(text string) bool {
	for _, tag := range thinkTags {
		if len(text) < len(tag.open) && strings.HasPrefix(tag.open, text) {
			return true
		}
	}
	return false
}

// tagTail is how many bytes at the end of text must be withheld because they
// are a proper prefix of a closing fence. Zero for text that ends in the middle
// of an ordinary word, which is nearly every delta.
func tagTail(text string) int {
	for _, tag := range thinkTags {
		for n := len(tag.close) - 1; n > 0; n-- {
			if n <= len(text) && strings.HasSuffix(text, tag.close[:n]) {
				return n
			}
		}
	}
	return 0
}

// splitOnePiece applies THE ONE DECISION to a response that arrived whole.
//
// Two things can be wrong with such a body and both are the shapes answer.go
// exists for: the working may be fenced inside the answer, and the answer may
// be missing because the whole reply went out on the reasoning field.
//
// THE SECOND DECODE IS PAID ONLY ON THE BROKEN SHAPE. Reading a megabyte of
// completion twice to recover one string is the cost [servedResponse] was built
// to avoid, so the reasoning field is looked for only when the answer is empty
// and nothing was called — a response that has already failed every other
// reading, where the alternative is a reply thrown away.
func (c *Client) splitOnePiece(ctx context.Context, response *ai.Response, payload []byte) {
	if response == nil || len(response.Choices) == 0 {
		return
	}
	prose := ProseAnswerAsked(ctx)
	message := &response.Choices[0].Message
	if text := messageText(*message); strings.TrimSpace(text) != "" {
		if answer, working := splitWhole(text); working != "" {
			// A fence that swallowed the WHOLE body leaves the working as the
			// only thing the model wrote, which is [answerSplit.promote]'s shape
			// said on this transport — and it is allowed for the same callers and
			// no others.
			if prose && strings.TrimSpace(answer) == "" && len(message.ToolCalls) == 0 {
				answer = working
			}
			message.Content = []ai.ContentPart{{Type: "text", Text: answer}}
		}
		return
	}
	if !prose || len(message.ToolCalls) > 0 {
		return
	}
	var carried struct {
		Choices []struct {
			Message struct {
				Reasoning        string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
				ReasoningText    string `json:"reasoning_text"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &carried); err != nil || len(carried.Choices) == 0 {
		return
	}
	working := firstNonBlank(
		carried.Choices[0].Message.Reasoning,
		carried.Choices[0].Message.ReasoningContent,
		carried.Choices[0].Message.ReasoningText,
	)
	if working == "" {
		return
	}
	message.Content = []ai.ContentPart{{Type: "text", Text: working}}
}

// messageText is one assistant message's words, joined.
func messageText(message ai.Message) string {
	var b strings.Builder
	for _, part := range message.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

// firstNonBlank is the first of the wire spellings that carried anything.
func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ── WHO THE PROMOTION IS FOR ────────────────────────────────────────────────
//
// A CALL THAT ASKS FOR PROSE AND A CALL THAT ASKS FOR A VALUE ARE NOT THE SAME
// QUESTION, and the promotion is only ever right for the first.
//
// The turn's reply is read by a person: if the whole of it came back in the
// working, the working is the reply and handing it over is the only honest
// thing to do. A gate is the other kind — it asks for `{"work": false}` and its
// answer is read by a parser (internal/session's route_judge.go, checkpoint.go)
// — and there, a model that deliberated in its working and never wrote the
// object did NOT answer. Promoting a deliberation into that slot hands the
// parser a draft to salvage a verdict out of, which is a task started off a
// sentence the model was still arguing with itself about. Measured on the real
// router on 2026-09-01, roughly one gate call in ten comes back with an empty
// answer and a full working, so this is not a corner.
//
// SO THE CALLER SAYS WHICH KIND ITS CALL IS, in the same way it already says
// who is waiting on it ([WithRoutingIntent]) and what it is for
// ([WithCallTag]): facts about the CALL, stated by the code making it, never
// guessed from the words that come back. The default is off, because a value
// slot filled with prose is the worse failure.
//
// THE FENCE IS STRIPPED EITHER WAY. A `<think>` region is junk in a parser's
// input exactly as it is junk in a person's answer, so nothing gates that half.
type proseAnswerKey struct{}

// WithProseAnswer marks a call whose words are A REPLY SOMEBODY READS rather
// than a value something parses. It is what turns the promotion on.
func WithProseAnswer(ctx context.Context) context.Context {
	return context.WithValue(ctx, proseAnswerKey{}, true)
}

// ProseAnswerAsked reports the mark above. It is exported for
// [MessageReasoningFrom]'s reason: a Completer test double has to be able to
// assert the same contract the real adapter reads, without learning this
// package's private context key.
func ProseAnswerAsked(ctx context.Context) bool {
	asked, _ := ctx.Value(proseAnswerKey{}).(bool)
	return asked
}
