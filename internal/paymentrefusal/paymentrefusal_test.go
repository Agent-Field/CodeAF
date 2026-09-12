package paymentrefusal

import (
	"net/http"
	"testing"
)

func TestOnlyAPaymentShaped429IsTerminal(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   bool
	}{
		{http.StatusPaymentRequired, "", true},
		{http.StatusTooManyRequests, `{"code":"1113","message":"try later"}`, true},
		{http.StatusTooManyRequests, `{"error":{"code":1113,"message":"try later"}}`, true},
		{http.StatusTooManyRequests, `{"message":"Insufficient balance. Please recharge."}`, true},
		{http.StatusTooManyRequests, `{"message":"quota exhausted"}`, true},
		{http.StatusTooManyRequests, `{"code":"1316","message":"Insufficient balance for extra usage"}`, false},
		{http.StatusTooManyRequests, `{"code":"9999","message":"Insufficient balance"}`, false},
		{http.StatusTooManyRequests, `{"message":"too many requests"}`, false},
		{http.StatusTooManyRequests, "", false},
		{http.StatusUnauthorized, `{"code":"1113","message":"Insufficient balance"}`, false},
	}
	for _, testCase := range tests {
		if got := Matches(testCase.status, []byte(testCase.body)); got != testCase.want {
			t.Errorf("Matches(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
		}
	}
}

func TestZAIResponseCodesDecideBeforeTheirWords(t *testing.T) {
	tests := []struct {
		code string
		want Kind
	}{
		{"1113", Payment},
		{"1302", Pacing},
		{"1305", Pacing},
		{"1308", WindowExhausted},
		{"1310", WindowExhausted},
		{"1316", WindowExhausted},
		{"1317", WindowExhausted},
		{"1318", WindowExhausted},
		{"1319", WindowExhausted},
		{"1320", WindowExhausted},
		{"1321", WindowExhausted},
		{"1309", NoPlan},
		{"1311", NoPlan},
		{"1315", NoPlan},
	}
	for _, testCase := range tests {
		body := []byte(`{"error":{"code":"` + testCase.code + `","message":"Insufficient balance for extra usage"}}`)
		if got := Classify(http.StatusTooManyRequests, body); got != testCase.want {
			t.Errorf("code %s = %v, want %v", testCase.code, got, testCase.want)
		}
	}
}
