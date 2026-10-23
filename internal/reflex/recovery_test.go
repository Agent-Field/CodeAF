package reflex

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

type recoveryCompleter struct {
	run    func(context.Context, string) (*ai.Response, error)
	models []string
}

func (c *recoveryCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	c.models = append(c.models, request.Model)
	return c.run(ctx, request.Model)
}

func TestStalledMemoryRequestLeavesTimeForFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		c := &recoveryCompleter{}
		c.run = func(ctx context.Context, model string) (*ai.Response, error) {
			deadline, ok := ctx.Deadline()
			want := start.Add(roles.PatienceFor(roles.RoleReflex))
			if model == "primary" {
				want = start.Add(roles.PatienceFor(roles.RoleReflex) / 2)
			}
			if !ok || !deadline.Equal(want) {
				t.Fatalf("%s deadline=%s want=%s", model, deadline, want)
			}
			if model == "primary" {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			if ctx.Err() != nil {
				t.Fatal("fallback inherited expired primary context")
			}
			time.Sleep(time.Second)
			return reply(`{"keep":"Use tabs","scope":"user"}`), nil
		}
		client := (&Session{}).Bind(c, "primary", "fallback", func(text string) {
			if text != "memory helper recovered using fallback; your model settings are unchanged" {
				t.Fatal(text)
			}
		})
		found, err := Notice(context.Background(), client, "I use tabs", "okay", nil, true)
		if err != nil || found.Keep != "Use tabs" {
			t.Fatalf("found=%+v error=%v", found, err)
		}
		if !reflect.DeepEqual(c.models, []string{"primary", "fallback"}) {
			t.Fatal(c.models)
		}
		if elapsed := time.Since(start); elapsed != roles.PatienceFor(roles.RoleReflex)/2+time.Second {
			t.Fatalf("recovery exhausted patience: %s", elapsed)
		}
		// A slow request does not declare the primary permanently unusable.
		c.run = func(context.Context, string) (*ai.Response, error) { return reply(`{"keep":""}`), nil }
		if _, err := Notice(context.Background(), client, "hello", "hi", nil, true); err != nil {
			t.Fatal(err)
		}
		if c.models[2] != "primary" {
			t.Fatal(c.models)
		}
	})
}

func TestMemoryFallbackCannotOutliveOwnerOrOverallPatience(t *testing.T) {
	for _, cancelOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "both stalled", true: "owner cancelled"}[cancelOwner], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if cancelOwner {
					go func() { time.Sleep(time.Second); cancel() }()
				}
				start := time.Now()
				c := &recoveryCompleter{run: func(ctx context.Context, _ string) (*ai.Response, error) { <-ctx.Done(); return nil, ctx.Err() }}
				_, err := Notice(ctx, (&Session{}).Bind(c, "primary", "fallback", nil), "tabs", "okay", nil, true)
				wantCalls := 2
				wantErr := context.DeadlineExceeded
				if cancelOwner {
					wantCalls = 1
					wantErr = context.Canceled
				}
				if !errors.Is(err, wantErr) || len(c.models) != wantCalls {
					t.Fatalf("calls=%v err=%v", c.models, err)
				}
				if time.Since(start) > roles.PatienceFor(roles.RoleReflex) {
					t.Fatal("exceeded total bound")
				}
			})
		})
	}
}

func TestTransientMemoryFailureCanRetrySameConfiguredModel(t *testing.T) {
	c := &recoveryCompleter{}
	c.run = func(context.Context, string) (*ai.Response, error) {
		if len(c.models) == 1 {
			return nil, context.DeadlineExceeded
		}
		return reply(`{"keep":"Use tabs"}`), nil
	}
	found, err := Notice(context.Background(), (&Session{}).Bind(c, "same", "same", nil), "tabs", "okay", nil, true)
	if err != nil || found.Keep != "Use tabs" || len(c.models) != 2 {
		t.Fatalf("found=%+v calls=%v err=%v", found, c.models, err)
	}
}

func TestPermanentMemoryFailureDoesNotStartFallback(t *testing.T) {
	c := &recoveryCompleter{run: func(context.Context, string) (*ai.Response, error) { return nil, errors.New("invalid credentials") }}
	_, err := Notice(context.Background(), (&Session{}).Bind(c, "primary", "fallback", nil), "tabs", "okay", nil, true)
	if err == nil || len(c.models) != 1 {
		t.Fatalf("calls=%v err=%v", c.models, err)
	}
}

