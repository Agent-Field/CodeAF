package appwire

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type AppRenderParams struct {
	App string `json:"app"`
	schema.RenderAction
}

type AppOutputFileParams struct {
	App string `json:"app"`
	ID  string `json:"id"`
}

type AppOutputListParams struct {
	App    string `json:"app"`
	Before string `json:"before,omitempty"`
	Run    string `json:"run,omitempty"`
	Inbox  bool   `json:"inbox,omitempty"`
}

const EventAppOutputAdded = "apps.output_added"

type AppOutputAdded struct {
	App   string `json:"app"`
	ID    string `json:"id"`
	Inbox bool   `json:"inbox,omitempty"`
}

type AppOutputFileResult struct {
	File    data.Output `json:"file"`
	Content []byte      `json:"content"`
}

type AppOutputs interface {
	RenderAppOutput(context.Context, AppRenderParams) (*data.Output, error)
	AppOutputFile(AppOutputFileParams) (*AppOutputFileResult, error)
	AppOutputList(AppOutputListParams) (*data.OutputPage, error)
}

func outputCall[P, R any](name string, call func(AppOutputs, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		d, ok := e.(AppOutputs)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		return call(d, p)
	})
}

func init() {
	eventShapes[EventAppOutputAdded] = reflect.TypeFor[AppOutputAdded]()
	methods["apps.render"] = shaped[AppRenderParams, data.Output](func(e Engine, raw json.RawMessage) (*data.Output, error) {
		d, ok := e.(AppOutputs)
		if !ok || !schema.HasAction("render") {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve apps.render")
		}
		var p AppRenderParams
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil {
			return nil, Errorf(CodeBadParams, "apps.render takes an app, output, optional record id and inbox flag")
		}
		if _, err := required("output", p.Output); err != nil {
			return nil, err
		}
		return d.RenderAppOutput(context.Background(), p)
	})
	methods["apps.output_file"] = outputCall("apps.output_file", AppOutputs.AppOutputFile)
	methods["apps.outputs"] = outputCall("apps.outputs", AppOutputs.AppOutputList)
}
