package session

import (
	"strings"
	"testing"
)

func TestHomeTaskPromptPromisesFolderRatherThanCopy(t *testing.T) {
	page := workerSystem(t)
	worker := section(page, "# You are a task:", "\n# ")
	if !strings.Contains(worker, "a folder of your own") {
		t.Error("task prompt must promise a folder of your own, including for home tasks")
	}
	for _, claim := range []string{
		"your own copy of the material",
		"YOU WRITE IN YOUR OWN COPY",
		"The copy you work in",
		"Paths inside your own working copy",
	} {
		if strings.Contains(worker, claim) {
			t.Errorf("home task prompt promises a copy: %q", claim)
		}
	}
	if !strings.Contains(page, "NEVER NAME THE METHOD") {
		t.Error("task prompt lost the rule never to name the method")
	}
}
