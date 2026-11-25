package appwire

import (
	"bufio"
	"context"
	"io"
	"testing"
)

// fakePictureKeeper is the fake engine with a picture maker: it records what
// it was asked and answers a fixed list.
type fakePictureKeeper struct {
	*fakeEngine
	listed string
	set    *PictureMakerSetParams
}

func (f *fakePictureKeeper) PictureMakers(workspace string) (PictureMakers, error) {
	f.listed = workspace
	price := 0.045
	return PictureMakers{
		Makers:     []PictureMaker{{ID: "seed/pro", Name: "Seedream 5 Pro", PricePerPictureUSD: &price, PriceUnit: "image", ReadsPhotos: true, Shapes: []string{"1:1"}, Automatic: true}},
		Everywhere: PictureMakerChoice{},
		Resolved:   PictureMakerRoute{Model: "seed/pro", Name: "Seedream 5 Pro", Scope: "automatic", Reason: "curated"},
	}, nil
}

func (f *fakePictureKeeper) SetPictureMaker(p PictureMakerSetParams) (PictureMakers, error) {
	f.set = &p
	return f.PictureMakers(p.Workspace)
}

func startPictureWire(t *testing.T) (*wire, *fakePictureKeeper) {
	t.Helper()
	engine := &fakePictureKeeper{fakeEngine: &fakeEngine{}}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	served := make(chan error, 1)
	server := NewServer(outW)
	go func() {
		served <- server.Serve(context.Background(), inR, Hello{Version: "test", PID: 42, Home: "/h"}, engine)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	t.Cleanup(func() { inW.Close() })
	return &wire{t: t, in: inW, lines: scanner, served: served, engine: engine.fakeEngine}, engine
}

// THE FEATURE IS CARRIED BY AN ENGINE THAT SERVES IT, and only by one.
func TestPictureMakersFeatureFollowsTheEngine(t *testing.T) {
	w, _ := startPictureWire(t)
	found := false
	for _, f := range features(w.next()) {
		found = found || f == FeaturePictureMakers
	}
	if !found {
		t.Fatal("an engine with a picture maker does not say so")
	}
	plain := startWire(t, &fakeEngine{})
	for _, f := range features(plain.next()) {
		if f == FeaturePictureMakers {
			t.Fatal("an engine without a picture maker claims one")
		}
	}
	plain.send(`{"id":1,"method":"pictures.makers"}`)
	if got := plain.next(); got["error"].(map[string]any)["code"] != CodeUnknownMethod {
		t.Fatalf("pictures.makers without the engine half = %v", got)
	}
}

func TestPictureMakerSetRefusesBadParamsBeforeTheEngine(t *testing.T) {
	w, engine := startPictureWire(t)
	w.next()
	for i, line := range []string{
		`{"id":1,"method":"pictures.maker.set","params":{"scope":"canvas","model":"a/b"}}`,
		`{"id":2,"method":"pictures.maker.set","params":{"scope":"canvas","workspace":"relative/work","model":"a/b"}}`,
		`{"id":3,"method":"pictures.maker.set","params":{"scope":"somewhere","model":"a/b"}}`,
		`{"id":4,"method":"pictures.maker.set","params":{"scope":"everywhere","model":"two words"}}`,
		`{"id":5,"method":"pictures.makers","params":{"workspace":"not/absolute"}}`,
	} {
		w.send(line)
		got := w.next()
		if got["id"] != float64(i+1) || got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
			t.Fatalf("%s = %v", line, got)
		}
	}
	if engine.set != nil {
		t.Fatalf("the engine was asked: %+v", engine.set)
	}
}

func TestPictureMakersAnswerTheirShapes(t *testing.T) {
	w, engine := startPictureWire(t)
	w.next()
	w.send(`{"id":1,"method":"pictures.makers","params":{"workspace":"/canvases/spring/work/"}}`)
	got := w.next()
	result, _ := got["result"].(map[string]any)
	if engine.listed != "/canvases/spring/work" || result == nil {
		t.Fatalf("pictures.makers = %v (asked %q)", got, engine.listed)
	}
	maker := result["makers"].([]any)[0].(map[string]any)
	if maker["price_per_picture_usd"] != 0.045 || maker["reads_photos"] != true || maker["automatic"] != true {
		t.Errorf("maker = %v", maker)
	}
	if resolved := result["resolved"].(map[string]any); resolved["name"] != "Seedream 5 Pro" || resolved["scope"] != "automatic" {
		t.Errorf("resolved = %v", resolved)
	}
	w.send(`{"id":2,"method":"pictures.maker.set","params":{"scope":"everywhere","workspace":"/ignored","model":" seed/pro "}}`)
	if got := w.next(); got["error"] != nil || engine.set == nil || engine.set.Model != "seed/pro" || engine.set.Workspace != "" {
		t.Fatalf("set = %v, engine saw %+v", got, engine.set)
	}
	w.send(`{"id":3,"method":"pictures.maker.set","params":{"scope":"canvas","workspace":"/canvases/spring/work","model":""}}`)
	if got := w.next(); got["error"] != nil || engine.set.Scope != PictureScopeCanvas || engine.set.Model != "" {
		t.Fatalf("Automatic for a canvas = %v, engine saw %+v", got, engine.set)
	}
}
