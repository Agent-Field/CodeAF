package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestForcedSchemaAnswerUsesPublishedNativeFormatting(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_BASE_URL", "")
	if err := catalog.Remember(catalog.Options{Dir: profile}, []catalog.Model{{
		ID: "vendor/work", Parameters: []string{"response_format", "structured_outputs"},
	}}); err != nil {
		t.Fatal(err)
	}
	api := (&fakeAPI{}).then(func(body wireRequest) (int, any) {
		if body.ResponseFormat != nil || len(body.Tools) == 0 {
			t.Error("the reading turn should keep its tools and ordinary format")
		}
		return callTool("read_file", `{"path":"README.md"}`)(body)
	}).then(func(body wireRequest) (int, any) {
		if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_schema" || len(body.Tools) != 0 {
			t.Errorf("the forced answer lacks its native schema: %+v", body.ResponseFormat)
		} else {
			want, _ := json.Marshal(findingSchema)
			if string(body.ResponseFormat.JSONSchema.Schema) != string(want) || body.ResponseFormat.JSONSchema.Strict {
				t.Error("the answer changed its original schema or imposed a provider's strict-mode dialect")
			}
		}
		return answer(`{"file":"README.md","line":1}`, 0)(body)
	})
	policy := testPolicy
	policy.StructuredAnswers = true
	result, err := RunSession(t.Context(), api.serve(t), SessionOrder{
		Root: repo(t), Model: "vendor/work", MaxTurns: 2, Policy: policy, Schema: findingSchema,
	})
	if err != nil || result.Failed != "" || len(result.JSON) == 0 || result.Turns != 2 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func nativeAnswerPolicy() Policy {
	policy := testPolicy
	policy.StructuredAnswers = true
	return policy
}

func nativeAnswerClient(t *testing.T, api *fakeAPI) *Client {
	client := api.serve(t)
	client.retries = 0
	client.supportsParameter = func(string, string) (bool, bool) { return true, true }
	return client
}

func TestNativeAnswerFormattingIsAbsentWhenNotPublishedOrNotOptedIn(t *testing.T) {
	for _, kind := range []string{"unsupported", "unknown", "JSON objects only", "program not opted in", "plain text"} {
		t.Run(kind, func(t *testing.T) {
			api := (&fakeAPI{}).then(func(body wireRequest) (int, any) {
				if body.ResponseFormat != nil {
					t.Error("an unrequested or unpublished schema format was sent")
				}
				return answer(`{"file":"README.md","line":1}`, 0)(body)
			})
			client := nativeAnswerClient(t, api)
			order := SessionOrder{Root: repo(t), Model: "vendor/work", MaxTurns: 1, Policy: nativeAnswerPolicy(), Schema: findingSchema}
			switch kind {
			case "unsupported":
				client.supportsParameter = func(string, string) (bool, bool) { return false, true }
			case "unknown":
				client.supportsParameter = func(string, string) (bool, bool) { return false, false }
			case "JSON objects only":
				client.supportsParameter = func(_, parameter string) (bool, bool) { return parameter == "response_format", true }
			case "program not opted in":
				order.Policy.StructuredAnswers = false
			case "plain text":
				order.Schema = nil
			}
			if result, err := RunSession(t.Context(), client, order); err != nil || result.Failed != "" {
				t.Fatalf("the existing answer path changed: %+v, %v", result, err)
			}
		})
	}
}

func TestNativeAnswerFallsBackOnceOnlyForExplicitUnsupportedFormatting(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			api := (&fakeAPI{}).then(func(body wireRequest) (int, any) {
				if body.ResponseFormat == nil || len(body.Tools) != 0 {
					t.Error("the published native format was not asked without tools")
				}
				return status, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "response_format is not supported"}}
			}).then(func(body wireRequest) (int, any) {
				if body.ResponseFormat != nil || len(body.Tools) != 0 || !strings.Contains(body.Messages[0].Content[0].Text, `"file"`) {
					t.Error("the fallback dropped its schema or resumed reading")
				}
				return answer(`{"file":"README.md","line":1}`, 0.1)(body)
			})
			result, err := RunSession(t.Context(), nativeAnswerClient(t, api), SessionOrder{
				Root: repo(t), Model: "vendor/work", MaxTurns: 1, Policy: nativeAnswerPolicy(), Schema: findingSchema,
			})
			if err != nil || result.Failed != "" || result.Turns != 2 || len(result.JSON) == 0 || result.CostUSD != 0.1 {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}

