package knobs

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

// Translation of src/session/knobs.test.ts. Subtest names are verbatim.

func byName(t *testing.T, name string) KnobDef {
	t.Helper()
	def := KnobDefFor(name)
	if def == nil {
		t.Fatalf("byName.%s is undefined", name)
	}
	return *def
}

func expectDefault(t *testing.T, name string, want float64) {
	t.Helper()
	if got := byName(t, name).Default; got != want {
		t.Fatalf("byName.%s.default: got %v, want %v", name, got, want)
	}
}

func TestKnobRegistryDefaultsMatchTodaysHardcodedTunables(t *testing.T) {
	// const byName = Object.fromEntries(KNOB_REGISTRY.map((k) => [k.name, k]))

	t.Run("capability streak / EWMA / threshold / prior strength", func(t *testing.T) {
		expectDefault(t, "PROMOTION_STREAK_K", 3)
		expectDefault(t, "PROMOTION_STREAK_BONUS", 1.0)
		expectDefault(t, "EWMA_DECAY", 0.85)
		expectDefault(t, "RELIABILITY_THRESHOLD", 0.7)
		expectDefault(t, "PRIOR_STRENGTH", 2)
	})

	t.Run("prior means (HIGH + LOW × five bands)", func(t *testing.T) {
		expectDefault(t, "PRIOR_MEAN_HIGH_XS", 0.93)
		expectDefault(t, "PRIOR_MEAN_HIGH_S", 0.88)
		expectDefault(t, "PRIOR_MEAN_HIGH_M", 0.82)
		expectDefault(t, "PRIOR_MEAN_HIGH_L", 0.75)
		expectDefault(t, "PRIOR_MEAN_HIGH_XL", 0.55)
		expectDefault(t, "PRIOR_MEAN_LOW_XS", 0.88)
		expectDefault(t, "PRIOR_MEAN_LOW_S", 0.78)
		expectDefault(t, "PRIOR_MEAN_LOW_M", 0.55)
		expectDefault(t, "PRIOR_MEAN_LOW_L", 0.4)
		expectDefault(t, "PRIOR_MEAN_LOW_XL", 0.22)
	})

	t.Run("size-band char/line/criteria/file/fan-in thresholds", func(t *testing.T) {
		expectDefault(t, "DESC_CHARS_S", 120)
		expectDefault(t, "DESC_CHARS_M", 400)
		expectDefault(t, "DESC_CHARS_L", 1200)
		expectDefault(t, "DESC_LINES_S", 4)
		expectDefault(t, "DESC_LINES_M", 12)
		expectDefault(t, "DESC_LINES_L", 30)
		expectDefault(t, "ACCEPTANCE_FEW", 2)
		expectDefault(t, "ACCEPTANCE_SEVERAL", 5)
		expectDefault(t, "FILES_FEW", 3)
		expectDefault(t, "FILES_SEVERAL", 7)
		expectDefault(t, "FANIN_FEW", 1)
		expectDefault(t, "FANIN_SEVERAL", 4)
		expectDefault(t, "FANIN_MANY", 9)
	})

	t.Run("coalesce / probe / audit / frontier caps", func(t *testing.T) {
		expectDefault(t, "COALESCE_MIN_GROUP", 2)
		expectDefault(t, "COALESCE_MAX_PER_CYCLE", 1)
		expectDefault(t, "PROBE_WAVE_CAPACITY", 1)
		expectDefault(t, "LIGHT_AUDIT_TURN_CAP", 7)
		expectDefault(t, "FRONTIER_MAX_TICKS", 5)
	})

	t.Run("every knob has min ≤ default ≤ max and a non-empty rationale", func(t *testing.T) {
		for _, k := range KNOB_REGISTRY {
			if !(k.Min <= k.Default) {
				t.Fatalf("%s: min %v > default %v", k.Name, k.Min, k.Default)
			}
			if !(k.Default <= k.Max) {
				t.Fatalf("%s: default %v > max %v", k.Name, k.Default, k.Max)
			}
			if len(k.Rationale) == 0 {
				t.Fatalf("%s: empty rationale", k.Name)
			}
		}
	})

	// Registry size is a port-side invariant: the W2 plan declares 35 knobs.
	t.Run("registry declares exactly 35 knobs", func(t *testing.T) {
		if len(KNOB_REGISTRY) != 35 {
			t.Fatalf("got %d knobs, want 35", len(KNOB_REGISTRY))
		}
	})
}

