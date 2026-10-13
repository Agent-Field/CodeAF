package prompt

import (
	"fmt"
	"strings"
)

// RenderShape gives the Builder definition files and synthetic rows only.
func RenderShape(in Inputs, sample string) (string, error) {
	if strings.TrimSpace(in.Request) == "" || len(in.Files) == 0 || in.Catalog == "" {
		return "", fmt.Errorf("prompt: shape needs Request, Files and Catalog")
	}
	var out strings.Builder
	data := struct {
		view
		Sample string
	}{view: view{Inputs: in}, Sample: sample}
	if err := templates.ExecuteTemplate(&out, "shape.tmpl", data); err != nil {
		return "", err
	}
	return out.String(), nil
}
