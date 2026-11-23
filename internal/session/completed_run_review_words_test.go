package session

import (
	"strings"
	"testing"
)

func TestCompletedRunReviewNamesThePersonsAnswers(t *testing.T) {
	if !strings.Contains(completedRunReview, "accept it or say it is not right") {
		t.Fatal("saved run guidance does not name the person's answers")
	}
	for _, word := range []string{"refute", "verified", "verdict", "auditor"} {
		if strings.Contains(completedRunReview, word) {
			t.Errorf("saved run guidance says %q", word)
		}
	}
}
