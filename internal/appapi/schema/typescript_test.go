package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTypescriptPreservesJSONShapes(t *testing.T) {
	g := tsGenerator{types: map[reflect.Type]string{}}
	for _, tc := range []struct {
		name      string
		value     any
		tag       string
		omitEmpty bool
		want      string
	}{
		{"string enum", "", "enum=send,enum=empty", false, `"send" | "empty"`},
		{"optional enum pointer", (*string)(nil), "enum=low,enum=medium,enum=high", true, `"low" | "medium" | "high"`},
		{"literal number", 0, "enum=180", false, "180"},
		{"false update", (*bool)(nil), "", true, "boolean"},
		{"nullable pointer", (*int)(nil), "", false, "number | null"},
		{"UTC timestamp", time.Time{}, "", false, "string"},
		{"optional timestamp", (*time.Time)(nil), "", true, "string"},
		{"nullable timestamp", (*time.Time)(nil), "", false, "string | null"},
		{"raw JSON", json.RawMessage{}, "", true, "JsonValue"},
		{"nil list", []string(nil), "", false, "Array<string> | null"},
		{"optional list", []string(nil), "", true, "Array<string>"},
		{"array of pointers", []*int(nil), "", false, "Array<number | null> | null"},
		{"fixed array", [3]string{}, "", false, "Array<string>"},
		{"JSON map", map[string]json.RawMessage(nil), "", false, "Record<string, JsonValue> | null"},
		{"byte string", []byte(nil), "", false, "string | null"},
		{"empty request", struct{}{}, "", false, "Record<string, never>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.value(reflect.TypeOf(tc.value), tc.tag, tc.omitEmpty, false)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestTypescriptFlattensEmbeddedJSONAndRejectsUnsupportedShapes(t *testing.T) {
	type Embedded struct {
		ID string `json:"id"`
	}
	type Response struct {
		Embedded
		Pinned *bool  `json:"pinned,omitempty"`
		Secret string `json:"-"`
	}
	g := tsGenerator{types: map[reflect.Type]string{}}
	fields, err := g.fields(reflect.TypeOf(Response{}))
	if err != nil || fields != "  \"id\": string;\n  \"pinned\"?: boolean;\n" {
		t.Fatalf("embedded JSON fields: %q, %v", fields, err)
	}
	for _, value := range []any{make(chan int), map[int]string{}, struct {
		Number int `json:"number,string"`
	}{}} {
		typ := reflect.TypeOf(value)
		var err error
		if typ.Kind() == reflect.Struct {
			_, err = g.fields(typ)
		} else {
			_, err = g.value(typ, "", false, false)
		}
		if err == nil {
			t.Errorf("unsupported wire %s was silently generated", typ)
		}
	}
}

// Go-only builders still enforce freshness and shape laws. App builders can
// supply their compiler without adding Node or npm dependencies to the binary.
func TestPublishedTypescriptCompilesForTheApp(t *testing.T) {
	compiler := os.Getenv("CODEAF_TYPESCRIPT_COMPILER")
	var args []string
	if compiler == "" {
		var err error
		compiler, err = exec.LookPath("tsc")
		if err != nil {
			t.Skip("set CODEAF_TYPESCRIPT_COMPILER to the app's typescript/bin/tsc for compiler acceptance")
		}
	} else if strings.HasSuffix(compiler, "/tsc") || strings.HasSuffix(compiler, ".js") {
		args = append(args, compiler)
		var err error
		compiler, err = exec.LookPath("node")
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The fixture needs no ambient Node definitions; an isolated project keeps
	// unrelated @types packages in an app checkout out of this contract proof.
	args = append(args, "--noEmit", "--strict", "--exactOptionalPropertyTypes", "--typeRoots", "testdata", "testdata/wire-client.ts")
	if output, err := exec.CommandContext(ctx, compiler, args...).CombinedOutput(); err != nil {
		t.Fatalf("TypeScript app contract: %v\n%s", err, output)
	}
}