func mustValue(t *testing.T, r ResolvedKnobs, name string) float64 {
	t.Helper()
	v, ok := r.Values.Get(name)
	if !ok {
		t.Fatalf("values.%s is undefined", name)
	}
	return v
}

func mustSource(t *testing.T, r ResolvedKnobs, name string) KnobSource {
	t.Helper()
	s, ok := r.Sources.Get(name)
	if !ok {
		t.Fatalf("sources.%s is undefined", name)
	}
	return s
}

func objectOf(pairs ...any) *Record[any] {
	out := NewRecord[any]()
	for i := 0; i+1 < len(pairs); i += 2 {
		out.Set(pairs[i].(string), pairs[i+1])
	}
	return out
}

func TestResolveKnobsResolutionOrderFileEnvDefault(t *testing.T) {
	t.Run("defaults when neither file nor env provide a value", func(t *testing.T) {
		r := ResolveKnobs(NewRecord[any](), map[string]string{})
		if got := mustValue(t, r, "PROMOTION_STREAK_K"); got != 3 {
			t.Fatalf("got %v, want 3", got)
		}
		if got := mustSource(t, r, "PROMOTION_STREAK_K"); got != SourceDefault {
			t.Fatalf("got %q, want %q", got, SourceDefault)
		}
		if len(r.Clamped) != 0 {
			t.Fatalf("got %v, want []", r.Clamped)
		}
	})

	t.Run("env CODEAF_KNOB_<NAME> overrides default", func(t *testing.T) {
		r := ResolveKnobs(NewRecord[any](), map[string]string{"CODEAF_KNOB_PROMOTION_STREAK_K": "5"})
		if got := mustValue(t, r, "PROMOTION_STREAK_K"); got != 5 {
			t.Fatalf("got %v, want 5", got)
		}
		if got := mustSource(t, r, "PROMOTION_STREAK_K"); got != SourceEnv {
			t.Fatalf("got %q, want %q", got, SourceEnv)
		}
	})

	t.Run("file override beats env", func(t *testing.T) {
		r := ResolveKnobs(
			objectOf("PROMOTION_STREAK_K", 7.0),
			map[string]string{"CODEAF_KNOB_PROMOTION_STREAK_K": "5"},
		)
		if got := mustValue(t, r, "PROMOTION_STREAK_K"); got != 7 {
			t.Fatalf("got %v, want 7", got)
		}
		if got := mustSource(t, r, "PROMOTION_STREAK_K"); got != SourceFile {
			t.Fatalf("got %q, want %q", got, SourceFile)
		}
	})

	t.Run("unknown keys in file/env are ignored", func(t *testing.T) {
		r := ResolveKnobs(objectOf("NOT_A_KNOB", 99.0), map[string]string{"CODEAF_KNOB_ALSO_FAKE": "1"})
		if _, ok := r.Values.Get("NOT_A_KNOB"); ok {
			t.Fatal("values.NOT_A_KNOB is defined")
		}
		got := r.Values.Keys()
		sort.Strings(got)
		want := make([]string, 0, len(KNOB_REGISTRY))
		for _, k := range KNOB_REGISTRY {
			want = append(want, k.Name)
		}
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("non-numeric file/env values fall through to the next source", func(t *testing.T) {
		r := ResolveKnobs(objectOf("EWMA_DECAY", "nope"), map[string]string{"CODEAF_KNOB_EWMA_DECAY": "0.5"})
		if got := mustValue(t, r, "EWMA_DECAY"); got != 0.5 {
			t.Fatalf("got %v, want 0.5", got)
		}
		if got := mustSource(t, r, "EWMA_DECAY"); got != SourceEnv {
			t.Fatalf("got %q, want %q", got, SourceEnv)
		}
	})
}

func TestResolveKnobsClamping(t *testing.T) {
	t.Run("values above max are clamped and recorded", func(t *testing.T) {
		r := ResolveKnobs(objectOf("PROMOTION_STREAK_K", 999.0), map[string]string{})
		if got := mustValue(t, r, "PROMOTION_STREAK_K"); got != 20 {
			t.Fatalf("got %v, want 20", got)
		}
		want := []ClampEvent{{Name: "PROMOTION_STREAK_K", Raw: 999, Clamped: 20}}
		if !reflect.DeepEqual(r.Clamped, want) {
			t.Fatalf("got %v, want %v", r.Clamped, want)
		}
	})

	t.Run("values below min are clamped and recorded", func(t *testing.T) {
		r := ResolveKnobs(objectOf("EWMA_DECAY", -1.0), map[string]string{})
		if got := mustValue(t, r, "EWMA_DECAY"); got != 0 {
			t.Fatalf("got %v, want 0", got)
		}
		want := ClampEvent{Name: "EWMA_DECAY", Raw: -1, Clamped: 0}
		found := false
		for _, c := range r.Clamped {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("clamped %v does not contain %v", r.Clamped, want)
		}
	})

	t.Run("in-range overrides are not listed in clamped", func(t *testing.T) {
		r := ResolveKnobs(objectOf("RELIABILITY_THRESHOLD", 0.8), map[string]string{})
		if got := mustValue(t, r, "RELIABILITY_THRESHOLD"); got != 0.8 {
			t.Fatalf("got %v, want 0.8", got)
		}
		for _, c := range r.Clamped {
			if c.Name == "RELIABILITY_THRESHOLD" {
				t.Fatalf("found clamp event %v", c)
			}
		}
	})
}

func TestKnobsSnapshotHash(t *testing.T) {
	t.Run("defaults produce a stable hex digest", func(t *testing.T) {
		a := KnobsSnapshotHash(DefaultKnobValues())
		b := KnobsSnapshotHash(DefaultKnobValues())
		if a != b {
			t.Fatalf("got %q, want %q", a, b)
		}
		if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(a) {
			t.Fatalf("%q does not match /^[0-9a-f]{40}$/", a)
		}
	})

	t.Run("insertion order of keys does not change the hash", func(t *testing.T) {
		forwards := DefaultKnobValues()
		keys := forwards.Keys()
		sort.Strings(keys)
		backwards := NewRecord[float64]()
		for i := len(keys) - 1; i >= 0; i-- {
			v, _ := forwards.Get(keys[i])
			backwards.Set(keys[i], v)
		}
		if got, want := KnobsSnapshotHash(backwards), KnobsSnapshotHash(forwards); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("a value change flips the hash", func(t *testing.T) {
		base := DefaultKnobValues()
		tweaked := NewRecord[float64]()
		for _, k := range base.Keys() {
			v, _ := base.Get(k)
			tweaked.Set(k, v)
		}
		tweaked.Set("FRONTIER_MAX_TICKS", 6)
		if KnobsSnapshotHash(tweaked) == KnobsSnapshotHash(base) {
			t.Fatal("hashes are equal")
		}
	})

	t.Run("no-arg call hashes the registry defaults", func(t *testing.T) {
		if got, want := KnobsSnapshotHash(nil), KnobsSnapshotHash(DefaultKnobValues()); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestLoadKnobFileResolveProjectKnobsThinIO(t *testing.T) {
	// function tmpProject(knobsJson?: string): string
	tmpProject := func(t *testing.T, knobsJSON *string) string {
		t.Helper()
		dir := t.TempDir()
		if knobsJSON != nil {
			if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".codeaf", "knobs.json"), []byte(*knobsJSON), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		return dir
	}

	t.Run("missing knobs.json yields {}", func(t *testing.T) {
		if got := LoadKnobFile(tmpProject(t, nil)); got.Len() != 0 {
			t.Fatalf("got %v keys, want 0", got.Keys())
		}
	})

	t.Run("malformed knobs.json yields {} rather than throwing", func(t *testing.T) {
		body := "{not json"
		if got := LoadKnobFile(tmpProject(t, &body)); got.Len() != 0 {
			t.Fatalf("got %v keys, want 0", got.Keys())
		}
	})

	t.Run("resolveProjectKnobs reads file overrides", func(t *testing.T) {
		body := `{"LIGHT_AUDIT_TURN_CAP":9}`
		r := ResolveProjectKnobs(tmpProject(t, &body), map[string]string{})
		if got := mustValue(t, r, "LIGHT_AUDIT_TURN_CAP"); got != 9 {
			t.Fatalf("got %v, want 9", got)
		}
		if got := mustSource(t, r, "LIGHT_AUDIT_TURN_CAP"); got != SourceFile {
			t.Fatalf("got %q, want %q", got, SourceFile)
		}
	})
}
