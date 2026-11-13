// Package judgetest is a fake [route.Judge] for other packages' tests: it
// answers from a table a test writes, records every question it was asked,
// and never calls a model.
package judgetest

import (
	"context"
	"errors"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// Model is what the fake says answered.
const Model = "judgetest/fake"

// ErrUnanswered is the fake's reply to a question it holds no answer for. A
// test that forgot to write one sees the intake treat it as the real judge's
// failure, which is no.
var ErrUnanswered = errors.New("judgetest: no answer for this question")

// Asked is one question the fake was asked.
type Asked struct {
	Ask  string
	Item factory.Item
}

// Judge is the fake. Answer is consulted first; else Answers by the question's
// exact words; else Err; else [ErrUnanswered]. The answer it returns carries
// [Model] and route.AnswerKey(ask, it), as the real judge's does.
type Judge struct {
	// Answer decides any question by code, when a test needs the item.
	Answer func(ask string, it factory.Item) (yes bool, why string, err error)
	// Answers decides by the question's words.
	Answers map[string]bool
	// Err is returned for every question not otherwise answered.
	Err error

	mu    sync.Mutex
	asked []Asked
}

// Judge answers one question.
func (f *Judge) Judge(_ context.Context, ask string, it factory.Item) (route.Answer, error) {
	f.mu.Lock()
	f.asked = append(f.asked, Asked{Ask: ask, Item: it})
	f.mu.Unlock()
	var (
		yes bool
		why string
		err error
	)
	switch {
	case f.Answer != nil:
		yes, why, err = f.Answer(ask, it)
	case f.Answers != nil && hasKey(f.Answers, ask):
		yes = f.Answers[ask]
	case f.Err != nil:
		err = f.Err
	default:
		err = ErrUnanswered
	}
	if err != nil {
		return route.Answer{}, err
	}
	if why == "" {
		why = "The fake judge said so."
		if !yes {
			why = "The fake judge said no."
		}
	}
	return route.Answer{Yes: yes, Why: why, Model: Model, Key: route.AnswerKey(ask, it)}, nil
}

// Asked is every question asked so far, oldest first.
func (f *Judge) Asked() []Asked {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Asked(nil), f.asked...)
}

func hasKey(m map[string]bool, k string) bool {
	_, ok := m[k]
	return ok
}

var _ route.Judge = (*Judge)(nil)
