package telemetry

import "encoding/json"

// jsonMarshal is json.Marshal under one name, so a hot path that needs compact
// bytes and the doc templates that need indentation stay visibly the same
// encoder.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// jsonMarshalIndent is the pretty form Show prints.
func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
