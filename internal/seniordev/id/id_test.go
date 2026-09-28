//go:build !windows

package id

import (
	"bytes"
	"errors"
	"testing"
)

func TestCounterSpillsIntoTimestampBits(t *testing.T) {
	entropy := bytes.NewReader(make([]byte, randomLength*4097))
	generator := NewGenerator(func() int64 { return 10 }, entropy)
	var value string
	for range 4097 {
		var err error
		value, err = generator.Create("evt", AscendingDirection, 10)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := Timestamp(value)
	if err != nil {
		t.Fatal(err)
	}
	if got != 11 {
		t.Fatalf("timestamp after counter spill = %d, want 11", got)
	}
}

func TestRandomFailurePropagates(t *testing.T) {
	generator := NewGenerator(func() int64 { return 1 }, failingReader{})
	if _, err := generator.Create("evt", AscendingDirection); !errors.Is(err, errEntropy) {
		t.Fatalf("error = %v", err)
	}
}

var errEntropy = errors.New("entropy failed")

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errEntropy }
