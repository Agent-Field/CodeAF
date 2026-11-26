package builder

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type classSourceKey struct{}

// ModelDataClasses reads the working definition and the known inherited
// types. The callback sees edits made during the same model turn.
func ModelDataClasses(ctx context.Context) ([]schema.DataClass, error) {
	classes, ok := ctx.Value(classSourceKey{}).(func() ([]schema.DataClass, error))
	if !ok {
		return nil, errors.New("the Builder has no data policy for this request")
	}
	return classes()
}

func definitionClasses(files map[string]string, inherited schema.TypeSet) ([]schema.DataClass, error) {
	var app schema.App
	if raw := files[schema.AppFile]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &app); err != nil {
			return nil, err
		}
	}
	var classes []schema.DataClass
	add := func(fields map[string]schema.Field) {
		for _, field := range fields {
			if field.Class.Restricted() && !slices.Contains(classes, field.Class) {
				classes = append(classes, field.Class)
			}
		}
	}
	for name, typ := range app.Records {
		add(typ.Fields)
		if typ.Uses {
			add(inherited[name].Fields)
			if typ.Common != "" {
				for _, t := range inherited {
					if t.Common == typ.Common {
						add(t.Fields)
					}
				}
			}
		}
	}
	return classes, nil
}
