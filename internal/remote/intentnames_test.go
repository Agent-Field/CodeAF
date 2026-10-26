package remote

import (
	"encoding/json"
	"testing"
)

func TestIntentSiblingNamesTravelWithoutChangingLaunchIdentity(t *testing.T) {
	original := LaunchShape{IntentNames: true, IntentSiblingNames: []string{"Release review", "Garden invitation"}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var received LaunchShape
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatal(err)
	}
	if len(received.IntentSiblingNames) != 2 || received.IntentSiblingNames[1] != "Garden invitation" {
		t.Fatalf("received names = %q", received.IntentSiblingNames)
	}
	if !received.Same(&LaunchShape{IntentNames: true}) {
		t.Fatal("names of other open chats changed launch identity")
	}
	if received.Same(&LaunchShape{}) {
		t.Fatal("intent naming policy lost from launch identity")
	}
}
