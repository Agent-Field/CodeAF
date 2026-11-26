package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Compile is the scoped command's adapter to the manifest lane. The compiler
// gets definition bytes and type shapes and cannot publish or open records.
func (b *appBackend) Compile(ctx context.Context, verb string, def schema.Definition, types schema.TypeSet, raw json.RawMessage) (builder.CompiledChange, error) {
	if err := ctx.Err(); err != nil {
		return builder.CompiledChange{}, err
	}
	switch verb {
	case "recipe":
		var input schema.Recipe
		if err := json.Unmarshal(raw, &input); err != nil {
			return builder.CompiledChange{}, err
		}
		result, err := schema.CompileRecipe(def, types, input)
		compiled := builder.CompiledChange{App: result.App, Screen: result.Screen, ScreenID: input.Screen}
		if compiled.ScreenID == "" {
			compiled.ScreenID = input.ID
		}
		return compiled, err
	case "markup":
		var input schema.ScreenMarkup
		if err := json.Unmarshal(raw, &input); err != nil {
			return builder.CompiledChange{}, err
		}
		screen, err := schema.CompileScreenMarkup(def, types, input)
		return builder.CompiledChange{ScreenID: input.Screen, Screen: screen}, err
	default:
		return builder.CompiledChange{}, fmt.Errorf("unknown screen command %q", verb)
	}
}
