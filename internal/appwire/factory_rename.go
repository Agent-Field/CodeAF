package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// ── factory.rename: the app's own name for a factory ──────────────────────
//
// A person names a factory as it appears in their app: the name is kept on the
// app's own record of the connection (never sent to the factory), and it wins
// over the name the factory reports. An empty name goes back to the default:
// the factory's own name, or for the factory on this Mac `<login>'s factory`
// once GitHub is connected.

// FeatureFactoryRename is announced in hello when the engine serves
// factory.rename.
const FeatureFactoryRename = "factory.rename"

// MaxFactoryName is the longest name a person gives a factory, in characters.
const MaxFactoryName = 60

// FactoryRenameParams names one connected factory.
type FactoryRenameParams struct {
	Factory string `json:"factory"`
	Name    string `json:"name"`
}

// FactoryRenamer is the engine's half of factory.rename.
type FactoryRenamer interface {
	FactoryRename(ctx context.Context, id, name string) error
}

func withFactoryRenameFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRenamer); ok {
		hello = withFeature(hello, FeatureFactoryRename)
	}
	return hello
}

func prepRename(p *FactoryRenameParams) error {
	id, err := factoryID(p.Factory)
	if err != nil {
		return err
	}
	p.Factory = id
	p.Name = strings.Join(strings.Fields(p.Name), " ")
	if utf8.RuneCountInString(p.Name) > MaxFactoryName {
		return Errorf(CodeBadParams, "name is at most %d characters", MaxFactoryName)
	}
	return nil
}

func init() {
	methods["factory.rename"] = shaped[FactoryRenameParams, Empty](rename)
}

func rename(e Engine, raw json.RawMessage) (*Empty, error) {
	const name = "factory.rename"
	r, ok := e.(FactoryRenamer)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryRenameParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	if err := prepRename(&p); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	return done(r.FactoryRename(ctx, p.Factory, p.Name))
}
