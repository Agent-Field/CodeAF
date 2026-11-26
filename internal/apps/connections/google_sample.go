package connections

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

var ErrNoGoogleSample = errors.New("this app has no Google sample for that operation")

// GoogleSampleRows runs the installed mapping against the fixture transport.
// The draft's own schema and sample store check the projected field values.
func (s Service) GoogleSampleRows(ctx context.Context, app string) (map[string][]map[string]any, error) {
	out := make(map[string][]map[string]any)
	list, err := s.List(app)
	if err != nil {
		return nil, err
	}
	adapter := googleSource{client: googleFixtureClient()}
	for _, summary := range list {
		if summary.Kind != Google {
			continue
		}
		c, err := s.scoped(app, summary.ID)
		if err != nil {
			return nil, err
		}
		for _, read := range c.Reads {
			rows, _, err := adapter.rows(ctx, read.Table, nil, defaultRows)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				values := make(map[string]any, len(read.Fields))
				for field, column := range read.Fields {
					// Relationship fixtures can keep the draft's explicit
					// seed rows. The fixture has no cross-app identity grant.
					if read.GoogleLinkIdentities && googleReferenceColumn(read.Table, column) != "" {
						continue
					}
					if value := row[column]; value != nil && value != "" {
						values[field] = value
					}
				}
				if len(values) != 0 {
					out[read.Type] = append(out[read.Type], values)
				}
			}
		}
	}
	return out, nil
}

// GoogleSampleEffect is a fixture-only adapter for draft Check. It resolves
// the same operator operation, constructs the same request body and decodes
// the same response as an approved live effect.
func (s Service) GoogleSampleEffect(ctx context.Context, app, id, operation string, args map[string]any) (Receipt, error) {
	c, err := s.scoped(app, id)
	if err != nil || c.Kind != Google {
		return Receipt{}, ErrNoGoogleSample
	}
	write, ok := c.Writes[operation]
	if !ok {
		return Receipt{}, ErrNoGoogleSample
	}
	read := c.Reads[write.Read]
	set, _ := args["set"].(map[string]any)
	values := make(map[string]any, len(set))
	for field, value := range set {
		if !slices.Contains(write.Fields, field) || !boundedScalar(value) || value == nil {
			return Receipt{}, ErrRejected
		}
		values[read.Fields[field]] = value
	}
	body, err := googlePayload(operation, values)
	if err != nil {
		return Receipt{}, err
	}
	adapter := googleSource{client: googleFixtureClient()}
	key, version := "", ""
	if operation == "update-event" || operation == "comment" {
		if args["id"] == nil {
			return Receipt{}, ErrRejected
		}
		rows, _, err := adapter.rows(ctx, read.Table, nil, 1)
		if err != nil || len(rows) != 1 {
			return Receipt{}, fmt.Errorf("the Google example for %s is unavailable", operation)
		}
		key = googleText(rows[0], "id")
		if operation == "update-event" {
			version = googleText(rows[0], "version")
		}
	}
	return adapter.apply(ctx, id, operation, key, version, body)
}
