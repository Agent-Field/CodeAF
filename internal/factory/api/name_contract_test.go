package api

import (
	"encoding/json"
	"testing"
)

func TestFactoryNameKeepsContractOneAndLegacyIdentity(t *testing.T) {
	if Contract != 1 {
		t.Fatalf("contract = %d", Contract)
	}
	want := Factory{Name: "Our team", Address: "http://factory", Version: "test"}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	assertKeys(t, raw, "name", "address", "version")
	var got Factory
	if err := json.Unmarshal(raw, &got); err != nil || got != want {
		t.Fatalf("identity = %+v %v", got, err)
	}
	var legacy struct {
		Address string `json:"address"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil || legacy.Address != want.Address || legacy.Version != want.Version {
		t.Fatalf("legacy reader = %+v %v", legacy, err)
	}
	var old Factory
	if err := json.Unmarshal([]byte(`{"address":"http://factory","version":"test"}`), &old); err != nil || old.Name != "" || old.Address != want.Address || old.Version != want.Version {
		t.Fatalf("old identity = %+v %v", old, err)
	}
}