func TestNativeAnswerDoesNotHideSchemaErrorsOutagesRefusalsOrCallerStops(t *testing.T) {
	for _, kind := range []string{"invalid schema", "unrelated bad request", "capacity outage", "key refusal", "ceiling", "caller stop"} {
		t.Run(kind, func(t *testing.T) {
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			status, message := http.StatusBadRequest, "response_format schema is invalid: missing required"
			switch kind {
			case "unrelated bad request":
				message = "invalid request messages"
			case "capacity outage":
				status, message = http.StatusBadGateway, "response_format is not supported; no capacity available"
			case "key refusal":
				status, message = http.StatusForbidden, "response_format is not supported; key refused"
			case "ceiling":
				status, message = http.StatusPaymentRequired, "response_format is not supported; dollar ceiling"
			}
			api := (&fakeAPI{}).then(func(body wireRequest) (int, any) {
				if body.ResponseFormat == nil {
					t.Error("native format not requested")
				}
				if kind == "caller stop" {
					stop()
				}
				return status, ai.ErrorResponse{Error: ai.ErrorDetail{Message: message}}
			})
			client := nativeAnswerClient(t, api)
			result, err := RunSession(ctx, client, SessionOrder{Root: repo(t), Model: "vendor/work", MaxTurns: 1,
				Policy: nativeAnswerPolicy(), Schema: findingSchema})
			if err == nil || result.Failed == "" || result.Turns != 1 || len(result.JSON) != 0 || len(api.bodies) != 1 {
				t.Fatalf("failure was hidden or bought a fallback: %+v, %v", result, err)
			}
			if (kind == "key refusal" || kind == "ceiling") && client.Refusal() == nil {
				t.Error("the account or ceiling refusal was not retained")
			}
		})
	}
}

func TestNativeAndFallbackAnswersStillNeedTheOriginalSchema(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "fallback"}[fallback], func(t *testing.T) {
			api := &fakeAPI{}
			if fallback {
				api.then(func(wireRequest) (int, any) {
					return 400, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "response_format is not supported"}}
				})
			}
			for range 1 + testPolicy.FollowUps {
				api.then(func(body wireRequest) (int, any) {
					if len(body.Tools) != 0 || (body.ResponseFormat == nil) != fallback {
						t.Error("an answer correction changed its format or resumed tools")
					}
					return answer(`{"file":"README.md"}`, 0)(body)
				})
			}
			result, err := RunSession(t.Context(), nativeAnswerClient(t, api), SessionOrder{Root: repo(t), Model: "vendor/work",
				MaxTurns: 1, Policy: nativeAnswerPolicy(), Schema: findingSchema})
			wantTurns := 1 + testPolicy.FollowUps
			if fallback {
				wantTurns++
			}
			if err != nil || result.Failed == "" || len(result.JSON) != 0 || result.Turns != wantTurns {
				t.Fatalf("invalid JSON was accepted or corrections became unbounded: %+v, %v", result, err)
			}
		})
	}
}

func TestNativeCompatibilityFallbackDoesNotRepeatASecondRefusal(t *testing.T) {
	api := &fakeAPI{}
	for range 2 {
		api.then(func(wireRequest) (int, any) {
			return 400, ai.ErrorResponse{Error: ai.ErrorDetail{Message: "response_format is not supported"}}
		})
	}
	result, err := RunSession(t.Context(), nativeAnswerClient(t, api), SessionOrder{Root: repo(t), Model: "vendor/work",
		MaxTurns: 1, Policy: nativeAnswerPolicy(), Schema: findingSchema})
	if !errors.Is(err, errResponseFormatUnsupported) || result.Failed == "" || result.Turns != 2 || len(api.bodies) != 2 {
		t.Fatalf("compatibility bought repeated calls: %+v, %v", result, err)
	}
}

