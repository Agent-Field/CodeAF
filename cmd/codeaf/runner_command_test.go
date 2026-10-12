package main

import "testing"

func TestRunnerJoinUsesTheFactoryAttachmentDoor(t *testing.T) {
	attachLab(t)
	if err := runRunner([]string{"join", "https://factory.example", "--name", "ci-2", "--token", "session-fixture"}); err != nil {
		t.Fatal(err)
	}
	got, ok := readFactoryAttachment()
	if !ok || got.URL != "https://factory.example" || got.Name != "ci-2" || got.Token != "session-fixture" {
		t.Fatalf("runner attachment: %+v %t", got, ok)
	}
}
