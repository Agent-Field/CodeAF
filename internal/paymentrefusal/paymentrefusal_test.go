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
		{http.StatusTooManyRequests, `{"message":"Insufficient balance. Please recharge."}`, true},
		{http.StatusTooManyRequests, `{"message":"quota exhausted"}`, true},
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
