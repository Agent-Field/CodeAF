package appwire

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const FeatureMemoryApplies = "memory.applies"

type MemoryAppliesAsk struct {
	Agent string `json:"agent"`
}

func (p *MemoryAppliesAsk) UnmarshalJSON(raw []byte) error {
	type plain MemoryAppliesAsk
	var v plain
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	*p = MemoryAppliesAsk(v)
	return nil
}

type MemoryApplier interface {
	ApplicableMemory(context.Context, string) (session.MemoryScopeSnapshot, error)
}

var memoryAppliesReads = make(chan struct{}, 4)

func withMemoryAppliesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(MemoryApplier); ok {
		return withFeature(hello, FeatureMemoryApplies)
	}
	return hello
}
func init() {
	methods[FeatureMemoryApplies] = typed(func(e Engine, p MemoryAppliesAsk) (*session.MemoryScopeSnapshot, error) {
		door, ok := e.(MemoryApplier)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine has no scoped memory reading")
		}
		if p.Agent == "" || len(p.Agent) > 256 || strings.TrimSpace(p.Agent) != p.Agent || !utf8.ValidString(p.Agent) || strings.IndexFunc(p.Agent, unicode.IsControl) >= 0 {
			return nil, Errorf(CodeBadParams, "memory.applies needs an agent")
		}
		select {
		case memoryAppliesReads <- struct{}{}:
			defer func() { <-memoryAppliesReads }()
		default:
			return nil, Errorf(CodeBusy, "memory readings are busy")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		snapshot, err := door.ApplicableMemory(ctx, p.Agent)
		if err != nil {
			return nil, err
		}
		return &snapshot, nil
	})
}
