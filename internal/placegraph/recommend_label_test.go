package placegraph

import (
	"errors"
	"testing"
)

// A real model shown "p1: Release pipeline" answered "Release pipeline"
// (deepseek/deepseek-v4.1-flash, 2026-10-09), and the whole answer was
// refused. A name the model was shown points at that place exactly as its
// label does; anything else is still refused whole.
func TestAnAnswerMayNameAShownPlaceButNeverAnUnshownOne(t *testing.T) {
	names := []string{"Release pipeline", "Kitchen", "Twins", "twins"}
	for _, c := range []struct {
		raw  string
		want int
		err  bool
	}{
		{`{"place":"p2","confidence":80}`, 1, false},
		{`{"place":"Release pipeline","confidence":96}`, 0, false},
		{`{"place":"  release   PIPELINE ","confidence":96}`, 0, false},
		{`{"place":"none","confidence":10}`, -1, false},
		{`{"place":"Garden","confidence":90}`, -1, true},
		{`{"place":"Twins","confidence":90}`, -1, true}, // two shown places share it
		{`{"place":"Release","confidence":90}`, -1, true},
		{`{"place":"p9","confidence":90}`, -1, true},
	} {
		i, _, err := readFileAnswer(c.raw, names)
		if (err != nil) != c.err || (err == nil && i != c.want) {
			t.Errorf("%s: got %d, %v", c.raw, i, err)
		}
	}
	sg, err := readSuggestAnswer(`{"belong":true,"chats":["c1","c2"],"use":"Kitchen","name":"","under":"root","confidence":90}`, 2, []string{"Release pipeline", "Kitchen"}, nil)
	if err != nil || sg.use != 1 {
		t.Fatalf("use by name: %+v, %v", sg, err)
	}
	if _, err := readSuggestAnswer(`{"belong":true,"chats":["c1"],"use":"","name":"Work","under":"Projects","confidence":90}`, 1, nil, []string{"Home"}); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("an unshown parent was accepted: %v", err)
	}
}

func TestAFilingAnswerThatNamesThePlaceIsOffered(t *testing.T) {
	// The organiser files a chat. This package no longer turns a model
	// answer into an offer; the parser still has to read the name.
	i, conf, err := readFileAnswer(`{"place": "Release pipeline", "confidence": 96}`, []string{"Release pipeline"})
	if err != nil || i != 0 || conf != 96 {
		t.Fatalf("%d %d %v", i, conf, err)
	}
}
