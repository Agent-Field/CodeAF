package appwire

import (
	"bufio"
	"context"
	"io"
	"slices"
	"testing"
)

// A FACTORY ENGINE'S HELLO SAYS ITS JOBS NAME THE AREA THEY TOUCHED, and a
// plain engine's says nothing, so a desk draws its per-area table only when
// the engine announced the word.
func TestAFactoryEngineAnnouncesJobAreas(t *testing.T) {
	for _, c := range []struct {
		engine Engine
		want   bool
	}{{&fakeEngine{}, false}, {&factoryEngine{}, true}} {
		if got := slices.Contains(helloFeatures(t, c.engine), "factory.job_area"); got != c.want {
			t.Fatalf("%T announced factory.job_area = %v, want %v", c.engine, got, c.want)
		}
	}
}

func helloFeatures(t *testing.T, engine Engine) []string {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		_ = NewServer(outW).Serve(context.Background(), inR, Hello{Version: "test"}, engine)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	w := &wire{t: t, in: inW, lines: bufio.NewScanner(outR)}
	hello, _ := w.next()["data"].(map[string]any)
	var features []string
	list, _ := hello["features"].([]any)
	for _, f := range list {
		if s, ok := f.(string); ok {
			features = append(features, s)
		}
	}
	return features
}
