package provider

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPayment429IsTerminalAndAPlain429StillPaces(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantPayment bool
	}{
		{
			name:        "authenticated account cannot pay",
			body:        `{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}`,
			wantPayment: true,
		},
		{
			name: "plain pacing",
			body: `{"message":"too many requests"}`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			oldLimiter := sharedLimiter
			sharedLimiter = newAdaptiveLimiter()
			defer func() { sharedLimiter = oldLimiter }()

			var requests atomic.Int64
			handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				writer.WriteHeader(http.StatusTooManyRequests)
				_, _ = writer.Write([]byte(testCase.body))
			})
			client, err := NewClient(Config{
				APIKey: "key", BaseURL: "https://direct.example/v1", Model: "glm-5.3-flash",
				Direct: true, HTTPClient: handlerClient(handler),
			})
			if err != nil {
				t.Fatal(err)
			}
			var waits []time.Duration
			client.wait = func(context.Context, time.Duration) error {
				waits = append(waits, 0)
				return nil
			}
			response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
			if err == nil || response != nil {
				t.Fatalf("refusal became an answer: response=%v error=%v", response, err)
			}
			if testCase.wantPayment {
				if requests.Load() != 1 || len(waits) != 0 {
					t.Fatalf("payment refusal made requests=%d waits=%d, want 1/0", requests.Load(), len(waits))
				}
			} else if requests.Load() < 2 || requests.Load() > 16 || int64(len(waits)) != requests.Load()-1 {
				t.Fatalf("ordinary pacing made requests=%d waits=%d inside one deadline", requests.Load(), len(waits))
			}
			refusal, ok := RefusalFrom(err)
			if !ok || refusal.AccountCannotPay() != testCase.wantPayment {
				t.Fatalf("refusal = %+v, payment=%t", refusal, testCase.wantPayment)
			}
			classified := client.laneRefusalFor(client.config.Model, "some-lane", err)
			if testCase.wantPayment {
				if classified.Kind != refusalPayment || !classified.Terminal || classified.paced() || classified.struck() {
					t.Fatalf("payment classification = %+v", classified)
				}
				if !strings.Contains(err.Error(), "Insufficient balance or no resource package. Please recharge.") {
					t.Fatalf("vendor words were lost: %v", err)
				}
			} else if classified.Kind != refusalPaced || classified.Terminal {
				t.Fatalf("plain 429 classification = %+v", classified)
			}
		})
	}
}
