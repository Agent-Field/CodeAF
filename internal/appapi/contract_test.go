package appapi

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	validator "github.com/santhosh-tekuri/jsonschema/v5"
)

var contractPathVariable = regexp.MustCompile(`\{[^}]+\}`)

func contractRouteKey(method, path string) string {
	return method + " " + contractPathVariable.ReplaceAllString(path, "{}")
}

// This law reads the design itself, so a newly documented route cannot silently remain a 404.
func TestEveryDocumentedAppRouteIsRegistered(t *testing.T) {
	doc, err := os.ReadFile("../../docs/design/app-v2/API.md")
	if err != nil {
		t.Fatal(err)
	}
	expression := regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE) (/app/v1/[A-Za-z0-9_./{}-]+)`)
	documented := make(map[string]bool)
	for _, match := range expression.FindAllStringSubmatch(string(doc), -1) {
		documented[contractRouteKey(match[1], match[2])] = true
	}
	if len(documented) < 90 {
		t.Fatalf("design route parser found only %d routes", len(documented))
	}
	declared := make(map[string]bool)
	s := New(testToken, Options{Credentials: config.NewAppCredentials()})
	for _, route := range ContractRoutes() {
		key := contractRouteKey(route.Method, route.Path)
		if declared[key] {
			t.Errorf("duplicate route %s", key)
		}
		declared[key] = true
		if !documented[key] && !(strings.HasPrefix(route.Path, "/v1/") && bytes.Contains(doc, []byte(route.Method+" "+route.Path))) {
			t.Errorf("route missing from API.md: %s %s", route.Method, route.Path)
		}
		path := contractPathVariable.ReplaceAllString(route.Path, "contract-id")
		r := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
		r.Host, r.Header = "127.0.0.1:4000", bearer()
		_, pattern := s.mux.Handler(r)
		if pattern == route.Path {
			continue
		}
		// Existing lifecycle actions share a dispatcher rather than one mux pattern per id.
		shape := strings.TrimPrefix(route.Path, "/app/v1/agents/")
		if route.Resource == "agent" && pattern == "/app/v1/agents/" {
			accepted := false
			for _, method := range strings.Split(agentRoutes[shape], ", ") {
				accepted = accepted || method == route.Method
			}
			if accepted {
				continue
			}
		}
		t.Errorf("documented route is not registered: %s %s matched %q", route.Method, route.Path, pattern)
	}
	for key := range documented {
		if !declared[key] {
			t.Errorf("documented route has no contract: %s", key)
		}
	}
}

func TestReservedRoutesAreGuarded501WithoutBehaviour(t *testing.T) {
	s := New(testToken, Options{})
	for _, resource := range resourceContracts() {
		for _, route := range resource.Routes {
			if !route.reserved {
				continue
			}
			t.Run(route.Method+" "+route.Path, func(t *testing.T) {
				path := contractPathVariable.ReplaceAllString(route.Path, "contract-id")
				w := request(t, s, route.Method, path, `{"unused":"not decoded by a stub"}`, bearer(), "127.0.0.1:4000")
				var body NotBuiltResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if w.Code != 501 || body.Error != "not built yet" || body.Route != route.Method+" "+route.Path || w.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("stub contract: %d %s", w.Code, w.Body)
				}
				for _, guard := range []struct {
					headers http.Header
					host    string
					code    int
				}{
					{nil, "127.0.0.1:4000", 401},
					{http.Header{"Authorization": []string{"Bearer " + testToken}, "Origin": []string{"https://foreign.example"}}, "127.0.0.1:4000", 403},
					{bearer(), "foreign.example:4000", 421},
				} {
					w = request(t, s, route.Method, path, "", guard.headers, guard.host)
					if w.Code != guard.code {
						t.Fatalf("guard returned %d, want %d", w.Code, guard.code)
					}
				}
				w = request(t, s, "TRACE", path, "", bearer(), "127.0.0.1:4000")
				var methods []string
				for _, other := range resource.Routes {
					if other.Path == route.Path {
						methods = append(methods, other.Method)
					}
				}
				sort.Strings(methods)
				if w.Code != 405 || w.Header().Get("Allow") != strings.Join(methods, ", ") {
					t.Fatalf("wrong method: %d Allow=%s", w.Code, w.Header().Get("Allow"))
				}
				w = request(t, s, route.Method, path+"/unknown/extra", "", bearer(), "127.0.0.1:4000")
				if w.Code != 404 {
					t.Fatalf("unregistered child path: %d %s", w.Code, w.Body)
				}
			})
		}
	}
	if s.events.seq != 0 {
		t.Fatal("reserved routes published engine events")
	}
}

// Walking wire declarations prevents a type from escaping both the generator and round-trip gate.
func TestEveryWireTypeAndEventHasAContract(t *testing.T) {
	registered := make(map[string]bool)
	for _, resource := range resourceContracts() {
		for _, value := range resource.Types {
			name := reflect.TypeOf(value).Name()
			if registered[name] {
				t.Errorf("type %s has two owners", name)
			}
			registered[name] = true
		}
	}
	files, err := filepath.Glob("*_wire.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if ok && spec.Name.IsExported() && !registered[spec.Name.Name] {
				t.Errorf("%s: %s is absent from the contract", path, spec.Name.Name)
			}
			return true
		})
	}
	for _, route := range ContractRoutes() {
		if !registered[route.Response] || (route.Request != "" && !registered[route.Request]) {
			t.Errorf("%s %s has an unknown JSON type", route.Method, route.Path)
		}
	}
	doc, err := os.ReadFile("../../docs/design/app-v2/API.md")
	if err != nil {
		t.Fatal(err)
	}
	events := ContractEvents()
	for event, payload := range events {
		if !registered[reflect.TypeOf(payload).Name()] {
			t.Errorf("%s has an unregistered payload", event)
		}
		if !bytes.Contains(doc, []byte("`"+event+"`")) {
			t.Errorf("%s is missing from API.md", event)
		}
	}
	tree, err := parser.ParseFile(token.NewFileSet(), "event.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(tree, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		typ, ok := spec.Type.(*ast.Ident)
		if !ok || typ.Name != "EventType" {
			return true
		}
		for _, value := range spec.Values {
			literal, ok := value.(*ast.BasicLit)
			if !ok {
				continue
			}
			name, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := events[EventType(name)]; !ok {
				t.Errorf("event %s has no payload contract", name)
			}
		}
		return true
	})
}

func TestEveryWireTypeRoundTripsAndMatchesItsSchema(t *testing.T) {
	compiler := validator.NewCompiler()
	compiler.AssertFormat = true
	files, err := filepath.Glob("../../docs/design/app-v2/schema/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource("https://contract.codeaf.test/"+filepath.Base(path), bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range resourceContracts() {
		for _, value := range resource.Types {
			typ := reflect.TypeOf(value)
			t.Run(typ.Name(), func(t *testing.T) {
				schema, err := compiler.Compile("https://contract.codeaf.test/" + resource.Name + ".json#/$defs/" + typ.Name())
				if err != nil {
					t.Fatal(err)
				}
				zero := reflect.New(typ).Elem()
				populated := reflect.New(typ).Elem()
				seedContractValue(populated, map[reflect.Type]bool{}, "")
				for i, sample := range []reflect.Value{zero, populated} {
					raw, err := json.Marshal(sample.Interface())
					if err != nil {
						t.Fatal(err)
					}
					back := reflect.New(typ)
					if err := json.Unmarshal(raw, back.Interface()); err != nil {
						t.Fatal(err)
					}
					reencoded, err := json.Marshal(back.Elem().Interface())
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(raw, reencoded) || (i == 1 && !reflect.DeepEqual(sample.Interface(), back.Elem().Interface())) {
						t.Fatalf("round-trip %d lost fields: %s", i, raw)
					}
					if i == 1 {
						var document any
						if err := json.Unmarshal(raw, &document); err != nil {
							t.Fatal(err)
						}
						if err := schema.Validate(document); err != nil {
							t.Fatalf("populated value disagrees with its schema: %s\n%v", raw, err)
						}
					}
				}
			})
		}
	}
}

// Populated fixtures exercise false/zero pointers, arrays, embedded fields and raw JSON as well as strings.
// Recursive cards stop at an already visited pointer type instead of relying on a time or depth limit.
func seedContractValue(value reflect.Value, seen map[reflect.Type]bool, tag string) {
	if value.Type() == reflect.TypeOf(time.Time{}) {
		value.Set(reflect.ValueOf(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)))
		return
	}
	if value.Type() == reflect.TypeOf(json.RawMessage{}) {
		value.SetBytes([]byte(`{"sample":true}`))
		return
	}
	switch value.Kind() {
	case reflect.Pointer:
		if seen[value.Type().Elem()] {
			return
		}
		value.Set(reflect.New(value.Type().Elem()))
		switch value.Elem().Kind() {
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			// An allocated zero pointer differs from an omitted update on the wire.
		default:
			seedContractValue(value.Elem(), seen, tag)
		}
	case reflect.Struct:
		seen[value.Type()] = true
		defer delete(seen, value.Type())
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if value.Field(i).CanSet() {
				seedContractValue(value.Field(i), seen, field.Tag.Get("jsonschema"))
			}
		}
	case reflect.Slice:
		length := 1
		for _, part := range strings.Split(tag, ",") {
			if strings.HasPrefix(part, "minItems=") {
				length, _ = strconv.Atoi(strings.TrimPrefix(part, "minItems="))
			}
		}
		value.Set(reflect.MakeSlice(value.Type(), length, length))
		for i := 0; i < length; i++ {
			seedContractValue(value.Index(i), seen, "")
		}
	case reflect.Map:
		value.Set(reflect.MakeMap(value.Type()))
		elem := reflect.New(value.Type().Elem()).Elem()
		seedContractValue(elem, seen, "")
		value.SetMapIndex(reflect.ValueOf("sample").Convert(value.Type().Key()), elem)
	case reflect.String:
		text := "sample"
		if value.Type() == reflect.TypeOf(EventType("")) {
			text = "agent.changed"
		}
		for _, part := range strings.Split(tag, ",") {
			if strings.HasPrefix(part, "enum=") {
				text = strings.TrimPrefix(part, "enum=")
				break
			}
		}
		value.SetString(text)
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number := int64(1)
		for _, part := range strings.Split(tag, ",") {
			if strings.HasPrefix(part, "enum=") {
				number, _ = strconv.ParseInt(strings.TrimPrefix(part, "enum="), 10, 64)
				break
			}
		}
		value.SetInt(number)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1.25)
	}
}