func TestNativeFormatRefusalClassificationKeepsInvalidSchemasAndOtherFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code, param, message string
		status                     int
		want                       bool
	}{
		{"structured unsupported", "unsupported_parameter", "response_format", "unavailable field", 400, true},
		{"invalid schema code wins", "invalid_json_schema", "response_format", "response_format is not supported", 400, false},
		{"invalid schema words win", "", "", "invalid schema: response_format is not supported", 400, false},
		{"unsupported keyword", "", "", "unsupported JSON Schema keyword allOf", 400, false},
		{"unrelated field", "unsupported_parameter", "tools", "tools are not supported", 400, false},
		{"structured upstream outage", "unsupported_parameter", "response_format", "response_format is not supported", 502, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"error": map[string]string{"code": tc.code, "param": tc.param, "message": tc.message}})
			if got := responseFormatUnsupported(tc.status, raw, tc.message); got != tc.want {
				t.Fatalf("format refusal = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNativeCapabilityLookupUsesTheExistingSessionClock(t *testing.T) {
	start := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &sessionTestClock{now: start}
	lookup := func(string, string) (bool, bool) {
		clock.advance(clock.now.Add(3 * time.Second))
		return true, true
	}
	complete := func(ctx context.Context, request Request) (*ai.Response, error) {
		deadline, _ := ctx.Deadline()
		if deadline != start.Add(45*time.Second) || request.ResponseFormat == nil {
			t.Error("the capability read moved the answer's deadline or lost formatting")
		}
		clock.advance(start.Add(7 * time.Second))
		_, value := answer(`{"file":"README.md","line":1}`, 0)(wireRequest{})
		response := value.(ai.Response)
		return &response, nil
	}
	result, err := runSession(t.Context(), complete, SessionOrder{Root: repo(t), MaxTurns: 1, Wall: 45 * time.Second,
		Policy: nativeAnswerPolicy(), Schema: findingSchema, supportsParameter: lookup}, clock.runtime())
	if err != nil || result.Failed != "" || result.Duration != 7*time.Second {
		t.Fatalf("capability reads escaped the session clock: %+v, %v", result, err)
	}
}

func TestNativeFormatFallbackKeepsTheOriginalAnswerDeadline(t *testing.T) {
	for _, expires := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "deadline"}[expires], func(t *testing.T) {
			start := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
			clock := &sessionTestClock{now: start}
			calls := 0
			format := &ai.ResponseFormat{Type: "json_schema"}
			complete := func(ctx context.Context, request Request) (*ai.Response, error) {
				calls++
				deadline, _ := ctx.Deadline()
				if calls == 1 {
					clock.advance(deadline)
					return nil, ctx.Err()
				}
				if deadline != start.Add(45*time.Second) || len(request.Tools) != 0 {
					t.Error("format handling extended the deadline or resumed reading")
				}
				if calls == 2 {
					if request.ResponseFormat != format {
						t.Error("the reading cutoff lost native formatting")
					}
					clock.advance(clock.now.Add(time.Second))
					return nil, errResponseFormatUnsupported
				}
				if request.ResponseFormat != nil {
					t.Error("the explicit unsupported format was asked again")
				}
				if expires {
					clock.advance(deadline)
					return nil, ctx.Err()
				}
				clock.advance(start.Add(43 * time.Second))
				_, value := answer(`{"file":"README.md","line":1}`, 0)(wireRequest{})
				response := value.(ai.Response)
				return &response, nil
			}
			result, err := runSession(t.Context(), complete, SessionOrder{Root: repo(t), MaxTurns: 6, Wall: 45 * time.Second,
				AnswerReserve: 22500 * time.Millisecond, Policy: nativeAnswerPolicy(), Schema: findingSchema, answerFormat: format}, clock.runtime())
			if result.Turns != 3 || calls != 3 || (expires && (!errors.Is(err, context.DeadlineExceeded) || result.Failed == "")) ||
				(!expires && (err != nil || result.Failed != "" || len(result.JSON) == 0 || result.Duration != 43*time.Second)) {
				t.Fatalf("result = %+v, error = %v, calls = %d", result, err, calls)
			}
		})
	}
}
