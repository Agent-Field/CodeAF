package config

import (
	"encoding/json"
	"testing"
)

type heldProfileValue struct {
	entered chan<- struct{}
	release <-chan struct{}
	value   string
}

func (v heldProfileValue) MarshalJSON() ([]byte, error) {
	v.entered <- struct{}{}
	<-v.release
	return json.Marshal(v.value)
}

// Two settings saved at once must compose. Marshaling starts after the writer
// has read the profile, so holding both marshalers forces both reads to finish
// before either rename and makes the formerly lossy ordering deterministic.
func TestConcurrentProfileWritesCompose(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan struct{}, 2)
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- writeProfileValue(dir, "test.first", heldProfileValue{entered, firstRelease, "one"})
	}()
	go func() {
		secondDone <- writeProfileValue(dir, "test.second", heldProfileValue{entered, secondRelease, "two"})
	}()
	<-entered
	<-entered
	close(firstRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("first write: %v", err)
	}
	close(secondRelease)
	if err := <-secondDone; err != nil {
		t.Fatalf("second write: %v", err)
	}

	values, err := readProfileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values["test.first"]; !ok {
		t.Fatal("the second rename discarded the first concurrent setting")
	}
	if _, ok := values["test.second"]; !ok {
		t.Fatal("the first rename discarded the second concurrent setting")
	}
}
