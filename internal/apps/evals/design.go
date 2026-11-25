package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
)

type Look struct {
	Screen string `yaml:"screen" json:"screen"`
	Recipe string `yaml:"recipe" json:"recipe"`
}

// DesignJudge receives one synthetic PNG and the platform's design language.
type DesignJudge interface {
	Score(context.Context, string, Look, []byte) (Score, error)
}

type Score struct {
	Hierarchy  int    `json:"hierarchy"`
	Legibility int    `json:"legibility"`
	RecipeFit  int    `json:"recipe_fit"`
	Evidence   string `json:"evidence"`
}

type DesignResult struct {
	Look
	Theme   string `json:"theme"`
	Width   int    `json:"width"`
	Score   *Score `json:"score,omitempty"`
	Problem string `json:"problem,omitempty"`
}

func ScoreLook(ctx context.Context, judge DesignJudge, language string, look Look, width int, data []byte) (Score, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Score{}, fmt.Errorf("look did not return a PNG: %w", err)
	}
	if config.Width != width {
		return Score{}, fmt.Errorf("look width is %d, expected %d", config.Width, width)
	}
	score, err := judge.Score(ctx, language, look, data)
	if err != nil {
		return Score{}, err
	}
	for _, value := range []int{score.Hierarchy, score.Legibility, score.RecipeFit} {
		if value < 1 || value > 5 {
			return Score{}, errors.New("design scores must be from 1 to 5")
		}
	}
	if score.Evidence == "" {
		return Score{}, errors.New("design score needs visible evidence")
	}
	return score, nil
}
