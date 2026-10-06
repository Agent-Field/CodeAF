package reasoners

// Input-schema parity for the router surface.
//
// Validation contract (behaviour, derived from what the Python node publishes
// to the control plane — NOT from input_schemas.go):
//
//   - every reasoner this package registers carries a schema derived from its
//     Python signature; none is left on the SDK's
//     `{"type":"object","additionalProperties":true}` placeholder;
//   - the schema the registry actually holds for a reasoner (read back through
//     Registry.InputSchema) is the schema the Python node published for that
//     reasoner id, compared key-order-insensitively;
//   - the capture covers exactly the node's surface: the 33 router reasoners
//     plus `audit`. A fixture entry with no registration, or a registration
//     with no fixture entry, is drift and fails;
//   - a registration for a name the capture does not know panics, so drift is
//     impossible to ship quietly;
//   - the published shapes carry Python's derivation quirks verbatim: a PEP 604
//     `X | None` is {"type":"object"}, a `dict[str, Any]` is
//     {"type":"object","additionalProperties":true}, parameters with defaults
//     are absent from `required`, and `required` keeps the Python parameter
//     order.

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
)

// sdkPlaceholderSchema is what the SDK stamped on a reasoner registered with
// no schema. A registration carrying it would mean the schema was lost.
const sdkPlaceholderSchema = `{"type":"object","additionalProperties":true}`

// discoverInputSchemas builds the registry and reads the per-reasoner input
// schemas back out of it, decoded.
//
// Reading back (rather than asserting on InputSchema directly) is what makes
// the test meaningful: it proves the schema survived the registration path and
// inspects what the registry actually holds.
func discoverInputSchemas(t *testing.T) map[string]any {
	t.Helper()

	reg := NewRegistry()
	RegisterAll(reg, &appx.Fake{})

	out := map[string]any{}
	for _, name := range reg.Names() {
		raw, ok := reg.InputSchema(name)
		if !ok {
			t.Fatalf("registry lists %q but holds no schema for it", name)
		}
		out[name] = decodeSchema(t, raw)
	}
	return out
}

// decodeSchema renders raw JSON as untyped Go values, which makes a comparison
// insensitive to object key order (JSON objects become maps) while staying
// sensitive to array order — `required` is a list whose order is Python's
// parameter order and must be reproduced.
func decodeSchema(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode schema %s: %v", raw, err)
	}
	return v
}

// TestCaptureCoversExactlyTheNodeSurface pins the two-way containment: the
// capture is neither missing a reasoner this port registers nor carrying one it
// does not. `audit` is in the capture but is the entry internal/secaf/audit
// owns rather than a registered reasoner, so it is added to the expected set
// here.
func TestCaptureCoversExactlyTheNodeSurface(t *testing.T) {
	want := append([]string{NameAudit}, Names...)
	sort.Strings(want)

	got := InputSchemaNames()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("captured schema ids mismatch:\n got  = %v\n want = %v", got, want)
	}
	if len(got) != 34 {
		t.Errorf("captured %d schemas, want 34 (audit + 33 router reasoners)", len(got))
	}
}

// TestEveryRegisteredReasonerPublishesItsPythonSchema is the whole-surface
// assertion: for all 33 router reasoners, what the registry holds equals what
// Python publishes, and nothing was left on the placeholder.
func TestEveryRegisteredReasonerPublishesItsPythonSchema(t *testing.T) {
	published := discoverInputSchemas(t)
	registered := RegisterAll(NewRegistry(), &appx.Fake{})

	if len(published) != len(registered) {
		t.Fatalf("registry holds %d reasoners, want %d", len(published), len(registered))
	}

	placeholder := decodeSchema(t, []byte(sdkPlaceholderSchema))

	for _, name := range registered {
		got, ok := published[name]
		if !ok {
			t.Errorf("%s: not present in the registry", name)
			continue
		}
		want := decodeSchema(t, InputSchema(name))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: published schema mismatch\n got  = %#v\n want = %#v", name, got, want)
		}
		if reflect.DeepEqual(got, placeholder) {
			t.Errorf("%s: published the SDK placeholder schema — WithInputSchema was not applied", name)
		}
	}
}

// TestEveryCapturedRouterSchemaIsRegistered walks the other direction: every
// captured id except `audit` must be reachable in the registry. A Python reasoner
// that was never ported would otherwise sit in the capture unnoticed.
func TestEveryCapturedRouterSchemaIsRegistered(t *testing.T) {
	published := discoverInputSchemas(t)

	for _, name := range InputSchemaNames() {
		if name == NameAudit {
			// The audit entry's own spec, used by audit.BindRequest rather
			// than registered.
			continue
		}
		if _, ok := published[name]; !ok {
			t.Errorf("captured schema %q has no registered reasoner", name)
		}
	}
}

