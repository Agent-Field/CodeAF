package evals

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

func TestScoreLookValidatesJudgeOutput(t *testing.T) {
	var capture bytes.Buffer
	if err := png.Encode(&capture, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	look := Look{Screen: "deals", Recipe: "add-view/board"}
	valid := Score{Hierarchy: 4, Legibility: 4, RecipeFit: 5, Evidence: "The stages follow the lifecycle."}
	model := testJudge(func(_ context.Context, language string, got Look, image []byte) (Score, error) {
		if got != look || language != "Use one primary action." || !bytes.Equal(image, capture.Bytes()) {
			t.Fatal("judge did not receive the design language, recipe and look")
		}
		return valid, nil
	})
	got, err := ScoreLook(t.Context(), model, "Use one primary action.", look, 2, capture.Bytes())
	if err != nil || got != valid {
		t.Fatalf("score = %+v, %v", got, err)
	}
	valid.Legibility = 6
	if _, err := ScoreLook(t.Context(), model, "Use one primary action.", look, 2, capture.Bytes()); err == nil {
		t.Fatal("judge score outside the rubric passed")
	}
}

type testJudge func(context.Context, string, Look, []byte) (Score, error)

func (j testJudge) Score(ctx context.Context, language string, look Look, data []byte) (Score, error) {
	return j(ctx, language, look, data)
}
