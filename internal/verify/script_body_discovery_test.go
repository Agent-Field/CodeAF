package verify

import "testing"

// Validation contract for build/typecheck discovery, derived from the
// commander-2342 benchmark run. commander.js declares real `tsc` typecheck
// scripts under its own names (check:type:ts, check:type:js), discovery matched
// only the three blessed NAMES, and the gate reported "no standard
// build/typecheck entrypoint was discoverable" while `npm test` exited 0. The
// agent's rational way out was to fabricate one, and its delivered patch added
//
//	"build": "echo \"commander is shipped as source and has no build step\"",
//
// which upstream would never merge. The contract:
//
//   - a compile/typecheck script is discoverable under ANY name, judged by what
//     its body runs;
//   - a script that delegates to other package scripts is never chosen, since
//     its full effect is unknowable from its own body (it may publish);
//   - the blessed names still win when present, so existing plans do not move;
//   - a project that genuinely compiles nothing is not asked for a build at all.
func TestBuildDiscoveryFromScriptBodies(t *testing.T) {
	manifest := func(scripts string) string {
		return "{\n \"name\": \"demo\",\n \"scripts\": {\n" + scripts + "\n }\n}\n"
	}

	t.Run("finds a tsc typecheck declared under a project-specific name", func(t *testing.T) {
		workspace := t.TempDir()
		writeDiscoveryFile(t, workspace, "package.json", manifest(
			`  "check:type": "npm run check:type:js && npm run check:type:ts",
  "check:type:ts": "tsd && tsc -p tsconfig.ts.json",
  "check:type:js": "tsc -p tsconfig.js.json",
  "check:lint": "eslint .",
  "test": "jest"`))
		writeDiscoveryFile(t, workspace, "tsconfig.json", "{}\n")

		plan := Discover(workspace)
		if !planHasEntrypointKind(plan, KindBuild) {
			t.Fatalf("no build entrypoint; gate would be unsatisfiable. plan = %#v", plan)
		}
		for _, entrypoint := range plan.Entrypoints {
			if entrypoint.Kind != KindBuild {
				continue
			}
			if entrypoint.Command != "npm run check:type:js" {
				t.Errorf("build command = %q, want %q", entrypoint.Command, "npm run check:type:js")
			}
			if entrypoint.Source != "package.json#scripts.check:type:js" {
				t.Errorf("build source = %q", entrypoint.Source)
			}
		}
	})

	t.Run("never picks a script that delegates to other package scripts", func(t *testing.T) {
		// `release` bundles a compile with a publish. Running it to satisfy a
		// verification gate would push a package to the registry.
		workspace := t.TempDir()
		writeDiscoveryFile(t, workspace, "package.json", manifest(
			`  "release": "tsc -p tsconfig.json && npm publish",
  "test": "jest"`))
		writeDiscoveryFile(t, workspace, "tsconfig.json", "{}\n")

		for _, entrypoint := range Discover(workspace).Entrypoints {
			if entrypoint.Kind == KindBuild {
				t.Fatalf("chose a delegating script as the build entrypoint: %#v", entrypoint)
			}
		}
	})

	t.Run("blessed names still win so existing plans do not move", func(t *testing.T) {
		workspace := t.TempDir()
		writeDiscoveryFile(t, workspace, "package.json", manifest(
			`  "build": "rollup -c",
  "check:type:js": "tsc -p tsconfig.js.json",
  "test": "jest"`))

		for _, entrypoint := range Discover(workspace).Entrypoints {
			if entrypoint.Kind != KindBuild {
				continue
			}
			if entrypoint.Command != "npm run build" {
				t.Errorf("build command = %q, want the blessed %q", entrypoint.Command, "npm run build")
			}
		}
	})

	t.Run("a script name is chosen deterministically", func(t *testing.T) {
		workspace := t.TempDir()
		writeDiscoveryFile(t, workspace, "package.json", manifest(
			`  "zeta:types": "tsc -p tsconfig.zeta.json",
  "alpha:types": "tsc -p tsconfig.alpha.json",
  "test": "jest"`))
		writeDiscoveryFile(t, workspace, "tsconfig.json", "{}\n")

		first := ""
		for run := 0; run < 8; run++ {
			got := ""
			for _, entrypoint := range Discover(workspace).Entrypoints {
				if entrypoint.Kind == KindBuild {
					got = entrypoint.Command
				}
			}
			if run == 0 {
				first = got
			} else if got != first {
				t.Fatalf("discovery is not deterministic: %q then %q", first, got)
			}
		}
		if first != "npm run alpha:types" {
			t.Errorf("build command = %q, want the name-sorted %q", first, "npm run alpha:types")
		}
	})
}

func TestBuildlessJavaScriptProjectIsNotAskedForABuild(t *testing.T) {
	// Plain JS with nothing to compile is the JS twin of the plain-Python
	// carve-out: no tsconfig, no TypeScript sources, no compile script.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "package.json",
		"{\n \"name\": \"plain\",\n \"scripts\": { \"test\": \"jest\" }\n}\n")
	writeDiscoveryFile(t, workspace, "index.js", "module.exports = 1\n")

	plan := Discover(workspace)
	if plan.BuildExpected {
		t.Errorf("BuildExpected = true for a project that compiles nothing; the gate "+
			"is unsatisfiable and invites a fabricated build script. plan = %#v", plan)
	}
	if !planHasEntrypointKind(plan, KindTest) {
		t.Errorf("lost the test entrypoint: %#v", plan)
	}
}

func TestTypeScriptProjectStillOwesABuild(t *testing.T) {
	// The carve-out must not swallow projects that really do compile.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "package.json",
		"{\n \"name\": \"typed\",\n \"scripts\": { \"test\": \"jest\" }\n}\n")
	writeDiscoveryFile(t, workspace, "src/index.ts", "export const x = 1\n")

	if plan := Discover(workspace); !plan.BuildExpected {
		t.Errorf("BuildExpected = false for a TypeScript project: %#v", plan)
	}
}
