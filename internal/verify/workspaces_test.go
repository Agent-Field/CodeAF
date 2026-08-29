package verify

import (
	"strings"
	"testing"
)

// happyDomShape is the project happy-dom actually is, in the manifests that
// decide how it is read. Copied out of the task image the s6/s7 runs worked in
// (`packages/happy-dom/package.json` really does declare `"test": "vitest run"`,
// and the root really does declare `turbo run test`).
func happyDomShape(t *testing.T) string {
	t.Helper()
	return project(t, map[string]string{
		"package.json": `{
  "name": "root",
  "private": true,
  "workspaces": ["packages/*", "packages/@happy-dom/*", "integration-test/*"],
  "scripts": {"test": "DO_NOT_TRACK=1 turbo run test && turbo run test:circular-dependencies"},
  "devDependencies": {"turbo": "^2.5.4", "vitest": "^4.0.16"}
}`,
		"turbo.json": `{"tasks": {"test": {}}}`,
		"packages/happy-dom/package.json": `{
  "name": "happy-dom",
  "scripts": {"test": "vitest run"},
  "devDependencies": {"vitest": "^4.0.16"}
}`,
		"packages/happy-dom/vitest.config.ts":                   "export default {}\n",
		"packages/happy-dom/src/nodes/element/Element.ts":       "export class Element {}\n",
		"packages/happy-dom/test/nodes/element/Element.test.ts": "it('has a tag name', () => {})\n",
		"packages/happy-dom/test/console/Console.test.ts":       "it('logs', () => {})\n",
		"packages/@happy-dom/jest-environment/package.json": `{
  "name": "@happy-dom/jest-environment", "scripts": {"test": "jest"},
  "devDependencies": {"jest": "^29"}
}`,
		"packages/@happy-dom/jest-environment/test/Env.test.ts": "it('boots', () => {})\n",
	})
}

// THE RUNNER LIVES IN A PACKAGE, NOT AT THE ROOT. This is happy-dom exactly. The
// root declares `npm test`, whose body is `turbo run test` — a fan-out whose
// whole job is to run each package's own command — and measured in the task
// image at its base commit it exits 1 in 5.3 seconds with 0 of 4 tasks
// successful, having named no check of any package. The runner that names
// happy-dom's checks is `vitest run`, declared in
// `packages/happy-dom/package.json`, which a root-only reader never opens.
//
// Measured in that image on 2026-08-29: the root's own command named nothing;
// `npx vitest run --reporter=json` AT THE ROOT was killed at a 180-second
// ceiling having named nothing; and the reading this test pins — vitest inside
// packages/happy-dom, scoped to the touched test file — exited 0 and named 173
// checks.
func TestTheReadingIsTakenInThePackageTheWorkTouched(t *testing.T) {
	root := happyDomShape(t)
	focus := Focus{"packages/happy-dom/src/nodes/element/Element.ts"}
	ladder, ok := ReadingStrategies(root, Discover(root), focus)
	if !ok {
		t.Fatal("a monorepo produced no strategy at all")
	}
	first := ladder[0]
	if first.Workdir != "packages/happy-dom" {
		t.Fatalf("the reading was not taken in the package the work touched: %#v", first)
	}
	if first.Runner != "vitest" {
		t.Fatalf("the package's own runner was not found: %#v", first)
	}
	if strings.Contains(first.Command, "turbo") {
		t.Errorf("the reading is still the root's fan-out: %q", first.Command)
	}
	if !strings.Contains(first.Command, "--reporter=json") {
		t.Errorf("the runner was not asked for a reading a machine can take: %q", first.Command)
	}
	// And the package that was NOT touched is not read. A reading is one
	// command in one place; reading a sibling package would measure work this
	// job never went near.
	for _, rung := range ladder {
		if strings.Contains(rung.Workdir, "jest-environment") {
			t.Errorf("a package the work never touched is on the ladder: %#v", rung)
		}
	}
	// The root is still down there. If reading the package names nothing,
	// reading the whole thing is what is left to try.
	whole := false
	for _, rung := range ladder {
		if rung.Workdir == "" {
			whole = true
		}
	}
	if !whole {
		t.Error("the ladder has no rung at the workspace root")
	}
}