// TestRepresentativeSchemasMatchPythonSignatures is the non-tautological half:
// four reasoners whose expected schema is transcribed HERE from the Python
// signature, so a bad regeneration of the capture fails too. Between them they
// cover every mapping the node exercises.
func TestRepresentativeSchemasMatchPythonSignatures(t *testing.T) {
	published := discoverInputSchemas(t)

	cases := []struct {
		// name is the reasoner id; signature is the Python one it transcribes.
		name      string
		signature string
		want      string
	}{
		{
			// The simplest shape: one required `str`.
			name:      NameRunArchitectureMapper,
			signature: "run_architecture_mapper(repo_path: str)",
			want: `{"type":"object",
			        "properties":{"repo_path":{"type":"string"}},
			        "required":["repo_path"]}`,
		},
		{
			// dict[str, Any] -> object + additionalProperties; the `= 30`
			// default keeps max_files_without_signal OUT of required, while
			// `depth` (no default) stays in — and required is in PARAMETER
			// order, not alphabetical.
			name: NameRunInjectionHunter,
			signature: "run_injection_hunter(repo_path: str, recon_context: dict[str, Any], " +
				"depth: str, max_files_without_signal: int = 30)",
			want: `{"type":"object",
			        "properties":{"repo_path":{"type":"string"},
			                      "recon_context":{"type":"object","additionalProperties":true},
			                      "depth":{"type":"string"},
			                      "max_files_without_signal":{"type":"integer"}},
			        "required":["repo_path","recon_context","depth"]}`,
		},
		{
			// `int | None` is a PEP 604 union, which _type_to_json_schema's
			// Union branch never sees (no __origin__), so it falls through to
			// the {"type":"object"} default instead of {"type":"integer"}.
			name: NameProvePhase,
			signature: "prove_phase(repo_path: str, hunt_result: dict[str, Any], depth: str = \"standard\", " +
				"max_provers: int | None = None, max_concurrent_provers: int = 3)",
			want: `{"type":"object",
			        "properties":{"repo_path":{"type":"string"},
			                      "hunt_result":{"type":"object","additionalProperties":true},
			                      "depth":{"type":"string"},
			                      "max_provers":{"type":"object"},
			                      "max_concurrent_provers":{"type":"integer"}},
			        "required":["repo_path","hunt_result"]}`,
		},
		{
			// list[dict[str, Any]] -> array whose items carry the dict mapping.
			name: NameRemediationPhase,
			signature: "remediation_phase(repo_path: str, verified_findings: list[dict[str, Any]], " +
				"max_concurrent_remediations: int = 3)",
			want: `{"type":"object",
			        "properties":{"repo_path":{"type":"string"},
			                      "verified_findings":{"type":"array",
			                                           "items":{"type":"object","additionalProperties":true}},
			                      "max_concurrent_remediations":{"type":"integer"}},
			        "required":["repo_path","verified_findings"]}`,
		},
		{
			// list[str] -> array of string, and a reasoner with NO defaulted
			// parameter requires all of them.
			name:      NameRunCWEExpansion,
			signature: "run_cwe_expansion(recon_summary: str, strategies: list[str])",
			want: `{"type":"object",
			        "properties":{"recon_summary":{"type":"string"},
			                      "strategies":{"type":"array","items":{"type":"string"}}},
			        "required":["recon_summary","strategies"]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := decodeSchema(t, []byte(tc.want))

			if got := decodeSchema(t, InputSchema(tc.name)); !reflect.DeepEqual(got, want) {
				t.Errorf("captured schema for %s does not match the Python signature\n  %s\n got  = %#v\n want = %#v",
					tc.name, tc.signature, got, want)
			}
			if got := published[tc.name]; !reflect.DeepEqual(got, want) {
				t.Errorf("published schema for %s does not match the Python signature\n  %s\n got  = %#v\n want = %#v",
					tc.name, tc.signature, got, want)
			}
		})
	}
}

// TestInputSchemaPanicsOnUnknownReasoner pins the loud-drift contract: a
// registration for a name the capture does not carry must crash rather than
// register a reasoner with no schema.
func TestInputSchemaPanicsOnUnknownReasoner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("InputSchema on an unknown reasoner returned instead of panicking")
		}
	}()
	_ = InputSchema("run_not_a_reasoner")
}

// TestInputSchemaReturnsACopy proves a caller cannot corrupt the shared fixture
// for every later registration — the schemas are handed out as json.RawMessage,
// which is a mutable slice.
func TestInputSchemaReturnsACopy(t *testing.T) {
	first := InputSchema(NameRunArchitectureMapper)
	original := append(json.RawMessage(nil), first...)

	for i := range first {
		first[i] = 'x'
	}

	if second := InputSchema(NameRunArchitectureMapper); !reflect.DeepEqual(second, original) {
		t.Errorf("mutating a returned schema changed the fixture: %s", second)
	}
}
