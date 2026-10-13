package main

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Factory routes use their own namespace because the app and factory have
// different identities and both have a type called Factory. Nested structs
// are discovered from the real JSON wire, never copied into the app contract.
func factoryTypescriptDocument() (string, error) {
	types := map[reflect.Type]string{}
	owners := map[string]reflect.Type{}
	var visit func(reflect.Type) error
	visit = func(t reflect.Type) error {
		if t == reflect.TypeOf(time.Time{}) {
			return nil
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			return visit(t.Elem())
		case reflect.Struct:
			if types[t] != "" {
				return nil
			}
			name := "Factory" + t.Name()
			if strings.HasSuffix(t.PkgPath(), "/factory") {
				name = "FactoryStored" + t.Name()
			}
			if strings.HasSuffix(t.PkgPath(), "/live") {
				name = "FactoryLive" + t.Name()
			}
			if t == reflect.TypeOf(api.Factory{}) {
				name = "FactoryIdentity"
			}
			if t.Name() == "" {
				return fmt.Errorf("unnamed factory wire struct %s", t)
			}
			if old, exists := owners[name]; exists && old != t {
				return fmt.Errorf("duplicate factory wire name %s", name)
			}
			types[t], owners[name] = name, t
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if !f.IsExported() || strings.Split(f.Tag.Get("json"), ",")[0] == "-" {
					continue
				}
				if err := visit(f.Type); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, route := range api.ServiceRoutes {
		if route.Request != nil {
			if err := visit(reflect.TypeOf(route.Request)); err != nil {
				return "", err
			}
		}
		if err := visit(reflect.TypeOf(route.Response)); err != nil {
			return "", err
		}
	}
	g := tsGenerator{types: types, integerMapKeys: true}
	var out strings.Builder
	out.WriteString("\n// Factory service contracts come from internal/factory/api; these routes use the factory listener.\n")
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fields, err := g.fields(owners[name])
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintf(&out, "export interface %s {\n%s}\n\n", name, fields)
	}
	out.WriteString("export interface FactoryRoutes {\n")
	seen := map[string]bool{}
	for _, route := range api.ServiceRoutes {
		key := route.Method + " " + route.Path
		if seen[key] {
			return "", fmt.Errorf("duplicate factory route %s", key)
		}
		seen[key] = true
		request := "never"
		if route.Request != nil {
			request = types[reflect.TypeOf(route.Request)]
		}
		fmt.Fprintf(&out, "  %s: { request: %s; response: %s };\n", strconv.Quote(key), request, types[reflect.TypeOf(route.Response)])
	}
	out.WriteString("}\n\nexport type FactoryRoute = keyof FactoryRoutes;\nexport type FactoryRequest<R extends FactoryRoute> = FactoryRoutes[R][\"request\"];\nexport type FactoryResponse<R extends FactoryRoute> = FactoryRoutes[R][\"response\"];\n")
	return out.String(), nil
}