func TestUnavailableMemoryModelsRecoverToConversationAndSkipBrokenRoles(t *testing.T) {
	c := &recoveryCompleter{}
	c.run = func(ctx context.Context, model string) (*ai.Response, error) {
		if model != "conversation" {
			return nil, &provider.APIError{Status: 404, Message: "model unavailable"}
		}
		return reply(`{"keep":"Use tabs","scope":"user"}`), nil
	}
	var notices []string
	client := (&Session{}).Bind(c, "primary", "low", func(s string) { notices = append(notices, s) }, "conversation")
	for range 2 {
		got, err := Notice(context.Background(), client, "I use tabs", "okay", nil, true)
		if err != nil || got.Keep != "Use tabs" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	}
	if !reflect.DeepEqual(c.models, []string{"primary", "low", "conversation", "conversation"}) {
		t.Fatal(c.models)
	}
	if len(notices) != 1 {
		t.Fatal(notices)
	}
}

func TestMemoryRecoveryDoesNotEvadeAccountOrPolicyRefusals(t *testing.T) {
	for _, status := range []int{401, 402, 403, 429, 400} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := &recoveryCompleter{run: func(context.Context, string) (*ai.Response, error) {
				return nil, &provider.APIError{Status: status, Message: "request refused"}
			}}
			_, err := Notice(context.Background(), (&Session{}).Bind(c, "primary", "low", nil, "conversation"), "tabs", "okay", nil, true)
			if err == nil || len(c.models) != 1 {
				t.Fatalf("models=%v err=%v", c.models, err)
			}
		})
	}
}

func TestMemoryRecoveryDeduplicatesUnavailableModels(t *testing.T) {
	c := &recoveryCompleter{run: func(context.Context, string) (*ai.Response, error) { return nil, &provider.APIError{Status: 404} }}
	_, err := Notice(context.Background(), (&Session{}).Bind(c, "same", "same", nil, "same"), "tabs", "okay", nil, true)
	if err == nil || len(c.models) != 1 {
		t.Fatalf("models=%v err=%v", c.models, err)
	}
}

func TestThreeStalledMemoryModelsShareOverallPatience(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		c := &recoveryCompleter{run: func(ctx context.Context, _ string) (*ai.Response, error) { <-ctx.Done(); return nil, ctx.Err() }}
		_, err := Notice(context.Background(), (&Session{}).Bind(c, "primary", "low", nil, "conversation"), "tabs", "okay", nil, true)
		if !errors.Is(err, context.DeadlineExceeded) || len(c.models) != 3 || time.Since(start) != roles.PatienceFor(roles.RoleReflex) {
			t.Fatalf("models=%v elapsed=%s err=%v", c.models, time.Since(start), err)
		}
	})
}

func TestMemoryRecoveryAnnouncesOnlyAfterValidJSON(t *testing.T) {
	c := &recoveryCompleter{}
	c.run = func(context.Context, string) (*ai.Response, error) {
		if len(c.models) == 1 {
			return nil, &provider.APIError{Status: 404}
		}
		return reply("not json"), nil
	}
	var notices []string
	_, err := Notice(context.Background(), (&Session{}).Bind(c, "primary", "low", func(s string) { notices = append(notices, s) }, "conversation"), "tabs", "okay", nil, true)
	if err == nil || len(notices) != 0 {
		t.Fatalf("notices=%v err=%v", notices, err)
	}
}

// A slow primary followed by a stale low-tier slug was the real product gap:
// the original exchange still has a third, bounded path to its chat model.
func TestTimedOutPrimaryAndInvalidFallbackStillKeepOriginalExchange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &recoveryCompleter{}
		c.run = func(ctx context.Context, model string) (*ai.Response, error) {
			switch model {
			case "primary":
				<-ctx.Done()
				return nil, ctx.Err()
			case "low":
				return nil, &provider.APIError{Status: 400, Message: "fixture/withdrawn is not a valid model ID"}
			default:
				if ctx.Err() != nil {
					t.Fatal("conversation inherited cancelled attempt")
				}
				return reply(`{"keep":"Use tabs","scope":"user"}`), nil
			}
		}
		got, err := Notice(context.Background(), (&Session{}).Bind(c, "primary", "low", nil, "conversation"), "Use tabs", "okay", nil, true)
		if err != nil || got.Keep != "Use tabs" || !reflect.DeepEqual(c.models, []string{"primary", "low", "conversation"}) {
			t.Fatalf("got=%+v models=%v err=%v", got, c.models, err)
		}
	})
}

func TestCompletedMemoryAnswerSurvivesOwnerShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &recoveryCompleter{run: func(context.Context, string) (*ai.Response, error) {
		// The response is already complete when shutdown cancellation arrives.
		cancel()
		return reply(`{"keep":"Use tabs","scope":"user"}`), nil
	}}
	got, err := Notice(ctx, (&Session{}).Bind(c, "primary", "low", nil, "conversation"), "Use tabs", "okay", nil, true)
	if err != nil || got.Keep != "Use tabs" || len(c.models) != 1 {
		t.Fatalf("got=%+v calls=%v err=%v", got, c.models, err)
	}
}