// A WORKSPACE'S PACKAGES ARE DECLARED, AND EVERY ECOSYSTEM DECLARES THEM. The
// discovery is of the declaration and never of a directory called "packages":
// each of these is a repository saying, in its own file, what it is made of.
func TestAWorkspaceIsReadFromWhateverDeclaresIt(t *testing.T) {
	for _, shape := range []struct {
		name  string
		files map[string]string
		want  string
	}{{
		name: "pnpm",
		files: map[string]string{
			"pnpm-workspace.yaml":    "packages:\n  - 'libs/*'\n",
			"package.json":           `{"name": "root", "private": true}`,
			"libs/core/package.json": `{"name": "core"}`,
		},
		want: "libs/core",
	}, {
		name: "lerna",
		files: map[string]string{
			"lerna.json":               `{"packages": ["modules/*"]}`,
			"modules/api/package.json": `{"name": "api"}`,
		},
		want: "modules/api",
	}, {
		name: "cargo",
		files: map[string]string{
			"Cargo.toml":               "[workspace]\nmembers = [\n  \"crates/engine\",\n]\n",
			"crates/engine/Cargo.toml": "[package]\nname = \"engine\"\n",
		},
		want: "crates/engine",
	}, {
		name: "go.work",
		files: map[string]string{
			"go.work":        "go 1.24\n\nuse (\n\t./service\n)\n",
			"service/go.mod": "module example.test/service\n",
		},
		want: "service",
	}, {
		name: "python-monorepo",
		files: map[string]string{
			// No workspaces key anywhere; the members are declared by each
			// package carrying its own manifest, and a fan-out tool at the root
			// saying this is a workspace at all.
			"turbo.json":                     `{"tasks": {"test": {}}}`,
			"services/ingest/pyproject.toml": "[project]\nname = \"ingest\"\n",
		},
		want: "services/ingest",
	}} {
		t.Run(shape.name, func(t *testing.T) {
			root := project(t, shape.files)
			found := false
			for _, member := range Members(root) {
				if member.Dir == shape.want {
					found = true
					if strings.TrimSpace(member.Source) == "" {
						t.Errorf("the member does not say what declared it: %#v", member)
					}
				}
			}
			if !found {
				t.Fatalf("%s declares %q and it was not found: %#v",
					shape.name, shape.want, Members(root))
			}
		})
	}
}

// A PATH BELONGS TO THE NEAREST MANIFEST ABOVE IT. That is the walk a package
// manager itself does, and it is how the record of what a job touched becomes
// the place a reading is taken.
func TestAPathBelongsToTheNearestManifestAboveIt(t *testing.T) {
	root := happyDomShape(t)
	deep := MemberFor(root, "packages/happy-dom/src/nodes/element/Element.ts")
	if deep.Dir != "packages/happy-dom" {
		t.Errorf("a file deep inside a package was filed under %q", deep.Dir)
	}
	if own := MemberFor(root, "packages/@happy-dom/jest-environment"); own.Dir != "packages/@happy-dom/jest-environment" {
		t.Errorf("a package named outright was filed under %q", own.Dir)
	}
	if loose := MemberFor(root, "README.md"); loose.Dir != "" {
		t.Errorf("a file with no manifest above it was filed under %q", loose.Dir)
	}
	// Most-touched first: a job that changed nine files in one package and one
	// in another is a job about the first, and that is the ladder's order.
	touched := TouchedMembers(root, Focus{
		"packages/@happy-dom/jest-environment/src/Env.ts",
		"packages/happy-dom/src/a.ts",
		"packages/happy-dom/src/b.ts",
	})
	if len(touched) != 2 || touched[0].Dir != "packages/happy-dom" {
		t.Fatalf("the packages are not ordered by how much of the work is in them: %#v", touched)
	}
}
